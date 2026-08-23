package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndResolve(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "internal", "value.go"), "package internal\n")

	ws, err := Open(context.Background(), filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	got, err := ws.Resolve("value.go")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(root, "internal", "value.go"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolveRejectsEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside.go")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, outside, "package outside\n")

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	for _, path := range []string{"../outside.go", outside} {
		t.Run(path, func(t *testing.T) {
			if _, err := ws.Resolve(path); err == nil {
				t.Fatalf("Resolve(%q) succeeded, want containment error", path)
			}
		})
	}
}

func TestResolveRejectsEscapingSymlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside.go")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, outside, "package outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := ws.Resolve("link.go"); err == nil {
		t.Fatal("Resolve() succeeded for symlink outside workspace")
	}
}

func TestOpenRejectsNonWorkspace(t *testing.T) {
	if _, err := Open(context.Background(), t.TempDir()); err == nil {
		t.Fatal("Open() succeeded without go.mod or go.work")
	}
}

func TestOpenAcceptsGoWorkRoot(t *testing.T) {
	root := t.TempDir()
	moduleRoot := filepath.Join(root, "module")
	writeFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "go.work"), "go 1.25.0\n\nuse ./module\n")

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if ws.Root() == "" {
		t.Fatal("Open() returned an empty root")
	}
}

func TestOpenHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Open(ctx, root); err == nil {
		t.Fatal("Open() succeeded with a cancelled context")
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
