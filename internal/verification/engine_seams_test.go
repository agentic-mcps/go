package verification_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestEngineShortAndSkipReachGoTest(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod":       failureFixtureModule,
		"calc/calc.go": failureFixtureCalc,
		"calc/calc_test.go": `package calc

import "testing"

func TestSign(t *testing.T) {
	if Sign(-1) != -1 || Sign(1) != 1 {
		t.Fatal("unexpected sign")
	}
}

func TestBroken(t *testing.T) { t.Fatal("pre-existing failure") }

func TestLong(t *testing.T) {
	if testing.Short() {
		t.Skip("long test")
	}
	t.Fatal("long test ran")
}
`,
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "calc/calc.go", failureFixtureChangedCalc)
	tests := []struct {
		name                         string
		request                      verification.Request
		passed, failed, skippedTests int
	}{
		{name: "default runs every test", request: verification.Request{Base: base}, passed: 1, failed: 2},
		{name: "short skips long tests", request: verification.Request{Base: base, Short: true}, passed: 1, failed: 1, skippedTests: 1},
		{name: "skip pattern omits named tests", request: verification.Request{Base: base, Short: true, Skip: "TestBroken"}, passed: 1, skippedTests: 1},
		{name: "test cache still runs tests", request: verification.Request{Base: base, TestCache: true, Short: true, Skip: "TestBroken"}, passed: 1, skippedTests: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := verifyRepository(t, repository, test.request)
			tests := evidenceByKind(t, report, verification.CheckTests)
			if tests.Tests == nil || tests.Tests.Passed != test.passed || tests.Tests.Failed != test.failed || tests.Tests.Skipped != test.skippedTests {
				t.Fatalf("test evidence = %#v, want %d passed, %d failed, %d skipped", tests.Tests, test.passed, test.failed, test.skippedTests)
			}
		})
	}
}

func TestEngineDirectAnalyzersOnly(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod":       failureFixtureModule,
		"calc/calc.go": failureFixtureCalc,
		"api/api.go":   "package api\n\nimport \"example.test/verify/calc\"\n\nfunc Sign(value int) int { return calc.Sign(value) }\n\nfunc Ignore(err error) { _ = err }\n",
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "calc/calc.go", failureFixtureChangedCalc)
	tests := []struct {
		name        string
		wantTargets []string
		want        verification.AnalysisSummary
		directOnly  bool
	}{
		{
			name: "reverse importers are analyzed by default", want: verification.AnalysisSummary{Base: 1, Current: 1, Existing: 1},
			wantTargets: []string{"example.test/verify/api", "example.test/verify/calc"},
		},
		{
			name: "direct only skips reverse importers", directOnly: true,
			wantTargets: []string{"example.test/verify/calc"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := verifyRepository(t, repository, verification.Request{Base: base, DirectAnalyzersOnly: test.directOnly})
			errorsEvidence := evidenceByKind(t, report, verification.CheckErrors)
			if errorsEvidence.Status != verification.EvidencePassed || errorsEvidence.Analysis == nil || *errorsEvidence.Analysis != test.want {
				t.Fatalf("errors evidence = %#v (%#v), want %#v", errorsEvidence, errorsEvidence.Analysis, test.want)
			}
			for _, check := range report.Plan {
				if check.Kind == verification.CheckErrors && !slices.Equal(check.Targets, test.wantTargets) {
					t.Fatalf("errors plan targets = %q, want %q", check.Targets, test.wantTargets)
				}
				if check.Kind == verification.CheckTests && len(check.Targets) != 2 {
					t.Fatalf("tests plan targets = %q, want the full affected closure", check.Targets)
				}
			}
		})
	}
}

func TestEngineSoftAnalyzerFailuresBecomeEvidence(t *testing.T) {
	tests := []struct {
		name string
		soft bool
	}{
		{name: "baseline failure is evidence by default", soft: false},
		{name: "baseline failure is evidence when soft", soft: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, analysis := stubbedEngine(t, errors.New("analyze is not called"))
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			collection, err := engine.CollectAnalysis(ctx, verification.Request{Base: "base", SoftAnalyzerFailures: test.soft}, analysis)
			if err != nil {
				t.Fatalf("CollectAnalysis() error = %v, want analyzer failure as evidence", err)
			}
			for _, kind := range []verification.CheckKind{verification.CheckConcurrency, verification.CheckErrors} {
				evidence := evidenceByKind(t, collection.Report, kind)
				if evidence.Status != verification.EvidenceError || evidence.Error == "" {
					t.Fatalf("%s evidence = %#v, want error evidence", kind, evidence)
				}
			}
			found := false
			for _, item := range collection.Report.Uncertainties {
				found = found || item.Code == "baseline_unavailable"
			}
			if !found {
				t.Fatalf("uncertainties = %#v, want baseline_unavailable", collection.Report.Uncertainties)
			}
		})
	}
}

func TestEngineCollectAnalysisSkipsDiscovery(t *testing.T) {
	analyzeErr := errors.New("analyze is not called")
	tests := []struct {
		name    string
		wantErr string
		request verification.Request
		collect bool
	}{
		{name: "collect analysis uses the supplied analysis", request: verification.Request{Base: "base"}},
		{name: "collect analysis normalizes the request", request: verification.Request{}, wantErr: "base is required"},
		{name: "collect still runs discovery", request: verification.Request{Base: "base"}, collect: true, wantErr: analyzeErr.Error()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, analysis := stubbedEngine(t, analyzeErr)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var collection verification.Collection
			var err error
			if test.collect {
				collection, err = engine.Collect(ctx, test.request)
			} else {
				collection, err = engine.CollectAnalysis(ctx, test.request, analysis)
			}
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if collection.Policy.FailOn != verification.FailOnError || collection.Report.ID != "" {
				t.Fatalf("collection policy/id = %#v/%q, want normalized and unfinalized", collection.Policy, collection.Report.ID)
			}
			if tests := evidenceByKind(t, collection.Report, verification.CheckTests); tests.Tests == nil || tests.Tests.Passed != 1 {
				t.Fatalf("test evidence = %#v, want the supplied package executed", tests)
			}
			if len(collection.Analysis.Packages) != 1 || len(collection.Report.Change.Files) != 1 {
				t.Fatalf("collection = %#v, want the supplied analysis", collection)
			}
		})
	}
}

// stubbedEngine returns an engine whose change analyzer fails discovery with
// analyzeErr and cannot materialize a merge-base, plus a complete analysis of
// a one-package workspace with a passing test.
func stubbedEngine(t *testing.T, analyzeErr error) (*verification.Engine, verification.ChangeAnalysis) {
	t.Helper()
	repository := t.TempDir()
	source := "package stub\n\nfunc Value() int { return 1 }\n"
	verificationWrite(t, repository, "go.mod", "module example.test/stub\n\ngo 1.25.0\n")
	verificationWrite(t, repository, "value.go", source)
	verificationWrite(t, repository, "value_test.go", "package stub\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatal(\"value\")\n\t}\n}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ws, err := workspace.Open(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	changed := verification.ChangedFile{Path: "value.go", Change: verification.ChangeModified, BaseRanges: []verification.LineRange{{Start: 3, End: 3}}, CurrentRanges: []verification.LineRange{{Start: 3, End: 3}}}
	analysis := verification.ChangeAnalysis{
		Repository:    verification.Repository{RequestedBase: "base", MergeBaseCommit: strings.Repeat("a", 40)},
		Change:        verification.Change{Files: []verification.ChangedFile{changed}, Declarations: []verification.ChangedDeclaration{}},
		Impact:        verification.Impact{Packages: []verification.ImpactedPackage{{Kind: "go.package", ID: "example.test/stub", Distance: 0, Reasons: []string{"changed_source"}}}},
		Files:         []verification.SourceFile{{Change: changed, CurrentContent: []byte(source)}},
		Packages:      []verification.ExecutionTarget{{ID: "example.test/stub", Dir: ws.Root(), Distance: 0, Reasons: []string{"changed_source"}}},
		Uncertainties: []verification.Uncertainty{}, Risks: []verification.RiskArea{}, Complete: true,
	}
	engine, err := verification.NewEngine(ws, runner, &failingDiscoveryAnalyzer{err: analyzeErr}, "0.2.0-test")
	if err != nil {
		t.Fatal(err)
	}
	return engine, analysis
}

type failingDiscoveryAnalyzer struct {
	err error
}

func (a *failingDiscoveryAnalyzer) Analyze(context.Context, verification.ChangeOptions) (verification.ChangeAnalysis, error) {
	return verification.ChangeAnalysis{}, a.err
}

func (a *failingDiscoveryAnalyzer) MaterializeBase(context.Context, verification.Repository, string) (string, error) {
	return "", errors.New("merge-base is unavailable")
}
