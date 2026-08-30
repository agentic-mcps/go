package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/progress"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStructuredInput configures one bounded go test -json run.
type TestStructuredInput struct {
	Package        string `json:"package" jsonschema:"Go package import path or ./relative/path"`
	Race           bool   `json:"race,omitempty" jsonschema:"enable the race detector; default false"`
	Verbose        bool   `json:"verbose,omitempty" jsonschema:"include passing and skipped test output; default false"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"test timeout in seconds; default 60, maximum 300"`
}

// TestCase is one terminal test result.
//
//nolint:govet // field order is the public JSON schema order.
type TestCase struct {
	Name     string  `json:"name"`
	Package  string  `json:"package"`
	Status   string  `json:"status"`
	ElapsedS float64 `json:"elapsed_s"`
	Output   string  `json:"output,omitempty"`
}

// PackageSummary aggregates test results for one package.
//
//nolint:govet // field order is the public JSON schema order.
type PackageSummary struct {
	Status  string `json:"status"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	Output  string `json:"output,omitempty"`
}

// TestStructuredOutput is the structured result of a go test invocation.
type TestStructuredOutput struct {
	Packages   map[string]PackageSummary `json:"packages"`
	Tests      []TestCase                `json:"tests"`
	Passed     int                       `json:"passed"`
	Failed     int                       `json:"failed"`
	Skipped    int                       `json:"skipped"`
	DurationMS int64                     `json:"duration_ms"`
}

// RegisterTestStructured registers go_test_structured with truthful hints.
func RegisterTestStructured(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_test_structured",
		Description: "Runs trusted workspace tests and returns bounded, structured pass/fail/skip results.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, runtime.testStructured)
}

func (r *Runtime) testStructured(ctx context.Context, request *mcp.CallToolRequest, input TestStructuredInput) (*mcp.CallToolResult, TestStructuredOutput, error) {
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	started := time.Now()
	if err := normalizeTestStructuredInput(&input); err != nil {
		r.recordTestTrace(input, started, TestStructuredOutput{}, trace.ErrorInvalidInput)
		return nil, TestStructuredOutput{}, fmt.Errorf("validating input: %w", err)
	}

	progress.Report(ctx, request, 0, 2, "validating package")
	pattern, err := r.resolvePackage(ctx, input.Package)
	if err != nil {
		r.recordTestTrace(input, started, TestStructuredOutput{}, classifyTraceError(err))
		return nil, TestStructuredOutput{}, fmt.Errorf("resolving package: %w", err)
	}

	arguments := []string{"test", "-json", "-timeout", fmt.Sprintf("%ds", input.TimeoutSeconds)}
	if input.Race {
		arguments = append(arguments, "-race")
	}
	if input.Verbose {
		arguments = append(arguments, "-v")
	}
	arguments = append(arguments, pattern)

	collector := newTestCollector(input.Verbose)
	progress.Report(ctx, request, 1, 2, "running tests")
	result, err := r.runTestJSON(ctx, arguments, collector.consume)
	if err != nil {
		r.recordTestTrace(input, started, TestStructuredOutput{}, classifyTraceError(err))
		return nil, TestStructuredOutput{}, err
	}

	output := collector.result(result.Duration)
	progress.Report(ctx, request, 2, 2, "tests completed")
	r.recordTestTrace(input, started, output, trace.ErrorNone)
	return nil, output, nil
}

func normalizeTestStructuredInput(input *TestStructuredInput) error {
	if input.Package == "" {
		return fmt.Errorf("package is required")
	}
	if input.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 60
	}
	if input.TimeoutSeconds > 300 {
		input.TimeoutSeconds = 300
	}
	return nil
}

func (r *Runtime) recordTestTrace(input TestStructuredInput, started time.Time, output TestStructuredOutput, kind trace.ErrorKind) {
	summary := ""
	if kind == trace.ErrorNone {
		summary = fmt.Sprintf("%d passed, %d failed, %d skipped", output.Passed, output.Failed, output.Skipped)
	}
	_ = r.tracer.Record(trace.Event{
		Tool:          "go_test_structured",
		Args:          input,
		Duration:      time.Since(started),
		ResultSummary: summary,
		ErrorKind:     kind,
	})
}

func classifyTraceError(err error) trace.ErrorKind {
	switch {
	case errors.Is(err, context.Canceled):
		return trace.ErrorCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return trace.ErrorDeadline
	default:
		return trace.ErrorSubprocess
	}
}

//nolint:govet // internal collector order follows event processing semantics.
type testCollector struct {
	verbose       bool
	tests         []TestCase
	packages      map[string]PackageSummary
	testOutput    map[string]*strings.Builder
	packageOutput map[string]*strings.Builder
	terminal      map[string]struct{}
	passed        int
	failed        int
	skipped       int
}

func newTestCollector(verbose bool) *testCollector {
	return &testCollector{
		verbose:       verbose,
		packages:      make(map[string]PackageSummary),
		testOutput:    make(map[string]*strings.Builder),
		packageOutput: make(map[string]*strings.Builder),
		terminal:      make(map[string]struct{}),
	}
}

func (c *testCollector) consume(event parser.TestEvent) error {
	if event.Package == "" {
		return nil
	}
	if _, ok := c.packages[event.Package]; !ok {
		c.packages[event.Package] = PackageSummary{}
	}
	if event.Action == "output" {
		if event.Test == "" {
			builder := c.packageOutput[event.Package]
			if builder == nil {
				builder = &strings.Builder{}
				c.packageOutput[event.Package] = builder
			}
			builder.WriteString(event.Output)
			return nil
		}
		key := testKey(event.Package, event.Test)
		builder := c.testOutput[key]
		if builder == nil {
			builder = &strings.Builder{}
			c.testOutput[key] = builder
		}
		builder.WriteString(event.Output)
		return nil
	}
	if event.Action != "pass" && event.Action != "fail" && event.Action != "skip" {
		return nil
	}
	if event.Test == "" {
		summary := c.packages[event.Package]
		switch event.Action {
		case "pass":
			summary.Status = "ok"
		case "skip":
			summary.Status = "skip"
		case "fail":
			summary.Status = "FAIL"
		}
		if event.Action == "fail" && c.packageOutput[event.Package] != nil {
			summary.Output = c.packageOutput[event.Package].String()
		}
		c.packages[event.Package] = summary
		return nil
	}

	key := testKey(event.Package, event.Test)
	if _, exists := c.terminal[key]; exists {
		return nil
	}
	c.terminal[key] = struct{}{}
	test := TestCase{Name: event.Test, Package: event.Package, Status: event.Action, ElapsedS: event.Elapsed}
	if builder := c.testOutput[key]; builder != nil && (event.Action == "fail" || c.verbose) {
		test.Output = builder.String()
	}
	c.tests = append(c.tests, test)

	summary := c.packages[event.Package]
	switch event.Action {
	case "pass":
		c.passed++
		summary.Passed++
	case "fail":
		c.failed++
		summary.Failed++
	case "skip":
		c.skipped++
		summary.Skipped++
	}
	c.packages[event.Package] = summary
	delete(c.testOutput, key)
	return nil
}

func (c *testCollector) result(duration time.Duration) TestStructuredOutput {
	sort.Slice(c.tests, func(i, j int) bool {
		if c.tests[i].Package == c.tests[j].Package {
			return c.tests[i].Name < c.tests[j].Name
		}
		return c.tests[i].Package < c.tests[j].Package
	})
	return TestStructuredOutput{
		Packages:   c.packages,
		Tests:      c.tests,
		Passed:     c.passed,
		Failed:     c.failed,
		Skipped:    c.skipped,
		DurationMS: duration.Milliseconds(),
	}
}

func testKey(pkg, test string) string { return pkg + "\x00" + test }
