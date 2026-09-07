package intelligence

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxFocusPackEntries = 32
	maxFocusPackBytes   = 2 << 20
	focusPackTTL        = 24 * time.Hour
)

var (
	// ErrArtifactNotFound means a cursor references no retained artifact.
	ErrArtifactNotFound = errors.New("artifact not found")
	// ErrArtifactMismatch means an artifact belongs to another snapshot or operation.
	ErrArtifactMismatch = errors.New("artifact binding mismatch")
	// ErrArtifactCorrupt means persisted artifact metadata failed validation.
	ErrArtifactCorrupt = errors.New("artifact corrupt")
	// ErrCursorInvalid means a continuation cursor is malformed or mismatched.
	ErrCursorInvalid = errors.New("invalid artifact cursor")
)

// ArtifactStore persists private, content-addressed context details.
type ArtifactStore struct{ root string }

// Artifact is one normalized payload bound to a snapshot and operation key.
type Artifact struct {
	ID         string `json:"id"`
	SnapshotID string `json:"snapshot_id"`
	Key        string `json:"key"`
	Payload    []byte `json:"payload"`
}

type artifactDisk struct {
	ID         string `json:"id"`
	SnapshotID string `json:"snapshot_id"`
	Key        string `json:"key"`
	Payload    []byte `json:"payload"`
	Version    int    `json:"version"`
}

// ArtifactCursor is the decoded position within one stored artifact.
type ArtifactCursor struct {
	ID     string
	Offset int64
}

type cursorDisk struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
}

// NewArtifactStore opens an explicit root or the default private user cache.
func NewArtifactStore(root string) (*ArtifactStore, error) {
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("cache directory: %w", err)
		}
		root = filepath.Join(cache, "agentic-go", "artifacts")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create artifact store: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure artifact store: %w", err)
	}
	return &ArtifactStore{root: root}, nil
}

// Put atomically persists one snapshot-bound normalized payload.
func (s *ArtifactStore) Put(snapshotID, key string, payload []byte) (Artifact, error) {
	if s == nil || s.root == "" || snapshotID == "" || key == "" {
		return Artifact{}, errors.New("invalid artifact binding")
	}
	id := artifactID(snapshotID, key, payload)
	a := artifactDisk{Version: 1, ID: id, SnapshotID: snapshotID, Key: key, Payload: append([]byte(nil), payload...)}
	b, err := json.Marshal(a)
	if err != nil {
		return Artifact{}, err
	}
	if err := atomicWrite(filepath.Join(s.root, id), b); err != nil {
		return Artifact{}, err
	}
	return Artifact{ID: id, SnapshotID: snapshotID, Key: key, Payload: append([]byte(nil), payload...)}, nil
}

// Get loads an artifact only when its snapshot and operation key match.
func (s *ArtifactStore) Get(id, expectedSnapshotID, expectedKey string) (Artifact, error) {
	a, err := s.getVerified(id)
	if err != nil {
		return Artifact{}, err
	}
	if a.SnapshotID != expectedSnapshotID || a.Key != expectedKey {
		return Artifact{}, ErrArtifactMismatch
	}
	return a, nil
}

func (s *ArtifactStore) getVerified(id string) (Artifact, error) {
	if s == nil || s.root == "" {
		return Artifact{}, ErrArtifactNotFound
	}
	if !validID(id) {
		return Artifact{}, ErrArtifactNotFound
	}
	b, err := os.ReadFile(filepath.Join(s.root, id))
	if errors.Is(err, os.ErrNotExist) {
		return Artifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return Artifact{}, err
	}
	var a artifactDisk
	if json.Unmarshal(b, &a) != nil || a.Version != 1 || a.ID != id || a.SnapshotID == "" || a.Key == "" || !validID(id) {
		return Artifact{}, ErrArtifactCorrupt
	}
	if artifactID(a.SnapshotID, a.Key, a.Payload) != id {
		return Artifact{}, ErrArtifactCorrupt
	}
	return Artifact{ID: a.ID, SnapshotID: a.SnapshotID, Key: a.Key, Payload: append([]byte(nil), a.Payload...)}, nil
}

func (s *ArtifactStore) getFocusPack(id string) (Artifact, error) {
	info, err := os.Stat(filepath.Join(s.root, id))
	if errors.Is(err, os.ErrNotExist) || (err == nil && time.Since(info.ModTime()) > focusPackTTL) {
		return Artifact{}, fmt.Errorf("focus pack metadata expired: %w", ErrArtifactNotFound)
	}
	if err != nil {
		return Artifact{}, err
	}
	artifact, err := s.getVerified(id)
	if err != nil {
		return Artifact{}, err
	}
	if artifact.Key != "focus-pack/v1" {
		return Artifact{}, ErrArtifactMismatch
	}
	return artifact, nil
}

func (s *ArtifactStore) pruneFocusPacks() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	type candidate struct {
		modified time.Time
		path     string
		size     int64
	}
	items := []candidate{}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		artifact, readErr := s.getVerified(entry.Name())
		if readErr != nil || artifact.Key != "focus-pack/v1" {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		items = append(items, candidate{modified: info.ModTime(), path: filepath.Join(s.root, entry.Name()), size: info.Size()})
		total += info.Size()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].modified.Before(items[j].modified) })
	for len(items) > maxFocusPackEntries || total > maxFocusPackBytes {
		item := items[0]
		items = items[1:]
		total -= item.size
		if err := os.Remove(item.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// EncodeArtifactCursor creates an opaque cursor for one artifact offset.
func EncodeArtifactCursor(id string, offset int64) (string, error) {
	if !validID(id) || offset < 0 {
		return "", ErrCursorInvalid
	}
	b, err := json.Marshal(cursorDisk{ID: id, Offset: offset})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeArtifactCursor validates an opaque cursor and optional artifact ID.
func DecodeArtifactCursor(cursor, expectedID string) (ArtifactCursor, error) {
	if cursor == "" {
		return ArtifactCursor{}, ErrCursorInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return ArtifactCursor{}, ErrCursorInvalid
	}
	var c cursorDisk
	if json.Unmarshal(b, &c) != nil || !validID(c.ID) || c.Offset < 0 || (expectedID != "" && c.ID != expectedID) {
		return ArtifactCursor{}, ErrCursorInvalid
	}
	return ArtifactCursor(c), nil
}

func artifactID(snapshotID, key string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(snapshotID))
	h.Write([]byte{0})
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func validID(id string) bool {
	return len(id) == sha256.Size*2 && strings.Trim(id, "0123456789abcdef") == ""
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".artifact-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err == nil {
		err = d.Sync()
		closeErr := d.Close()
		if err == nil {
			err = closeErr
		}
	}
	return err
}
