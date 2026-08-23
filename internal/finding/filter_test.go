package finding

import "testing"

func TestValidateSeverity(t *testing.T) {
	for _, value := range []Severity{SeverityError, SeverityWarning, SeverityInfo} {
		if err := ValidateSeverity(value); err != nil {
			t.Fatalf("ValidateSeverity(%q) = %v", value, err)
		}
	}
	if err := ValidateSeverity("critical"); err == nil {
		t.Fatal("ValidateSeverity(critical) succeeded")
	}
}

func TestFilterPreservesPreTruncationAggregates(t *testing.T) {
	input := AuditResult{
		Findings: []Finding{
			{Rule: "test-01", Severity: SeverityError},
			{Rule: "test-02", Severity: SeverityWarning},
			{Rule: "test-03", Severity: SeverityWarning},
			{Rule: "test-04", Severity: SeverityInfo},
		},
		FilesScanned: 4,
		DurationMS:   12,
	}

	got := Filter(input, SeverityWarning, 2)
	if len(got.Findings) != 2 || got.Total != 3 || !got.Truncated {
		t.Fatalf("Filter() = %+v", got)
	}
	if got.CountsBySeverity[SeverityError] != 1 || got.CountsBySeverity[SeverityWarning] != 2 || got.CountsBySeverity[SeverityInfo] != 0 {
		t.Fatalf("counts = %#v", got.CountsBySeverity)
	}
	if got.FilesScanned != 4 || got.DurationMS != 12 {
		t.Fatalf("metadata = files %d, duration %d", got.FilesScanned, got.DurationMS)
	}
}

func TestFilterCleanResultUsesNonNilCollections(t *testing.T) {
	got := Filter(AuditResult{}, SeverityInfo, 200)
	if got.Findings == nil || got.CountsBySeverity == nil {
		t.Fatalf("Filter() returned nil collections: %+v", got)
	}
}
