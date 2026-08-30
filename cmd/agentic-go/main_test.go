package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/changeimpact"
	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/intelligence"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestRunVerifyValidatesArgumentsBeforeWorkspaceSetup(t *testing.T) {
	tests := []struct { //nolint:govet // Table fields follow request readability.
		name string
		want string
		args []string
	}{
		{name: "missing base", args: nil, want: "--base is required"},
		{name: "bad format", args: []string{"--base", "HEAD", "--format", "yaml"}, want: "--format"},
		{name: "bad severity", args: []string{"--base", "HEAD", "--fail-on", "fatal"}, want: "--fail-on"},
		{name: "bad coverage", args: []string{"--base", "HEAD", "--min-changed-coverage", "101"}, want: "--min-changed-coverage"},
		{name: "bad package limit", args: []string{"--base", "HEAD", "--max-packages", "501"}, want: "--max-packages"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exit := runVerify(test.args, &stdout, &stderr); exit != 2 {
				t.Fatalf("exit = %d, want 2", exit)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stdout=%q stderr=%q, want %q diagnostic", stdout.String(), stderr.String(), test.want)
			}
		})
	}
}

func TestRunVerifyRejectsUnpublishedOperationalFlags(t *testing.T) {
	for _, name := range []string{"--max-concurrent-loads", "--max-tool-seconds"} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exit := runVerify([]string{name, "1"}, &stdout, &stderr); exit != 2 {
				t.Fatalf("exit = %d, want 2", exit)
			}
			if !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("stderr = %q, want unknown-flag diagnostic", stderr.String())
			}
		})
	}
}

func TestRunContractExportValidatesArgumentsBeforeWorkspaceSetup(t *testing.T) {
	//nolint:govet // Table fields follow request readability.
	tests := []struct {
		args []string
		name string
		want string
	}{
		{name: "missing subcommand", args: nil, want: "subcommand export is required"},
		{name: "missing output", args: []string{"export"}, want: "--output is required"},
		{name: "invalid format", args: []string{"export", "--output", "handoff.json", "--format", "yaml"}, want: "--format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exit := runContract(test.args, &stdout, &stderr); exit != 2 {
				t.Fatalf("exit = %d, want 2", exit)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stdout=%q stderr=%q, want %q diagnostic", stdout.String(), stderr.String(), test.want)
			}
		})
	}
}

func TestRunContractExportMapsRequestAndWritesCanonicalResults(t *testing.T) {
	want := intelligence.ContractExport{ContractID: "chg_123", SnapshotID: "snap_123", Path: "handoff/change.json", Digest: "sha256:123"}
	var workspacePath string
	var request intelligence.ContractExportRequest
	dependencies := contractExportDependencies{export: func(_ context.Context, workspace string, got intelligence.ContractExportRequest) (intelligence.ContractExport, error) {
		workspacePath, request = workspace, got
		return want, nil
	}}
	var stdout, stderr bytes.Buffer
	exit := runContractWithDependencies([]string{"export", "--workspace", "workspace", "--output", "handoff/change.json", "--id", "chg_123", "--format", "json"}, &stdout, &stderr, dependencies)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if workspacePath != "workspace" || request.ContractID != "chg_123" || request.Destination != "handoff/change.json" {
		t.Fatalf("workspace=%q request=%#v", workspacePath, request)
	}
	var decoded intelligence.ContractExport
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil || decoded != want {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}

	stdout.Reset()
	exit = runContractWithDependencies([]string{"export", "--output", "handoff/change.json"}, &stdout, &stderr, dependencies)
	if exit != 0 || !strings.Contains(stdout.String(), "handoff/change.json") || strings.Contains(stdout.String(), "/Users/") {
		t.Fatalf("text exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunContractExportReportsOperationalFailure(t *testing.T) {
	dependencies := contractExportDependencies{export: func(context.Context, string, intelligence.ContractExportRequest) (intelligence.ContractExport, error) {
		return intelligence.ContractExport{}, errors.New("contract not found")
	}}
	var stdout, stderr bytes.Buffer
	if exit := runContractWithDependencies([]string{"export", "--output", "handoff/change.json"}, &stdout, &stderr, dependencies); exit != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "contract not found") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunVerifyWritesCanonicalJSONAndPolicyExit(t *testing.T) {
	repository := cliRepository(t)
	base := cliGit(t, repository, "rev-parse", "HEAD")
	cliWrite(t, repository, "value.go", "package fixture\n\nfunc value() int { return 2 }\n")

	var stdout, stderr bytes.Buffer
	exit := runVerifyWithFactory([]string{"--workspace", repository, "--base", base, "--format", "json"}, &stdout, &stderr, testVerificationService)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr=%q stdout=%q", exit, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var report verification.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON report: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion != verification.SchemaVersion || report.Result.Status != verification.ResultPass {
		t.Fatalf("report = %#v", report)
	}

	stdout.Reset()
	stderr.Reset()
	exit = runVerifyWithFactory([]string{"--workspace", repository, "--base", base, "--format", "json", "--min-changed-coverage", "100"}, &stdout, &stderr, testVerificationService)
	if exit != 1 {
		t.Fatalf("coverage-policy exit = %d, want 1; stderr=%q stdout=%q", exit, stderr.String(), stdout.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode policy JSON report: %v", err)
	}
	if report.Result.Status != verification.ResultFindings {
		t.Fatalf("policy status = %s, want findings", report.Result.Status)
	}
}

func testVerificationService(
	_ context.Context,
	ws *workspace.Workspace,
	runner *execution.Runner,
	impact *changeimpact.Analyzer,
) (verificationService, func(), error) {
	engine, err := verification.NewEngine(ws, runner, impact, "test")
	return engine, func() {}, err
}

func TestRenderTextDoesNotClaimSafety(t *testing.T) {
	report := verification.NewReport("0.2.0-test", verification.Repository{RequestedBase: "main"})
	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := renderTextReport(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Status: pass") || !strings.Contains(output.String(), "does not prove the change safe") {
		t.Fatalf("text output = %q", output.String())
	}
}

func TestRenderTextDisclosesBoundedDetails(t *testing.T) {
	report := verification.NewReport("0.2.0-test", verification.Repository{RequestedBase: "main"})
	report.Change.Files = []verification.ChangedFile{{Path: "visible.go"}}
	report.Change.FilesTotal = 3
	report.Change.FilesTruncated = true
	report.Change.Declarations = []verification.ChangedDeclaration{{Name: "Visible"}}
	report.Change.DeclarationsTotal = 2
	report.Change.DeclarationsTruncated = true
	report.Findings = []verification.Finding{{Severity: verification.SeverityError, Message: "visible finding"}}
	report.FindingsTotal = 4
	report.FindingsTruncated = true

	var output bytes.Buffer
	if err := renderTextReport(&output, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Change: 3 (1 shown) files, 2 (1 shown) declarations",
		"Findings (4 total; 1 shown):",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("text output %q does not contain %q", output.String(), want)
		}
	}
}

func cliRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	cliGit(t, repository, "init", "-b", "main")
	cliGit(t, repository, "config", "user.name", "Fixture")
	cliGit(t, repository, "config", "user.email", "fixture@example.test")
	cliWrite(t, repository, "go.mod", "module example.test/cli\n\ngo 1.25.0\n")
	cliWrite(t, repository, "value.go", "package fixture\n\nfunc value() int { return 1 }\n")
	cliGit(t, repository, "add", ".")
	cliGit(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	return repository
}

func cliWrite(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cliGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
