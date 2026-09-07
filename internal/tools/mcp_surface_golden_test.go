package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type frozenMCPSurface struct {
	Tools     []frozenTool     `json:"tools"`
	Resources []frozenResource `json:"resources"`
	Templates []frozenTemplate `json:"resource_templates"`
	Prompts   []frozenPrompt   `json:"prompts"`
}

type frozenTool struct {
	Name               string            `json:"name"`
	InputSchemaSHA256  string            `json:"input_schema_sha256"`
	OutputSchemaSHA256 string            `json:"output_schema_sha256"`
	Annotations        frozenAnnotations `json:"annotations"`
}

type frozenAnnotations struct {
	ReadOnly    bool `json:"read_only"`
	Destructive bool `json:"destructive"`
	Idempotent  bool `json:"idempotent"`
	OpenWorld   bool `json:"open_world"`
}

type frozenResource struct {
	Name     string `json:"name"`
	URI      string `json:"uri"`
	MIMEType string `json:"mime_type"`
}

type frozenTemplate struct {
	Name        string `json:"name"`
	URITemplate string `json:"uri_template"`
	MIMEType    string `json:"mime_type"`
}

type frozenPrompt struct {
	Name      string                 `json:"name"`
	Arguments []frozenPromptArgument `json:"arguments"`
}

type frozenPromptArgument struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

func TestFrozenMCPV1Surface(t *testing.T) {
	surface := readFrozenSurface(t)
	encoded, err := json.MarshalIndent(surface, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join("testdata", "mcp-v1-surface.json")
	if os.Getenv("AGENTIC_GO_UPDATE_MCP_GOLDEN") == "1" {
		if writeErr := os.WriteFile(path, encoded, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatal("MCP v1 interface differs from the frozen normalized golden")
	}
}

func TestPostV1FocusToolIsAdditiveAndDiscoverable(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "agentic-go-focus", Version: "dev"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	runtime := newTestRuntime(t)
	runtime.intelligence = &fakeIntelligence{}
	RegisterAll(server, runtime)
	RegisterContext(server, runtime)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "focus-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 15 {
		t.Fatalf("post-v1 tool count = %d, want 15", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.Name != "go_context" {
			continue
		}
		for _, phrase := range []string{"one selector", "previous_pack_id only", "stale selectors", "verification applicability"} {
			if !strings.Contains(tool.Description, phrase) {
				t.Fatalf("go_context description %q missing %q", tool.Description, phrase)
			}
		}
		encoded, marshalErr := json.Marshal(tool.InputSchema)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		for _, field := range []string{"previous_pack_id", "focus_file", "focus_package", "query", "symbol_ref", "max_bytes"} {
			if !bytes.Contains(encoded, []byte(`"`+field+`"`)) {
				t.Fatalf("go_context input schema lacks %q: %s", field, encoded)
			}
		}
		result, callErr := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "go_context", Arguments: map[string]any{"base": "HEAD", "query": "Worker"}})
		if callErr != nil || result.IsError {
			t.Fatalf("go_context call error=%v result=%#v", callErr, result)
		}
		payload, marshalErr := json.Marshal(result.StructuredContent)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var focus struct {
			SchemaVersion string `json:"schema_version"`
			PackID        string `json:"pack_id"`
		}
		if err := json.Unmarshal(payload, &focus); err != nil || focus.SchemaVersion != "agentic.focus/v1" || focus.PackID == "" {
			t.Fatalf("go_context structured output = %s, err %v", payload, err)
		}
		return
	}
	t.Fatal("post-v1 tools/list omitted go_context")
}

func TestProductionServerInitializeInstructionsAndUniqueFocusRegistration(t *testing.T) {
	if len(ServerInstructions) > 512 {
		t.Fatalf("server instructions length = %d, want <= 512", len(ServerInstructions))
	}
	for _, phrase := range []string{"unfamiliar", "cross-package", "go_context", "before editing", "one selector", "previous_pack_id only", "stale selectors", "impact", "verification applicability", "planning guidance", "smallest task owner", "path/package scope", "trivial edits"} {
		if !strings.Contains(ServerInstructions, phrase) {
			t.Errorf("server instructions %q missing %q", ServerInstructions, phrase)
		}
	}

	ctx := context.Background()
	server := NewProductionServer(&mcp.Implementation{Name: "agentic-go-production", Version: "test"})
	runtime := newTestRuntime(t)
	runtime.intelligence = &fakeIntelligence{}
	RegisterProduction(server, runtime)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "production-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	init := clientSession.InitializeResult()
	if init == nil || init.Instructions != ServerInstructions {
		t.Fatalf("initialize instructions = %#v, want %q", init, ServerInstructions)
	}
	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tool := range listed.Tools {
		if tool.Name == "go_context" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("go_context registration count = %d, want 1", count)
	}
}

func readFrozenSurface(t *testing.T) frozenMCPSurface {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(
		&mcp.Implementation{Name: "agentic-go-freeze", Version: "v1"},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}},
	)
	runtime := newTestRuntime(t)
	runtime.intelligence = &fakeIntelligence{}
	RegisterAll(server, runtime)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "agentic-go-freeze-client", Version: "v1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := clientSession.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	prompts, err := clientSession.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	surface := frozenMCPSurface{
		Tools: make([]frozenTool, 0, len(tools.Tools)), Resources: make([]frozenResource, 0, len(resources.Resources)),
		Templates: make([]frozenTemplate, 0, len(templates.ResourceTemplates)), Prompts: make([]frozenPrompt, 0, len(prompts.Prompts)),
	}
	for _, tool := range tools.Tools {
		annotations := frozenAnnotations{}
		if tool.Annotations != nil {
			annotations.ReadOnly = tool.Annotations.ReadOnlyHint
			annotations.Idempotent = tool.Annotations.IdempotentHint
			if tool.Annotations.DestructiveHint != nil {
				annotations.Destructive = *tool.Annotations.DestructiveHint
			}
			if tool.Annotations.OpenWorldHint != nil {
				annotations.OpenWorld = *tool.Annotations.OpenWorldHint
			}
		}
		surface.Tools = append(surface.Tools, frozenTool{
			Name: tool.Name, InputSchemaSHA256: schemaDigest(t, tool.InputSchema), OutputSchemaSHA256: schemaDigest(t, tool.OutputSchema), Annotations: annotations,
		})
	}
	for _, resource := range resources.Resources {
		surface.Resources = append(surface.Resources, frozenResource{Name: resource.Name, URI: resource.URI, MIMEType: resource.MIMEType})
	}
	for _, template := range templates.ResourceTemplates {
		surface.Templates = append(surface.Templates, frozenTemplate{Name: template.Name, URITemplate: template.URITemplate, MIMEType: template.MIMEType})
	}
	for _, prompt := range prompts.Prompts {
		item := frozenPrompt{Name: prompt.Name, Arguments: make([]frozenPromptArgument, 0, len(prompt.Arguments))}
		for _, argument := range prompt.Arguments {
			item.Arguments = append(item.Arguments, frozenPromptArgument{Name: argument.Name, Required: argument.Required})
		}
		sort.Slice(item.Arguments, func(i, j int) bool { return item.Arguments[i].Name < item.Arguments[j].Name })
		surface.Prompts = append(surface.Prompts, item)
	}
	sort.Slice(surface.Tools, func(i, j int) bool { return surface.Tools[i].Name < surface.Tools[j].Name })
	sort.Slice(surface.Resources, func(i, j int) bool { return surface.Resources[i].URI < surface.Resources[j].URI })
	sort.Slice(surface.Templates, func(i, j int) bool { return surface.Templates[i].URITemplate < surface.Templates[j].URITemplate })
	sort.Slice(surface.Prompts, func(i, j int) bool { return surface.Prompts[i].Name < surface.Prompts[j].Name })
	return surface
}

func schemaDigest(t *testing.T, schema any) string {
	t.Helper()
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest)
}
