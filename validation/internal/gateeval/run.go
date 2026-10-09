package gateeval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Defaults for RunOptions.
const (
	defaultWorkers        = 2
	defaultCommandTimeout = 10 * time.Minute
)

// allArms is the arm order used when RunOptions.Arms is empty.
var allArms = []Arm{ArmB0, ArmB1, ArmB2, ArmB2Star, ArmB3, ArmGCI, ArmGHook}

// RunOptions configures RunArms.
//
//nolint:govet // Keep option fields grouped by meaning.
type RunOptions struct {
	WorkDir        string // clones at WorkDir/clones/<project>; worktrees at WorkDir/work/
	Out            string // runs JSONL; resumable: skip (variant, arm, attempt) already present
	GateBin        string // agentic-go binary
	B3Script       string // validation/gate/b3-grep.sh (absolute)
	Variants       []Variant
	Arms           []Arm
	Workers        int           // default 2
	CommandTimeout time.Duration // per arm command; default 10m
	Timing         bool          // run every arm twice on ClassTrue variants (attempt 1 and 2)
}

// runKey identifies one planned run.
type runKey struct {
	variant string
	arm     Arm
	attempt int
}

// runner holds the shared state of one RunArms call.
//
//nolint:govet // Keep mutexes next to the state they guard.
type runner struct {
	options  RunOptions
	appendMu sync.Mutex
	locksMu  sync.Mutex
	locks    map[string]*sync.Mutex
}

// RunArms runs every requested arm against every variant and appends one Run
// per (variant, arm, attempt) to options.Out. Runs already present in Out are
// skipped, so an interrupted call can be repeated. Each worker owns one git
// worktree per project and checks variants out in it.
func RunArms(ctx context.Context, options RunOptions) error {
	options, err := normalizeOptions(options)
	if err != nil {
		return err
	}
	done, err := completedRuns(options.Out)
	if err != nil {
		return err
	}
	r := &runner{options: options, locks: map[string]*sync.Mutex{}}
	jobs := make(chan Variant)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	fail := func(err error) { errOnce.Do(func() { firstErr = err }) }
	for n := range options.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := &worker{runner: r, index: n, trees: map[string]string{}}
			for variant := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if err := w.runVariant(ctx, variant, r.pending(variant, done)); err != nil {
					fail(err)
				}
			}
		}()
	}
	for _, variant := range options.Variants {
		jobs <- variant
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("running arms: %w", err)
	}
	return nil
}

// normalizeOptions applies defaults and resolves paths to absolute form,
// because commands run in other directories.
func normalizeOptions(options RunOptions) (RunOptions, error) {
	if options.WorkDir == "" || options.Out == "" {
		return options, errors.New("work directory and output path are required")
	}
	if options.Workers <= 0 {
		options.Workers = defaultWorkers
	}
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = defaultCommandTimeout
	}
	if len(options.Arms) == 0 {
		options.Arms = allArms
	}
	var err error
	if options.WorkDir, err = filepath.Abs(options.WorkDir); err != nil {
		return options, fmt.Errorf("resolving work directory: %w", err)
	}
	for _, arm := range options.Arms {
		switch arm {
		case ArmGCI, ArmGHook:
			if options.GateBin == "" {
				return options, fmt.Errorf("arm %s needs the gate binary", arm)
			}
			if options.GateBin, err = filepath.Abs(options.GateBin); err != nil {
				return options, fmt.Errorf("resolving gate binary: %w", err)
			}
		case ArmB3:
			if options.B3Script == "" {
				return options, fmt.Errorf("arm %s needs the grep script", arm)
			}
			if options.B3Script, err = filepath.Abs(options.B3Script); err != nil {
				return options, fmt.Errorf("resolving grep script: %w", err)
			}
		}
	}
	return options, nil
}

// completedRuns reads the runs already recorded in path, if any.
func completedRuns(path string) (map[runKey]bool, error) {
	done := map[runKey]bool{}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return done, nil
	}
	runs, err := ReadJSONL[Run](path)
	if err != nil {
		return nil, fmt.Errorf("reading existing runs: %w", err)
	}
	for _, run := range runs {
		done[runKey{run.VariantID, run.Arm, run.Attempt}] = true
	}
	return done, nil
}

// pending lists the runs still to do for a variant: for each arm, attempt 1,
// then attempt 2 when timing is on and the variant is a true patch.
func (r *runner) pending(variant Variant, done map[runKey]bool) []runKey {
	attempts := 1
	if r.options.Timing && variant.Class == ClassTrue {
		attempts = 2
	}
	keys := make([]runKey, 0, len(r.options.Arms)*attempts)
	for _, arm := range r.options.Arms {
		for attempt := 1; attempt <= attempts; attempt++ {
			key := runKey{variant.ID, arm, attempt}
			if !done[key] {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// lock returns the mutex for a name, creating it on first use.
func (r *runner) lock(name string) *sync.Mutex {
	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	mutex, ok := r.locks[name]
	if !ok {
		mutex = &sync.Mutex{}
		r.locks[name] = mutex
	}
	return mutex
}

// record appends one run to the output file.
func (r *runner) record(run Run) error {
	r.appendMu.Lock()
	defer r.appendMu.Unlock()
	return AppendJSONL(r.options.Out, run)
}

// worker owns one worktree per project.
type worker struct {
	runner *runner
	trees  map[string]string
	index  int
}

// runVariant checks the variant out and runs its pending arms. Commands that
// are identical across arms are run once per tree, except when the variant is
// timed, where every arm runs its own commands so durations are not borrowed.
func (w *worker) runVariant(ctx context.Context, variant Variant, pending []runKey) error {
	if len(pending) == 0 {
		return nil
	}
	dir, err := w.prepare(ctx, variant)
	if err != nil {
		return w.recordAll(ctx, pending, fmt.Sprintf("preparing worktree: %v", err))
	}
	direct, err := directPackages(ctx, dir, variant.Base)
	if err != nil {
		return w.recordAll(ctx, pending, fmt.Sprintf("finding direct packages: %v", err))
	}
	ac := &armContext{
		variant:  variant,
		dir:      dir,
		direct:   direct,
		gateBin:  w.runner.options.GateBin,
		b3Script: w.runner.options.B3Script,
		timeout:  w.runner.options.CommandTimeout,
		baseFailures: func(ctx context.Context) (testFailures, error) {
			return w.runner.baseFailures(ctx, variant.Project, variant.Base)
		},
	}
	timed := w.runner.options.Timing && variant.Class == ClassTrue
	if !timed {
		ac.memo = map[string]stepResult{}
	}
	for _, key := range pending {
		run := ac.evaluate(ctx, key.arm, key.attempt)
		if ctx.Err() != nil {
			return nil
		}
		if err := w.runner.record(run); err != nil {
			return err
		}
	}
	return nil
}

// recordAll records every pending run as unknown with the given error, so an
// unusable variant is visible in the results instead of silently missing.
func (w *worker) recordAll(ctx context.Context, pending []runKey, message string) error {
	if ctx.Err() != nil {
		return nil
	}
	for _, key := range pending {
		run := Run{VariantID: key.variant, Arm: key.arm, Attempt: key.attempt, Unknown: true, Error: message}
		if err := w.runner.record(run); err != nil {
			return err
		}
	}
	return nil
}

// prepare returns this worker's worktree for the variant's project with the
// variant's branch checked out and untracked files removed.
func (w *worker) prepare(ctx context.Context, variant Variant) (string, error) {
	dir, ok := w.trees[variant.Project]
	if !ok {
		var err error
		dir, err = w.runner.ensureWorktree(ctx, variant.Project, fmt.Sprintf("%s-%d", variant.Project, w.index))
		if err != nil {
			return "", err
		}
		w.trees[variant.Project] = dir
	}
	if err := checkout(ctx, dir, variant.Branch); err != nil {
		return "", err
	}
	return dir, nil
}

// checkout detaches the worktree at ref and removes everything untracked.
func checkout(ctx context.Context, dir, ref string) error {
	if _, err := git(ctx, dir, "checkout", "--detach", "--force", ref); err != nil {
		return err
	}
	_, err := git(ctx, dir, "clean", "-fdxq")
	return err
}

// git runs git in dir and returns stdout. Failures carry stderr.
func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ensureWorktree creates WorkDir/work/<name> as a detached worktree of the
// project's clone, or reuses it when it exists.
func (r *runner) ensureWorktree(ctx context.Context, project, name string) (string, error) {
	mutex := r.lock("worktree:" + project)
	mutex.Lock()
	defer mutex.Unlock()
	path := filepath.Join(r.options.WorkDir, "work", name)
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return path, nil
	}
	clone := filepath.Join(r.options.WorkDir, "clones", project)
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		return "", fmt.Errorf("clone of %s: %w", project, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("creating work directory: %w", err)
	}
	if _, err := git(ctx, clone, "worktree", "prune"); err != nil {
		return "", err
	}
	if _, err := git(ctx, clone, "worktree", "add", "--detach", path); err != nil {
		return "", err
	}
	return path, nil
}

// directPackages lists the directories of .go files changed between base and
// HEAD that still contain .go files, as ./dir patterns ("." for the root).
// Directories that go ./... would ignore (testdata, names starting with "." or
// "_") and directories inside nested modules are left out, since go cannot
// build them as patterns of this module.
func directPackages(ctx context.Context, dir, base string) ([]string, error) {
	out, err := git(ctx, dir, "diff", "--name-only", "--no-renames", "-z", base, "HEAD")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, name := range strings.Split(string(out), "\x00") {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		pkg := filepath.ToSlash(filepath.Dir(name))
		if seen[pkg] || !buildableDir(dir, pkg) {
			continue
		}
		seen[pkg] = true
	}
	patterns := make([]string, 0, len(seen))
	for pkg := range seen {
		if pkg == "." {
			patterns = append(patterns, ".")
			continue
		}
		patterns = append(patterns, "./"+pkg)
	}
	sort.Strings(patterns)
	return patterns, nil
}

// buildableDir reports whether pkg (slash form, relative to root) still holds
// .go files and is addressable as a pattern of the root module.
func buildableDir(root, pkg string) bool {
	if pkg != "." {
		for _, element := range strings.Split(pkg, "/") {
			if element == "testdata" || strings.HasPrefix(element, ".") || strings.HasPrefix(element, "_") {
				return false
			}
		}
		for current := pkg; current != "."; current = filepath.ToSlash(filepath.Dir(current)) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(current), "go.mod")); err == nil {
				return false
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(pkg)))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true
		}
	}
	return false
}

// baseFailureFile is the cached form of testFailures.
type baseFailureFile struct {
	Tests    []string `json:"tests"`
	Packages []string `json:"packages"`
}

// baseFailures returns the test failures at base, computing them once per
// (project, base) by running go test -count=1 -json ./... in a dedicated
// worktree and caching the result under WorkDir/basefail.
func (r *runner) baseFailures(ctx context.Context, project, base string) (testFailures, error) {
	key := project + "-" + base
	mutex := r.lock("basefail:" + key)
	mutex.Lock()
	defer mutex.Unlock()
	path := filepath.Join(r.options.WorkDir, "basefail", key+".json")
	if data, err := os.ReadFile(path); err == nil {
		var cached baseFailureFile
		if err := json.Unmarshal(data, &cached); err != nil {
			return testFailures{}, fmt.Errorf("reading %s: %w", path, err)
		}
		return cached.failures(), nil
	}
	failures, err := r.computeBaseFailures(ctx, project, base)
	if err != nil {
		return testFailures{}, err
	}
	data, err := json.Marshal(baseFailureFile{Tests: newKeys(failures.Tests, nil), Packages: newKeys(failures.Packages, nil)})
	if err != nil {
		return testFailures{}, fmt.Errorf("encoding base failures: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return testFailures{}, err
	}
	return failures, nil
}

func (f baseFailureFile) failures() testFailures {
	failures := newTestFailures()
	for _, test := range f.Tests {
		failures.Tests[test] = true
	}
	for _, pkg := range f.Packages {
		failures.Packages[pkg] = true
	}
	return failures
}

// computeBaseFailures checks base out in the project's base worktree and runs
// the full test suite there.
func (r *runner) computeBaseFailures(ctx context.Context, project, base string) (testFailures, error) {
	dir, err := r.ensureWorktree(ctx, project, project+"-base")
	if err != nil {
		return testFailures{}, err
	}
	treeLock := r.lock("basetree:" + project)
	treeLock.Lock()
	defer treeLock.Unlock()
	if err := checkout(ctx, dir, base); err != nil {
		return testFailures{}, err
	}
	result := runCommand(ctx, dir, r.options.CommandTimeout, "go", "test", "-count=1", "-json", "./...")
	if result.err != nil {
		return testFailures{}, result.err
	}
	failures := parseTestFailures(result.stdout)
	if result.exit != 0 && len(failures.Tests) == 0 && len(failures.Packages) == 0 {
		failures.Packages[noEventsFailure] = true
	}
	return failures, nil
}

// writeFileAtomic writes data to path through a temporary file and a rename.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return fmt.Errorf("closing %s: %w", path, err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		_ = os.Remove(temp.Name())
		return fmt.Errorf("renaming %s: %w", path, err)
	}
	return nil
}
