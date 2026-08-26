package tools

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/ashwingopalsamy/agentic-go/internal/changeimpact"
	"github.com/ashwingopalsamy/agentic-go/internal/trace"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// VerifyChangeInput configures one complete change-verification report.
//
//nolint:govet // field order is the public JSON schema order.
type VerifyChangeInput struct {
	Base               string              `json:"base" jsonschema:"required local commit or ref compared with HEAD and the final worktree"`
	Package            string              `json:"package,omitempty" jsonschema:"Go package scope; default ./..."`
	Race               bool                `json:"race,omitempty" jsonschema:"include race detection; default false"`
	FailOn             verification.FailOn `json:"fail_on,omitempty" jsonschema:"blocking analyzer severity: error, warning, info, or none; default error"`
	MinChangedCoverage *float64            `json:"min_changed_coverage,omitempty" jsonschema:"optional changed-statement coverage minimum from 0 through 100"`
	MaxPackages        int                 `json:"max_packages,omitempty" jsonschema:"maximum affected package closure; default 200, maximum 500"`
	ContractID         string              `json:"contract_id,omitempty" jsonschema:"optional private Change Contract evaluated against the exact verification snapshot"`
	ExpectedSnapshotID string              `json:"expected_snapshot_id,omitempty" jsonschema:"optional exact semantic snapshot required for this verification"`
}

// RegisterVerifyChange registers the approval-gated verification adapter.
func RegisterVerifyChange(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_verify_change",
		Description: "Runs the same trusted repository tests a developer would run locally with the MCP process privileges and workspace, deadline, concurrency, and output containment, then reports impact, findings, risk guidance, and uncertainty.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, runtime.verifyChange)
}

func (r *Runtime) verifyChange(ctx context.Context, _ *mcp.CallToolRequest, input VerifyChangeInput) (*mcp.CallToolResult, verification.Report, error) {
	started := time.Now()
	if err := normalizeVerifyChangeInput(&input); err != nil {
		r.recordVerifyTrace(input, started, verification.Report{}, trace.ErrorInvalidInput)
		return nil, verification.Report{}, fmt.Errorf("validating input: %w", err)
	}
	request := verification.Request{
		Base:               input.Base,
		Package:            input.Package,
		Race:               input.Race,
		FailOn:             input.FailOn,
		MinChangedCoverage: input.MinChangedCoverage,
		MaxPackages:        input.MaxPackages,
		ContractID:         input.ContractID,
		ExpectedSnapshotID: input.ExpectedSnapshotID,
	}
	var report verification.Report
	var err error
	if r.intelligence != nil {
		report, err = r.intelligence.Verify(ctx, request)
	} else {
		engine, setupErr := r.newVerificationEngine()
		if setupErr != nil {
			r.recordVerifyTrace(input, started, verification.Report{}, trace.ErrorInternal)
			return nil, verification.Report{}, fmt.Errorf("setting up verification: %w", setupErr)
		}
		report, err = engine.Verify(ctx, request)
	}
	if err != nil {
		r.recordVerifyTrace(input, started, verification.Report{}, classifyTraceError(err))
		return nil, verification.Report{}, fmt.Errorf("verifying change: %w", err)
	}
	r.recordVerifyTrace(input, started, report, trace.ErrorNone)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: verifyChangeTextSummary(report)}}}, report, nil
}

func verifyChangeTextSummary(report verification.Report) string {
	return fmt.Sprintf(
		"agentic-go verification %s (exit %d): %d changed files, %d changed declarations, %d affected packages, %d findings; the canonical %s report is in structuredContent",
		report.Result.Status,
		report.Result.ExitCode,
		report.Change.FilesTotal,
		report.Change.DeclarationsTotal,
		report.Impact.PackagesTotal,
		report.FindingsTotal,
		report.SchemaVersion,
	)
}

func normalizeVerifyChangeInput(input *VerifyChangeInput) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}
	if input.Base == "" {
		return fmt.Errorf("base is required")
	}
	if invalidSingleArgument(input.Base) {
		return fmt.Errorf("base is invalid: use one local commit or ref without whitespace or a leading dash")
	}
	if input.Package == "" {
		input.Package = "./..."
	}
	if invalidSingleArgument(input.Package) {
		return fmt.Errorf("package is invalid: use one Go package pattern without whitespace or a leading dash")
	}
	if input.FailOn == "" {
		input.FailOn = verification.FailOnError
	}
	switch input.FailOn {
	case verification.FailOnError, verification.FailOnWarning, verification.FailOnInfo, verification.FailOnNone:
	default:
		return fmt.Errorf("fail_on must be error, warning, info, or none")
	}
	if input.MinChangedCoverage != nil && (math.IsNaN(*input.MinChangedCoverage) || math.IsInf(*input.MinChangedCoverage, 0) || *input.MinChangedCoverage < 0 || *input.MinChangedCoverage > 100) {
		return fmt.Errorf("min_changed_coverage must be a finite number from 0 through 100")
	}
	if input.MaxPackages < 0 || input.MaxPackages > 500 {
		return fmt.Errorf("max_packages must be between 1 and 500 when set")
	}
	if input.MaxPackages == 0 {
		input.MaxPackages = 200
	}
	if input.ContractID != "" && invalidSingleArgument(input.ContractID) {
		return fmt.Errorf("contract_id is invalid: use one private contract ID without whitespace")
	}
	if input.ExpectedSnapshotID != "" && invalidSingleArgument(input.ExpectedSnapshotID) {
		return fmt.Errorf("expected_snapshot_id is invalid: use one snapshot ID without whitespace")
	}
	return nil
}

func invalidSingleArgument(value string) bool {
	return strings.HasPrefix(value, "-") || strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0
}

func (r *Runtime) recordVerifyTrace(input VerifyChangeInput, started time.Time, report verification.Report, kind trace.ErrorKind) {
	summary := ""
	counts := make(map[string]int)
	if kind == trace.ErrorNone {
		packageTotal := report.Impact.PackagesTotal
		if packageTotal < len(report.Impact.Packages) {
			packageTotal = len(report.Impact.Packages)
		}
		findingTotal := report.FindingsTotal
		if findingTotal < len(report.Findings) {
			findingTotal = len(report.Findings)
		}
		summary = fmt.Sprintf("%s: %d affected packages, %d findings", report.Result.Status, packageTotal, findingTotal)
		for _, finding := range report.Findings {
			counts[string(finding.Severity)]++
		}
	}
	_ = r.tracer.Record(trace.Event{
		Tool: "go_verify_change", Args: input, Duration: time.Since(started),
		FindingsBySeverity: counts, ResultSummary: summary, ErrorKind: kind,
	})
}

func (r *Runtime) newVerificationEngine() (*verification.Engine, error) {
	analyzer, err := changeimpact.New(r.workspace, r.runner)
	if err != nil {
		return nil, err
	}
	return verification.NewEngine(r.workspace, r.runner, analyzer, r.providerVersion)
}
