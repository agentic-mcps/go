package verification_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/changeimpact"
	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
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
