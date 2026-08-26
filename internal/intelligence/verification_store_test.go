package intelligence

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
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
	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), repositoryID, report); err != nil {
		t.Fatal(err)
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
	if _, err := store.Current(context.Background(), repositoryID); !errors.Is(err, ErrVerificationNotFound) {
		t.Fatalf("missing Current() error = %v", err)
	}
	report := verification.NewReport("0.7.0-test", verification.Repository{})
	report.Snapshot.CurrentID = "sha256:" + strings.Repeat("d", 64)
	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), repositoryID, report); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, strings.TrimPrefix(repositoryID, "sha256:"), report.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"status": "pass"`), []byte(`"status": "findings"`), 1)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Current(context.Background(), repositoryID); !errors.Is(err, ErrVerificationCorrupt) {
		t.Fatalf("tampered Current() error = %v", err)
	}
}
