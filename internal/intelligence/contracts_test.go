package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultStructuralPoliciesAndValidation(t *testing.T) {
	want := StructuralPolicies{
		OutsideAllowedPaths: PolicyForbid,
		OutsideFocus:        PolicyWarn,
		ExportedAPI:         PolicyWarn,
		Dependency:          PolicyWarn,
		CrossModule:         PolicyWarn,
		GeneratedFile:       PolicyForbid,
		TestDeletion:        PolicyWarn,
	}
	if got := DefaultStructuralPolicies(); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %#v, want %#v", got, want)
	}

	partial := StructuralPolicies{ExportedAPI: PolicyAllow}
	if err := normalizeStructuralPolicies(&partial); err != nil {
		t.Fatal(err)
	}
	if partial.ExportedAPI != PolicyAllow || partial.GeneratedFile != PolicyForbid {
		t.Fatalf("normalized policies = %#v", partial)
	}

	invalid := DefaultStructuralPolicies()
	invalid.Dependency = "block"
	if err := normalizeStructuralPolicies(&invalid); err == nil || !strings.Contains(err.Error(), "dependency") {
		t.Fatalf("invalid policy error = %v", err)
	}
}

func TestContractStoreRoundTripAndRepositoryIsolation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "contracts")
	store, err := NewContractStore(root)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, time.August, 25, 12, 34, 56, 0, time.UTC)
	repositoryID := "sha256:" + strings.Repeat("a", 64)
	contractID := "chg_" + strings.Repeat("b", 64)
	contract := ChangeContract{
		SchemaVersion:       ChangeSchemaVersion,
		ID:                  contractID,
		RepositoryID:        repositoryID,
		Goal:                "keep the adapter deterministic",
		Base:                "main",
		Scope:               "./...",
		InitialSnapshot:     SnapshotRef{ID: "sha256:" + strings.Repeat("c", 64), RepositoryID: repositoryID},
		LatestSnapshot:      SnapshotRef{ID: "sha256:" + strings.Repeat("c", 64), RepositoryID: repositoryID},
		FocusedPaths:        []string{},
		FocusedPackages:     []string{},
		FocusedSymbols:      []SymbolRef{},
		AllowedPaths:        []string{},
		Policies:            DefaultStructuralPolicies(),
		Decisions:           []Decision{},
		UnresolvedQuestions: []string{},
		Checkpoints:         []CheckpointRef{},
		Active:              true,
		CreatedAt:           created,
		UpdatedAt:           created,
	}
	if saveErr := store.Save(context.Background(), contract); saveErr != nil {
		t.Fatal(saveErr)
	}

	got, err := store.Load(context.Background(), repositoryID, contractID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, contract) {
		t.Fatalf("loaded contract = %#v, want %#v", got, contract)
	}
	current, err := store.Current(context.Background(), repositoryID)
	if err != nil || current.ID != contractID {
		t.Fatalf("current = %#v, error %v", current, err)
	}

	repositoryDir := filepath.Join(root, strings.Repeat("a", 64))
	if info, statErr := os.Stat(repositoryDir); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("repository directory mode: %v, error %v", info, statErr)
	}
	if info, statErr := os.Stat(filepath.Join(repositoryDir, contractID+".json")); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("contract mode: %v, error %v", info, statErr)
	}
	if _, err := store.Load(context.Background(), "sha256:"+strings.Repeat("d", 64), contractID); !errors.Is(err, ErrContractNotFound) {
		t.Fatalf("cross-repository load error = %v", err)
	}
}

func TestContractStoreRejectsCorruptionAndCancellation(t *testing.T) {
	root := t.TempDir()
	store, err := NewContractStore(root)
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("1", 64)
	contractID := "chg_" + strings.Repeat("2", 64)
	repositoryDir := filepath.Join(root, strings.Repeat("1", 64))
	if mkdirErr := os.MkdirAll(repositoryDir, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	if err := os.WriteFile(filepath.Join(repositoryDir, contractID+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), repositoryID, contractID); !errors.Is(err, ErrContractCorrupt) {
		t.Fatalf("corrupt load error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(cancelled, repositoryID, contractID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load error = %v", err)
	}
}

func TestContractStorePromotesAlphaOnExplicitSave(t *testing.T) {
	root := t.TempDir()
	store, err := NewContractStore(root)
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("3", 64)
	contractID := "chg_" + strings.Repeat("4", 64)
	recorded := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	contract := ChangeContract{
		SchemaVersion: changeAlphaSchemaVersion, ID: contractID, RepositoryID: repositoryID,
		Goal: "preserve upgrade behavior", Base: "main", Scope: "./...",
		InitialSnapshot: SnapshotRef{ID: "sha256:" + strings.Repeat("5", 64), RepositoryID: repositoryID},
		LatestSnapshot:  SnapshotRef{ID: "sha256:" + strings.Repeat("5", 64), RepositoryID: repositoryID},
		FocusedPaths:    []string{}, FocusedPackages: []string{}, FocusedSymbols: []SymbolRef{}, AllowedPaths: []string{},
		Policies: DefaultStructuralPolicies(), Decisions: []Decision{}, UnresolvedQuestions: []string{}, Checkpoints: []CheckpointRef{},
		Active: true, CreatedAt: recorded, UpdatedAt: recorded,
	}
	repositoryDir := filepath.Join(root, strings.Repeat("3", 64))
	if mkdirErr := os.MkdirAll(repositoryDir, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	encoded, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repositoryDir, contractID+".json")
	alphaBytes := append(encoded, '\n')
	if writeErr := os.WriteFile(path, alphaBytes, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	loaded, err := store.Load(context.Background(), repositoryID, contractID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != ChangeSchemaVersion {
		t.Fatalf("loaded schema = %q, want %q", loaded.SchemaVersion, ChangeSchemaVersion)
	}
	afterLoad, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterLoad, alphaBytes) {
		t.Fatal("loading an alpha contract rewrote private state")
	}
	if saveErr := store.Save(context.Background(), loaded); saveErr != nil {
		t.Fatal(saveErr)
	}
	afterSave, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(afterSave, []byte(changeAlphaSchemaVersion)) || !bytes.Contains(afterSave, []byte(ChangeSchemaVersion)) {
		t.Fatal("explicit save did not promote the contract to v1")
	}
}
