package gopls

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPinnedSidecarContract(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	if sidecar == "" {
		t.Skip("AGENTIC_GO_GOPLS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	installation, err := Locate(ctx, os.Args[0], sidecar)
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}

	workspace := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/semantic\n\ngo 1.25.0\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	source := []byte("package semantic\n\nconst Emoji = \"🙂\"; var Target = Emoji\n")
	file := filepath.Join(workspace, "semantic.go")
	if writeErr := os.WriteFile(file, source, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	client, err := Start(ctx, Config{Command: installation.Path, Args: []string{"serve"}, Workspace: workspace})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = client.Close(closeCtx)
	})

	capabilities := client.Capabilities()
	if !capabilities.WorkspaceSymbol || !capabilities.Hover || !capabilities.Definition || !capabilities.TypeDefinition ||
		!capabilities.References || !capabilities.Implementation || !capabilities.DocumentSymbol || !capabilities.CallHierarchy ||
		!capabilities.Diagnostics || !capabilities.Rename || !capabilities.Formatting || !capabilities.CodeAction {
		t.Fatalf("incomplete pinned capability manifest: %#v", capabilities)
	}

	uri := (&url.URL{Scheme: "file", Path: file}).String()
	if notifyErr := client.Notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": 1, "text": string(source)},
	}); notifyErr != nil {
		t.Fatalf("didOpen: %v", notifyErr)
	}
	var symbols []struct {
		Name string `json:"name"`
	}
	if err := client.Request(ctx, "workspace/symbol", map[string]any{"query": "Target"}, &symbols); err != nil {
		t.Fatalf("workspace/symbol: %v", err)
	}
	if len(symbols) == 0 || symbols[0].Name != "Target" {
		t.Fatalf("workspace symbols = %#v", symbols)
	}

	targetOffset := strings.Index(string(source), "Target")
	position, positionErr := PositionForOffset(source, targetOffset)
	if positionErr != nil {
		t.Fatal(positionErr)
	}
	var hover map[string]any
	if err := client.Request(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": position,
	}, &hover); err != nil {
		t.Fatalf("textDocument/hover: %v", err)
	}
	if len(hover) == 0 {
		t.Fatal("textDocument/hover returned no source-grounded result")
	}

	var diagnostics struct {
		Kind  string `json:"kind"`
		Items []any  `json:"items"`
	}
	if err := client.Request(ctx, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &diagnostics); err != nil {
		t.Fatalf("textDocument/diagnostic: %v", err)
	}
	if diagnostics.Items == nil {
		t.Fatal("textDocument/diagnostic returned a null item collection")
	}
}
