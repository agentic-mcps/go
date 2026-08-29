package intelligence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	manager, err := gopls.NewManager(lifecycle, gopls.Config{Command: sidecar, Args: []string{"serve"}, Workspace: snapshots.workspace.Root(), ClientVersion: "test"})
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
	core, err := newCore(snapshots.workspace, snapshots.runner, snapshots, provider, artifacts, contracts, refactors, newTestVerificationStore(t), fakeChangeAnalyzer{}, fakeVerifier{})
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

func TestPinnedGoplsGuardedRefactorContract(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	if sidecar == "" {
		t.Skip("AGENTIC_GO_GOPLS is not set")
	}
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "rename.go", "package fixture\n\nvar RenameTarget = 1\nfunc UseRename() int { return RenameTarget }\n")
	writeSnapshotFile(t, root, "format.go", "package fixture\n\nvar Unformatted=1\n")
	writeSnapshotFile(t, root, "imports.go", "package fixture\n\nimport (\n\t\"strings\"\n\t\"fmt\"\n)\n\nfunc UseImports() string { return fmt.Sprint(strings.TrimSpace(\" value \")) }\n")
	snapshots := newTestSnapshotter(t, root)
	lifecycle, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	manager, err := gopls.NewManager(lifecycle, gopls.Config{Command: sidecar, Args: []string{"serve"}, Workspace: snapshots.workspace.Root(), ClientVersion: "test"})
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
	core, err := newCore(snapshots.workspace, snapshots.runner, snapshots, provider, artifacts, contracts, refactors, newTestVerificationStore(t), fakeChangeAnalyzer{}, fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 45*time.Second)
	defer stop()

	search, err := core.Search(ctx, SearchRequest{Query: "RenameTarget"})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Matches) != 1 {
		t.Fatalf("rename search = %#v", search)
	}
	rename, err := core.Refactor(ctx, RefactorRequest{
		Operation: RefactorRename, Ref: search.Matches[0].Ref, NewName: "RenamedTarget", ExpectedSnapshotID: search.Snapshot.ID,
	})
	if err != nil || rename.PlanID == "" || len(rename.AffectedFiles) != 1 {
		t.Fatalf("rename preview = %#v, error = %v", rename, err)
	}
	renamed, err := core.Refactor(ctx, RefactorRequest{PlanID: rename.PlanID, ExpectedSnapshotID: search.Snapshot.ID, Apply: true})
	if err != nil || !renamed.Applied {
		t.Fatalf("rename apply = %#v, error = %v", renamed, err)
	}

	format, err := core.Refactor(ctx, RefactorRequest{
		Operation: RefactorFormat, Files: []string{"format.go"}, ExpectedSnapshotID: renamed.Snapshot.ID,
	})
	if err != nil || format.PlanID == "" {
		t.Fatalf("format preview = %#v, error = %v", format, err)
	}
	formatted, err := core.Refactor(ctx, RefactorRequest{PlanID: format.PlanID, ExpectedSnapshotID: renamed.Snapshot.ID, Apply: true})
	if err != nil || !formatted.Applied {
		t.Fatalf("format apply = %#v, error = %v", formatted, err)
	}

	organized, err := core.Refactor(ctx, RefactorRequest{
		Operation: RefactorOrganizeImports, Files: []string{"imports.go"}, ExpectedSnapshotID: formatted.Snapshot.ID,
	})
	if err != nil {
		t.Fatalf("organize imports preview: %v", err)
	}
	current := organized.Snapshot
	if organized.PlanID != "" {
		applied, applyErr := core.Refactor(ctx, RefactorRequest{PlanID: organized.PlanID, ExpectedSnapshotID: formatted.Snapshot.ID, Apply: true})
		if applyErr != nil {
			t.Fatal(applyErr)
		}
		current = applied.Snapshot
	}
	if _, fixErr := core.Refactor(ctx, RefactorRequest{
		Operation: RefactorFixAll, Files: []string{"format.go"}, ExpectedSnapshotID: current.ID,
	}); fixErr != nil {
		t.Fatalf("fix-all preview: %v", fixErr)
	}

	renamedSource, err := os.ReadFile(filepath.Join(root, "rename.go"))
	if err != nil || !strings.Contains(string(renamedSource), "RenamedTarget") || strings.Contains(string(renamedSource), "RenameTarget") {
		t.Fatalf("renamed source = %q, error = %v", renamedSource, err)
	}
	formattedSource, err := os.ReadFile(filepath.Join(root, "format.go"))
	if err != nil || !strings.Contains(string(formattedSource), "var Unformatted = 1") {
		t.Fatalf("formatted source = %q, error = %v", formattedSource, err)
	}
}
