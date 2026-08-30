package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/agentic-mcps/go/internal/analysis/concurrency"
	"github.com/agentic-mcps/go/internal/audit"
	"github.com/agentic-mcps/go/internal/finding"
	"github.com/agentic-mcps/go/internal/progress"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/tools/go/analysis"
)

// AuditConcurrencyInput configures the concurrency audit.
type AuditConcurrencyInput struct {
	Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
	MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default info"`
	MaxFindings int              `json:"max_findings,omitempty" jsonschema:"maximum findings to return; default 200, maximum 1000"`
}

// AuditConcurrencyOutput is the structured concurrency audit result.
type AuditConcurrencyOutput struct {
	Result finding.AuditResult `json:"result"`
}

// RegisterAuditConcurrency registers the concurrency audit tool.
func RegisterAuditConcurrency(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_audit_concurrency", Description: "Audits workspace Go code for precise concurrency hazards.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}}, runtime.auditConcurrency)
}

func (r *Runtime) auditConcurrency(ctx context.Context, request *mcp.CallToolRequest, input AuditConcurrencyInput) (*mcp.CallToolResult, AuditConcurrencyOutput, error) {
	started := time.Now()
	options := auditOptions(input)
	if err := normalizeAuditInput(&options); err != nil {
		r.recordAuditTrace("go_audit_concurrency", input, started, finding.AuditResult{}, trace.ErrorInvalidInput)
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("validating input: %w", err)
	}
	input.MinSeverity, input.MaxFindings = options.MinSeverity, options.MaxFindings
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	progress.Report(ctx, request, 0, 2, "validating package")
	selection, err := r.resolvePackages(ctx, input.Package)
	if err != nil {
		r.recordAuditTrace("go_audit_concurrency", input, started, finding.AuditResult{}, classifyTraceError(err))
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("resolving package: %w", err)
	}
	progress.Report(ctx, request, 1, 2, "running concurrency audit")
	release, err := r.runner.Permit(ctx)
	if err != nil {
		r.recordAuditTrace("go_audit_concurrency", input, started, finding.AuditResult{}, classifyAuditError(err))
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("waiting for analysis capacity: %w", err)
	}
	defer release()
	result, err := audit.Run(ctx, r.workspace.Root(), selection.Pattern, []*analysis.Analyzer{concurrency.Analyzer})
	if err != nil {
		r.recordAuditTrace("go_audit_concurrency", input, started, finding.AuditResult{}, classifyAuditError(err))
		return nil, AuditConcurrencyOutput{}, fmt.Errorf("running concurrency audit: %w", err)
	}
	result = finding.Filter(result, input.MinSeverity, input.MaxFindings)
	progress.Report(ctx, request, 2, 2, "concurrency audit completed")
	r.recordAuditTrace("go_audit_concurrency", input, started, result, trace.ErrorNone)
	return nil, AuditConcurrencyOutput{Result: result}, nil
}
