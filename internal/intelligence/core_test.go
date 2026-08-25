package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

type fakeSemanticProvider struct {
	reader   *fakeSemanticReader
	identity SemanticIdentity
	reads    int
}

func (p *fakeSemanticProvider) Identity() SemanticIdentity { return p.identity }
func (p *fakeSemanticProvider) Read(_ context.Context, _ SnapshotRef, fn func(semanticReader) error) error {
	p.reads++
	return fn(p.reader)
}

type fakeSemanticReader struct {
	hover           string
	diagnostics     []Diagnostic
	search          semanticSymbols
	definitions     semanticLocations
	typeDefinitions semanticLocations
	references      semanticLocations
	implementations semanticSymbols
	calls           semanticCalls
	symbol          SymbolMatch
	position        Position
}

func (r *fakeSemanticReader) Search(context.Context, string) (semanticSymbols, error) {
	return r.search, nil
}

func (r *fakeSemanticReader) SymbolAt(_ context.Context, _ string, position Position) (SymbolMatch, error) {
	r.position = position
	return r.symbol, nil
}

func (r *fakeSemanticReader) Hover(context.Context, string, Position) (string, error) {
	return r.hover, nil
}

func (r *fakeSemanticReader) Definition(context.Context, string, Position) (semanticLocations, error) {
	return r.definitions, nil
}

func (r *fakeSemanticReader) TypeDefinition(context.Context, string, Position) (semanticLocations, error) {
	return r.typeDefinitions, nil
}

func (r *fakeSemanticReader) References(context.Context, string, Position) (semanticLocations, error) {
	return r.references, nil
}

func (r *fakeSemanticReader) Implementations(context.Context, string, Position) (semanticSymbols, error) {
	return r.implementations, nil
}

func (r *fakeSemanticReader) Diagnostics(context.Context, string) ([]Diagnostic, error) {
	return append([]Diagnostic(nil), r.diagnostics...), nil
}

func (r *fakeSemanticReader) Calls(context.Context, string, Position) (semanticCalls, error) {
	return r.calls, nil
}

type fakeChangeAnalyzer struct{ analysis verification.ChangeAnalysis }

func (a fakeChangeAnalyzer) Analyze(context.Context, verification.ChangeOptions) (verification.ChangeAnalysis, error) {
	if !a.analysis.Complete && a.analysis.Change.Files == nil {
		a.analysis.Complete = true
	}
	return a.analysis, nil
}

func (fakeChangeAnalyzer) MaterializeBase(context.Context, verification.Repository, string) (string, error) {
	return "", errors.New("not used")
}

type fakeVerifier struct{}

func (fakeVerifier) Verify(context.Context, verification.Request) (verification.Report, error) {
	return verification.Report{}, nil
}

func TestCoreSearchPaginatesStableSnapshotBoundSymbols(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{search: semanticSymbols{Items: []SymbolMatch{
		{Name: "Zulu", Qualified: "fixture.Zulu", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 4, Column: 1}},
		{Name: "Alpha", Qualified: "fixture.Alpha", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 2, Column: 1}},
	}}}
	core := newTestCore(t, snapshotter, reader)

	first, err := core.Search(context.Background(), SearchRequest{Query: "a", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || len(first.Matches) != 1 || first.Matches[0].Name != "Alpha" || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	identity, err := decodeSymbolRef(first.Matches[0].Ref)
	if err != nil || identity.SnapshotID != first.Snapshot.ID {
		t.Fatalf("symbol ref = %#v, %v", identity, err)
	}
	second, err := core.Search(context.Background(), SearchRequest{Query: "a", Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Matches) != 1 || second.Matches[0].Name != "Zulu" || second.Truncated || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}
	if core.semantic.(*fakeSemanticProvider).reads != 1 {
		t.Fatalf("semantic reads = %d, want cached continuation", core.semantic.(*fakeSemanticProvider).reads)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 101\n")
	if _, err := core.Search(context.Background(), SearchRequest{Query: "a", Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("stale continuation error = %v, want ErrSnapshotChanged", err)
	}
}

func TestCoreSymbolConvertsUTF8BytesAndReportsExternalOmissions(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "unicode.go", "package fixture\n\nvar πValue = 1\n")
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{
		symbol:     SymbolMatch{Name: "πValue", Qualified: "fixture.πValue", Kind: "go.variable", Package: "fixture", Location: Location{File: "unicode.go", Line: 3, Column: 5}},
		references: semanticLocations{Items: []Location{{File: "unicode_test.go", Line: 4, Column: 2}}, Omitted: 2},
	}
	core := newTestCore(t, snapshotter, reader)
	result, err := core.Symbol(context.Background(), SymbolRequest{Position: &SourcePosition{File: "unicode.go", Line: 3, Column: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if reader.position != (Position{Line: 2, Character: 5}) {
		t.Fatalf("LSP position = %#v, want line 2 character 5", reader.position)
	}
	if result.Symbol.Ref == "" || result.References.Total != 3 || len(result.RelatedTests.Items) != 1 || len(result.Uncertainties) == 0 {
		t.Fatalf("symbol context = %#v", result)
	}
}

func TestCoreRejectsStaleExpectedSnapshot(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	core := newTestCore(t, snapshotter, &fakeSemanticReader{})
	result, err := core.Search(context.Background(), SearchRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 100\n")
	_, err = core.Search(context.Background(), SearchRequest{Query: "x", ExpectedSnapshotID: result.Snapshot.ID})
	if !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("Search() error = %v, want ErrSnapshotChanged", err)
	}
}

func TestCoreBriefReturnsBoundedNonNullWorkspaceContext(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	core := newTestCore(t, snapshotter, &fakeSemanticReader{diagnostics: []Diagnostic{}})
	base := snapshotGit(t, root, "rev-parse", "HEAD")
	brief, err := core.Brief(context.Background(), BriefRequest{Base: base, MaxBytes: DefaultBriefBytes})
	if err != nil {
		t.Fatal(err)
	}
	if brief.SchemaVersion != ContextSchemaVersion || brief.Snapshot.ID == "" || len(brief.Modules) != 1 || len(brief.Packages) != 1 {
		t.Fatalf("brief identity = %#v", brief)
	}
	if brief.Modules == nil || brief.Packages == nil || brief.Symbols == nil || brief.Diagnostics == nil || brief.Guidance == nil || brief.Risks == nil || brief.Uncertainties == nil {
		t.Fatalf("brief contains nil collections: %#v", brief)
	}
	if brief.Change == nil || brief.Change.Files == nil || brief.Change.DirectUnits == nil || brief.Change.ReverseDependents == nil {
		t.Fatalf("change context = %#v", brief.Change)
	}
	encoded, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > DefaultBriefBytes {
		t.Fatalf("brief size = %d, maximum %d", len(encoded), DefaultBriefBytes)
	}
}

func TestCoreCapabilitiesAndArtifactCursor(t *testing.T) {
	snapshotter := newTestSnapshotter(t, snapshotRepository(t))
	core := newTestCore(t, snapshotter, &fakeSemanticReader{})
	capabilities := core.Capabilities()
	if capabilities.Provider.Version != "v0.21.0" || !capabilities.Semantic.WorkspaceSymbol || capabilities.ContextSchema != ContextSchemaVersion {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	artifact, err := core.artifacts.Put("snapshot", "detail", []byte("detail"))
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := EncodeArtifactCursor(artifact.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := core.ReadArtifact(context.Background(), cursor, 3)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Text != "det" || chunk.Complete || chunk.NextCursor == "" || chunk.SnapshotID != "snapshot" {
		t.Fatalf("chunk = %#v", chunk)
	}
}

func newTestCore(t *testing.T, snapshots *Snapshotter, reader *fakeSemanticReader) *Core {
	t.Helper()
	artifacts, err := NewArtifactStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	semantic := &fakeSemanticProvider{identity: SemanticIdentity{Version: "v0.21.0", Capabilities: CapabilityManifest{
		WorkspaceSymbol: true, DocumentSymbol: true, Hover: true, Definition: true,
		TypeDefinition: true, References: true, Implementation: true, Diagnostics: true,
	}}, reader: reader}
	core, err := newCore(snapshots.workspace, snapshots.runner, snapshots, semantic, artifacts, fakeChangeAnalyzer{}, fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func TestSourcePositionRejectsSplitUTF8Encoding(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value.go")
	if err := os.WriteFile(path, []byte("var π = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sourcePosition(path, SourcePosition{Line: 1, Column: 6}); err == nil {
		t.Fatal("sourcePosition accepted a byte offset inside UTF-8 encoding")
	}
}
