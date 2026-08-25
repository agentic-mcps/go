package gopls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocatePrefersPinnedSibling(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "agentic-go")
	writeExecutable(t, host, "#!/bin/sh\nexit 0\n")
	sibling := filepath.Join(root, "agentic-go-gopls")
	writeExecutable(t, sibling, "#!/bin/sh\nprintf 'golang.org/x/tools/gopls v0.21.0\\n'\n")
	pathDir := t.TempDir()
	writeExecutable(t, filepath.Join(pathDir, "agentic-go-gopls"), "#!/bin/sh\nprintf 'golang.org/x/tools/gopls v0.20.0\\n'\n")
	t.Setenv("PATH", pathDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	installation, err := Locate(ctx, host, "")
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if installation.Path != wantPath || installation.Version != SupportedVersion || !installation.Bundled {
		t.Fatalf("installation = %#v", installation)
	}
}

func TestLocateRejectsUnpinnedOverride(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "gopls")
	writeExecutable(t, candidate, "#!/bin/sh\nprintf 'golang.org/x/tools/gopls v0.22.0\\n'\n")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Locate(ctx, filepath.Join(root, "agentic-go"), candidate)
	if err == nil || !strings.Contains(err.Error(), SupportedVersion) || !strings.Contains(err.Error(), "v0.22.0") {
		t.Fatalf("Locate() error = %v", err)
	}
}

func TestLocateExplainsMissingCompanion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Locate(ctx, filepath.Join(root, "agentic-go"), "")
	if err == nil || !strings.Contains(err.Error(), "agentic-go-gopls") || !strings.Contains(err.Error(), SupportedVersion) {
		t.Fatalf("Locate() error = %v", err)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
