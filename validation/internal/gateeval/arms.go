package gateeval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// reasonLimit bounds Run.Reason.
const reasonLimit = 300

// noEventsFailure is the synthetic package failure recorded when go test exits
// non-zero without emitting any failure event, so the failure still counts.
const noEventsFailure = "(go test failed without failure events)"

// testFailures holds the failing top-level tests ("package.TestName") and
// failing packages found in one go test -json stream.
type testFailures struct {
	Tests    map[string]bool
	Packages map[string]bool
}

func newTestFailures() testFailures {
	return testFailures{Tests: map[string]bool{}, Packages: map[string]bool{}}
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

// decideB2Star applies the B2* rule. buildFailed and vetFailed report that
// go build ./... or go vet ./... exited non-zero; lintBlocked reports lint
// issues. Test and package failures block only when they are absent from base.
func decideB2Star(buildFailed, vetFailed, lintBlocked bool, current, base testFailures) (blocked bool, reason string) {
	switch {
	case buildFailed:
		return true, "go build failed"
	case vetFailed:
		return true, "go vet failed"
	case lintBlocked:
		return true, "lint reported issues"
	}
	if keys := newKeys(current.Tests, base.Tests); len(keys) > 0 {
		return true, "new test failure: " + strings.Join(keys, ", ")
	}
	if keys := newKeys(current.Packages, base.Packages); len(keys) > 0 {
		return true, "new package failure: " + strings.Join(keys, ", ")
	}
	return false, ""
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

// stepResult is one finished command. err is set only when the command did not
// produce a verdict: it failed to start, timed out, was canceled, or was
// killed by a signal. A non-zero exit is a normal result.
type stepResult struct {
	err      error
	stdout   []byte
	stderr   []byte
	duration time.Duration
	exit     int
}

// commandEnv is the environment of every evaluated command.
func commandEnv() []string {
	return append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
}

// runCommand runs one command in dir under its own timeout.
func runCommand(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) stepResult {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	cmd.Dir = dir
	cmd.Env = commandEnv()
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

// outcome is what one arm decided about one variant. A non-empty fatal makes
// the run unknown; note is informational.
type outcome struct {
	reason  string
	note    string
	fatal   string
	exit    int
	blocked bool
	warned  bool
}

// armContext is everything an arm needs to evaluate one variant tree. memo is
// nil when every command must run for real.
type armContext struct {
	baseFailures func(ctx context.Context) (testFailures, error)
	memo         map[string]stepResult
	variant      Variant
	dir          string
	gateBin      string
	b3Script     string
	direct       []string
	timeout      time.Duration
}

// run executes one command, adds it to the tally, and reuses an identical
// command already run on this tree when memoization is on. A reused command
// contributes its original duration and output size.
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

// evaluate runs one arm against the variant tree and returns its record.
func (ac *armContext) evaluate(ctx context.Context, arm Arm, attempt int) Run {
	t := &tally{}
	var decided outcome
	var gate gateOutcome
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
		decided, gate = ac.runGate(ctx, t, "ci")
	case ArmGHook:
		decided, gate = ac.runGate(ctx, t, "hook")
	default:
		decided = outcome{fatal: fmt.Sprintf("unknown arm %q", arm)}
	}
	run := Run{
		VariantID:   ac.variant.ID,
		Arm:         arm,
		Attempt:     attempt,
		Blocked:     decided.blocked,
		Warned:      decided.warned,
		Unknown:     decided.fatal != "" || gate.unknown,
		ExitCode:    decided.exit,
		DurationMS:  t.duration.Milliseconds(),
		OutputBytes: t.bytes,
		Reason:      truncate(decided.reason),
		Error:       strings.Join(nonEmpty(decided.fatal, decided.note), "; "),
	}
	if run.Unknown {
		run.Blocked = false
	}
	return run
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

// step is one command of a sequential arm.
type step struct {
	name string
	args []string
}

// agentHabitSteps are B0's commands in order. Vet and test are skipped when
// the change has no direct packages, because go vet with no pattern would
// check the current directory instead.
func (ac *armContext) agentHabitSteps() []step {
	steps := []step{{"go", []string{"build", "./..."}}}
	if len(ac.direct) == 0 {
		return steps
	}
	return append(steps,
		step{"go", append([]string{"vet"}, ac.direct...)},
		step{"go", append([]string{"test", "-count=1"}, ac.direct...)},
	)
}

// runB runs B0 (withLint false) or B1 (withLint true), stopping at the first
// failing command.
func (ac *armContext) runB(ctx context.Context, t *tally, withLint bool) outcome {
	for _, s := range ac.agentHabitSteps() {
		result := ac.run(ctx, t, s.name, s.args...)
		if result.err != nil {
			return outcome{fatal: result.err.Error()}
		}
		if result.exit != 0 {
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
type lintResult struct {
	reason  string
	note    string
	fatal   string
	exit    int
	blocked bool
}

// lint runs golangci-lint on lines changed since base. Exit 1 means issues.
// Any other non-zero exit is a lint error, retried once with the default
// linters and no config; if that also errors, lint is recorded as not run and
// does not block.
func (ac *armContext) lint(ctx context.Context, t *tally) lintResult {
	base := "--new-from-rev=" + ac.variant.Base
	first := ac.run(ctx, t, "golangci-lint", "run", base, "./...")
	if first.err != nil {
		return lintResult{fatal: first.err.Error()}
	}
	switch first.exit {
	case 0:
		return lintResult{}
	case 1:
		return lintResult{blocked: true, exit: 1, reason: snippet("golangci-lint", first)}
	}
	retry := ac.run(ctx, t, "golangci-lint", "run", base, "--no-config", "--default=standard", "./...")
	if retry.err != nil {
		return lintResult{fatal: retry.err.Error()}
	}
	fallback := fmt.Sprintf("note: lint errored (exit %d); retried with --no-config --default=standard", first.exit)
	switch retry.exit {
	case 0:
		return lintResult{note: fallback}
	case 1:
		return lintResult{blocked: true, exit: 1, reason: snippet("golangci-lint", retry), note: fallback}
	}
	return lintResult{note: fmt.Sprintf("note: lint not run: exit %d, then exit %d with --no-config --default=standard", first.exit, retry.exit)}
}

// conventional holds the results of the B2 commands for one tree.
//
//nolint:govet // Keep fields in command order.
type conventional struct {
	build  stepResult
	vet    stepResult
	tests  stepResult
	lint   lintResult
	failed testFailures
	fatal  string
}

// runConventional runs go build, go vet, go test -json over ./... and lint. It
// runs every command so the arms can report the first failure in order.
func (ac *armContext) runConventional(ctx context.Context, t *tally) conventional {
	var conv conventional
	conv.build = ac.run(ctx, t, "go", "build", "./...")
	conv.vet = ac.run(ctx, t, "go", "vet", "./...")
	conv.tests = ac.run(ctx, t, "go", "test", "-count=1", "-json", "./...")
	for _, result := range []stepResult{conv.build, conv.vet, conv.tests} {
		if result.err != nil {
			conv.fatal = result.err.Error()
			return conv
		}
	}
	conv.failed = parseTestFailures(conv.tests.stdout)
	if conv.tests.exit != 0 && len(conv.failed.Tests) == 0 && len(conv.failed.Packages) == 0 {
		conv.failed.Packages[noEventsFailure] = true
	}
	conv.lint = ac.lint(ctx, t)
	conv.fatal = conv.lint.fatal
	return conv
}

// firstFailure reports the first failing B2 command in order, if any.
func (conv conventional) firstFailure() (exit int, reason string, ok bool) {
	switch {
	case conv.build.exit != 0:
		return conv.build.exit, snippet("go build", conv.build), true
	case conv.vet.exit != 0:
		return conv.vet.exit, snippet("go vet", conv.vet), true
	case conv.tests.exit != 0:
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
	if conv.fatal != "" {
		return outcome{fatal: conv.fatal}
	}
	if star {
		return ac.decideStar(ctx, conv)
	}
	exit, reason, failed := conv.firstFailure()
	return outcome{blocked: failed, exit: exit, reason: reason, note: conv.lint.note}
}

// decideStar applies decideB2Star to a finished B2 run.
func (ac *armContext) decideStar(ctx context.Context, conv conventional) outcome {
	base, err := ac.baseFailures(ctx)
	if err != nil {
		return outcome{fatal: fmt.Sprintf("computing base failures: %v", err)}
	}
	blocked, reason := decideB2Star(conv.build.exit != 0, conv.vet.exit != 0, conv.lint.blocked, conv.failed, base)
	if !blocked {
		return outcome{note: conv.lint.note}
	}
	exit, detail, _ := conv.firstFailure()
	if conv.build.exit != 0 || conv.vet.exit != 0 || conv.lint.blocked {
		reason += ": " + detail
	} else {
		exit = conv.tests.exit
	}
	return outcome{blocked: true, exit: exit, reason: reason, note: conv.lint.note}
}

// runB3 runs B2* and the grep gate. Grep exit 1 blocks and exit 2 is an error
// that does not block.
func (ac *armContext) runB3(ctx context.Context, t *tally) outcome {
	conv := ac.runConventional(ctx, t)
	if conv.fatal != "" {
		return outcome{fatal: conv.fatal}
	}
	decided := ac.decideStar(ctx, conv)
	if decided.fatal != "" {
		return decided
	}
	grep := ac.run(ctx, t, "bash", ac.b3Script, ac.variant.Base)
	if grep.err != nil {
		return outcome{fatal: grep.err.Error()}
	}
	switch grep.exit {
	case 0:
		return decided
	case 1:
		if decided.blocked {
			return decided
		}
		return outcome{blocked: true, exit: 1, reason: snippet("b3-grep", grep), note: decided.note}
	case 2:
		decided.note = strings.Join(nonEmpty(decided.note, "note: b3-grep error (exit 2): "+snippet("b3-grep", grep)), "; ")
		return decided
	}
	return outcome{fatal: fmt.Sprintf("b3-grep exited %d: %s", grep.exit, snippet("b3-grep", grep))}
}

// runGate runs agentic-go check with the given profile. Output that does not
// parse makes the run unknown.
func (ac *armContext) runGate(ctx context.Context, t *tally, profile string) (outcome, gateOutcome) {
	args := []string{"check", "--profile", profile, "--base", ac.variant.Base, "--format", "json", "--no-cache"}
	result := runCommand(ctx, ac.dir, ac.timeout, ac.gateBin, args...)
	t.duration += result.duration
	t.bytes += len(result.stdout) + len(result.stderr)
	if result.err != nil {
		return outcome{fatal: result.err.Error()}, gateOutcome{}
	}
	parsed, err := parseGateOutput(result.stdout)
	if err != nil {
		return outcome{exit: result.exit, fatal: err.Error(), reason: snippet("agentic-go check", result)}, gateOutcome{}
	}
	decided := outcome{exit: result.exit, blocked: parsed.blocked, warned: parsed.warned}
	if parsed.blocked || parsed.unknown {
		decided.reason = snippet("agentic-go check", result)
	}
	return decided, parsed
}
