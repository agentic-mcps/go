package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/progress"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxCoverageProfileSize = 8 << 20

// CoverageGapsInput selects packages for one bounded coverage run.
type CoverageGapsInput struct {
	Package string `json:"package" jsonschema:"Go package import path or ./relative/path"`
}

// CoverageGap is one uncovered source range.
type CoverageGap struct {
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	StartCol   int    `json:"start_col"`
	EndLine    int    `json:"end_line"`
	EndCol     int    `json:"end_col"`
	Statements int    `json:"statements"`
}

// FileCoverage contains statement coverage and gaps for one source file.
//
//nolint:govet // field order is the public JSON schema order.
type FileCoverage struct {
	File    string        `json:"file"`
	Percent float64       `json:"percent"`
	Gaps    []CoverageGap `json:"gaps"`
}

// CoverageGapsOutput is a statement-weighted coverage report.
type CoverageGapsOutput struct {
	Files          []FileCoverage `json:"files"`
	OverallPercent float64        `json:"overall_percent"`
}

// RegisterCoverageGaps registers go_coverage_gaps with execution hints.
func RegisterCoverageGaps(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_coverage_gaps",
		Description: "Runs trusted workspace tests and returns source-level uncovered statement ranges.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, runtime.coverageGaps)
}

func (r *Runtime) coverageGaps(ctx context.Context, request *mcp.CallToolRequest, input CoverageGapsInput) (*mcp.CallToolResult, CoverageGapsOutput, error) {
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	started := time.Now()
	if input.Package == "" {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorInvalidInput)
		return nil, CoverageGapsOutput{}, fmt.Errorf("validating input: package is required")
	}

	progress.Report(ctx, request, 0, 3, "validating package")
	selection, err := r.resolvePackages(ctx, input.Package)
	if err != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, classifyTraceError(err))
		return nil, CoverageGapsOutput{}, fmt.Errorf("resolving package: %w", err)
	}

	runDir, err := createRunTempDir("coverage")
	if err != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorInternal)
		return nil, CoverageGapsOutput{}, fmt.Errorf("creating coverage run: %w", err)
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	profilePath := filepath.Join(runDir, "coverage.out")

	progress.Report(ctx, request, 1, 3, "running coverage tests")
	result, err := r.runner.Run(ctx, execution.Command{
		Name: "go",
		Args: []string{
			"test",
			"-coverprofile=" + profilePath,
			"-covermode=atomic",
			selection.Pattern,
		},
		Env: map[string]string{"GOWORK": "auto"},
	}, execution.Streams{})
	if err != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, classifyTraceError(err))
		return nil, CoverageGapsOutput{}, fmt.Errorf("running coverage tests: %w", err)
	}
	if result.ExitCode != 0 {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorSubprocess)
		return nil, CoverageGapsOutput{}, fmt.Errorf("coverage tests exited %d", result.ExitCode)
	}

	progress.Report(ctx, request, 2, 3, "parsing coverage profile")
	info, err := os.Stat(profilePath)
	if err != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorSubprocess)
		return nil, CoverageGapsOutput{}, fmt.Errorf("inspecting coverage profile: %w", err)
	}
	if info.Size() > maxCoverageProfileSize {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorSubprocess)
		return nil, CoverageGapsOutput{}, fmt.Errorf("coverage profile exceeds %d bytes", maxCoverageProfileSize)
	}
	profile, err := os.Open(profilePath)
	if err != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorInternal)
		return nil, CoverageGapsOutput{}, fmt.Errorf("opening coverage profile: %w", err)
	}
	report, parseErr := parser.ParseCoverage(profile)
	closeErr := profile.Close()
	if parseErr != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorSubprocess)
		return nil, CoverageGapsOutput{}, fmt.Errorf("parsing coverage profile: %w", parseErr)
	}
	if closeErr != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorInternal)
		return nil, CoverageGapsOutput{}, fmt.Errorf("closing coverage profile after read: %w", closeErr)
	}

	output, err := r.coverageOutput(selection, report)
	if err != nil {
		r.recordCoverageTrace(input, started, CoverageGapsOutput{}, trace.ErrorSubprocess)
		return nil, CoverageGapsOutput{}, fmt.Errorf("normalizing coverage paths: %w", err)
	}
	progress.Report(ctx, request, 3, 3, "coverage analysis completed")
	r.recordCoverageTrace(input, started, output, trace.ErrorNone)
	return nil, output, nil
}

func (r *Runtime) coverageOutput(selection packageSelection, report parser.CoverageReport) (CoverageGapsOutput, error) {
	output := CoverageGapsOutput{Files: make([]FileCoverage, 0, len(report.Files)), OverallPercent: report.OverallPercent}
	for _, parsedFile := range report.Files {
		file, err := r.coverageFile(selection, parsedFile.File)
		if err != nil {
			return CoverageGapsOutput{}, err
		}
		converted := FileCoverage{File: file, Percent: parsedFile.Percent, Gaps: make([]CoverageGap, 0, len(parsedFile.Gaps))}
		for _, gap := range parsedFile.Gaps {
			if gap.Statements > uint64(maxInt()) {
				return CoverageGapsOutput{}, fmt.Errorf("statement count exceeds platform integer range")
			}
			converted.Gaps = append(converted.Gaps, CoverageGap{
				File:       file,
				StartLine:  gap.StartLine,
				StartCol:   gap.StartCol,
				EndLine:    gap.EndLine,
				EndCol:     gap.EndCol,
				Statements: int(gap.Statements),
			})
		}
		output.Files = append(output.Files, converted)
	}
	sort.Slice(output.Files, func(i, j int) bool { return output.Files[i].File < output.Files[j].File })
	return output, nil
}

func (r *Runtime) coverageFile(selection packageSelection, file string) (string, error) {
	if filepath.IsAbs(file) {
		return r.workspace.Relative(file)
	}
	if resolved, err := r.workspace.Relative(file); err == nil {
		return resolved, nil
	}
	for _, pkg := range selection.Packages {
		prefix := pkg.ImportPath + "/"
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		candidate := filepath.Join(pkg.Dir, filepath.FromSlash(strings.TrimPrefix(file, prefix)))
		if relative, err := r.workspace.Relative(candidate); err == nil {
			return relative, nil
		}
	}
	return "", fmt.Errorf("coverage file could not be mapped into the workspace")
}

func (r *Runtime) recordCoverageTrace(input CoverageGapsInput, started time.Time, output CoverageGapsOutput, kind trace.ErrorKind) {
	summary := ""
	if kind == trace.ErrorNone {
		gaps := 0
		for _, file := range output.Files {
			gaps += len(file.Gaps)
		}
		summary = fmt.Sprintf("%d files, %d gaps", len(output.Files), gaps)
	}
	_ = r.tracer.Record(trace.Event{
		Tool:          "go_coverage_gaps",
		Args:          input,
		Duration:      time.Since(started),
		ResultSummary: summary,
		ErrorKind:     kind,
	})
}

func maxInt() int { return int(^uint(0) >> 1) }
