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
	if len(listed.Tools) != 2 {
		t.Fatalf("len(tools/list) = %d, want 2", len(listed.Tools))
	}
	for _, name := range []string{"go_test_structured", "go_race_report"} {
		tool := listedTool(t, listed.Tools, name)
		annotations := tool.Annotations
		if annotations == nil || annotations.ReadOnlyHint || annotations.IdempotentHint || annotations.DestructiveHint == nil || !*annotations.DestructiveHint || annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
			t.Fatalf("unexpected %s annotations: %+v", name, annotations)
		}
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

	called, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "go_race_report",
		Arguments: map[string]any{"package": "."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called.IsError {
		t.Fatalf("race tools/call returned an MCP tool error: %+v", called.Content)
	}
	encoded, err = json.Marshal(called.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var raceOutput RaceReportOutput
	if err := json.Unmarshal(encoded, &raceOutput); err != nil {
		t.Fatal(err)
	}
	if raceOutput.RawBlocksFound == 0 || len(raceOutput.Conflicts) == 0 {
		t.Fatalf("race structured content = %+v, want a conflict", raceOutput)
	}
}

func listedTool(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %+v", name, tools)
	return nil
}
