package intelligence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefactorStorePersistsContentAddressedPlan(t *testing.T) {
	store, err := NewRefactorStore(filepath.Join(t.TempDir(), "refactors"))
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("a", 64)
	plan := refactorPlan{
		SchemaVersion: refactorPlanSchemaVersion,
		RepositoryID:  repositoryID,
		Operation:     RefactorRename,
		Snapshot:      SnapshotRef{ID: "sha256:" + strings.Repeat("b", 64), RepositoryID: repositoryID},
		Files: []refactorFileEdit{{
			Path: "value.go", PreimageDigest: digestBytes([]byte("old\n")), PostimageDigest: digestBytes([]byte("new\n")),
			Preimage: []byte("old\n"), Postimage: []byte("new\n"),
		}},
		Diff: "--- a/value.go\n+++ b/value.go\n",
	}
	saved, err := store.savePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(saved.ID, "rfp_") || len(saved.ID) != len("rfp_")+64 {
		t.Fatalf("plan ID = %q", saved.ID)
	}
	loaded, err := store.loadPlan(context.Background(), repositoryID, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != saved.ID || string(loaded.Files[0].Postimage) != "new\n" {
		t.Fatalf("loaded plan = %#v", loaded)
	}
	info, err := os.Stat(store.planPath(repositoryID, saved.ID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("plan mode = %o", info.Mode().Perm())
	}
}

func TestRefactorStoreRejectsTamperedPlan(t *testing.T) {
	store, err := NewRefactorStore(filepath.Join(t.TempDir(), "refactors"))
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("c", 64)
	plan, err := store.savePlan(context.Background(), refactorPlan{
		SchemaVersion: refactorPlanSchemaVersion,
		RepositoryID:  repositoryID,
		Operation:     RefactorFormat,
		Snapshot:      SnapshotRef{ID: "sha256:" + strings.Repeat("d", 64), RepositoryID: repositoryID},
		Files:         []refactorFileEdit{{Path: "value.go", PreimageDigest: digestBytes([]byte("a")), PostimageDigest: digestBytes([]byte("b")), Preimage: []byte("a"), Postimage: []byte("b")}},
		Diff:          "diff",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := store.planPath(repositoryID, plan.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-2] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.loadPlan(context.Background(), repositoryID, plan.ID); !errors.Is(err, errRefactorPlanCorrupt) {
		t.Fatalf("loadPlan() error = %v, want errRefactorPlanCorrupt", err)
	}
}

func TestRefactorRecoveryRestoresOnlyJournaledPostimages(t *testing.T) {
	root := t.TempDir()
	store, err := NewRefactorStore(filepath.Join(t.TempDir(), "refactors"))
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("e", 64)
	first := filepath.Join(root, "first.go")
	second := filepath.Join(root, "second.go")
	if writeErr := os.WriteFile(first, []byte("new first\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if writeErr := os.WriteFile(second, []byte("old second\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	plan, err := store.savePlan(context.Background(), refactorPlan{
		SchemaVersion: refactorPlanSchemaVersion, RepositoryID: repositoryID, Operation: RefactorFormat,
		Snapshot: SnapshotRef{ID: "sha256:" + strings.Repeat("f", 64), RepositoryID: repositoryID},
		Files: []refactorFileEdit{
			{Path: "first.go", PreimageDigest: digestBytes([]byte("old first\n")), PostimageDigest: digestBytes([]byte("new first\n")), Preimage: []byte("old first\n"), Postimage: []byte("new first\n")},
			{Path: "second.go", PreimageDigest: digestBytes([]byte("old second\n")), PostimageDigest: digestBytes([]byte("new second\n")), Preimage: []byte("old second\n"), Postimage: []byte("new second\n")},
		}, Diff: "diff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if beginErr := store.beginRecovery(context.Background(), plan); beginErr != nil {
		t.Fatal(beginErr)
	}
	if beginErr := store.beginRecovery(context.Background(), plan); !errors.Is(beginErr, errRefactorRecoveryRequired) {
		t.Fatalf("second beginRecovery() error = %v, want errRefactorRecoveryRequired", beginErr)
	}
	recovered, err := store.recover(context.Background(), repositoryID, root)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered files = %d, want 1", recovered)
	}
	contents, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "old first\n" {
		t.Fatalf("first.go = %q", contents)
	}
	if _, pendingErr := store.pending(context.Background(), repositoryID); !errors.Is(pendingErr, errRefactorRecoveryNotFound) {
		t.Fatalf("pending() error = %v, want clean recovery state", pendingErr)
	}
}

func TestRefactorRecoveryRefusesDivergedFilesWithoutMutation(t *testing.T) {
	root := t.TempDir()
	store, err := NewRefactorStore(filepath.Join(t.TempDir(), "refactors"))
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("1", 64)
	path := filepath.Join(root, "value.go")
	if writeErr := os.WriteFile(path, []byte("user edit\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	plan, err := store.savePlan(context.Background(), refactorPlan{
		SchemaVersion: refactorPlanSchemaVersion, RepositoryID: repositoryID, Operation: RefactorFormat,
		Snapshot: SnapshotRef{ID: "sha256:" + strings.Repeat("2", 64), RepositoryID: repositoryID},
		Files: []refactorFileEdit{{
			Path: "value.go", PreimageDigest: digestBytes([]byte("old\n")), PostimageDigest: digestBytes([]byte("new\n")), Preimage: []byte("old\n"), Postimage: []byte("new\n"),
		}}, Diff: "diff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if beginErr := store.beginRecovery(context.Background(), plan); beginErr != nil {
		t.Fatal(beginErr)
	}
	if _, recoverErr := store.recover(context.Background(), repositoryID, root); !errors.Is(recoverErr, errRefactorRecoveryDiverged) {
		t.Fatalf("recover() error = %v, want errRefactorRecoveryDiverged", recoverErr)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "user edit\n" {
		t.Fatalf("diverged file mutated to %q", contents)
	}
	if _, pendingErr := store.pending(context.Background(), repositoryID); pendingErr != nil {
		t.Fatalf("journal was discarded: %v", pendingErr)
	}
}
