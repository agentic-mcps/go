package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	if len(listed.Tools) != 7 {
		t.Fatalf("len(tools/list) = %d, want 7", len(listed.Tools))
	}

	resources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Resources) != 4 {
		t.Fatalf("len(resources/list) = %d, want 4", len(resources.Resources))
	}
	wantResources := map[string]bool{
		"agentic-go://module":         false,
		"agentic-go://packages":       false,
		"agentic-go://analysis-rules": false,
		traceSummaryURI:               false,
	}
	for _, resource := range resources.Resources {
		if _, ok := wantResources[resource.URI]; !ok {
			t.Fatalf("unexpected resource %q", resource.URI)
		}
		wantResources[resource.URI] = true
	}
	for uri, found := range wantResources {
		if !found {
			t.Errorf("resource %q is not registered", uri)
		}
	}

	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts.Prompts) != 4 {
		t.Fatalf("len(prompts/list) = %d, want 4", len(prompts.Prompts))
	}
	wantPrompts := map[string]bool{"audit-package": false, "pre-commit-check": false, "bisect-flake": false, "verify-change": false}
	for _, prompt := range prompts.Prompts {
		if _, ok := wantPrompts[prompt.Name]; !ok {
			t.Fatalf("unexpected prompt %q", prompt.Name)
		}
		wantPrompts[prompt.Name] = true
	}
	for name, found := range wantPrompts {
		if !found {
			t.Errorf("prompt %q is not registered", name)
		}
	}
	for _, name := range []string{"go_audit_concurrency", "go_audit_errors"} {
		tool := listedTool(t, listed.Tools, name)
		annotations := tool.Annotations
		if annotations == nil || !annotations.ReadOnlyHint || !annotations.IdempotentHint || annotations.DestructiveHint == nil || *annotations.DestructiveHint || annotations.OpenWorldHint == nil || *annotations.OpenWorldHint {
			t.Fatalf("unexpected %s annotations: %+v", name, annotations)
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{`"package"`, `"min_severity"`, `"max_findings"`} {
			if !strings.Contains(string(schema), field) {
				t.Fatalf("%s input schema %s missing %s", name, schema, field)
			}
		}
	}
	for _, name := range []string{"go_test_structured", "go_race_report", "go_coverage_gaps", "go_benchmark_diff", "go_flake_finder"} {
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
	if output.Passed != 2 || output.Failed != 1 || output.Skipped != 1 {
		t.Fatalf("structured content counts = %d/%d/%d, want 2/1/1", output.Passed, output.Failed, output.Skipped)
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

	called, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "go_coverage_gaps",
		Arguments: map[string]any{"package": "."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called.IsError {
		t.Fatalf("coverage tools/call returned an MCP tool error: %+v", called.Content)
	}
	encoded, err = json.Marshal(called.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var coverageOutput CoverageGapsOutput
	if err := json.Unmarshal(encoded, &coverageOutput); err != nil {
		t.Fatal(err)
	}
	if coverageOutput.OverallPercent >= 100 || len(coverageOutput.Files) == 0 {
		t.Fatalf("coverage structured content = %+v, want incomplete coverage", coverageOutput)
	}

	called, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "go_flake_finder",
		Arguments: map[string]any{"package": ".", "runs": 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called.IsError {
		t.Fatalf("flake tools/call returned an MCP tool error: %+v", called.Content)
	}
	encoded, err = json.Marshal(called.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var flakeOutput FlakeFinderOutput
	if err := json.Unmarshal(encoded, &flakeOutput); err != nil {
		t.Fatal(err)
	}
	if len(flakeOutput.Flaky) != 1 || flakeOutput.Flaky[0].FlakeRate != 0.5 {
		t.Fatalf("flake structured content = %+v, want deterministic mixed result", flakeOutput)
	}

	for _, tc := range []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "go_audit_concurrency", decode: func(data []byte) error {
			var output AuditConcurrencyOutput
			if err := json.Unmarshal(data, &output); err != nil {
				return err
			}
			if output.Result.Findings == nil || output.Result.CountsBySeverity == nil {
				return fmt.Errorf("uninitialized result: %+v", output.Result)
			}
			return nil
		}},
		{name: "go_audit_errors", decode: func(data []byte) error {
			var output AuditErrorsOutput
			if err := json.Unmarshal(data, &output); err != nil {
				return err
			}
			if output.Result.Findings == nil || output.Result.CountsBySeverity == nil {
				return fmt.Errorf("uninitialized result: %+v", output.Result)
			}
			return nil
		}},
	} {
		called, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: map[string]any{"package": "."}})
		if err != nil {
			t.Fatal(err)
		}
		if called.IsError {
			t.Fatalf("%s tools/call returned an MCP tool error: %+v", tc.name, called.Content)
		}
		encoded, err = json.Marshal(called.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := tc.decode(encoded); err != nil {
			t.Fatalf("%s structured content: %v", tc.name, err)
		}
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
