package trace

import (
	"fmt"
	"testing"
	"time"
)

func TestSummaryDisabled(t *testing.T) {
	got, err := (&Tracer{}).Summary()
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Tools == nil || len(got.Tools) != 0 {
		t.Fatalf("Summary() = %+v", got)
	}
}

func TestSummaryBoundsRecentRecords(t *testing.T) {
	tracer, err := NewWithBaseDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tracer.Close() })
	for i := 0; i < maxSummaryRecords+1; i++ {
		if recordErr := tracer.Record(Event{Tool: fmt.Sprintf("tool-%d", i%2), Duration: time.Millisecond}); recordErr != nil {
			t.Fatal(recordErr)
		}
	}
	got, err := tracer.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if got.RecordsConsidered != maxSummaryRecords || !got.Truncated {
		t.Fatalf("Summary() = %+v", got)
	}
	if len(got.Tools) != 2 || got.Tools[0].Calls+got.Tools[1].Calls != maxSummaryRecords {
		t.Fatalf("tools = %+v", got.Tools)
	}
}

func TestSummaryAggregatesCurrentRun(t *testing.T) {
	tracer, err := NewWithBaseDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tracer.Close() })
	for _, event := range []Event{
		{Tool: "go_test_structured", Duration: 10 * time.Millisecond},
		{Tool: "go_test_structured", Duration: 20 * time.Millisecond, ErrorKind: ErrorSubprocess},
		{Tool: "go_test_structured", Duration: 30 * time.Millisecond},
		{Tool: "go_audit_errors", Duration: 5 * time.Millisecond},
	} {
		if recordErr := tracer.Record(event); recordErr != nil {
			t.Fatal(recordErr)
		}
	}
	got, err := tracer.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.RecordsConsidered != 4 || got.Truncated || len(got.Tools) != 2 {
		t.Fatalf("Summary() = %+v", got)
	}
	if got.Tools[0].Tool != "go_audit_errors" || got.Tools[1].Tool != "go_test_structured" {
		t.Fatalf("tools = %+v", got.Tools)
	}
	tests := got.Tools[1]
	if tests.Calls != 3 || tests.ErrorCount != 1 || tests.P50DurationMS != 20 || tests.P99DurationMS != 30 {
		t.Fatalf("test summary = %+v", tests)
	}
}
