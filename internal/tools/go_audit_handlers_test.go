package tools

import (
	"context"
	"testing"
)

func TestAuditHandlersReturnStructuredResults(t *testing.T) {
	runtime := newTestRuntime(t)
	_, concurrencyOutput, err := runtime.auditConcurrency(context.Background(), nil, AuditConcurrencyInput{Package: ".", MaxFindings: 20})
	if err != nil {
		t.Fatalf("auditConcurrency() error = %v", err)
	}
	if concurrencyOutput.Result.Findings == nil || concurrencyOutput.Result.CountsBySeverity == nil {
		t.Fatalf("concurrency result = %+v, want initialized structured result", concurrencyOutput.Result)
	}

	_, errorsOutput, err := runtime.auditErrors(context.Background(), nil, AuditErrorsInput{Package: ".", MaxFindings: 20})
	if err != nil {
		t.Fatalf("auditErrors() error = %v", err)
	}
	if errorsOutput.Result.Findings == nil || errorsOutput.Result.CountsBySeverity == nil {
		t.Fatalf("errors result = %+v, want initialized structured result", errorsOutput.Result)
	}
}
