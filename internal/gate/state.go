package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	stateKeepResults    = 20
	stateSessionMaxAge  = 7 * 24 * time.Hour
	stateTempMaxAge     = time.Hour
	stateTempPrefix     = ".tmp-"
	stateLockPoll       = 50 * time.Millisecond
	stateStoreDirPerm   = 0o700
	stateStoreFilePerm  = 0o600
	stateDirNameHexLen  = 16
	stateSessionHexLen  = 32
	stateFileExtension  = ".json"
	stateResultsSubdir  = "results"
	stateSessionsSubdir = "sessions"
)

var stateSafeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Fingerprint identifies the exact inputs of a gate run: the caller's salt
// (gate version and options), the base commit, HEAD, git's view of the working
// tree, and the content of every path git reports as changed.
func Fingerprint(ctx context.Context, git GitFunc, root, baseCommit, salt string) (string, error) {
	head, err := git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolving HEAD: %w", err)
	}
	prefix, err := git(ctx, "rev-parse", "--show-prefix")
	if err != nil {
		return "", fmt.Errorf("locating workspace in repository: %w", err)
	}
	// Scope status to the workspace: sibling directories of a monorepo must
	// neither be hashed nor invalidate the cache.
	status, err := git(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	if err != nil {
		return "", fmt.Errorf("reading working tree status: %w", err)
	}
	// git status prints paths relative to the repository root, so every path
	// must start with the workspace's prefix below that root.
	workspacePrefix := strings.Trim(strings.TrimSpace(string(prefix)), "/")
	if workspacePrefix != "" {
		workspacePrefix += "/"
	}

	sum := sha256.New()
	stateWriteField(sum, []byte(salt))
	stateWriteField(sum, []byte(baseCommit))
	stateWriteField(sum, bytes.TrimSpace(head))
	stateWriteField(sum, status)

	for _, name := range stateStatusPaths(status) {
		rel, inside := strings.CutPrefix(name, workspacePrefix)
		if !inside {
			return "", fmt.Errorf("status path %q escapes the workspace", name)
		}
		abs, err := stateJoinWithin(root, rel)
		if err != nil {
			return "", fmt.Errorf("status path %q escapes the workspace", name)
		}
		digest, ok, err := stateFileDigest(abs)
		if err != nil {
			return "", fmt.Errorf("hashing %s: %w", name, err)
		}
		if !ok {
			continue
		}
		stateWriteField(sum, []byte(name))
		stateWriteField(sum, digest)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func stateJoinWithin(dir, name string) (string, error) {
	local := filepath.FromSlash(name)
	if filepath.IsAbs(local) || !filepath.IsLocal(local) {
		return "", fmt.Errorf("path %q escapes the workspace", name)
	}
	return filepath.Join(filepath.Clean(dir), local), nil
}

// stateFileDigest hashes a regular file. ok is false for missing files and
// non-regular entries, which contribute only through the raw status text.
func stateFileDigest(path string) (digest []byte, ok bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer func() { _ = file.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return nil, false, err
	}
	return sum.Sum(nil), true, nil
}

// stateWriteField writes data preceded by its 8-byte big-endian length so
// adjacent fields cannot run into each other.
func stateWriteField(h hash.Hash, data []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(data)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(data)
}

// stateStatusPaths extracts the paths from `git status --porcelain=v1 -z`.
// Rename and copy entries are followed by their original path, which is
// skipped.
func stateStatusPaths(raw []byte) []string {
	fields := bytes.Split(raw, []byte{0})
	paths := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, string(entry[3:]))
		if stateIsRenameOrCopy(entry[0]) || stateIsRenameOrCopy(entry[1]) {
			i++
		}
	}
	return paths
}

func stateIsRenameOrCopy(code byte) bool {
	return code == 'R' || code == 'C'
}

// Store persists cached gate results and per-session state under one
// directory, and serializes concurrent gate runs with a file lock.
type Store struct {
	dir string
}

// OpenStore returns the store for the workspace at root, located under the
// user cache directory and keyed by the absolute root path.
func OpenStore(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace path: %w", err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locating user cache directory: %w", err)
	}
	sum := sha256.Sum256([]byte(abs))
	name := hex.EncodeToString(sum[:])[:stateDirNameHexLen]
	return NewStore(filepath.Join(cache, "agentic-go", "gate", name))
}

// NewStore returns a store rooted at dir, creating it with owner-only access.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, stateStoreDirPerm); err != nil {
		return nil, fmt.Errorf("creating gate store: %w", err)
	}
	return &Store{dir: dir}, nil
}

// LoadResult returns the cached result for fingerprint. Missing, unreadable,
// and corrupt entries are all misses.
func (s *Store) LoadResult(fingerprint string) (Result, bool) {
	if !stateSafeIDPattern.MatchString(fingerprint) {
		return Result{}, false
	}
	path := filepath.Join(s.dir, stateResultsSubdir, fingerprint+stateFileExtension)
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, false
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return Result{}, false
	}
	// Refresh the modification time so recently used entries survive pruning.
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return result, true
}

// SaveResult stores result under fingerprint and keeps only the newest 20
// cached results.
func (s *Store) SaveResult(fingerprint string, result Result) error {
	if !stateSafeIDPattern.MatchString(fingerprint) {
		return fmt.Errorf("invalid fingerprint %q", fingerprint)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encoding result: %w", err)
	}
	dir := filepath.Join(s.dir, stateResultsSubdir)
	if err := stateWriteFileAtomic(dir, fingerprint+stateFileExtension, data); err != nil {
		return fmt.Errorf("saving result: %w", err)
	}
	stateKeepNewest(dir, stateKeepResults)
	return nil
}

// Session is the gate's memory of one agent session, used to find the commit
// the session started from and to avoid blocking twice on identical state.
//
//nolint:govet // Keep the JSON field order readable for humans and agents.
type Session struct {
	ID                     string    `json:"id"`
	StartHead              string    `json:"start_head"`
	LastBlockedFingerprint string    `json:"last_blocked_fingerprint"`
	UpdatedAt              time.Time `json:"updated_at"`
	Blocks                 int       `json:"blocks"`
}

// LoadSession returns the stored session for id. A missing, unreadable, or
// corrupt file yields a fresh Session{ID: id}; the error is always nil so
// session state can never stop the gate.
func (s *Store) LoadSession(id string) (Session, error) {
	fresh := Session{ID: id}
	data, err := os.ReadFile(s.sessionPath(id))
	if err != nil {
		// Session state must never stop the gate, whatever the read error.
		return fresh, nil
	}
	var session Session
	if json.Unmarshal(data, &session) != nil {
		return fresh, nil
	}
	session.ID = id
	return session, nil
}

// SaveSession stores session and removes session files not updated in 7 days.
func (s *Store) SaveSession(session Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encoding session: %w", err)
	}
	dir := filepath.Join(s.dir, stateSessionsSubdir)
	name := filepath.Base(s.sessionPath(session.ID))
	if err := stateWriteFileAtomic(dir, name, data); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	stateRemoveOlderThan(dir, name, time.Now().Add(-stateSessionMaxAge))
	return nil
}

func (s *Store) sessionPath(id string) string {
	return filepath.Join(s.dir, stateSessionsSubdir, stateSafeID(id)+stateFileExtension)
}

// stateSafeID keeps ids that are already safe file names and hashes the rest.
func stateSafeID(id string) string {
	if stateSafeIDPattern.MatchString(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:stateSessionHexLen]
}

// Lock takes an exclusive advisory lock on the store, polling until it is
// free or ctx ends. The returned function releases it and is safe to call more
// than once.
func (s *Store) Lock(ctx context.Context) (unlock func(), err error) {
	file, err := os.OpenFile(filepath.Join(s.dir, "lock"), os.O_CREATE|os.O_RDWR, stateStoreFilePerm)
	if err != nil {
		return nil, fmt.Errorf("opening gate lock: %w", err)
	}
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // file descriptors fit in int
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return nil, fmt.Errorf("locking gate store: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("waiting for gate lock: %w", ctx.Err())
		case <-time.After(stateLockPoll):
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) //nolint:gosec // file descriptors fit in int
			_ = file.Close()
		})
	}, nil
}

// stateWriteFileAtomic writes data to dir/name through a temp file and rename
// so readers never see a partial file.
func stateWriteFileAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, stateStoreDirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, stateTempPrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(stateStoreFilePerm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		cleanup()
		return err
	}
	stateRemoveStaleTemps(dir, time.Now().Add(-stateTempMaxAge))
	return nil
}

// stateRemoveStaleTemps deletes temp files left behind by interrupted writes.
func stateRemoveStaleTemps(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), stateTempPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// stateKeepNewest removes all but the newest keep JSON files in dir. Pruning
// is best effort: a failure never fails the save that triggered it.
func stateKeepNewest(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type aged struct {
		mod  time.Time
		name string
	}
	files := make([]aged, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), stateFileExtension) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, aged{name: entry.Name(), mod: info.ModTime()})
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].mod.Equal(files[j].mod) {
			return files[i].mod.After(files[j].mod)
		}
		return files[i].name < files[j].name
	})
	for _, old := range files[keep:] {
		_ = os.Remove(filepath.Join(dir, old.name))
	}
}

// stateRemoveOlderThan removes JSON files in dir last modified before cutoff,
// except the file named keep.
func stateRemoveOlderThan(dir, keep string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == keep || !strings.HasSuffix(entry.Name(), stateFileExtension) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}
