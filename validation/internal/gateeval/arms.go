package gateeval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agentic-mcps/go/internal/gate"
)

const (
	// reasonLimit bounds Run.Reason.
	reasonLimit = 300
	// dirtLimit bounds the dirty-tree note.
	dirtLimit = 200
	// gateTextLimit is the byte budget the gate's text report is rendered with.
	gateTextLimit = 2048
	// timeoutReasonPrefix starts the Reason of a B-arm block caused by a go test
	// timeout, so the summary can count them.
	timeoutReasonPrefix = "go test timed out"
)

// Synthetic failure keys recorded when a command exits non-zero without
// emitting anything attributable, so the failure still counts.
const (
	noEventsFailure = "(go test failed without failure events)"
	noVetFailure    = "(go vet failed without package headers)"
)

// testFailures holds the failing top-level tests ("package.TestName"), failing
// packages found in one go test -json stream, and packages go vet reported.
type testFailures struct {
	Tests    map[string]bool
	Packages map[string]bool
	Vet      map[string]bool
}

func newTestFailures() testFailures {
	return testFailures{Tests: map[string]bool{}, Packages: map[string]bool{}, Vet: map[string]bool{}}
}

// testEvent is the subset of a go test -json event needed to find failures.
type testEvent struct {
	Action     string `json:"Action"`
	Package    string `json:"Package"`
	Test       string `json:"Test"`
	ImportPath string `json:"ImportPath"`
}

// parseTestFailures reads a go test -json stream. An Action of fail with a
// Test is a test failure keyed by package and top-level test name; a fail
// without a Test is a package failure; a build-fail event is a package failure
// for its import path. Lines that are not JSON events are ignored.
func parseTestFailures(stream []byte) testFailures {
	failures := newTestFailures()
	for _, line := range bytes.Split(stream, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event testEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch {
		case event.Action == "fail" && event.Test != "":
			top, _, _ := strings.Cut(event.Test, "/")
			failures.Tests[event.Package+"."+top] = true
		case event.Action == "fail":
			failures.Packages[event.Package] = true
		case event.Action == "build-fail":
			path, _, _ := strings.Cut(event.ImportPath, " ")
			failures.Packages[path] = true
		}
	}
	return failures
}

// vetDiagnostic matches a go vet diagnostic line: a file path, a line, an
// optional column, then the message. Go 1.26 prints only these lines; older
// versions print a "# package" header before them.
var vetDiagnostic = regexp.MustCompile(`^(?:vet: )?(\S+?\.go):\d+(?::\d+)?: `)

// parseVetPackages returns the packages go vet reported on. With "# package"
// headers (Go 1.25 and older) they are import paths. Without headers (Go 1.26)
// each diagnostic's file maps to its package directory, as "./dir" relative to
// root ("." for the root package).
func parseVetPackages(output []byte, root string) map[string]bool {
	packages := map[string]bool{}
	var files []string
	for _, line := range strings.Split(string(output), "\n") {
		if header, ok := strings.CutPrefix(line, "# "); ok {
			if name := vetHeaderPackage(header); name != "" {
				packages[name] = true
			}
			continue
		}
		if match := vetDiagnostic.FindStringSubmatch(line); match != nil {
			files = append(files, match[1])
		}
	}
	if len(packages) > 0 {
		return packages
	}
	for _, file := range files {
		packages[vetFileDir(file, root)] = true
	}
	return packages
}

// vetFileDir maps a diagnostic's file to "./dir" relative to root.
func vetFileDir(file, root string) string {
	file = filepath.ToSlash(file)
	if filepath.IsAbs(file) {
		if rel, err := filepath.Rel(filepath.ToSlash(root), file); err == nil {
			file = filepath.ToSlash(rel)
		}
	}
	dir := strings.TrimPrefix(path.Dir(file), "./")
	if dir == "." || dir == "" {
		return "."
	}
	return "./" + dir
}

// vetFailureKeys names the failures in a go vet run that exited non-zero. When
// no package can be attributed, the key is a sentinel bound to a hash of the
// output, so it matches base only when the vet output is identical.
func vetFailureKeys(result stepResult, root string) map[string]bool {
	output := append(append([]byte{}, result.stderr...), result.stdout...)
	if keys := parseVetPackages(output, root); len(keys) > 0 {
		return keys
	}
	sum := sha256.Sum256(bytes.TrimSpace(output))
	return map[string]bool{noVetFailure + " " + hex.EncodeToString(sum[:6]): true}
}

// vetHeaderPackage extracts the import path from a go vet header, which reads
// "pkg", "pkg [pkg.test]", or "[pkg]" depending on the Go version.
func vetHeaderPackage(header string) string {
	header = strings.TrimSpace(header)
	if inner, ok := strings.CutPrefix(header, "["); ok {
		inner, _, _ = strings.Cut(inner, "]")
		return strings.TrimSpace(inner)
	}
	path, _, _ := strings.Cut(header, " ")
	return path
}

// decideB2Star applies the B2* rule. buildFailed and vetFailed report that
// go build ./... or go vet ./... exited non-zero; lintBlocked reports lint
// issues. Test failures, package failures and vet failures block only when
// they are absent from base. A vet failure with no package attribution in
// current blocks.
func decideB2Star(buildFailed, vetFailed, lintBlocked bool, current, base testFailures) (blocked bool, reason string) {
	blocked, _, reason = starBlock(buildFailed, vetFailed, lintBlocked, current, base)
	return blocked, reason
}

// Causes reported by starBlock.
const (
	causeBuild   = "build"
	causeVet     = "vet"
	causeLint    = "lint"
	causeTest    = "test"
	causePackage = "package"
)

// starBlock is decideB2Star with the cause of the block.
func starBlock(buildFailed, vetFailed, lintBlocked bool, current, base testFailures) (blocked bool, cause, reason string) {
	if buildFailed {
		return true, causeBuild, "go build failed"
	}
	if vetFailed {
		keys := newKeys(current.Vet, base.Vet)
		if len(keys) > 0 {
			return true, causeVet, "go vet failed: " + strings.Join(keys, ", ")
		}
		if len(current.Vet) == 0 {
			return true, causeVet, "go vet failed"
		}
	}
	if lintBlocked {
		return true, causeLint, "lint reported issues"
	}
	if keys := newKeys(current.Tests, base.Tests); len(keys) > 0 {
		return true, causeTest, "new test failure: " + strings.Join(keys, ", ")
	}
	if keys := newKeys(current.Packages, base.Packages); len(keys) > 0 {
		return true, causePackage, "new package failure: " + strings.Join(keys, ", ")
	}
	return false, "", ""
}

// newKeys returns the sorted keys of current that are not in base.
func newKeys(current, base map[string]bool) []string {
	keys := make([]string, 0, len(current))
	for key := range current {
		if !base[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// gateOutcome is the parsed result of one agentic-go check invocation.
type gateOutcome struct {
	blocked bool
	warned  bool
	unknown bool
}

// gateOutput is the subset of the check JSON the evaluation reads.
type gateOutput struct {
	Verdict string `json:"verdict"`
	Items   []struct {
		Severity string `json:"severity"`
	} `json:"items"`
}

// parseGateOutput parses agentic-go check JSON from stdout. Any verdict other
// than pass, block, or unknown is an error.
func parseGateOutput(stdout []byte) (gateOutcome, error) {
	var parsed gateOutput
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &parsed); err != nil {
		return gateOutcome{}, fmt.Errorf("unparseable gate output: %w", err)
	}
	var outcome gateOutcome
	switch parsed.Verdict {
	case "pass":
	case "block":
		outcome.blocked = true
	case "unknown":
		outcome.unknown = true
	default:
		return gateOutcome{}, fmt.Errorf("unrecognized gate verdict %q", parsed.Verdict)
	}
	for _, item := range parsed.Items {
		if strings.EqualFold(item.Severity, "warn") {
			outcome.warned = true
		}
	}
	return outcome, nil
}

// gateTextBytes is the size of the gate's compact text report for the given
// JSON output, rendered with the same budget an agent would see. It is 0, which
// leaves Run.TextBytes unset (omitted from the record and skipped by the median
// in score.go), when the JSON does not decode or render as a gate result.
func gateTextBytes(stdout []byte) int {
	var result gate.Result
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &result); err != nil {
		return 0
	}
	var text bytes.Buffer
	if err := gate.WriteText(&text, result, gateTextLimit); err != nil {
		return 0
	}
	return text.Len()
}

// stepResult is one finished command. err is set only when the command did not
// produce a verdict: it failed to start, timed out, was canceled, or was
// killed by a signal. A non-zero exit is a normal result. timedOut marks an
// err caused by the command's own timeout.
type stepResult struct {
	err      error
	stdout   []byte
	stderr   []byte
	duration time.Duration
	exit     int
	timedOut bool
}

// failedExit reports a command that ran and exited non-zero.
func failedExit(result stepResult) bool {
	return result.err == nil && result.exit != 0
}

// commandEnv is the environment of every evaluated command. CI is removed so
// variants that branch on it behave the same locally and in CI.
func commandEnv() []string {
	environ := os.Environ()
	env := make([]string, 0, len(environ)+2)
	for _, entry := range environ {
		if !strings.HasPrefix(entry, "CI=") {
			env = append(env, entry)
		}
	}
	return append(env, "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
}

// runCommand runs one command in dir under its own timeout.
func runCommand(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) stepResult {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	cmd.Dir = dir
	cmd.Env = commandEnv()
	if filepath.Base(name) == "golangci-lint" {
		// The shared lint cache returns stale results across worktrees with
		// --new-from-rev, so every invocation gets a fresh one.
		cacheDir, err := os.MkdirTemp("", "golangci-lint-cache-")
		if err != nil {
			return stepResult{err: fmt.Errorf("creating lint cache directory: %w", err)}
		}
		defer func() { _ = os.RemoveAll(cacheDir) }()
		cmd.Env = append(cmd.Env, "GOLANGCI_LINT_CACHE="+cacheDir)
	}
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	result := stepResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), duration: time.Since(start)}
	if err == nil {
		return result
	}
	label := name + " " + strings.Join(args, " ")
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		result.err = fmt.Errorf("%s: canceled: %w", label, ctx.Err())
	case cmdCtx.Err() != nil:
		result.err = fmt.Errorf("%s: timed out after %s", label, timeout)
		result.timedOut = true
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		result.exit = exitErr.ExitCode()
	case errors.As(err, &exitErr):
		result.err = fmt.Errorf("%s: terminated by signal: %w", label, err)
	default:
		result.err = fmt.Errorf("%s: %w", label, err)
	}
	return result
}

// tally accumulates the duration and output size of an arm's commands.
type tally struct {
	duration time.Duration
	bytes    int
}

// infraError marks a failure of the harness itself (git, worktrees), as
// opposed to a result about the variant. Such runs are not recorded.
type infraError struct{ err error }

func (e *infraError) Error() string { return e.err.Error() }
func (e *infraError) Unwrap() error { return e.err }

// outcome is what one arm decided about one variant. A non-empty fatal makes
// the run unknown; note is informational.
type outcome struct {
	reason    string
	note      string
	fatal     string
	exit      int
	textBytes int
	blocked   bool
	warned    bool
}

// armContext is everything an arm needs to evaluate one variant tree. memo is
// nil when every command must run for real. reset restores the variant tree
// exactly; status reports uncommitted changes (git status --porcelain).
type armContext struct {
	baseFailures func(ctx context.Context) (testFailures, error)
	reset        func(ctx context.Context) error
	status       func(ctx context.Context) (string, error)
	// lintReference returns the lint issues on the unmodified commit.
	lintReference func(ctx context.Context) ([]lintIssue, error)
	memo          map[string]stepResult
	infra         error
	variant       Variant
	dir           string
	gateBin       string
	b3Script      string
	direct        []string
	timeout       time.Duration
}

// run executes one command, adds it to the tally, and reuses an identical
// command already run on this variant's tree when memoization is on. Every arm
// starts from a freshly reset tree, so a reused result describes the same
// content. A reused command contributes its original duration and output size.
func (ac *armContext) run(ctx context.Context, t *tally, name string, args ...string) stepResult {
	key := name + "\x00" + strings.Join(args, "\x00")
	result, ok := ac.memo[key]
	if !ok {
		result = runCommand(ctx, ac.dir, ac.timeout, name, args...)
		if ac.memo != nil && ctx.Err() == nil {
			ac.memo[key] = result
		}
	}
	t.duration += result.duration
	t.bytes += len(result.stdout) + len(result.stderr)
	return result
}

// resetTree restores the variant tree, recording a harness failure.
func (ac *armContext) resetTree(ctx context.Context) {
	if ac.infra != nil {
		return
	}
	if err := ac.reset(ctx); err != nil {
		ac.infra = &infraError{fmt.Errorf("resetting tree: %w", err)}
	}
}

// dirtNote reports uncommitted changes in the tree as a note, or "" when clean.
func (ac *armContext) dirtNote(ctx context.Context) string {
	if ac.infra != nil {
		return ""
	}
	out, err := ac.status(ctx)
	if err != nil {
		ac.infra = &infraError{fmt.Errorf("checking tree status: %w", err)}
		return ""
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	return "note: dirty tree before arm: " + oneLine(out, dirtLimit)
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", "; ")), " ")
	if len(text) > limit {
		text = text[:limit]
	}
	return text
}

// evaluate resets the tree and runs one arm against it. It returns an error
// when the harness failed or the context ended; such a run must not be recorded.
func (ac *armContext) evaluate(ctx context.Context, arm Arm, attempt int) (Run, error) {
	ac.infra = nil
	ac.resetTree(ctx)
	if ac.infra != nil {
		return Run{}, ac.infra
	}
	t := &tally{}
	var decided outcome
	var parsed gateOutcome
	switch arm {
	case ArmB0:
		decided = ac.runB(ctx, t, false)
	case ArmB1:
		decided = ac.runB(ctx, t, true)
	case ArmB2:
		decided = ac.runB2(ctx, t, false)
	case ArmB2Star:
		decided = ac.runB2(ctx, t, true)
	case ArmB3:
		decided = ac.runB3(ctx, t)
	case ArmGCI:
		decided, parsed = ac.runGate(ctx, t, "ci")
	case ArmGHook:
		decided, parsed = ac.runGate(ctx, t, "hook")
	default:
		decided = outcome{fatal: fmt.Sprintf("unknown arm %q", arm)}
	}
	if ac.infra != nil {
		return Run{}, ac.infra
	}
	if err := ctx.Err(); err != nil {
		return Run{}, err
	}
	run := Run{
		VariantID:   ac.variant.ID,
		Arm:         arm,
		Attempt:     attempt,
		Blocked:     decided.blocked,
		Warned:      decided.warned,
		Unknown:     decided.fatal != "" || parsed.unknown,
		ExitCode:    decided.exit,
		DurationMS:  t.duration.Milliseconds(),
		OutputBytes: t.bytes,
		TextBytes:   decided.textBytes,
		Reason:      truncate(decided.reason),
		Error:       strings.Join(nonEmpty(decided.fatal, decided.note), "; "),
	}
	if run.Unknown {
		run.Blocked = false
	}
	return run, nil
}

func nonEmpty(values ...string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

// truncate keeps the first reasonLimit bytes without splitting a rune.
func truncate(text string) string {
	if len(text) <= reasonLimit {
		return text
	}
	end := reasonLimit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

// snippet describes a failed command from its output, falling back to its exit
// status when it printed nothing.
func snippet(label string, result stepResult) string {
	text := strings.TrimSpace(string(result.stderr) + string(result.stdout))
	if text == "" {
		return fmt.Sprintf("%s exited %d", label, result.exit)
	}
	return truncate(text)
}

// timeoutReason is the Reason of a go test timeout block.
func (ac *armContext) timeoutReason() string {
	return fmt.Sprintf("%s after %s", timeoutReasonPrefix, ac.timeout)
}

// step is one command of a sequential arm.
type step struct {
	name string
	args []string
	test bool
}

// agentHabitSteps are B0's commands in order. Vet and test are skipped when
// the change has no direct packages, because go vet with no pattern would
// check the current directory instead.
func (ac *armContext) agentHabitSteps() []step {
	steps := []step{{name: "go", args: []string{"build", "./..."}}}
	if len(ac.direct) == 0 {
		return steps
	}
	return append(steps,
		step{name: "go", args: append([]string{"vet"}, ac.direct...)},
		step{name: "go", args: append([]string{"test", "-count=1"}, ac.direct...), test: true},
	)
}

// runB runs B0 (withLint false) or B1 (withLint true), stopping at the first
// failing command. A go test timeout is a block, as it would fail a CI job.
func (ac *armContext) runB(ctx context.Context, t *tally, withLint bool) outcome {
	for _, s := range ac.agentHabitSteps() {
		result := ac.run(ctx, t, s.name, s.args...)
		switch {
		case result.err != nil && s.test && result.timedOut:
			return outcome{blocked: true, exit: -1, reason: ac.timeoutReason()}
		case result.err != nil:
			return outcome{fatal: result.err.Error()}
		case result.exit != 0:
			return outcome{blocked: true, exit: result.exit, reason: snippet(s.name+" "+s.args[0], result)}
		}
	}
	if !withLint {
		return outcome{}
	}
	lint := ac.lint(ctx, t)
	return outcome{blocked: lint.blocked, exit: lint.exit, reason: lint.reason, note: lint.note, fatal: lint.fatal}
}

// lintResult is the outcome of golangci-lint for one tree.
//
//nolint:govet // Keep fields in reporting order.
type lintResult struct {
	reason  string
	note    string
	fatal   string
	output  []byte
	exit    int
	blocked bool
	notRun  bool
}

// lint runs golangci-lint on lines changed since base, through the step cache.
func (ac *armContext) lint(ctx context.Context, t *tally) lintResult {
	return lintWith(func(args ...string) stepResult {
		return ac.run(ctx, t, "golangci-lint", args...)
	}, ac.variant.Base)
}

// lintWith runs golangci-lint on lines changed since base using run to execute
// each command. Exit 1 means issues. Any other non-zero exit is a lint error,
// retried once with the default linters and no config; if that also errors,
// lint is recorded as not run and does not block.
func lintWith(run func(args ...string) stepResult, base string) lintResult {
	rev := "--new-from-rev=" + base
	first := run("run", rev, "./...")
	if first.err != nil {
		return lintResult{fatal: first.err.Error()}
	}
	switch first.exit {
	case 0:
		return lintResult{}
	case 1:
		return lintResult{blocked: true, exit: 1, reason: snippet("golangci-lint", first), output: stepOutput(first)}
	}
	retry := run("run", rev, "--no-config", "--default=standard", "./...")
	if retry.err != nil {
		return lintResult{fatal: retry.err.Error()}
	}
	fallback := fmt.Sprintf("note: lint errored (exit %d); retried with --no-config --default=standard", first.exit)
	switch retry.exit {
	case 0:
		return lintResult{note: fallback}
	case 1:
		return lintResult{blocked: true, exit: 1, reason: snippet("golangci-lint", retry), output: stepOutput(retry), note: fallback}
	}
	return lintResult{notRun: true, note: fmt.Sprintf("note: lint not run: exit %d, then exit %d with --no-config --default=standard", first.exit, retry.exit)}
}

// stepOutput is a command's stdout followed by its stderr.
func stepOutput(result stepResult) []byte {
	return append(append([]byte{}, result.stdout...), result.stderr...)
}

// lintIssue is one lint finding, identified without its line number because
// edits shift lines.
type lintIssue struct {
	File    string `json:"file"`
	Linter  string `json:"linter"`
	Message string `json:"message"`
}

// lintText matches a golangci-lint text issue: "file:line:col: message (linter)".
var lintText = regexp.MustCompile(`^(\S+?\.go):\d+(?::\d+)?: (.+) \(([A-Za-z0-9_.-]+)\)$`)

// lintJSON is the subset of golangci-lint's JSON report the evaluation reads.
type lintJSON struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"`
		Text       string `json:"Text"`
		Pos        struct {
			Filename string `json:"Filename"`
		} `json:"Pos"`
	} `json:"Issues"`
}

// parseLintIssues reads golangci-lint output in its JSON report form or its
// default text form.
func parseLintIssues(output []byte) []lintIssue {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var report lintJSON
		if err := json.Unmarshal(trimmed, &report); err == nil {
			issues := make([]lintIssue, 0, len(report.Issues))
			for _, issue := range report.Issues {
				issues = append(issues, lintIssue{File: lintFile(issue.Pos.Filename), Linter: issue.FromLinter, Message: issue.Text})
			}
			return issues
		}
	}
	var issues []lintIssue
	for _, line := range strings.Split(string(output), "\n") {
		if match := lintText.FindStringSubmatch(strings.TrimRight(line, "\r")); match != nil {
			issues = append(issues, lintIssue{File: lintFile(match[1]), Linter: match[3], Message: match[2]})
		}
	}
	return issues
}

func lintFile(name string) string {
	return strings.TrimPrefix(filepath.ToSlash(name), "./")
}

// newLintIssues returns the issues in current beyond those in reference,
// comparing as multisets of (file, linter, message).
func newLintIssues(current, reference []lintIssue) []lintIssue {
	remaining := map[lintIssue]int{}
	for _, issue := range reference {
		remaining[issue]++
	}
	var added []lintIssue
	for _, issue := range current {
		if remaining[issue] > 0 {
			remaining[issue]--
			continue
		}
		added = append(added, issue)
	}
	return added
}

func describeLintIssue(issue lintIssue) string {
	return fmt.Sprintf("%s: %s (%s)", issue.File, issue.Message, issue.Linter)
}

// filterLint applies Amendment 3 to B2*: lint blocks only for issues the same
// lint command does not also report on the unmodified commit. When the
// reference is unavailable, raw lint stands and a note says so.
func (ac *armContext) filterLint(ctx context.Context, lint lintResult) (lintResult, string) {
	if !lint.blocked || ac.lintReference == nil {
		return lint, ""
	}
	current := parseLintIssues(lint.output)
	if len(current) == 0 {
		return lint, "note: lint issues could not be parsed; raw lint used"
	}
	reference, err := ac.lintReference(ctx)
	if err != nil {
		var infra *infraError
		if errors.As(err, &infra) {
			ac.infra = err
			return lint, ""
		}
		return lint, fmt.Sprintf("note: reference lint unavailable (%v); raw lint used", err)
	}
	added := newLintIssues(current, reference)
	if len(added) == 0 {
		lint.blocked, lint.reason, lint.exit = false, "", 0
		return lint, fmt.Sprintf("note: %d lint issue(s) also reported on the unmodified commit ignored", len(current))
	}
	lint.reason = fmt.Sprintf("%d new lint issue(s), first: %s", len(added), describeLintIssue(added[0]))
	return lint, ""
}

// conventional holds the results of the B2 commands for one tree. errs lists
// commands that gave no verdict (failed to start, timed out other than go
// test); they make a run unknown only when nothing else blocked it.
//
//nolint:govet // Keep fields in command order.
type conventional struct {
	build         stepResult
	vet           stepResult
	tests         stepResult
	lint          lintResult
	failed        testFailures
	errs          []string
	timeoutReason string
}

// runConventional runs go build, go vet, go test -json over ./... and lint. It
// runs every command so the arms can report the first failure in order.
func (ac *armContext) runConventional(ctx context.Context, t *tally) conventional {
	conv := conventional{failed: newTestFailures(), timeoutReason: ac.timeoutReason()}
	conv.build = ac.run(ctx, t, "go", "build", "./...")
	conv.vet = ac.run(ctx, t, "go", "vet", "./...")
	conv.tests = ac.run(ctx, t, "go", "test", "-count=1", "-json", "./...")
	conv.lint = ac.lint(ctx, t)
	for _, result := range []stepResult{conv.build, conv.vet} {
		if result.err != nil {
			conv.errs = append(conv.errs, result.err.Error())
		}
	}
	if conv.tests.err != nil && !conv.tests.timedOut {
		conv.errs = append(conv.errs, conv.tests.err.Error())
	}
	if conv.lint.fatal != "" {
		conv.errs = append(conv.errs, conv.lint.fatal)
	}
	if failedExit(conv.vet) {
		conv.failed.Vet = vetFailureKeys(conv.vet, ac.dir)
	}
	if conv.tests.err == nil || conv.tests.timedOut {
		parsed := parseTestFailures(conv.tests.stdout)
		conv.failed.Tests, conv.failed.Packages = parsed.Tests, parsed.Packages
	}
	if failedExit(conv.tests) && len(conv.failed.Tests) == 0 && len(conv.failed.Packages) == 0 {
		conv.failed.Packages[noEventsFailure] = true
	}
	return conv
}

// notes joins informational notes, including commands that gave no verdict.
func (conv conventional) notes() string {
	notes := []string{conv.lint.note}
	for _, err := range conv.errs {
		notes = append(notes, "note: "+err)
	}
	return strings.Join(nonEmpty(notes...), "; ")
}

// firstFailure reports the first failing B2 command in order, if any. A go
// test timeout is a failure.
func (conv conventional) firstFailure() (exit int, reason string, ok bool) {
	switch {
	case failedExit(conv.build):
		return conv.build.exit, snippet("go build", conv.build), true
	case failedExit(conv.vet):
		return conv.vet.exit, snippet("go vet", conv.vet), true
	case conv.tests.timedOut:
		return -1, conv.timeoutReason, true
	case failedExit(conv.tests):
		return conv.tests.exit, "go test failed: " + failureSummary(conv.failed), true
	case conv.lint.blocked:
		return conv.lint.exit, conv.lint.reason, true
	}
	return 0, "", false
}

func failureSummary(failed testFailures) string {
	keys := append(newKeys(failed.Tests, nil), newKeys(failed.Packages, nil)...)
	return strings.Join(keys, ", ")
}

// runB2 runs B2, or B2* when star is true.
func (ac *armContext) runB2(ctx context.Context, t *tally, star bool) outcome {
	conv := ac.runConventional(ctx, t)
	if star {
		return ac.decideStar(ctx, conv)
	}
	if exit, reason, failed := conv.firstFailure(); failed {
		return outcome{blocked: true, exit: exit, reason: reason, note: conv.notes()}
	}
	if len(conv.errs) > 0 {
		return outcome{fatal: conv.errs[0], note: conv.lint.note}
	}
	return outcome{note: conv.lint.note}
}

// decideStar applies the B2* rule to a finished B2 run. A block that does not
// depend on base (build failure, go test timeout) is never downgraded by a
// failure to compute base or by a command that gave no verdict.
func (ac *armContext) decideStar(ctx context.Context, conv conventional) outcome {
	switch {
	case failedExit(conv.build):
		return outcome{blocked: true, exit: conv.build.exit, reason: "go build failed: " + snippet("go build", conv.build), note: conv.notes()}
	case conv.tests.timedOut:
		return outcome{blocked: true, exit: -1, reason: conv.timeoutReason, note: conv.notes()}
	}
	lint, lintNote := ac.filterLint(ctx, conv.lint)
	if ac.infra != nil {
		return outcome{}
	}
	conv.lint.note = strings.Join(nonEmpty(conv.lint.note, lintNote), "; ")
	base, err := ac.baseFailures(ctx)
	if err != nil {
		var infra *infraError
		if errors.As(err, &infra) {
			ac.infra = err
			return outcome{}
		}
		if lint.blocked {
			return outcome{
				blocked: true, exit: lint.exit, reason: "lint reported issues: " + lint.reason,
				note: strings.Join(nonEmpty(conv.notes(), fmt.Sprintf("note: base failures unavailable: %v", err)), "; "),
			}
		}
		return outcome{fatal: fmt.Sprintf("computing base failures: %v", err), note: conv.notes()}
	}
	blocked, cause, reason := starBlock(failedExit(conv.build), failedExit(conv.vet), lint.blocked, conv.failed, base)
	if !blocked {
		if len(conv.errs) > 0 {
			return outcome{fatal: conv.errs[0], note: conv.lint.note}
		}
		return outcome{note: conv.lint.note}
	}
	decided := outcome{blocked: true, reason: reason, note: conv.notes()}
	switch cause {
	case causeVet:
		decided.exit = conv.vet.exit
		decided.reason += ": " + snippet("go vet", conv.vet)
	case causeLint:
		decided.exit = lint.exit
		decided.reason += ": " + lint.reason
	default:
		decided.exit = conv.tests.exit
	}
	return decided
}

// runB3 runs B2* and the grep gate. Grep exit 1 blocks and exit 2 is an error
// that does not block. The tree is reset before the grep, so build and test
// side effects (such as go.mod rewrites) cannot reach it. A grep block is
// never downgraded by an earlier command that gave no verdict.
func (ac *armContext) runB3(ctx context.Context, t *tally) outcome {
	conv := ac.runConventional(ctx, t)
	decided := ac.decideStar(ctx, conv)
	ac.resetTree(ctx)
	note := ac.dirtNote(ctx)
	if ac.infra != nil {
		return outcome{}
	}
	grep := ac.run(ctx, t, "bash", ac.b3Script, ac.variant.Base)
	decided.note = strings.Join(nonEmpty(decided.note, note), "; ")
	switch {
	case grep.err != nil && decided.blocked:
		decided.note = strings.Join(nonEmpty(decided.note, "note: "+grep.err.Error()), "; ")
		return decided
	case grep.err != nil:
		decided.fatal = grep.err.Error()
		return decided
	case grep.exit == 0:
		return decided
	case grep.exit == 1 && decided.blocked:
		return decided
	case grep.exit == 1:
		return outcome{blocked: true, exit: 1, reason: snippet("b3-grep", grep), note: decided.note}
	case grep.exit == 2:
		decided.note = strings.Join(nonEmpty(decided.note, "note: b3-grep error (exit 2): "+snippet("b3-grep", grep)), "; ")
		return decided
	}
	failure := fmt.Sprintf("b3-grep exited %d: %s", grep.exit, snippet("b3-grep", grep))
	if decided.blocked {
		decided.note = strings.Join(nonEmpty(decided.note, "note: "+failure), "; ")
		return decided
	}
	decided.fatal = failure
	return decided
}

// runGate runs agentic-go check with the given profile. Output that does not
// parse makes the run unknown.
func (ac *armContext) runGate(ctx context.Context, t *tally, profile string) (outcome, gateOutcome) {
	note := ac.dirtNote(ctx)
	if ac.infra != nil {
		return outcome{}, gateOutcome{}
	}
	args := []string{"check", "--profile", profile, "--base", ac.variant.Base, "--format", "json", "--no-cache"}
	result := runCommand(ctx, ac.dir, ac.timeout, ac.gateBin, args...)
	t.duration += result.duration
	t.bytes += len(result.stdout) + len(result.stderr)
	if result.err != nil {
		return outcome{fatal: result.err.Error(), note: note}, gateOutcome{}
	}
	parsed, err := parseGateOutput(result.stdout)
	if err != nil {
		return outcome{exit: result.exit, fatal: err.Error(), reason: snippet("agentic-go check", result), note: note}, gateOutcome{}
	}
	decided := outcome{exit: result.exit, blocked: parsed.blocked, warned: parsed.warned, note: note, textBytes: gateTextBytes(result.stdout)}
	if parsed.blocked || parsed.unknown {
		decided.reason = snippet("agentic-go check", result)
	}
	return decided, parsed
}
