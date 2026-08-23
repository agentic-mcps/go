package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/trace"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func resourceRuntime(t *testing.T) *Runtime {
	t.Helper()
	t.Setenv("AGENTIC_GO_TRACE", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test\n\ngo 1.25\n\nrequire (\n\tgolang.org/x/tools v0.42.0\n)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tracer, err := trace.Init()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(ws, runner, tracer)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestWorkspaceResourcesReturnReducedFreshJSON(t *testing.T) {
	runtime := resourceRuntime(t)
	ctx := context.Background()
	module, err := runtime.moduleResource(ctx, &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://module"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(module.Contents) != 1 || module.Contents[0].MIMEType != "application/json" {
		t.Fatalf("module contents = %+v", module.Contents)
	}
	var got moduleResource
	if decodeErr := json.Unmarshal([]byte(module.Contents[0].Text), &got); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if got.Module != "example.test" || got.GoVersion != "1.25" {
		t.Fatalf("module = %+v", got)
	}
	packages, err := runtime.packagesResource(ctx, &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://packages"}})
	if err != nil {
		t.Fatal(err)
	}
	var listed []packageResource
	if decodeErr := json.Unmarshal([]byte(packages.Contents[0].Text), &listed); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(listed) != 1 || listed[0].ImportPath != "example.test" || listed[0].GoFiles != 1 {
		t.Fatalf("packages = %+v", listed)
	}
	rules, err := runtime.analysisRulesResource(ctx, &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://analysis-rules"}})
	if err != nil {
		t.Fatal(err)
	}
	var manifest []analysisRuleResource
	if decodeErr := json.Unmarshal([]byte(rules.Contents[0].Text), &manifest); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(manifest) != 11 {
		t.Fatalf("rules = %d, want 11", len(manifest))
	}
	if manifest[0].SourceDoc == "" {
		t.Fatalf("rules = %+v", manifest)
	}
	for _, excluded := range []string{"concurrency-11", "concurrency-13", "concurrency-16", "errors-08", "errors-18"} {
		for _, rule := range manifest {
			if rule.Rule == excluded {
				t.Fatalf("excluded rule %s is present", excluded)
			}
		}
	}
	if writeErr := os.WriteFile(filepath.Join(runtime.workspace.Root(), "extra_test.go"), []byte("package example\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	updated, err := runtime.packagesResource(ctx, &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://packages"}})
	if err != nil {
		t.Fatal(err)
	}
	var refreshed []packageResource
	if err := json.Unmarshal([]byte(updated.Contents[0].Text), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed[0].TestGoFiles != 1 {
		t.Fatalf("fresh package result = %+v", refreshed)
	}
}

func TestWorkspaceResourcesRejectWrongURI(t *testing.T) {
	runtime := resourceRuntime(t)
	if _, err := runtime.moduleResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://packages"}}); err == nil {
		t.Fatal("module resource accepted wrong URI")
	}
}

func TestRegisterWorkspaceResources(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	RegisterResources(server, resourceRuntime(t))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientSession.Close() }()
	listed, err := clientSession.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Resources) != 4 {
		t.Fatalf("resources = %d, want 4", len(listed.Resources))
	}
	for _, uri := range []string{"agentic-go://module", "agentic-go://packages", "agentic-go://analysis-rules", traceSummaryURI} {
		read, err := clientSession.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatal(err)
		}
		if len(read.Contents) != 1 {
			t.Fatalf("%s contents = %d", uri, len(read.Contents))
		}
		if read.Contents[0].MIMEType != "application/json" {
			t.Fatalf("%s MIME type = %q", uri, read.Contents[0].MIMEType)
		}
	}
}
