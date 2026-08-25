package verification_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

func TestNewReportEmitsPortableEmptyCollections(t *testing.T) {
	report := verification.NewReport("0.2.0", verification.Repository{
		RequestedBase:   "origin/main",
		BaseCommit:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MergeBaseCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		HeadCommit:      "cccccccccccccccccccccccccccccccccccccccc",
	})

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got := value["schema_version"]; got != verification.SchemaVersion {
		t.Fatalf("schema_version = %v, want %q", got, verification.SchemaVersion)
	}
	for _, field := range []string{"plan", "evidence", "findings", "risks", "uncertainties"} {
		collection, ok := value[field].([]any)
		if !ok {
			t.Fatalf("%s = %#v, want JSON array", field, value[field])
		}
		if len(collection) != 0 {
			t.Fatalf("%s = %#v, want empty array", field, collection)
		}
	}
	change := value["change"].(map[string]any)
	impact := value["impact"].(map[string]any)
	for field, collection := range map[string]any{
		"change.files":        change["files"],
		"change.declarations": change["declarations"],
		"impact.packages":     impact["packages"],
	} {
		if got, ok := collection.([]any); !ok || len(got) != 0 {
			t.Fatalf("%s = %#v, want empty JSON array", field, collection)
		}
	}
}

func TestFinalizeAppliesPolicyPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		policy   verification.Policy
		mutate   func(*verification.Report)
		want     verification.ResultStatus
		wantExit int
	}{
		{
			name: "clean required evidence passes",
			mutate: func(report *verification.Report) {
				report.Plan = append(report.Plan, requiredCheck("tests"))
				report.Evidence = append(report.Evidence, verification.Evidence{
					CheckID: "tests", Kind: verification.CheckTests, Status: verification.EvidencePassed,
				})
			},
			want: verification.ResultPass,
		},
		{
			name: "error finding blocks",
			mutate: func(report *verification.Report) {
				report.Findings = append(report.Findings, verification.Finding{
					Kind: "go.analysis", Severity: verification.SeverityError,
					Baseline: verification.BaselineIntroduced,
				})
			},
			want: verification.ResultFindings, wantExit: 1,
		},
		{
			name: "warning is advisory by default",
			mutate: func(report *verification.Report) {
				report.Findings = append(report.Findings, verification.Finding{
					Kind: "go.analysis", Severity: verification.SeverityWarning,
					Baseline: verification.BaselineIntroduced,
				})
			},
			want: verification.ResultPass,
		},
		{
			name:   "warning threshold blocks",
			policy: verification.Policy{FailOn: verification.FailOnWarning},
			mutate: func(report *verification.Report) {
				report.Findings = append(report.Findings, verification.Finding{
					Kind: "go.analysis", Severity: verification.SeverityWarning,
					Baseline: verification.BaselineIntroduced,
				})
			},
			want: verification.ResultFindings, wantExit: 1,
		},
		{
			name:   "failed test blocks when analyzers are advisory",
			policy: verification.Policy{FailOn: verification.FailOnNone},
			mutate: func(report *verification.Report) {
				report.Findings = append(report.Findings, verification.Finding{
					Kind: "test.failure", Severity: verification.SeverityError,
				})
			},
			want: verification.ResultFindings, wantExit: 1,
		},
		{
			name: "missing required evidence is incomplete",
			mutate: func(report *verification.Report) {
				report.Plan = append(report.Plan, requiredCheck("tests"))
			},
			want: verification.ResultIncomplete, wantExit: 2,
		},
		{
			name: "required check error outranks findings",
			mutate: func(report *verification.Report) {
				report.Plan = append(report.Plan, requiredCheck("tests"))
				report.Evidence = append(report.Evidence, verification.Evidence{
					CheckID: "tests", Kind: verification.CheckTests, Status: verification.EvidenceError,
				})
				report.Findings = append(report.Findings, verification.Finding{
					Kind: "test.failure", Severity: verification.SeverityError,
				})
			},
			want: verification.ResultIncomplete, wantExit: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := verification.NewReport("0.2.0", verification.Repository{})
			test.mutate(&report)
			if err := report.Finalize(test.policy); err != nil {
				t.Fatalf("finalize report: %v", err)
			}
			if report.Result.Status != test.want || report.Result.ExitCode != test.wantExit {
				t.Fatalf("result = %s/%d, want %s/%d", report.Result.Status, report.Result.ExitCode, test.want, test.wantExit)
			}
		})
	}
}

func TestFinalizeOrdersPortableCollections(t *testing.T) {
	report := verification.NewReport("0.2.0", verification.Repository{})
	report.Impact.Packages = []verification.ImpactedPackage{
		{ID: "example/z", Kind: "go.package", Distance: 2, Reasons: []string{"reverse_import"}},
		{ID: "example/a", Kind: "go.package", Distance: 0, Reasons: []string{"changed_source"}},
	}
	report.Plan = []verification.Check{{ID: "z"}, {ID: "a"}}
	report.Risks = []verification.RiskArea{{Code: "z"}, {Code: "a"}}
	report.Uncertainties = []verification.Uncertainty{{Code: "z"}, {Code: "a"}}

	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatalf("finalize report: %v", err)
	}

	if got := []string{report.Impact.Packages[0].ID, report.Impact.Packages[1].ID}; !reflect.DeepEqual(got, []string{"example/a", "example/z"}) {
		t.Fatalf("package order = %v", got)
	}
	if got := []string{report.Plan[0].ID, report.Plan[1].ID}; !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("plan order = %v", got)
	}
	if got := []string{report.Risks[0].Code, report.Risks[1].Code}; !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("risk order = %v", got)
	}
	if got := []string{report.Uncertainties[0].Code, report.Uncertainties[1].Code}; !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("uncertainty order = %v", got)
	}
}

func TestFinalizeInitializesNestedEvidenceCollections(t *testing.T) {
	report := verification.NewReport("0.2.0", verification.Repository{})
	report.Evidence = append(report.Evidence, verification.Evidence{
		CheckID:  "tests",
		Kind:     verification.CheckTests,
		Status:   verification.EvidencePassed,
		Tests:    &verification.TestSummary{},
		Coverage: &verification.CoverageSummary{},
	})

	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatalf("finalize report: %v", err)
	}
	if report.Evidence[0].Tests.Packages == nil || report.Evidence[0].Tests.Nonpassing == nil {
		t.Fatalf("test evidence contains nil collections: %#v", report.Evidence[0].Tests)
	}
	if report.Evidence[0].Coverage.Uncovered == nil {
		t.Fatalf("coverage evidence contains nil uncovered collection")
	}
}

func TestPublishedSchemaMatchesReportVersion(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "schema", "verification-report-v1alpha1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read published schema: %v", err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		ID         string         `json:"$id"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode published schema: %v", err)
	}
	if schema.ID != verification.SchemaVersion {
		t.Fatalf("schema id = %q, want %q", schema.ID, verification.SchemaVersion)
	}
	want := []string{"schema_version", "provider", "repository", "change", "impact", "plan", "evidence", "findings", "risks", "uncertainties", "result"}
	if !reflect.DeepEqual(schema.Required, want) {
		t.Fatalf("required fields = %v, want %v", schema.Required, want)
	}
	for _, field := range want {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("schema has no %q property", field)
		}
	}
}

func requiredCheck(id string) verification.Check {
	return verification.Check{ID: id, Kind: verification.CheckTests, Required: true}
}
