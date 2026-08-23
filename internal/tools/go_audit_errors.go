package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/errors"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/ashwingopalsamy/agentic-go/internal/progress"
	"github.com/ashwingopalsamy/agentic-go/internal/trace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/tools/go/analysis"
)

type AuditErrorsInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default info"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"maximum findings to return; default 200, maximum 1000"`
}

type AuditErrorsOutput struct {
	Result finding.AuditResult `json:"result"`
}

func RegisterAuditErrors(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_audit_errors", Description: "Audits workspace Go code for precise error-handling hazards.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}}, runtime.auditErrors)
}

func (r *Runtime) auditErrors(ctx context.Context, request *mcp.CallToolRequest, input AuditErrorsInput) (*mcp.CallToolResult, AuditErrorsOutput, error) {
	started := time.Now()
	options := auditOptions{Package: input.Package, MinSeverity: input.MinSeverity, MaxFindings: input.MaxFindings}
	if err := normalizeAuditInput(&options); err != nil {
		r.recordAuditTrace("go_audit_errors", input, started, finding.AuditResult{}, trace.ErrorInvalidInput)
		return nil, AuditErrorsOutput{}, fmt.Errorf("validating input: %w", err)
	}
	input.MinSeverity, input.MaxFindings = options.MinSeverity, options.MaxFindings
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	progress.Report(ctx, request, 0, 2, "validating package")
	selection, err := r.resolvePackages(ctx, input.Package)
	if err != nil {
		r.recordAuditTrace("go_audit_errors", input, started, finding.AuditResult{}, classifyTraceError(err))
		return nil, AuditErrorsOutput{}, fmt.Errorf("resolving package: %w", err)
	}
	progress.Report(ctx, request, 1, 2, "running error audit")
	result, err := audit.Run(ctx, r.workspace.Root(), selection.Pattern, []*analysis.Analyzer{errors.Analyzer})
	if err != nil {
		r.recordAuditTrace("go_audit_errors", input, started, finding.AuditResult{}, classifyAuditError(err))
		return nil, AuditErrorsOutput{}, fmt.Errorf("running errors audit: %w", err)
	}
	result = finding.Filter(result, input.MinSeverity, input.MaxFindings)
	progress.Report(ctx, request, 2, 2, "error audit completed")
	r.recordAuditTrace("go_audit_errors", input, started, result, trace.ErrorNone)
	return nil, AuditErrorsOutput{Result: result}, nil
}
