package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/progress"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RaceReportInput configures one bounded race-detector run.
type RaceReportInput struct {
	Package        string `json:"package" jsonschema:"Go package import path or ./relative/path"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"test timeout in seconds; default 60, maximum 300"`
}

// RaceReportOutput contains every race detector block recovered from a run.
type RaceReportOutput struct {
	Conflicts      []parser.RaceConflict `json:"conflicts"`
	RawBlocksFound int                   `json:"raw_blocks_found"`
}

// RegisterRaceReport registers go_race_report with truthful execution hints.
func RegisterRaceReport(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_race_report",
		Description: "Runs trusted workspace tests with Go's race detector and returns source-located conflicts.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, runtime.raceReport)
}

func (r *Runtime) raceReport(ctx context.Context, request *mcp.CallToolRequest, input RaceReportInput) (*mcp.CallToolResult, RaceReportOutput, error) {
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	started := time.Now()
	if err := normalizeRaceReportInput(&input); err != nil {
		r.recordRaceTrace(input, started, RaceReportOutput{}, trace.ErrorInvalidInput)
		return nil, RaceReportOutput{}, fmt.Errorf("validating input: %w", err)
	}

	progress.Report(ctx, request, 0, 2, "validating package")
	pattern, err := r.resolvePackage(ctx, input.Package)
	if err != nil {
		r.recordRaceTrace(input, started, RaceReportOutput{}, classifyTraceError(err))
		return nil, RaceReportOutput{}, fmt.Errorf("resolving package: %w", err)
	}

	collector := newRaceCollector()
	progress.Report(ctx, request, 1, 2, "running race detector")
	_, err = r.runTestJSON(ctx, []string{
		"test",
		"-race",
		"-json",
		"-timeout", fmt.Sprintf("%ds", input.TimeoutSeconds),
		pattern,
	}, collector.consume)
	if err != nil {
		r.recordRaceTrace(input, started, RaceReportOutput{}, classifyTraceError(err))
		return nil, RaceReportOutput{}, err
	}

	output := collector.result()
	r.makeRaceLocationsRelative(&output)
	progress.Report(ctx, request, 2, 2, "race analysis completed")
	r.recordRaceTrace(input, started, output, trace.ErrorNone)
	return nil, output, nil
}

func (r *Runtime) makeRaceLocationsRelative(output *RaceReportOutput) {
	for i := range output.Conflicts {
		conflict := &output.Conflicts[i]
		r.makeRaceLocationRelative(&conflict.Current)
		r.makeRaceLocationRelative(&conflict.Previous)

		creations := conflict.GoroutineCreation[:0]
		for _, creation := range conflict.GoroutineCreation {
			if r.makeRaceLocationRelative(&creation) {
				creations = append(creations, creation)
			}
		}
		conflict.GoroutineCreation = creations
	}
}

func (r *Runtime) makeRaceLocationRelative(access *parser.RaceAccess) bool {
	if access.Location.File == "" {
		return false
	}
	file, err := r.workspace.Relative(access.Location.File)
	if err != nil {
		access.Location.File = ""
		access.Location.Line = 0
		access.Location.Col = 0
		return false
	}
	access.Location.File = file
	return true
}

func normalizeRaceReportInput(input *RaceReportInput) error {
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

func (r *Runtime) recordRaceTrace(input RaceReportInput, started time.Time, output RaceReportOutput, kind trace.ErrorKind) {
	summary := ""
	if kind == trace.ErrorNone {
		summary = fmt.Sprintf("%d race conflicts", len(output.Conflicts))
	}
	_ = r.tracer.Record(trace.Event{
		Tool:          "go_race_report",
		Args:          input,
		Duration:      time.Since(started),
		ResultSummary: summary,
		ErrorKind:     kind,
	})
}

type raceCollector struct {
	packages map[string]*strings.Builder
}

func newRaceCollector() *raceCollector {
	return &raceCollector{packages: make(map[string]*strings.Builder)}
}

func (c *raceCollector) consume(event parser.TestEvent) error {
	if event.Action != "output" || event.Package == "" {
		return nil
	}
	builder := c.packages[event.Package]
	if builder == nil {
		builder = &strings.Builder{}
		c.packages[event.Package] = builder
	}
	builder.WriteString(event.Output)
	return nil
}

func (c *raceCollector) result() RaceReportOutput {
	packages := make([]string, 0, len(c.packages))
	for pkg := range c.packages {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)

	output := RaceReportOutput{Conflicts: make([]parser.RaceConflict, 0)}
	for _, pkg := range packages {
		report := parser.Parse(c.packages[pkg].String())
		output.Conflicts = append(output.Conflicts, report.Conflicts...)
		output.RawBlocksFound += report.RawBlocksFound
	}
	return output
}
