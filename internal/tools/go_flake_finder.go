package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/progress"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FlakeFinderInput configures repeated tests in one bounded subprocess.
type FlakeFinderInput struct {
	Package        string `json:"package" jsonschema:"Go package import path or ./relative/path"`
	Runs           int    `json:"runs,omitempty" jsonschema:"repetitions; default 20, maximum 200"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"test timeout in seconds; default 120, maximum 300"`
}

// FlakeResult is one test observed both passing and failing.
type FlakeResult struct {
	Test      string  `json:"test"`
	Package   string  `json:"package"`
	Runs      int     `json:"runs"`
	Passes    int     `json:"passes"`
	Failures  int     `json:"failures"`
	FlakeRate float64 `json:"flake_rate"`
}

// FlakeFinderOutput contains only observed flakes and the sampled test count.
type FlakeFinderOutput struct {
	Flaky         []FlakeResult `json:"flaky"`
	TotalTestsRun int           `json:"total_tests_run"`
}

// RegisterFlakeFinder registers go_flake_finder with execution hints.
func RegisterFlakeFinder(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_flake_finder",
		Description: "Repeats trusted workspace tests and reports only tests observed both passing and failing.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, runtime.flakeFinder)
}

func (r *Runtime) flakeFinder(ctx context.Context, request *mcp.CallToolRequest, input FlakeFinderInput) (*mcp.CallToolResult, FlakeFinderOutput, error) {
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	started := time.Now()
	if err := normalizeFlakeFinderInput(&input); err != nil {
		r.recordFlakeTrace(input, started, FlakeFinderOutput{}, trace.ErrorInvalidInput)
		return nil, FlakeFinderOutput{}, fmt.Errorf("validating input: %w", err)
	}

	progress.Report(ctx, request, 0, 2, "validating package")
	pattern, err := r.resolvePackage(ctx, input.Package)
	if err != nil {
		r.recordFlakeTrace(input, started, FlakeFinderOutput{}, classifyTraceError(err))
		return nil, FlakeFinderOutput{}, fmt.Errorf("resolving package: %w", err)
	}

	collector := newFlakeCollector()
	progress.Report(ctx, request, 1, 2, "repeating tests")
	result, err := r.runTestJSON(ctx, []string{
		"test",
		"-json",
		"-count=" + strconv.Itoa(input.Runs),
		"-timeout", fmt.Sprintf("%ds", input.TimeoutSeconds),
		pattern,
	}, collector.consume)
	if err != nil {
		r.recordFlakeTrace(input, started, FlakeFinderOutput{}, classifyTraceError(err))
		return nil, FlakeFinderOutput{}, err
	}
	if failedPackage := collector.executionFailure(); result.ExitCode != 0 && failedPackage != "" {
		r.recordFlakeTrace(input, started, FlakeFinderOutput{}, trace.ErrorSubprocess)
		return nil, FlakeFinderOutput{}, fmt.Errorf("tests in package %s failed before producing test results", failedPackage)
	}

	output := collector.result()
	progress.Report(ctx, request, 2, 2, "flake analysis completed")
	r.recordFlakeTrace(input, started, output, trace.ErrorNone)
	return nil, output, nil
}

func normalizeFlakeFinderInput(input *FlakeFinderInput) error {
	if input.Package == "" {
		return fmt.Errorf("package is required")
	}
	if input.Runs < 0 {
		return fmt.Errorf("runs must not be negative")
	}
	if input.Runs == 0 {
		input.Runs = 20
	}
	if input.Runs > 200 {
		input.Runs = 200
	}
	if input.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must not be negative")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 120
	}
	if input.TimeoutSeconds > 300 {
		input.TimeoutSeconds = 300
	}
	return nil
}

type flakeCounts struct {
	test, pkg      string
	passes, failed int
}

type flakeCollector struct {
	results        map[string]*flakeCounts
	packageTests   map[string]int
	failedPackages map[string]struct{}
	total          int
}

func newFlakeCollector() *flakeCollector {
	return &flakeCollector{
		results:        make(map[string]*flakeCounts),
		packageTests:   make(map[string]int),
		failedPackages: make(map[string]struct{}),
	}
}

func (c *flakeCollector) consume(event parser.TestEvent) error {
	if event.Test == "" && event.Action == "fail" && event.Package != "" {
		c.failedPackages[event.Package] = struct{}{}
		return nil
	}
	if event.Test == "" || (event.Action != "pass" && event.Action != "fail" && event.Action != "skip") {
		return nil
	}
	c.total++
	c.packageTests[event.Package]++
	key := testKey(event.Package, event.Test)
	counts := c.results[key]
	if counts == nil {
		counts = &flakeCounts{test: event.Test, pkg: event.Package}
		c.results[key] = counts
	}
	switch event.Action {
	case "pass":
		counts.passes++
	case "fail":
		counts.failed++
	}
	return nil
}

func (c *flakeCollector) executionFailure() string {
	packages := make([]string, 0)
	for pkg := range c.failedPackages {
		if c.packageTests[pkg] == 0 {
			packages = append(packages, pkg)
		}
	}
	sort.Strings(packages)
	if len(packages) == 0 {
		return ""
	}
	return packages[0]
}

func (c *flakeCollector) result() FlakeFinderOutput {
	flaky := make([]FlakeResult, 0)
	for _, counts := range c.results {
		if counts.passes == 0 || counts.failed == 0 {
			continue
		}
		runs := counts.passes + counts.failed
		flaky = append(flaky, FlakeResult{
			Test:      counts.test,
			Package:   counts.pkg,
			Runs:      runs,
			Passes:    counts.passes,
			Failures:  counts.failed,
			FlakeRate: float64(counts.failed) / float64(runs),
		})
	}
	sort.Slice(flaky, func(i, j int) bool {
		if flaky[i].Package == flaky[j].Package {
			return flaky[i].Test < flaky[j].Test
		}
		return flaky[i].Package < flaky[j].Package
	})
	return FlakeFinderOutput{Flaky: flaky, TotalTestsRun: c.total}
}

func (r *Runtime) recordFlakeTrace(input FlakeFinderInput, started time.Time, output FlakeFinderOutput, kind trace.ErrorKind) {
	summary := ""
	if kind == trace.ErrorNone {
		summary = fmt.Sprintf("%d flaky tests across %d runs", len(output.Flaky), output.TotalTestsRun)
	}
	_ = r.tracer.Record(trace.Event{Tool: "go_flake_finder", Args: input, Duration: time.Since(started), ResultSummary: summary, ErrorKind: kind})
}
