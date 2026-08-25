package intelligence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactStorePermissionsAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := NewArtifactStore(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Put("snap-1", "brief:q", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.Stat(filepath.Join(root, "artifacts")); err != nil || got.Mode().Perm() != 0700 {
		t.Fatalf("root mode: %v %v", got, err)
	}
	info, err := os.Stat(filepath.Join(root, "artifacts", a.ID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
	got, err := s.Get(a.ID, "snap-1", "brief:q")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Payload) != "hello" || got.ID != a.ID {
		t.Fatalf("got %+v", got)
	}
}

func TestArtifactIDAndBinding(t *testing.T) {
	s, _ := NewArtifactStore(t.TempDir())
	a, _ := s.Put("s", "k", []byte("p"))
	b, _ := s.Put("s", "k", []byte("p"))
	if a.ID != b.ID {
		t.Fatal("identical artifacts must have identical IDs")
	}
	if _, err := s.Get(a.ID, "other", "k"); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("snapshot mismatch: %v", err)
	}
	if _, err := s.Get(a.ID, "s", "other"); !errors.Is(err, ErrArtifactMismatch) {
		t.Fatalf("key mismatch: %v", err)
	}
}

func TestArtifactCorruptionAndAtomicReplacement(t *testing.T) {
	s, _ := NewArtifactStore(t.TempDir())
	a, _ := s.Put("s", "k", []byte("old"))
	if _, err := s.Put("s", "k", []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(a.ID, "s", "k")
	if err != nil || string(got.Payload) != "old" {
		t.Fatalf("content-addressed replacement: %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(s.root, a.ID), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID, "s", "k"); !errors.Is(err, ErrArtifactCorrupt) {
		t.Fatalf("corruption: %v", err)
	}
}

func TestArtifactCursorRoundTrip(t *testing.T) {
	s, _ := NewArtifactStore(t.TempDir())
	a, _ := s.Put("s", "k", []byte("p"))
	c, err := EncodeArtifactCursor(a.ID, 42)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeArtifactCursor(c, a.ID)
	if err != nil || got.ID != a.ID || got.Offset != 42 {
		t.Fatalf("cursor: %+v %v", got, err)
	}
	if _, err := DecodeArtifactCursor(c, "other"); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("cursor mismatch: %v", err)
	}
	if _, err := EncodeArtifactCursor(a.ID, -1); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("negative cursor: %v", err)
	}
}
