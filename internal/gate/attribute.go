package gate

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	testjson "github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

const (
	// attributeLimit is how many failing tests one run attributes.
	attributeLimit = 10
	// attributeTestTimeout bounds one attribution go test process.
	attributeTestTimeout = 60 * time.Second
	// baseKeepTrees is how many cached merge-base trees a store keeps.
	baseKeepTrees = 2
	// baseCompleteMarker records a fully materialized tree and its workspace
	// path relative to the tree directory.
	baseCompleteMarker = ".agentic-go-complete"
	// baseTempPrefix names trees still being materialized.
	baseTempPrefix = ".tmp-"
	// baseCommitLen is how many commit characters name a cached tree.
	baseCommitLen = 12
)

// probeOutcome is what one targeted go test run showed.
type probeOutcome int

const (
	// probeFail: the test failed, or the package crashed.
	probeFail probeOutcome = iota
	// probePass: the test passed, or the package did not crash.
	probePass
	// probeAbsent: the named test does not exist ("no tests to run").
	probeAbsent
	// probeMissing: the package directory does not exist.
	probeMissing
	// probeBroken: go test produced no result, for example a build failure.
	probeBroken
)

// probeResult is one targeted go test outcome. reason explains probeBroken;
// lastStarted names the last top-level test that started but never finished.
type probeResult struct {
	reason      string
	lastStarted string
	outcome     probeOutcome
}

// attributor decides whether failing tests were caused by the change: it
// reruns each one at the current tree, then in the merge-base tree.
type attributor struct {
	materialize func(context.Context, verification.Repository, string) (string, error)
	runner      *execution.Runner
	targets     map[string]verification.ExecutionTarget
	base        *attributeBase
	root        string
	baseShort   string
	// baseDir is where merge-base trees are cached; empty means a temporary
	// tree removed after the run.
	baseDir    string
	repository verification.Repository
	flags      []string
}

// attributeBase is the merge-base tree, prepared at most once per run.
type attributeBase struct {
	err    error
	runner *execution.Runner
	root   string
	temp   string
}

// attribution is the outcome of attributing all failures of one run.
type attribution struct {
	items    []Item
	notes    []string
	complete bool
}

// testFlags returns the go test flags a probe shares with the original run.
func testFlags(request verification.Request) []string {
	flags := make([]string, 0, 3)
	if request.Short {
		flags = append(flags, "-short")
	}
	if request.Skip != "" {
		flags = append(flags, "-skip="+request.Skip)
	}
	if request.Race {
		flags = append(flags, "-race")
	}
	return flags
}

// close removes a temporary merge-base tree. Cached trees are kept.
func (a *attributor) close() {
	if a.base != nil && a.base.temp != "" {
		_ = os.RemoveAll(a.base.temp)
	}
}

// attribute classifies up to attributeLimit failures; the rest are reported
// as not attributed and make the run incomplete.
func (a *attributor) attribute(ctx context.Context, failures []testFailure) attribution {
	out := attribution{items: []Item{}, notes: []string{}, complete: true}
	expanded := attributeExpandTimeouts(failures)
	if len(expanded) > attributeLimit {
		out.notes = append(out.notes, fmt.Sprintf("attributed %d of %d failing tests; the rest were not compared with base", attributeLimit, len(expanded)))
	}
	for index, failure := range expanded {
		if index >= attributeLimit {
			out.items = append(out.items, a.failureItem(failure, "", SeverityWarn, CodeTestFailed,
				fmt.Sprintf("%s fails and was not attributed: limit of %d failing tests reached", attributeSubject(failure), attributeLimit),
				"Fix the failures shown first, then run the check again."))
			out.complete = false
			continue
		}
		item, complete := a.attributeOne(ctx, failure)
		out.items = append(out.items, item)
		if !complete {
			out.notes = append(out.notes, item.Message)
		}
		out.complete = out.complete && complete
	}
	return out
}

// attributeOne returns the item for one failure and whether attribution
// reached a conclusion. Only evidence that the base behaves differently
// blocks; when the base cannot be checked the failure is a warning and the
// verdict becomes unknown.
func (a *attributor) attributeOne(ctx context.Context, failure testFailure) (Item, bool) {
	target, known := a.targets[failure.Package]
	if !known {
		return a.notCompared(failure, "", "package directory is unknown"), false
	}
	rel, err := filepath.Rel(a.root, target.Dir)
	if err != nil {
		return a.notCompared(failure, "", err.Error()), false
	}
	current, err := a.probe(ctx, a.runner, a.root, rel, failure.Test)
	if err != nil && ctx.Err() != nil {
		return a.notCompared(failure, "", "time budget ran out"), false
	}
	// Limitation: a failure that depends on test order or shared state can
	// pass when its test runs alone and is then reported as flaky.
	if err == nil && current.outcome == probePass {
		return a.flakyItem(failure), true
	}
	base, err := a.baseTree(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return a.notCompared(failure, current.lastStarted, "time budget ran out"), false
		}
		return a.notCompared(failure, current.lastStarted, attributeOneLine(err.Error())), false
	}
	previous, err := a.probe(ctx, base.runner, base.root, rel, failure.Test)
	if err != nil {
		if ctx.Err() != nil {
			return a.notCompared(failure, current.lastStarted, "time budget ran out"), false
		}
		return a.notCompared(failure, current.lastStarted, attributeOneLine(err.Error())), false
	}
	switch previous.outcome {
	case probeFail:
		return a.preexistingItem(failure, current.lastStarted), true
	case probeBroken:
		return a.notCompared(failure, current.lastStarted, previous.reason), false
	default:
		return a.blockItem(failure, target, current.lastStarted), true
	}
}

// blockItem reports a failure the change caused, naming consumer packages.
func (a *attributor) blockItem(failure testFailure, target verification.ExecutionTarget, lastStarted string) Item {
	if failure.Test == "" {
		message := fmt.Sprintf("tests in %s crash outside a named test after this change", failure.Package)
		if lastStarted != "" {
			message += "; last test started: " + lastStarted
		}
		return a.failureItem(failure, lastStarted, SeverityBlock, CodeTestFailed, message,
			"Fix the code so the tests in "+failure.Package+" run to completion; do not change or skip tests to make them pass.")
	}
	subject := failure.Test
	if target.Distance > 0 {
		return a.failureItem(failure, "", SeverityBlock, CodeTestConsumerFailed,
			fmt.Sprintf("%s in consumer package %s fails after this change", subject, failure.Package),
			fmt.Sprintf("Fix the code so %s in consumer package %s passes; do not change or skip the test to make it pass.", subject, failure.Package))
	}
	return a.failureItem(failure, "", SeverityBlock, CodeTestFailed, subject+" fails after this change",
		"Fix the code so "+subject+" passes; do not change or skip the test to make it pass.")
}

func (a *attributor) flakyItem(failure testFailure) Item {
	subject := attributeSubject(failure)
	message := subject + " failed, then passed when rerun; it looks flaky"
	if failure.Test == "" {
		message = subject + " crashed, then passed when rerun; they look flaky"
	}
	return a.failureItem(failure, "", SeverityWarn, CodeTestFlaky, message, "Check "+subject+" for timing or ordering assumptions.")
}

func (a *attributor) preexistingItem(failure testFailure, lastStarted string) Item {
	verb := "fails"
	if failure.Test == "" {
		verb = "crash"
	}
	return a.failureItem(failure, lastStarted, SeverityWarn, CodeTestPreexisting,
		fmt.Sprintf("%s also %s at %s; not caused by this change", attributeSubject(failure), verb, a.baseShort), "")
}

// notCompared is the warning for a failure whose cause could not be
// established; callers mark the run incomplete.
func (a *attributor) notCompared(failure testFailure, lastStarted, reason string) Item {
	return a.failureItem(failure, lastStarted, SeverityWarn, CodeTestFailed,
		fmt.Sprintf("%s not compared with base: %s", attributeSubject(failure), reason),
		"Check whether this change caused the failure, then run the check again.")
}

// failureItem builds an item located at the failing test, or at locate when
// the failure is outside a named test.
func (a *attributor) failureItem(failure testFailure, locate string, severity Severity, code, message, fix string) Item {
	item := Item{Severity: severity, Code: code, Message: message, Fix: fix, Detail: attributeDetail(failure)}
	if locate == "" {
		locate = failure.Test
	}
	if target, ok := a.targets[failure.Package]; ok && locate != "" {
		item.File, item.Line = attributeLocate(a.root, target.Dir, locate)
	}
	return item
}

// attributeSubject names the failing test, or the package's tests for a
// failure outside any named test.
func attributeSubject(failure testFailure) string {
	if failure.Test == "" {
		return "tests in " + failure.Package
	}
	return failure.Test
}

// attributeDetail lists failing subtests and the first lines of the output.
func attributeDetail(failure testFailure) string {
	detail := mapDetail(failure.Output)
	if len(failure.Subtests) == 0 {
		return detail
	}
	header := "failing subtests: " + strings.Join(failure.Subtests, ", ")
	if detail == "" {
		return header
	}
	return header + "\n" + detail
}

var attributeTimedOut = regexp.MustCompile(`^\s+(Test[^\s/]*)\s+\(`)

// attributeExpandTimeouts replaces a package-level timeout with the tests go
// test listed as still running, so they are attributed by name; rerunning
// the whole package would only time out again.
func attributeExpandTimeouts(failures []testFailure) []testFailure {
	named := make(map[[2]string]bool, len(failures))
	for _, failure := range failures {
		named[[2]string{failure.Package, failure.Test}] = true
	}
	expanded := make([]testFailure, 0, len(failures))
	for _, failure := range failures {
		if failure.Test != "" || !strings.Contains(failure.Output, "panic: test timed out") {
			expanded = append(expanded, failure)
			continue
		}
		names := attributeRunningTests(failure.Output)
		if len(names) == 0 {
			expanded = append(expanded, failure)
			continue
		}
		for _, name := range names {
			if !named[[2]string{failure.Package, name}] {
				named[[2]string{failure.Package, name}] = true
				expanded = append(expanded, testFailure{Package: failure.Package, Test: name, Output: failure.Output, Subtests: []string{}})
			}
		}
	}
	return expanded
}

func attributeRunningTests(output string) []string {
	_, running, found := strings.Cut(output, "running tests:")
	if !found {
		return nil
	}
	seen := make(map[string]bool)
	names := make([]string, 0)
	for _, line := range strings.Split(running, "\n") {
		match := attributeTimedOut.FindStringSubmatch(line)
		if match == nil || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		names = append(names, match[1])
	}
	sort.Strings(names)
	return names
}

// baseTree prepares the merge-base tree once; later calls reuse the result.
func (a *attributor) baseTree(ctx context.Context) (*attributeBase, error) {
	if a.base != nil {
		return a.base, a.base.err
	}
	a.base = &attributeBase{}
	a.base.err = a.prepareBase(ctx, a.base)
	return a.base, a.base.err
}

func (a *attributor) prepareBase(ctx context.Context, base *attributeBase) error {
	var root string
	var err error
	if a.baseDir == "" {
		root, base.temp, err = a.temporaryBase(ctx)
	} else {
		root, err = a.cachedBase(ctx)
	}
	if err != nil {
		return err
	}
	ws, err := workspace.Open(ctx, root)
	if err != nil {
		return fmt.Errorf("opening base workspace: %w", err)
	}
	runner, err := a.runner.ForWorkspace(ws)
	if err != nil {
		return fmt.Errorf("creating base runner: %w", err)
	}
	base.root, base.runner = ws.Root(), runner
	return nil
}

func (a *attributor) temporaryBase(ctx context.Context) (string, string, error) {
	temp, err := os.MkdirTemp("", "agentic-go-gate-base-")
	if err != nil {
		return "", "", fmt.Errorf("creating base directory: %w", err)
	}
	root, err := a.materialize(ctx, a.repository, temp)
	if err != nil {
		return "", temp, fmt.Errorf("materializing base: %w", err)
	}
	return root, temp, nil
}

// cachedBase returns the merge-base tree cached under baseDir, materializing
// it on first use. A tree is published by renaming a fully written temporary
// directory carrying a completion marker, so a partial tree is never reused.
func (a *attributor) cachedBase(ctx context.Context) (string, error) {
	commit := a.repository.MergeBaseCommit
	if len(commit) < baseCommitLen {
		return "", fmt.Errorf("merge-base commit %q is too short", commit)
	}
	final := filepath.Join(a.baseDir, commit[:baseCommitLen])
	if root, ok := baseReuse(final); ok {
		return root, nil
	}
	if err := os.MkdirAll(a.baseDir, stateStoreDirPerm); err != nil {
		return "", fmt.Errorf("creating base cache: %w", err)
	}
	temp, err := os.MkdirTemp(a.baseDir, baseTempPrefix)
	if err != nil {
		return "", fmt.Errorf("creating base directory: %w", err)
	}
	root, err := a.publishBase(ctx, temp, final)
	if err != nil {
		_ = os.RemoveAll(temp)
		if reused, ok := baseReuse(final); ok {
			return reused, nil
		}
		return "", err
	}
	basePrune(a.baseDir, final)
	return root, nil
}

func (a *attributor) publishBase(ctx context.Context, temp, final string) (string, error) {
	root, err := a.materialize(ctx, a.repository, temp)
	if err != nil {
		return "", fmt.Errorf("materializing base: %w", err)
	}
	rel, err := filepath.Rel(temp, root)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("materialized base %q is outside %q", root, temp)
	}
	if err := os.WriteFile(filepath.Join(temp, baseCompleteMarker), []byte(filepath.ToSlash(rel)), stateStoreFilePerm); err != nil {
		return "", fmt.Errorf("marking base complete: %w", err)
	}
	// A directory without a marker is a leftover from an interrupted run.
	_ = os.RemoveAll(final)
	if err := os.Rename(temp, final); err != nil {
		return "", fmt.Errorf("publishing base: %w", err)
	}
	return filepath.Join(final, rel), nil
}

// baseReuse returns the workspace root of a complete cached tree and marks
// it recently used.
func baseReuse(final string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(final, baseCompleteMarker))
	if err != nil {
		return "", false
	}
	rel := filepath.FromSlash(strings.TrimSpace(string(data)))
	if !filepath.IsLocal(rel) {
		return "", false
	}
	root := filepath.Join(final, rel)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", false
	}
	now := time.Now()
	_ = os.Chtimes(final, now, now)
	return root, true
}

// basePrune keeps the newest cached trees, always including keep, and
// removes temporary trees abandoned by interrupted runs.
func basePrune(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type tree struct {
		modified time.Time
		path     string
	}
	trees := make([]tree, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil || !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if strings.HasPrefix(entry.Name(), baseTempPrefix) {
			if time.Since(info.ModTime()) > stateTempMaxAge {
				_ = os.RemoveAll(path)
			}
			continue
		}
		if path != keep {
			trees = append(trees, tree{modified: info.ModTime(), path: path})
		}
	}
	sort.Slice(trees, func(i, j int) bool { return trees[i].modified.After(trees[j].modified) })
	for index, item := range trees {
		if index >= baseKeepTrees-1 {
			_ = os.RemoveAll(item.path)
		}
	}
}

// probe runs one named test, or for an empty name the whole package with the
// original flags, in the package at rel under root. Errors are returned only
// when go test could not run at all, such as on cancellation.
func (a *attributor) probe(ctx context.Context, runner *execution.Runner, root, rel, test string) (probeResult, error) {
	if info, err := os.Stat(filepath.Join(root, rel)); err != nil || !info.IsDir() {
		return probeResult{outcome: probeMissing, reason: "package directory " + filepath.ToSlash(rel) + " does not exist"}, nil
	}
	args := []string{"test", "-json", "-count=1", fmt.Sprintf("-timeout=%ds", int(attributeTestTimeout.Seconds()))}
	if test != "" {
		args = append(args, "-run=^"+regexp.QuoteMeta(test)+"$")
	}
	args = append(args, a.flags...)
	args = append(args, attributePattern(rel))
	var stdout, stderr bytes.Buffer
	result, err := runner.Run(ctx, execution.Command{
		Name: "go", Args: args, Env: map[string]string{"GOWORK": "auto", "GOTOOLCHAIN": "local"},
	}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return probeResult{}, fmt.Errorf("running go test: %w", err)
	}
	if ctx.Err() != nil {
		return probeResult{}, ctx.Err()
	}
	facts := newProbeFacts()
	if _, err := testjson.DecodeTestJSON(&stdout, func(event testjson.TestEvent) error {
		facts.observe(event, test)
		return nil
	}); err != nil {
		return probeResult{outcome: probeBroken, reason: "reading go test output: " + err.Error()}, nil
	}
	return facts.classify(test, result.ExitCode, stderr.String()), nil
}

func attributePattern(rel string) string {
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return "."
	}
	return "./" + rel
}

// probeFacts accumulates the go test -json events relevant to one probe.
type probeFacts struct {
	finished    map[string]bool
	buildOutput strings.Builder
	started     []string
	testFail    bool
	testPass    bool
	anyTestFail bool
	buildFail   bool
	packageFail bool
}

func newProbeFacts() *probeFacts {
	return &probeFacts{finished: map[string]bool{}, started: []string{}}
}

func (f *probeFacts) observe(event testjson.TestEvent, test string) {
	f.observeTopLevel(event)
	switch {
	case event.Action == "build-output":
		f.buildOutput.WriteString(event.Output)
	case event.Action == "build-fail":
		f.buildFail = true
	case event.Test == "" && event.Action == "fail":
		if event.FailedBuild != "" {
			f.buildFail = true
		} else {
			f.packageFail = true
		}
	case event.Test != "" && event.Action == "fail":
		f.anyTestFail = true
		f.testFail = f.testFail || (test != "" && (event.Test == test || strings.HasPrefix(event.Test, test+"/")))
	case test != "" && event.Test == test && event.Action == "pass":
		f.testPass = true
	}
}

// observeTopLevel tracks which top-level tests started and finished, so a
// crash can be pinned to the test that was running.
func (f *probeFacts) observeTopLevel(event testjson.TestEvent) {
	if event.Test == "" || strings.Contains(event.Test, "/") {
		return
	}
	switch event.Action {
	case "run":
		f.started = append(f.started, event.Test)
	case "pass", "fail", "skip":
		f.finished[event.Test] = true
	}
}

// lastStarted is the most recent top-level test without a terminal event.
func (f *probeFacts) lastStarted() string {
	for i := len(f.started) - 1; i >= 0; i-- {
		if !f.finished[f.started[i]] {
			return f.started[i]
		}
	}
	return ""
}

func (f *probeFacts) classify(test string, exitCode int, stderr string) probeResult {
	if test == "" {
		return f.classifyPackage(exitCode, stderr)
	}
	switch {
	case f.testFail:
		return probeResult{outcome: probeFail}
	case f.buildFail:
		return f.broken()
	case f.testPass && exitCode == 0:
		return probeResult{outcome: probePass}
	case f.packageFail:
		return probeResult{outcome: probeFail, lastStarted: f.lastStarted()}
	case exitCode == 0:
		return probeResult{outcome: probeAbsent}
	default:
		return probeResult{outcome: probeBroken, reason: "go test exited " + strconv.Itoa(exitCode) + ": " + attributeOneLine(stderr)}
	}
}

// classifyPackage judges a whole-package run by whether the test binary
// crashed: it failed with a test left unfinished, or failed without any named
// test failing. Named test failures alone are not a crash.
func (f *probeFacts) classifyPackage(exitCode int, stderr string) probeResult {
	last := f.lastStarted()
	switch {
	case f.buildFail:
		return f.broken()
	case f.packageFail && (last != "" || !f.anyTestFail):
		return probeResult{outcome: probeFail, lastStarted: last}
	case f.packageFail || exitCode == 0:
		return probeResult{outcome: probePass}
	default:
		return probeResult{outcome: probeBroken, reason: "go test exited " + strconv.Itoa(exitCode) + ": " + attributeOneLine(stderr)}
	}
}

func (f *probeFacts) broken() probeResult {
	return probeResult{outcome: probeBroken, reason: "package does not build: " + attributeOneLine(attributeBuildMessage(f.buildOutput.String()))}
}

// attributeBuildMessage drops go's "# package" header lines.
func attributeBuildMessage(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return "no compiler output"
}

func attributeOneLine(text string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return first
}

// attributeLocate finds the declaration of a top-level test function in the
// package directory and returns its workspace-relative file and line.
func attributeLocate(root, dir, test string) (string, int) {
	names, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return "", 0
	}
	sort.Strings(names)
	for _, name := range names {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != test {
				continue
			}
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return "", 0
			}
			return filepath.ToSlash(rel), fset.Position(fn.Pos()).Line
		}
	}
	return "", 0
}
