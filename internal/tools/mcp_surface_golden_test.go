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
