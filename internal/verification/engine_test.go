package verification_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/changeimpact"
	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestEngineRunsOneAffectedTestAndCoveragePass(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod": "module example.test/verify\n\ngo 1.25.0\n",
		"calc/calc.go": `package calc

func Sign(value int) int {
	return 1
}
`,
		"calc/calc_test.go": `package calc

import "testing"

func TestSign(t *testing.T) {
	if Sign(1) != 1 { t.Fatal("unexpected sign") }
}
`,
		"api/api.go": `package api

import "example.test/verify/calc"

func Sign(value int) int { return calc.Sign(value) }
`,
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "calc/calc.go", `package calc

func Sign(value int) int {
	if value < 0 {
		return -1
	}
	return 1
}
`)

	report := verifyRepository(t, repository, verification.Request{Base: base})
	if report.Result.Status != verification.ResultPass {
		t.Fatalf("result = %#v, want pass", report.Result)
	}
	if len(report.Impact.Packages) != 2 {
		t.Fatalf("impact = %#v, want changed package and reverse importer", report.Impact)
	}
	tests := evidenceByKind(t, report, verification.CheckTests)
	if tests.Status != verification.EvidencePassed || tests.Tests == nil || tests.Tests.Passed != 1 || tests.Tests.Failed != 0 {
		t.Fatalf("test evidence = %#v", tests)
	}
	if len(tests.Tests.Packages) != 2 || tests.Tests.Packages[0].Package != "example.test/verify/api" || tests.Tests.Packages[0].Status != "skip" {
		t.Fatalf("package summaries = %#v, want no-test package skip", tests.Tests.Packages)
	}
	coverage := evidenceByKind(t, report, verification.CheckCoverage)
	if coverage.Status != verification.EvidencePassed || coverage.Coverage == nil {
		t.Fatalf("coverage evidence = %#v", coverage)
	}
	if coverage.Coverage.TotalStatements == 0 || coverage.Coverage.CoveredStatements >= coverage.Coverage.TotalStatements {
		t.Fatalf("changed coverage = %#v, want covered and uncovered statements", coverage.Coverage)
	}
}

func TestEngineReturnsTestFailureAsPolicyFinding(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod":  "module example.test/verify\n\ngo 1.25.0\n",
		"calc.go": "package verify\n\nfunc Value() int { return 1 }\n",
		"calc_test.go": `package verify

import "testing"

func TestValue(t *testing.T) {
	if Value() != 1 { t.Fatalf("Value = %d", Value()) }
}
`,
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "calc.go", "package verify\n\nfunc Value() int { return 2 }\n")

	report := verifyRepository(t, repository, verification.Request{Base: base})
	if report.Result.Status != verification.ResultFindings || report.Result.ExitCode != 1 {
		t.Fatalf("result = %#v, want findings/1", report.Result)
	}
	if len(report.Findings) == 0 || report.Findings[0].Kind != "test.failure" {
		t.Fatalf("findings = %#v, want test.failure", report.Findings)
	}
	tests := evidenceByKind(t, report, verification.CheckTests)
	if tests.Status != verification.EvidenceFailed || tests.Tests == nil || tests.Tests.Failed != 1 {
		t.Fatalf("test evidence = %#v", tests)
	}
}

func TestEngineSuppressesMappedBaselineFindings(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod": "module example.test/verify\n\ngo 1.25.0\n",
		"errors.go": `package verify

func Existing(err error) { _ = err }

func Changed(err error) {}
`,
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "errors.go", `package verify

func Existing(err error) { _ = err }

func Changed(err error) { _ = err }
`)

	report := verifyRepository(t, repository, verification.Request{Base: base, FailOn: verification.FailOnNone})
	errorsEvidence := evidenceByKind(t, report, verification.CheckErrors)
	if errorsEvidence.Status != verification.EvidencePassed || errorsEvidence.Analysis == nil {
		t.Fatalf("error analysis evidence = %#v", errorsEvidence)
	}
	want := verification.AnalysisSummary{Base: 1, Current: 2, Introduced: 1, Existing: 1}
	if *errorsEvidence.Analysis != want {
		t.Fatalf("error analysis = %#v, want %#v", *errorsEvidence.Analysis, want)
	}
	if len(report.Findings) != 1 || report.Findings[0].Baseline != verification.BaselineIntroduced || report.Findings[0].Location == nil || report.Findings[0].Location.File != "errors.go" {
		t.Fatalf("primary findings = %#v, want one introduced finding", report.Findings)
	}
}

func TestEngineSkipsCoverageWithoutChangedExecutableStatements(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod":   "module example.test/verify\n\ngo 1.25.0\n",
		"value.go": "// Original documentation.\npackage verify\n\nfunc Value() int { return 1 }\n",
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "value.go", "// Revised documentation.\npackage verify\n\nfunc Value() int { return 1 }\n")

	report := verifyRepository(t, repository, verification.Request{Base: base})
	coverage := evidenceByKind(t, report, verification.CheckCoverage)
	if coverage.Status != verification.EvidenceSkipped || coverage.Coverage != nil {
		t.Fatalf("coverage evidence = %#v, want skipped without a coverage payload", coverage)
	}
}

func TestEngineRemovesWorkspaceAndRunCachePathsFromReport(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod":   "module example.test/verify\n\ngo 1.25.0\n",
		"value.go": "package verify\n\nfunc Value() int { return 1 }\n",
		"value_test.go": `package verify

import (
	"os"
	"testing"
)

func TestValue(t *testing.T) {
	if Value() != 1 {
		cwd, _ := os.Getwd()
		t.Fatalf("workspace=%s", cwd)
	}
}
`,
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "value.go", "package verify\n\nfunc Value() int { return missingIdentifier }\n")

	report := verifyRepository(t, repository, verification.Request{Base: base})
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{repository, filepath.Join(cache, "agentic-go", "runs")} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("portable report contains absolute path %q:\n%s", forbidden, encoded)
		}
	}
}

func TestEngineRemovesWorkspacePathsFromFailingTestOutput(t *testing.T) {
	repository := verificationRepository(t, map[string]string{
		"go.mod":   "module example.test/verify\n\ngo 1.25.0\n",
		"value.go": "package verify\n\nfunc Value() int { return 1 }\n",
		"value_test.go": `package verify

import (
	"os"
	"testing"
)

func TestValue(t *testing.T) {
	if Value() != 1 {
		cwd, _ := os.Getwd()
		t.Fatalf("workspace=%s", cwd)
	}
}
`,
	})
	base := verificationGit(t, repository, "rev-parse", "HEAD")
	verificationWrite(t, repository, "value.go", "package verify\n\nfunc Value() int { return 2 }\n")

	report := verifyRepository(t, repository, verification.Request{Base: base})
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), repository) {
		t.Fatalf("portable report contains workspace path %q:\n%s", repository, encoded)
	}
	tests := evidenceByKind(t, report, verification.CheckTests)
	if tests.Tests == nil || tests.Tests.Failed != 1 || len(tests.Tests.Nonpassing) != 1 {
		t.Fatalf("test evidence = %#v, want one failed test", tests.Tests)
	}
	if !strings.Contains(tests.Tests.Nonpassing[0].Output, "workspace=.") {
		t.Fatalf("sanitized output = %q, want workspace-relative marker", tests.Tests.Nonpassing[0].Output)
	}
}

func TestEngineRetainsEvidenceWhenAnalyzerBaselineIsUnavailable(t *testing.T) {
	repository := t.TempDir()
	verificationWrite(t, repository, "go.mod", "module example.test/unavailable\n\ngo 1.25.0\n")
	verificationWrite(t, repository, "errors.go", "package unavailable\n\nfunc Ignore(err error) { _ = err }\n")
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
	analyzer := &unavailableBaselineAnalyzer{analysis: verification.ChangeAnalysis{
		Repository:    verification.Repository{RequestedBase: "base", MergeBaseCommit: strings.Repeat("a", 40)},
		Change:        verification.Change{Files: []verification.ChangedFile{{Path: "errors.go", Change: verification.ChangeModified, BaseRanges: []verification.LineRange{{Start: 3, End: 3}}, CurrentRanges: []verification.LineRange{{Start: 3, End: 3}}}}, Declarations: []verification.ChangedDeclaration{}},
		Impact:        verification.Impact{Packages: []verification.ImpactedPackage{{Kind: "go.package", ID: "example.test/unavailable", Distance: 0, Reasons: []string{"changed_source"}}}},
		Files:         []verification.SourceFile{{Change: verification.ChangedFile{Path: "errors.go", Change: verification.ChangeModified, CurrentRanges: []verification.LineRange{{Start: 3, End: 3}}}, CurrentContent: []byte("package unavailable\n\nfunc Ignore(err error) { _ = err }\n")}},
		Packages:      []verification.ExecutionTarget{{ID: "example.test/unavailable", Dir: repository, Distance: 0, Reasons: []string{"changed_source"}}},
		Uncertainties: []verification.Uncertainty{}, Risks: []verification.RiskArea{}, Complete: true,
	}}
	engine, err := verification.NewEngine(ws, runner, analyzer, "0.2.0-test")
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Verify(ctx, verification.Request{Base: "base"})
	if err != nil {
		t.Fatalf("Verify() error = %v, want incomplete report", err)
	}
	if report.Result.Status != verification.ResultIncomplete {
		t.Fatalf("result = %#v, want incomplete", report.Result)
	}
	if evidenceByKind(t, report, verification.CheckTests).Tests == nil {
		t.Fatal("test evidence was discarded")
	}
	errorsEvidence := evidenceByKind(t, report, verification.CheckErrors)
	if errorsEvidence.Status != verification.EvidenceError || errorsEvidence.Analysis == nil || errorsEvidence.Analysis.Current == 0 || errorsEvidence.Analysis.Unknown != errorsEvidence.Analysis.Current {
		t.Fatalf("errors evidence = %#v, want retained current diagnostics marked unknown", errorsEvidence)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if analyzer.destination == "" {
		t.Fatal("baseline materializer was not called")
	}
	if strings.Contains(string(encoded), analyzer.destination) || strings.Contains(string(encoded), repository) {
		t.Fatalf("report leaks an absolute path:\n%s", encoded)
	}
}

type unavailableBaselineAnalyzer struct {
	destination string
	analysis    verification.ChangeAnalysis
}

func (a *unavailableBaselineAnalyzer) Analyze(context.Context, verification.ChangeOptions) (verification.ChangeAnalysis, error) {
	return a.analysis, nil
}

func (a *unavailableBaselineAnalyzer) MaterializeBase(_ context.Context, _ verification.Repository, destination string) (string, error) {
	a.destination = destination
	return "", fmt.Errorf("opening unavailable baseline %s", filepath.Join(destination, "tree"))
}

func verifyRepository(t *testing.T, repository string, request verification.Request) verification.Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ws, err := workspace.Open(ctx, repository)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	impact, err := changeimpact.New(ws, runner)
	if err != nil {
		t.Fatalf("new change analyzer: %v", err)
	}
	engine, err := verification.NewEngine(ws, runner, impact, "0.2.0-test")
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	report, err := engine.Verify(ctx, request)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return report
}

func evidenceByKind(t *testing.T, report verification.Report, kind verification.CheckKind) verification.Evidence {
	t.Helper()
	for _, evidence := range report.Evidence {
		if evidence.Kind == kind {
			return evidence
		}
	}
	t.Fatalf("report has no %s evidence: %#v", kind, report.Evidence)
	return verification.Evidence{}
}

func verificationRepository(t *testing.T, files map[string]string) string {
	t.Helper()
	repository := t.TempDir()
	verificationGit(t, repository, "init", "-b", "main")
	verificationGit(t, repository, "config", "user.name", "Fixture")
	verificationGit(t, repository, "config", "user.email", "fixture@example.test")
	for path, content := range files {
		verificationWrite(t, repository, path, content)
	}
	verificationGit(t, repository, "add", ".")
	verificationGit(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	return repository
}

func verificationWrite(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func verificationGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
