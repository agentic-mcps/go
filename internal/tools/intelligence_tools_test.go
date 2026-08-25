package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/intelligence"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeIntelligence struct {
	brief  intelligence.BriefRequest
	search intelligence.SearchRequest
	symbol intelligence.SymbolRequest
}

func (f *fakeIntelligence) Brief(_ context.Context, request intelligence.BriefRequest) (intelligence.ContextPack, error) {
	f.brief = request
	return intelligence.ContextPack{Snapshot: intelligence.SnapshotRef{ID: "snap-brief"}, Packages: make([]intelligence.PackageSummary, 2)}, nil
}

func (f *fakeIntelligence) Search(_ context.Context, request intelligence.SearchRequest) (intelligence.SearchResult, error) {
	f.search = request
	return intelligence.SearchResult{Snapshot: intelligence.SnapshotRef{ID: "snap-search"}, Matches: []intelligence.SymbolMatch{{Name: "Widget"}}, Total: 1, Uncertainties: []intelligence.Uncertainty{}}, nil
}

func (f *fakeIntelligence) Symbol(_ context.Context, request intelligence.SymbolRequest) (intelligence.SymbolContext, error) {
	f.symbol = request
	return intelligence.SymbolContext{Snapshot: intelligence.SnapshotRef{ID: "snap-symbol"}, Symbol: intelligence.SymbolMatch{Name: "Widget"}}, nil
}

func (*fakeIntelligence) Capabilities() intelligence.Capabilities {
	return intelligence.Capabilities{ContextSchema: intelligence.ContextSchemaVersion}
}

func (*fakeIntelligence) ReadArtifact(_ context.Context, cursor string, _ int64) (intelligence.ArtifactChunk, error) {
	return intelligence.ArtifactChunk{ID: cursor, SnapshotID: "snapshot", Text: "detail", Complete: true}, nil
}

func testIntelligenceRuntime(service IntelligenceService) *Runtime {
	return &Runtime{intelligence: service}
}

func TestIntelligenceToolsMapRequestsAndReturnCanonicalResults(t *testing.T) {
	fake := &fakeIntelligence{}
	runtime := testIntelligenceRuntime(fake)
	ctx := context.Background()
	if _, got, err := runtime.workspaceBrief(ctx, nil, WorkspaceBriefInput{Base: "origin/main", Package: "./internal/...", MaxBytes: 4096}); err != nil || got.Snapshot.ID != "snap-brief" {
		t.Fatalf("brief = %#v, err %v", got, err)
	}
	if fake.brief.Base != "origin/main" || fake.brief.Scope != "./internal/..." || fake.brief.MaxBytes != 4096 {
		t.Fatalf("brief request = %#v", fake.brief)
	}
	if _, got, err := runtime.search(ctx, nil, SearchInput{Query: "Widget", Package: "./...", Limit: 7, Cursor: "cursor"}); err != nil || got.Snapshot.ID != "snap-search" {
		t.Fatalf("search = %#v, err %v", got, err)
	}
	if fake.search.Query != "Widget" || fake.search.Scope != "./..." || fake.search.Limit != 7 || fake.search.Cursor != "cursor" {
		t.Fatalf("search request = %#v", fake.search)
	}
	if _, got, err := runtime.symbolContext(ctx, nil, SymbolContextInput{SymbolRef: "ref", CallHierarchy: true, TypeDefinition: true, MaxBytes: 1000}); err != nil || got.Snapshot.ID != "snap-symbol" {
		t.Fatalf("symbol = %#v, err %v", got, err)
	}
	if fake.symbol.Ref != "ref" || !fake.symbol.Facets.CallHierarchy || !fake.symbol.Facets.TypeDefinition || fake.symbol.MaxBytes != 1000 {
		t.Fatalf("symbol request = %#v", fake.symbol)
	}
}

func TestIntelligenceToolsRejectInvalidInputAndMissingService(t *testing.T) {
	if _, _, err := testIntelligenceRuntime(nil).search(context.Background(), nil, SearchInput{}); err == nil {
		t.Fatal("search without service unexpectedly succeeded")
	}
	fake := &fakeIntelligence{}
	runtime := testIntelligenceRuntime(fake)
	if _, _, err := runtime.search(context.Background(), nil, SearchInput{Query: "  "}); err == nil {
		t.Fatal("blank query unexpectedly succeeded")
	}
	if _, _, err := runtime.workspaceBrief(context.Background(), nil, WorkspaceBriefInput{Base: " origin/main"}); err == nil {
		t.Fatal("whitespace-padded base unexpectedly succeeded")
	}
	if _, _, err := runtime.symbolContext(context.Background(), nil, SymbolContextInput{File: "main.go", Line: 0, Column: 1}); err == nil {
		t.Fatal("invalid source position unexpectedly succeeded")
	}
}

func TestIntelligenceResourcesReturnCapabilitiesAndArtifactChunks(t *testing.T) {
	runtime := testIntelligenceRuntime(&fakeIntelligence{})
	capabilities, err := runtime.capabilitiesResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: capabilitiesURI}})
	if err != nil {
		t.Fatal(err)
	}
	var manifest intelligence.Capabilities
	if decodeErr := json.Unmarshal([]byte(capabilities.Contents[0].Text), &manifest); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if manifest.ContextSchema != intelligence.ContextSchemaVersion {
		t.Fatalf("capabilities = %#v", manifest)
	}
	artifact, err := runtime.artifactResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://artifact/cursor_1"}})
	if err != nil {
		t.Fatal(err)
	}
	var chunk intelligence.ArtifactChunk
	if err := json.Unmarshal([]byte(artifact.Contents[0].Text), &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.ID != "cursor_1" || chunk.Text != "detail" {
		t.Fatalf("artifact = %#v", chunk)
	}
	if _, err := runtime.artifactResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "agentic-go://artifact/a/b"}}); err == nil {
		t.Fatal("artifact resource accepted a nested path")
	}
}
