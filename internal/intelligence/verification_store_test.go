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

	"github.com/agentic-mcps/go/internal/verification"
)

func TestVerificationStorePersistsPrivateContentAddressedReport(t *testing.T) {
	root := filepath.Join(t.TempDir(), "verifications")
	store, err := NewVerificationStore(root)
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("a", 64)
	report := verification.NewReport("0.7.0-test", verification.Repository{})
	report.Snapshot.CurrentID = "sha256:" + strings.Repeat("b", 64)
	if finalizeErr := report.Finalize(verification.Policy{}); finalizeErr != nil {
		t.Fatal(finalizeErr)
	}
	if saveErr := store.Save(context.Background(), repositoryID, report); saveErr != nil {
		t.Fatal(saveErr)
	}
	current, err := store.Current(context.Background(), repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, report) {
		t.Fatalf("current report differs: got %#v want %#v", current, report)
	}
	repositoryDir := filepath.Join(root, strings.TrimPrefix(repositoryID, "sha256:"))
	for _, path := range []string{repositoryDir, filepath.Join(repositoryDir, report.ID+".json"), filepath.Join(repositoryDir, "latest.json")} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.IsDir() && info.Mode().Perm() != 0o700 || !info.IsDir() && info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", path, info.Mode().Perm())
		}
	}
}

func TestVerificationStoreRejectsTamperedAndMissingState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "verifications")
	store, err := NewVerificationStore(root)
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("c", 64)
	if _, currentErr := store.Current(context.Background(), repositoryID); !errors.Is(currentErr, ErrVerificationNotFound) {
		t.Fatalf("missing Current() error = %v", currentErr)
	}
	report := verification.NewReport("0.7.0-test", verification.Repository{})
	report.Snapshot.CurrentID = "sha256:" + strings.Repeat("d", 64)
	if finalizeErr := report.Finalize(verification.Policy{}); finalizeErr != nil {
		t.Fatal(finalizeErr)
	}
	if saveErr := store.Save(context.Background(), repositoryID, report); saveErr != nil {
		t.Fatal(saveErr)
	}
	path := filepath.Join(root, strings.TrimPrefix(repositoryID, "sha256:"), report.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"status": "pass"`), []byte(`"status": "findings"`), 1)
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, currentErr := store.Current(context.Background(), repositoryID); !errors.Is(currentErr, ErrVerificationCorrupt) {
		t.Fatalf("tampered Current() error = %v", currentErr)
	}
}

func TestVerificationStoreReadsArchivedBetaWithoutRewriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "verifications")
	store, err := NewVerificationStore(root)
	if err != nil {
		t.Fatal(err)
	}
	repositoryID := "sha256:" + strings.Repeat("e", 64)
	reportBytes, err := os.ReadFile(filepath.Join("..", "verification", "testdata", "archive", "v1beta1", "report-pass.json"))
	if err != nil {
		t.Fatal(err)
	}
	var archived verification.Report
	if decodeErr := json.Unmarshal(reportBytes, &archived); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if archived.SchemaVersion != verification.ArchivedBetaSchemaVersion || archived.ValidateStoredID() != nil {
		t.Fatal("archived beta fixture has invalid identity")
	}
	repositoryDir := filepath.Join(root, strings.TrimPrefix(repositoryID, "sha256:"))
	if mkdirErr := os.MkdirAll(repositoryDir, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	reportPath := filepath.Join(repositoryDir, archived.ID+".json")
	if writeErr := os.WriteFile(reportPath, reportBytes, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	pointer, err := json.Marshal(latestVerification{ID: archived.ID})
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(filepath.Join(repositoryDir, "latest.json"), append(pointer, '\n'), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	current, err := store.Current(context.Background(), repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if current.SchemaVersion != verification.ArchivedBetaSchemaVersion || current.ID != archived.ID {
		t.Fatalf("archived report identity = %q/%q", current.SchemaVersion, current.ID)
	}
	after, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, reportBytes) {
		t.Fatal("reading an archived report rewrote private state")
	}
	if err := store.Save(context.Background(), repositoryID, current); !errors.Is(err, ErrVerificationCorrupt) {
		t.Fatalf("saving archived report error = %v, want corrupt-state rejection", err)
	}
}
