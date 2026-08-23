package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegistryAndStructuredProtocolResult(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(
		&mcp.Implementation{Name: "agentic-go-test", Version: "0.1.0-dev"},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}},
	)
	RegisterAll(server, newTestRuntime(t))

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "agentic-go-test-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "go_test_structured" {
		t.Fatalf("tools/list = %+v, want only go_test_structured", listed.Tools)
	}
	annotations := listed.Tools[0].Annotations
	if annotations == nil || annotations.ReadOnlyHint || annotations.IdempotentHint || annotations.DestructiveHint == nil || !*annotations.DestructiveHint || annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
		t.Fatalf("unexpected tool annotations: %+v", annotations)
	}

	called, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "go_test_structured",
		Arguments: map[string]any{"package": "./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called.IsError {
		t.Fatalf("tools/call returned an MCP tool error: %+v", called.Content)
	}
	encoded, err := json.Marshal(called.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output TestStructuredOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.Passed != 1 || output.Failed != 1 || output.Skipped != 1 {
		t.Fatalf("structured content counts = %d/%d/%d, want 1/1/1", output.Passed, output.Failed, output.Skipped)
	}
}
