package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/ashwingopalsamy/agentic-go/internal/trace"
)

type auditOptions struct {
	Package     string
	MinSeverity finding.Severity
	MaxFindings int
}

func normalizeAuditInput(input *auditOptions) error {
	if input.Package == "" {
		return fmt.Errorf("package is required")
	}
	if input.MinSeverity == "" {
		input.MinSeverity = finding.SeverityInfo
	}
	if err := finding.ValidateSeverity(input.MinSeverity); err != nil {
		return err
	}
	if input.MaxFindings < 0 {
		return fmt.Errorf("max_findings must not be negative")
	}
	if input.MaxFindings == 0 {
		input.MaxFindings = 200
	}
	if input.MaxFindings > 1000 {
		input.MaxFindings = 1000
	}
	return nil
}

func (r *Runtime) recordAuditTrace(tool string, input any, started time.Time, result finding.AuditResult, kind trace.ErrorKind) {
	counts := make(map[string]int, len(result.CountsBySeverity))
	for severity, count := range result.CountsBySeverity {
		counts[string(severity)] = count
	}
	summary := ""
	if kind == trace.ErrorNone {
		summary = fmt.Sprintf("%d findings across %d files", result.Total, result.FilesScanned)
	}
	_ = r.tracer.Record(trace.Event{
		Tool:               tool,
		Args:               input,
		Duration:           time.Since(started),
		Analysis:           time.Duration(result.DurationMS) * time.Millisecond,
		FindingsBySeverity: counts,
		ResultSummary:      summary,
		ErrorKind:          kind,
	})
}

func classifyAuditError(err error) trace.ErrorKind {
	switch {
	case errors.Is(err, context.Canceled):
		return trace.ErrorCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return trace.ErrorDeadline
	default:
		return trace.ErrorAnalysis
	}
}
