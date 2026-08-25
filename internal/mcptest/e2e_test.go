package mcptest

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerProtocolSurfaceAndTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "agentic-go")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/agentic-go")
	build.Dir = repoRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build server: %v\n%s", buildErr, output)
	}
	version := exec.CommandContext(ctx, bin, "--version")
	versionOutput, versionErr := version.CombinedOutput()
	if versionErr != nil {
		t.Fatalf("agentic-go --version: %v\n%s", versionErr, versionOutput)
	}
	if string(versionOutput) != "agentic-go 0.2.0-dev\n" {
		t.Fatalf("agentic-go --version = %q", versionOutput)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "agentic-go-e2e", Version: "test"}, nil)
	cmd := exec.CommandContext(ctx, bin, "--workspace", repoRoot, "--max-tool-seconds", "30")
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	var tools []*mcp.Tool
	for item, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, item)
	}
	var resources []*mcp.Resource
	for item, err := range session.Resources(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		resources = append(resources, item)
	}
	var prompts []*mcp.Prompt
	for item, err := range session.Prompts(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		prompts = append(prompts, item)
	}
	if len(tools) != 7 || len(resources) != 4 || len(prompts) != 4 {
		t.Fatalf("surface counts: tools=%d resources=%d prompts=%d", len(tools), len(resources), len(prompts))
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "go_test_structured", Arguments: map[string]any{"package": "./internal/finding", "timeout_seconds": 30}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned protocol error: %#v", result.Content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	//nolint:govet // decode shape follows the public structured output schema.
	var output struct {
		Failed   int `json:"failed"`
		Passed   int `json:"passed"`
		Packages map[string]struct {
			Status string `json:"status"`
		} `json:"packages"`
	}
	if decodeErr := json.Unmarshal(data, &output); decodeErr != nil {
		t.Fatalf("decode structured output: %v; raw=%s", decodeErr, data)
	}
	if output.Failed != 0 || output.Passed == 0 {
		t.Fatalf("unexpected test output: %s", data)
	}
	if len(output.Packages) != 1 {
		t.Fatalf("package summaries = %d, want 1: %s", len(output.Packages), data)
	}
	for pkg, summary := range output.Packages {
		if summary.Status != "ok" {
			t.Fatalf("package %q status = %q, want ok", pkg, summary.Status)
		}
	}
}
