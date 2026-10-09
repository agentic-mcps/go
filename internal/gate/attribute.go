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
)

// probeOutcome is what one targeted go test run showed.
type probeOutcome int

const (
	probeFail probeOutcome = iota
	probePass
	probeAbsent
	probeBroken
)

// probeResult is one targeted go test outcome; reason explains probeBroken.
type probeResult struct {
	reason  string
	outcome probeOutcome
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
	repository  verification.Repository
	flags       []string
}

// attributeBase is the merge-base tree, materialized at most once per run.
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

// close removes the materialized merge-base tree.
func (a *attributor) close() {
	if a.base != nil && a.base.temp != "" {
		_ = os.RemoveAll(a.base.temp)
	}
}

// attribute classifies up to attributeLimit failures; the rest are reported
// as not attributed and make the run incomplete.
func (a *attributor) attribute(ctx context.Context, failures []testFailure) attribution {
	out := attribution{items: []Item{}, notes: []string{}, complete: true}
	for index, failure := range attributeExpandTimeouts(failures) {
		subject := attributeSubject(failure)
		if index >= attributeLimit {
			out.items = append(out.items, a.failureItem(failure, SeverityWarn, CodeTestFailed,
				fmt.Sprintf("%s fails and was not attributed: limit of %d failing tests reached", subject, attributeLimit),
				"Fix the failures shown first, then run the check again."))
			out.complete = false
			continue
		}
		item, note, complete := a.attributeOne(ctx, failure)
		out.items = append(out.items, item)
		if note != "" {
			out.notes = append(out.notes, note)
		}
		out.complete = out.complete && complete
	}
	return out
}

// attributeOne returns the item for one failure, an optional note, and
// whether attribution finished.
func (a *attributor) attributeOne(ctx context.Context, failure testFailure) (Item, string, bool) {
	subject := attributeSubject(failure)
	budgetItem := a.failureItem(failure, SeverityWarn, CodeTestFailed, subject+" fails and was not compared with base (time budget)",
		"Run the check again with a larger time budget.")
	target, known := a.targets[failure.Package]
	if !known {
		return a.blockItem(failure, target), "could not compare " + subject + " with base: package directory is unknown", true
	}
	rel, err := filepath.Rel(a.root, target.Dir)
	if err != nil {
		return a.blockItem(failure, target), "could not compare " + subject + " with base: " + err.Error(), true
	}
	current, err := a.probe(ctx, a.runner, a.root, rel, failure.Test)
	if err != nil && ctx.Err() != nil {
		return budgetItem, "", false
	}
	// A rerun that could not start is no evidence of flakiness, so the
	// failure goes on to the base comparison.
	if err == nil && current.outcome == probePass {
		return a.failureItem(failure, SeverityWarn, CodeTestFlaky, subject+" failed, then passed when rerun; it looks flaky",
			"Check "+subject+" for timing or ordering assumptions."), "", true
	}
	base, err := a.baseTree(ctx)
	if err != nil && ctx.Err() != nil {
		return budgetItem, "", false
	}
	if err != nil {
		return a.blockItem(failure, target), "could not compare " + subject + " with base: " + attributeOneLine(err.Error()), true
	}
	previous, err := a.probe(ctx, base.runner, base.root, rel, failure.Test)
	if err != nil && ctx.Err() != nil {
		return budgetItem, "", false
	}
	if err != nil {
		return a.blockItem(failure, target), "could not compare " + subject + " with base: " + attributeOneLine(err.Error()), true
	}
	switch previous.outcome {
	case probeFail:
		return a.failureItem(failure, SeverityWarn, CodeTestPreexisting,
			fmt.Sprintf("%s also fails at %s; not caused by this change", subject, a.baseShort), ""), "", true
	case probeBroken:
		return a.blockItem(failure, target), "could not compare " + subject + " with base: " + previous.reason, true
	default:
		return a.blockItem(failure, target), "", true
	}
}

// blockItem reports a failure the change caused, naming consumer packages.
func (a *attributor) blockItem(failure testFailure, target verification.ExecutionTarget) Item {
	subject := attributeSubject(failure)
	item := a.failureItem(failure, SeverityBlock, CodeTestFailed, subject+" fails after this change",
		"Fix the code so "+subject+" passes; do not change or skip the test to make it pass.")
	if target.Distance > 0 {
		item.Code = CodeTestConsumerFailed
		item.Message = fmt.Sprintf("%s in consumer package %s fails after this change", subject, failure.Package)
		item.Fix = fmt.Sprintf("Fix the code so %s in consumer package %s passes; do not change or skip the test to make it pass.", subject, failure.Package)
	}
	return item
}

func (a *attributor) failureItem(failure testFailure, severity Severity, code, message, fix string) Item {
	item := Item{Severity: severity, Code: code, Message: message, Fix: fix, Detail: attributeDetail(failure)}
	if target, ok := a.targets[failure.Package]; ok && failure.Test != "" {
		item.File, item.Line = attributeLocate(a.root, target.Dir, failure.Test)
	}
	return item
}

// attributeSubject names the failing test, or the package's test binary for
// a failure outside any named test.
func attributeSubject(failure testFailure) string {
	if failure.Test == "" {
		return "the test binary of " + failure.Package
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
// the package without tests could never reproduce a timeout.
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

// baseTree materializes the merge-base once; later calls reuse the result.
func (a *attributor) baseTree(ctx context.Context) (*attributeBase, error) {
	if a.base != nil {
		return a.base, a.base.err
	}
	a.base = &attributeBase{}
	a.base.err = a.materializeBase(ctx, a.base)
	return a.base, a.base.err
}

func (a *attributor) materializeBase(ctx context.Context, base *attributeBase) error {
	temp, err := os.MkdirTemp("", "agentic-go-gate-base-")
	if err != nil {
		return fmt.Errorf("creating base directory: %w", err)
	}
	base.temp = temp
	root, err := a.materialize(ctx, a.repository, temp)
	if err != nil {
		return fmt.Errorf("materializing base: %w", err)
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

// probe runs one test (or, for an empty name, the package's test binary with
// no tests) in the package at rel under root. Errors are returned only when
// go test could not run at all, such as on cancellation.
func (a *attributor) probe(ctx context.Context, runner *execution.Runner, root, rel, test string) (probeResult, error) {
	if info, err := os.Stat(filepath.Join(root, rel)); err != nil || !info.IsDir() {
		return probeResult{outcome: probeBroken, reason: "package directory " + filepath.ToSlash(rel) + " does not exist"}, nil
	}
	pattern := "^$"
	if test != "" {
		pattern = "^" + regexp.QuoteMeta(test) + "$"
	}
	args := []string{"test", "-json", "-count=1", fmt.Sprintf("-timeout=%ds", int(attributeTestTimeout.Seconds())), "-run=" + pattern}
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
	facts := probeFacts{}
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
	buildOutput strings.Builder
	testFail    bool
	testPass    bool
	buildFail   bool
	packageFail bool
}

func (f *probeFacts) observe(event testjson.TestEvent, test string) {
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
	case test != "" && (event.Test == test || strings.HasPrefix(event.Test, test+"/")):
		if event.Action == "fail" {
			f.testFail = true
		} else if event.Action == "pass" && event.Test == test {
			f.testPass = true
		}
	}
}

func (f *probeFacts) classify(test string, exitCode int, stderr string) probeResult {
	switch {
	case f.testFail:
		return probeResult{outcome: probeFail}
	case f.buildFail:
		return probeResult{outcome: probeBroken, reason: "package does not build: " + attributeOneLine(attributeBuildMessage(f.buildOutput.String()))}
	case f.testPass && exitCode == 0:
		return probeResult{outcome: probePass}
	case f.packageFail:
		return probeResult{outcome: probeFail}
	case exitCode == 0 && test == "":
		return probeResult{outcome: probePass}
	case exitCode == 0:
		return probeResult{outcome: probeAbsent}
	default:
		return probeResult{outcome: probeBroken, reason: "go test exited " + strconv.Itoa(exitCode) + ": " + attributeOneLine(stderr)}
	}
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
