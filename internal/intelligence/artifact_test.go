package intelligence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactReadChunkIsUTF8SafeAndCursorBound(t *testing.T) {
	s, _ := NewArtifactStore(t.TempDir())
	a, _ := s.Put("snap", "context", []byte("aé🙂z"))
	got, err := s.ReadChunk(context.Background(), a.ID, "", 0, 3)
	if err != nil || got.Text != "aé" || got.Offset != 0 || got.TotalBytes != 8 || got.Complete || got.NextCursor == "" {
		t.Fatalf("first chunk: %+v %v", got, err)
	}
	next, err := s.ReadChunk(context.Background(), a.ID, got.NextCursor, 999, 4)
	if err != nil || next.Text != "🙂" || next.Offset != 3 || next.Complete {
		t.Fatalf("second chunk: %+v %v", next, err)
	}
	last, err := s.ReadChunk(context.Background(), a.ID, next.NextCursor, 0, 64)
	if err != nil || last.Text != "z" || !last.Complete || last.NextCursor != "" {
		t.Fatalf("last chunk: %+v %v", last, err)
	}
	end, err := s.ReadChunk(context.Background(), a.ID, "", int64(len(a.Payload)), 1)
	if err != nil || !end.Complete || end.Text != "" {
		t.Fatalf("end chunk: %+v %v", end, err)
	}
}

func TestArtifactReadChunkRejectsInvalidRequests(t *testing.T) {
	s, _ := NewArtifactStore(t.TempDir())
	a, _ := s.Put("snap", "context", []byte("é"))
	cases := []struct {
		fn   func() error
		name string
	}{
		{name: "id", fn: func() error {
			_, err := s.ReadChunk(context.Background(), "bad", "", 0, 1)
			return err
		}},
		{name: "offset", fn: func() error {
			_, err := s.ReadChunk(context.Background(), a.ID, "", 1, 1)
			return err
		}},
		{name: "negative", fn: func() error {
			_, err := s.ReadChunk(context.Background(), a.ID, "", -1, 1)
			return err
		}},
		{name: "limit", fn: func() error {
			_, err := s.ReadChunk(context.Background(), a.ID, "", 0, 0)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ReadChunk(cancelled, a.ID, "", 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

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
	if got, statErr := os.Stat(filepath.Join(root, "artifacts")); statErr != nil || got.Mode().Perm() != 0o700 {
		t.Fatalf("root mode: %v %v", got, statErr)
	}
	info, err := os.Stat(filepath.Join(root, "artifacts", a.ID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
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
	if err := os.WriteFile(filepath.Join(s.root, a.ID), []byte("broken"), 0o600); err != nil {
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
