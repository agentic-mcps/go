package verification_test

import (
	"encoding/json"
	"fmt"
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
	report.Risks = append(report.Risks, verification.RiskArea{Code: "empty"})
	report.Uncertainties = append(report.Uncertainties, verification.Uncertainty{Code: "empty"})

	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatalf("finalize report: %v", err)
	}
	if report.Evidence[0].Tests.Packages == nil || report.Evidence[0].Tests.Nonpassing == nil {
		t.Fatalf("test evidence contains nil collections: %#v", report.Evidence[0].Tests)
	}
	if report.Evidence[0].Coverage.Uncovered == nil {
		t.Fatalf("coverage evidence contains nil uncovered collection")
	}
	if report.Risks[0].Locations == nil || report.Uncertainties[0].Locations == nil {
		t.Fatalf("localized facts contain nil collections: risks=%#v uncertainties=%#v", report.Risks, report.Uncertainties)
	}
}

func TestFinalizeBoundsPortableDetailsAfterPolicy(t *testing.T) {
	report := verification.NewReport("0.2.0", verification.Repository{})
	ranges := make([]verification.LineRange, 30)
	for index := range ranges {
		ranges[index] = verification.LineRange{Start: index + 1, End: index + 1}
	}
	locations := make([]verification.Location, 250)
	impacted := make([]verification.ImpactedPackage, 250)
	targets := make([]string, 250)
	packages := make([]verification.TestPackageSummary, 250)
	nonpassing := make([]verification.TestCaseSummary, 250)
	uncovered := make([]verification.SourceRange, 250)
	for index := 0; index < 250; index++ {
		path := fmt.Sprintf("pkg/file-%03d.go", index)
		location := verification.Location{File: path, Line: index + 1}
		locations[index] = location
		targets[index] = fmt.Sprintf("example.test/pkg%03d", index)
		impacted[index] = verification.ImpactedPackage{Kind: "go.package", ID: targets[index], Distance: index}
		packages[index] = verification.TestPackageSummary{Package: targets[index], Status: "FAIL", Failed: 1}
		nonpassing[index] = verification.TestCaseSummary{Package: targets[index], Name: "TestFailure", Status: "fail"}
		uncovered[index] = verification.SourceRange{File: path, StartLine: 1, EndLine: 1, Statements: 1}
		report.Change.Files = append(report.Change.Files, verification.ChangedFile{
			Path: path, Change: verification.ChangeModified,
			BaseRanges: append([]verification.LineRange(nil), ranges...), CurrentRanges: append([]verification.LineRange(nil), ranges...),
		})
		report.Change.Declarations = append(report.Change.Declarations, verification.ChangedDeclaration{
			Kind: "function", Package: targets[index], Name: "Changed", Change: verification.ChangeModified, CurrentLocation: &location,
		})
		report.Findings = append(report.Findings, verification.Finding{
			Kind: "go.analysis", Severity: verification.SeverityError, Baseline: verification.BaselineIntroduced, Location: &location,
		})
		report.Uncertainties = append(report.Uncertainties, verification.Uncertainty{
			Code: "same_limit", Message: "same known limit", Locations: []verification.Location{location},
		})
	}
	report.Impact.Packages = impacted
	report.Plan = []verification.Check{{ID: "tests", Kind: verification.CheckTests, Required: true, Targets: targets}}
	report.Evidence = []verification.Evidence{{
		CheckID: "tests", Kind: verification.CheckTests, Status: verification.EvidencePassed,
		Tests:    &verification.TestSummary{Packages: packages, Nonpassing: nonpassing},
		Coverage: &verification.CoverageSummary{Uncovered: uncovered},
	}}
	report.Risks = []verification.RiskArea{{Code: "risk", Locations: locations}}

	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatalf("finalize report: %v", err)
	}
	if report.Result.Status != verification.ResultFindings || report.Result.BlockingFindings != 250 {
		t.Fatalf("policy result = %#v, want every pre-truncation finding to block", report.Result)
	}
	if len(report.Change.Files) != 15 || report.Change.FilesTotal != 250 || !report.Change.FilesTruncated {
		t.Fatalf("changed files = %d/%d truncated=%v", len(report.Change.Files), report.Change.FilesTotal, report.Change.FilesTruncated)
	}
	if len(report.Change.Files[0].BaseRanges) != 5 || report.Change.Files[0].BaseRangesTotal != 30 || !report.Change.Files[0].BaseRangesTruncated || len(report.Change.Files[0].CurrentRanges) != 5 || report.Change.Files[0].CurrentRangesTotal != 30 || !report.Change.Files[0].CurrentRangesTruncated {
		t.Fatalf("changed ranges were not bounded with totals: %#v", report.Change.Files[0])
	}
	if len(report.Change.Declarations) != 20 || report.Change.DeclarationsTotal != 250 || !report.Change.DeclarationsTruncated {
		t.Fatalf("declarations = %d/%d truncated=%v", len(report.Change.Declarations), report.Change.DeclarationsTotal, report.Change.DeclarationsTruncated)
	}
	if len(report.Impact.Packages) != 20 || report.Impact.PackagesTotal != 250 || !report.Impact.PackagesTruncated {
		t.Fatalf("impacted packages = %#v", report.Impact)
	}
	if len(report.Plan[0].Targets) != 20 || report.Plan[0].TargetsTotal != 250 || !report.Plan[0].TargetsTruncated {
		t.Fatalf("plan targets = %#v", report.Plan[0])
	}
	tests := report.Evidence[0].Tests
	if len(tests.Packages) != 20 || tests.PackagesTotal != 250 || !tests.PackagesTruncated || len(tests.Nonpassing) != 20 || tests.NonpassingTotal != 250 || !tests.NonpassingTruncated {
		t.Fatalf("test details were not bounded with totals: %#v", tests)
	}
	coverage := report.Evidence[0].Coverage
	if len(coverage.Uncovered) != 20 || coverage.UncoveredTotal != 250 || !coverage.UncoveredTruncated {
		t.Fatalf("coverage details were not bounded with totals: %#v", coverage)
	}
	if len(report.Findings) != 50 || report.FindingsTotal != 250 || !report.FindingsTruncated {
		t.Fatalf("findings = %d/%d truncated=%v", len(report.Findings), report.FindingsTotal, report.FindingsTruncated)
	}
	if len(report.Risks) != 1 || len(report.Risks[0].Locations) != 5 || report.Risks[0].LocationsTotal != 250 || !report.Risks[0].LocationsTruncated {
		t.Fatalf("risk details = %#v", report.Risks)
	}
	if len(report.Uncertainties) != 1 || len(report.Uncertainties[0].Locations) != 5 || report.Uncertainties[0].LocationsTotal != 250 || !report.Uncertainties[0].LocationsTruncated {
		t.Fatalf("uncertainty details = %#v", report.Uncertainties)
	}
}

func TestPublishedSchemaMatchesReportVersion(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "schema", "verification-report-v1beta1.json")
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
	want := []string{"schema_version", "id", "provider", "providers", "snapshot", "provenance", "repository", "change", "impact", "plan", "evidence", "findings", "findings_total", "findings_truncated", "risks", "uncertainties", "result"}
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
