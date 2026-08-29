package mcptest

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerProtocolSurfaceAndTool(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	if sidecar == "" {
		t.Skip("AGENTIC_GO_GOPLS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "agentic-go")
	if symlinkErr := os.Symlink(sidecar, filepath.Join(filepath.Dir(bin), "agentic-go-gopls")); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}
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
	if string(versionOutput) != "agentic-go 1.0.0-dev\n" {
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
	var templates []*mcp.ResourceTemplate
	for item, err := range session.ResourceTemplates(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		templates = append(templates, item)
	}
	if len(tools) != 14 || len(resources) != 7 || len(templates) != 1 || len(prompts) != 6 {
		t.Fatalf("surface counts: tools=%d resources=%d templates=%d prompts=%d", len(tools), len(resources), len(templates), len(prompts))
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
		Packages map[string]struct {
			Status string `json:"status"`
		} `json:"packages"`
		Failed int `json:"failed"`
		Passed int `json:"passed"`
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
	search, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "go_search", Arguments: map[string]any{"query": "NewCore", "limit": 5}})
	if err != nil {
		t.Fatal(err)
	}
	if search.IsError {
		t.Fatalf("semantic tool returned protocol error: %#v", search.Content)
	}
	encodedSearch, err := json.Marshal(search.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var searchResult struct {
		Matches []struct {
			Ref string `json:"ref"`
		} `json:"matches"`
		Total int `json:"total"`
	}
	if decodeErr := json.Unmarshal(encodedSearch, &searchResult); decodeErr != nil || searchResult.Total == 0 || len(searchResult.Matches) == 0 || searchResult.Matches[0].Ref == "" {
		t.Fatalf("semantic search = %s, error %v", encodedSearch, decodeErr)
	}
	symbol, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "go_symbol_context", Arguments: map[string]any{"symbol_ref": searchResult.Matches[0].Ref}})
	if err != nil || symbol.IsError {
		t.Fatalf("symbol context error=%v result=%#v", err, symbol)
	}
	brief, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "go_workspace_brief", Arguments: map[string]any{"max_bytes": 4096}})
	if err != nil || brief.IsError {
		t.Fatalf("workspace brief error=%v result=%#v", err, brief)
	}
	encodedBrief, err := json.Marshal(brief.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var briefResult struct {
		NextCursor string `json:"next_cursor"`
	}
	if decodeErr := json.Unmarshal(encodedBrief, &briefResult); decodeErr != nil || briefResult.NextCursor == "" {
		t.Fatalf("workspace brief has no artifact cursor: %s; error %v", encodedBrief, decodeErr)
	}
	artifact, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "agentic-go://artifact/" + briefResult.NextCursor})
	if err != nil || len(artifact.Contents) != 1 || artifact.Contents[0].Text == "" {
		t.Fatalf("artifact resource error=%v result=%#v", err, artifact)
	}
}
