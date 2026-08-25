package intelligence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/gopls"
)

func TestPinnedGoplsNormalizedIntelligenceContract(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	if sidecar == "" {
		t.Skip("AGENTIC_GO_GOPLS is not set")
	}
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "semantic_unicode.go", "package fixture\n\nvar πTarget = 1\n")
	snapshots := newTestSnapshotter(t, root)
	lifecycle, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager, err := gopls.NewManager(lifecycle, gopls.Config{Command: sidecar, Args: []string{"serve"}, Workspace: snapshots.workspace.Root()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = manager.Close(ctx)
	})
	provider, err := newGoplsProvider(manager, snapshots.workspace, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := NewArtifactStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := NewContractStore(filepath.Join(t.TempDir(), "contracts"))
	if err != nil {
		t.Fatal(err)
	}
	refactors, err := NewRefactorStore(filepath.Join(t.TempDir(), "refactors"))
	if err != nil {
		t.Fatal(err)
	}
	core, err := newCore(snapshots.workspace, snapshots.runner, snapshots, provider, artifacts, contracts, refactors, fakeChangeAnalyzer{}, fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	search, err := core.Search(ctx, SearchRequest{Query: "πTarget"})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Matches) != 1 || search.Matches[0].Name != "πTarget" || search.Matches[0].Location.File != "semantic_unicode.go" || search.Matches[0].Location.Column != 5 {
		t.Fatalf("normalized search = %#v", search)
	}
	symbol, err := core.Symbol(ctx, SymbolRequest{Ref: search.Matches[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	if symbol.Symbol.Name != "πTarget" || symbol.Symbol.Location.Column != 5 || symbol.Hover == "" {
		t.Fatalf("normalized symbol = %#v", symbol)
	}
}
