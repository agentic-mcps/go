package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/intelligence"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeIntelligence struct {
	brief      intelligence.BriefRequest
	search     intelligence.SearchRequest
	symbol     intelligence.SymbolRequest
	begin      intelligence.BeginRequest
	checkpoint intelligence.CheckpointRequest
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

func (f *fakeIntelligence) Begin(_ context.Context, request intelligence.BeginRequest) (intelligence.ChangeContract, error) {
	f.begin = request
	return intelligence.ChangeContract{ID: "chg_1", LatestSnapshot: intelligence.SnapshotRef{ID: "snap-begin"}}, nil
}

func (f *fakeIntelligence) Checkpoint(_ context.Context, request intelligence.CheckpointRequest) (intelligence.Checkpoint, error) {
	f.checkpoint = request
	return intelligence.Checkpoint{ID: "cp_1", ContractID: request.ContractID, Current: intelligence.SnapshotRef{ID: "snap-checkpoint"}, Complete: true, AffectedPackages: []string{}, Diagnostics: []intelligence.Diagnostic{}, Violations: []intelligence.PolicyViolation{}, Uncertainties: []intelligence.Uncertainty{}}, nil
}

func (*fakeIntelligence) Capabilities() intelligence.Capabilities {
	return intelligence.Capabilities{ContextSchema: intelligence.ContextSchemaVersion}
}

func (*fakeIntelligence) ReadArtifact(_ context.Context, cursor string, _ int64) (intelligence.ArtifactChunk, error) {
	return intelligence.ArtifactChunk{ID: cursor, SnapshotID: "snapshot", Text: "detail", Complete: true}, nil
}

func (*fakeIntelligence) CurrentChangeContract(context.Context) (intelligence.ChangeContract, error) {
	return intelligence.ChangeContract{ID: "chg_current", Goal: "private goal", Checkpoints: []intelligence.CheckpointRef{}}, nil
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

func TestChangeToolsMapRequestsAndReturnCanonicalResults(t *testing.T) {
	fake := &fakeIntelligence{}
	runtime := testIntelligenceRuntime(fake)
	ctx := context.Background()
	policies := intelligence.StructuralPolicies{GeneratedFile: intelligence.PolicyAllow}
	if _, got, err := runtime.beginChange(ctx, nil, BeginChangeInput{
		Base: "origin/main", Goal: "preserve the goal exactly", Package: "./internal/...",
		FocusedPaths: []string{"internal/tools"}, FocusedPackages: []string{"example.test/tools"},
		FocusedSymbols: []string{"symbol-ref"}, AllowedPaths: []string{"internal"}, Policies: policies,
	}); err != nil || got.ID != "chg_1" {
		t.Fatalf("begin = %#v, err %v", got, err)
	}
	if fake.begin.Base != "origin/main" || fake.begin.Goal != "preserve the goal exactly" || fake.begin.Scope != "./internal/..." ||
		len(fake.begin.FocusedSymbols) != 1 || fake.begin.FocusedSymbols[0] != "symbol-ref" || fake.begin.Policies.GeneratedFile != intelligence.PolicyAllow {
		t.Fatalf("begin request = %#v", fake.begin)
	}
	if _, got, err := runtime.checkpointChange(ctx, nil, CheckpointChangeInput{
		ContractID: "chg_1", ExpectedSnapshotID: "snap-begin",
		Decisions: []string{"decision"}, UnresolvedQuestions: []string{"question"},
	}); err != nil || got.ID != "cp_1" {
		t.Fatalf("checkpoint = %#v, err %v", got, err)
	}
	if fake.checkpoint.ContractID != "chg_1" || fake.checkpoint.ExpectedSnapshot != "snap-begin" || len(fake.checkpoint.Decisions) != 1 {
		t.Fatalf("checkpoint request = %#v", fake.checkpoint)
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
	if _, _, err := runtime.beginChange(context.Background(), nil, BeginChangeInput{Base: "-main", Goal: "goal"}); err == nil {
		t.Fatal("invalid change base unexpectedly succeeded")
	}
	if _, _, err := runtime.beginChange(context.Background(), nil, BeginChangeInput{Base: "main", Goal: "  "}); err == nil {
		t.Fatal("blank change goal unexpectedly succeeded")
	}
	if _, _, err := runtime.checkpointChange(context.Background(), nil, CheckpointChangeInput{ContractID: "chg_1"}); err == nil {
		t.Fatal("missing expected snapshot unexpectedly succeeded")
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
	contractResource, err := runtime.currentChangeContractResource(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: changeContractCurrentURI}})
	if err != nil {
		t.Fatal(err)
	}
	var contract intelligence.ChangeContract
	if err := json.Unmarshal([]byte(contractResource.Contents[0].Text), &contract); err != nil || contract.ID != "chg_current" || contract.Goal != "private goal" {
		t.Fatalf("current contract = %#v, error %v", contract, err)
	}
}
