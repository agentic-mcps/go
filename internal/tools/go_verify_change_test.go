package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestVerifyChangeToolReturnsCanonicalReport(t *testing.T) {
	repository := verifyToolRepository(t)
	base := verifyToolGit(t, repository, "rev-parse", "HEAD")
	verifyToolWrite(t, repository, "calc.go", "package fixture\n\nfunc Value() int { return 2 }\n")

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
	runtime, err := NewRuntimeWithVersion(ws, runner, &trace.Tracer{}, "0.2.0-test")
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "verify-tool-test", Version: "0.2.0-test"}, nil)
	RegisterVerifyChange(server, runtime)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "verify-tool-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	called, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "go_verify_change", Arguments: map[string]any{"base": base, "fail_on": "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called.IsError {
		t.Fatalf("tools/call returned an MCP tool error: %+v", called.Content)
	}
	if len(called.Content) != 1 {
		t.Fatalf("content blocks = %d, want one concise fallback", len(called.Content))
	}
	text, ok := called.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "report is in structuredContent") || len(text.Text) > 512 {
		t.Fatalf("text fallback = %#v, want concise structured-content guidance", called.Content[0])
	}
	encoded, err := json.Marshal(called.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var report verification.Report
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != verification.SchemaVersion || report.Provider.Version != "0.2.0-test" {
		t.Fatalf("report identity = %q/%q", report.SchemaVersion, report.Provider.Version)
	}
	if report.Change.Files == nil || report.Impact.Packages == nil || report.Evidence == nil || report.Findings == nil || report.Risks == nil || report.Uncertainties == nil {
		t.Fatalf("report has nil collections: %#v", report)
	}
	if report.Result.Status != verification.ResultPass {
		t.Fatalf("result = %#v, want pass", report.Result)
	}
}

func TestNormalizeVerifyChangeInput(t *testing.T) {
	coverage := 75.0
	//nolint:govet // table order keeps the failure expectation beside its name.
	for _, test := range []struct {
		name  string
		want  string
		input VerifyChangeInput
	}{
		{name: "missing base", input: VerifyChangeInput{}, want: "base is required"},
		{name: "option base", input: VerifyChangeInput{Base: "-main"}, want: "base is invalid"},
		{name: "spaced base", input: VerifyChangeInput{Base: "main branch"}, want: "base is invalid"},
		{name: "option package", input: VerifyChangeInput{Base: "main", Package: "-run"}, want: "package is invalid"},
		{name: "bad severity", input: VerifyChangeInput{Base: "main", FailOn: "fatal"}, want: "fail_on"},
		{name: "large closure", input: VerifyChangeInput{Base: "main", MaxPackages: 501}, want: "max_packages"},
		{name: "bad coverage", input: VerifyChangeInput{Base: "main", MinChangedCoverage: func() *float64 { value := 101.0; return &value }()}, want: "min_changed_coverage"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := normalizeVerifyChangeInput(&test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
	valid := VerifyChangeInput{Base: "origin/main", MinChangedCoverage: &coverage}
	if err := normalizeVerifyChangeInput(&valid); err != nil {
		t.Fatal(err)
	}
	if valid.Package != "./..." || valid.FailOn != verification.FailOnError || valid.MaxPackages != 200 {
		t.Fatalf("defaults = %#v", valid)
	}
}

func verifyToolRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	verifyToolGit(t, repository, "init", "-b", "main")
	verifyToolGit(t, repository, "config", "user.name", "Fixture")
	verifyToolGit(t, repository, "config", "user.email", "fixture@example.test")
	verifyToolWrite(t, repository, "go.mod", "module example.test/mcpverify\n\ngo 1.25.0\n")
	verifyToolWrite(t, repository, "calc.go", "package fixture\n\nfunc Value() int { return 1 }\n")
	verifyToolWrite(t, repository, "calc_test.go", "package fixture\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) { if Value() < 1 { t.Fatal(Value()) } }\n")
	verifyToolGit(t, repository, "add", ".")
	verifyToolGit(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	return repository
}

func verifyToolWrite(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func verifyToolGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
