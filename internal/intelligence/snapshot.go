// Package intelligence provides snapshot-bound, source-grounded Go context.
package intelligence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

// ErrSnapshotChanged means the caller's reference no longer identifies the
// workspace observed by the current operation.
var ErrSnapshotChanged = errors.New("workspace snapshot changed")

// CapabilityManifest is the normalized semantic-provider feature set.
type CapabilityManifest struct {
	WorkspaceSymbol bool `json:"workspace_symbol"`
	Hover           bool `json:"hover"`
	Definition      bool `json:"definition"`
	TypeDefinition  bool `json:"type_definition"`
	References      bool `json:"references"`
	Implementation  bool `json:"implementation"`
	DocumentSymbol  bool `json:"document_symbol"`
	CallHierarchy   bool `json:"call_hierarchy"`
	Diagnostics     bool `json:"diagnostics"`
	Rename          bool `json:"rename"`
	Formatting      bool `json:"formatting"`
	CodeAction      bool `json:"code_action"`
}

// SemanticIdentity records the exact provider used to interpret a snapshot.
type SemanticIdentity struct {
	Version      string             `json:"version"`
	Capabilities CapabilityManifest `json:"capabilities"`
}

// BuildConfig records source-selection inputs that affect Go semantics.
type BuildConfig struct {
	GOOS       string   `json:"goos"`
	GOARCH     string   `json:"goarch"`
	CGOEnabled bool     `json:"cgo_enabled"`
	GOFLAGS    string   `json:"goflags"`
	Tags       []string `json:"tags"`
	Workspace  string   `json:"workspace"`
}

// SnapshotRef is an immutable, portable reference to one observed workspace.
// It contains identities and versions, never absolute repository paths.
type SnapshotRef struct {
	ID              string             `json:"id"`
	RepositoryID    string             `json:"repository_id"`
	Workspace       string             `json:"workspace"`
	RequestedBase   string             `json:"requested_base,omitempty"`
	BaseCommit      string             `json:"base_commit,omitempty"`
	MergeBaseCommit string             `json:"merge_base_commit,omitempty"`
	HeadCommit      string             `json:"head_commit"`
	ContentDigest   string             `json:"content_digest"`
	GoVersion       string             `json:"go_version"`
	GoplsVersion    string             `json:"gopls_version"`
	Capabilities    CapabilityManifest `json:"capabilities"`
	Build           BuildConfig        `json:"build"`
	Scope           string             `json:"scope"`
}

// SnapshotRequest selects the local base, package scope, and semantic provider
// whose inputs must be bound into the snapshot.
type SnapshotRequest struct {
	Base     string
	Scope    string
	Semantic SemanticIdentity
}

// Snapshotter captures and validates immutable workspace identities.
type Snapshotter struct {
	workspace *workspace.Workspace
	runner    *execution.Runner
	mu        sync.RWMutex
	manifests map[string][]contentRecord
	order     []string
}

// NewSnapshotter constructs a snapshot source over shared contained execution.
func NewSnapshotter(ws *workspace.Workspace, runner *execution.Runner) (*Snapshotter, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	return &Snapshotter{workspace: ws, runner: runner, manifests: make(map[string][]contentRecord)}, nil
}

// Capture observes the final worktree twice and rejects concurrent drift.
func (s *Snapshotter) Capture(ctx context.Context, request SnapshotRequest) (SnapshotRef, error) {
	if err := normalizeSnapshotRequest(&request); err != nil {
		return SnapshotRef{}, err
	}
	callCtx, cancel := s.runner.Deadline(ctx)
	defer cancel()
	first, err := s.capture(callCtx, request)
	if err != nil {
		return SnapshotRef{}, err
	}
	second, err := s.capture(callCtx, request)
	if err != nil {
		return SnapshotRef{}, err
	}
	if first.ID != second.ID {
		return SnapshotRef{}, fmt.Errorf("%w during capture", ErrSnapshotChanged)
	}
	return second, nil
}

// Validate recaptures the supplied scope and rejects stale references.
func (s *Snapshotter) Validate(ctx context.Context, expected SnapshotRef) (SnapshotRef, error) {
	current, err := s.Capture(ctx, SnapshotRequest{
		Base:  expected.RequestedBase,
		Scope: expected.Scope,
		Semantic: SemanticIdentity{
			Version: expected.GoplsVersion, Capabilities: expected.Capabilities,
		},
	})
	if err != nil {
		return SnapshotRef{}, err
	}
	if expected.ID == "" || current.ID != expected.ID {
		return SnapshotRef{}, fmt.Errorf("%w: expected %s, observed %s", ErrSnapshotChanged, expected.ID, current.ID)
	}
	return current, nil
}

type snapshotState struct {
	ref     SnapshotRef
	records []contentRecord
	status  []byte
	index   []byte
}

type contentRecord struct {
	Path   string
	Kind   string
	Digest string
}

func (s *Snapshotter) capture(ctx context.Context, request SnapshotRequest) (SnapshotRef, error) {
	state, err := s.readState(ctx, request)
	if err != nil {
		return SnapshotRef{}, err
	}
	contentHash := sha256.New()
	writeHashPart(contentHash, state.ref.HeadCommit)
	contentHash.Write(state.status)
	contentHash.Write(state.index)
	for _, record := range state.records {
		writeHashPart(contentHash, record.Path)
		writeHashPart(contentHash, record.Kind)
		writeHashPart(contentHash, record.Digest)
	}
	state.ref.ContentDigest = fmt.Sprintf("sha256:%x", contentHash.Sum(nil))

	identity := sha256.New()
	for _, value := range []string{
		state.ref.RepositoryID, state.ref.Workspace, state.ref.RequestedBase,
		state.ref.BaseCommit, state.ref.MergeBaseCommit, state.ref.HeadCommit,
		state.ref.ContentDigest, state.ref.GoVersion, state.ref.GoplsVersion,
		state.ref.Build.GOOS, state.ref.Build.GOARCH, fmt.Sprint(state.ref.Build.CGOEnabled),
		state.ref.Build.GOFLAGS, state.ref.Build.Workspace, state.ref.Scope,
	} {
		writeHashPart(identity, value)
	}
	for _, tag := range state.ref.Build.Tags {
		writeHashPart(identity, tag)
	}
	capabilities, err := json.Marshal(state.ref.Capabilities)
	if err != nil {
		return SnapshotRef{}, fmt.Errorf("encoding semantic capabilities: %w", err)
	}
	identity.Write(capabilities)
	state.ref.ID = fmt.Sprintf("sha256:%x", identity.Sum(nil))
	s.remember(state.ref, state.records)
	return state.ref, nil
}

func (s *Snapshotter) remember(ref SnapshotRef, records []contentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.manifests[ref.ID]; !exists {
		s.order = append(s.order, ref.ID)
	}
	s.manifests[ref.ID] = append([]contentRecord(nil), records...)
	for len(s.order) > 32 {
		delete(s.manifests, s.order[0])
		s.order = s.order[1:]
	}
}

func (s *Snapshotter) manifest(id string) ([]contentRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records, found := s.manifests[id]
	return append([]contentRecord(nil), records...), found
}

func (s *Snapshotter) readState(ctx context.Context, request SnapshotRequest) (snapshotState, error) {
	repositoryRoot, err := s.gitText(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return snapshotState{}, fmt.Errorf("resolving repository root: %w", err)
	}
	repositoryRoot, err = filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return snapshotState{}, fmt.Errorf("resolving repository root: %w", err)
	}
	workspaceRelative, err := filepath.Rel(repositoryRoot, s.workspace.Root())
	if err != nil || workspaceRelative == ".." || strings.HasPrefix(workspaceRelative, ".."+string(filepath.Separator)) {
		return snapshotState{}, fmt.Errorf("workspace is outside the Git repository")
	}
	workspaceRelative = filepath.ToSlash(workspaceRelative)
	if workspaceRelative == "" {
		workspaceRelative = "."
	}
	repositoryHash := sha256.Sum256([]byte(repositoryRoot))

	head, err := s.gitText(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return snapshotState{}, fmt.Errorf("resolving HEAD: %w", err)
	}
	base, mergeBase := "", ""
	if request.Base != "" {
		if strings.HasPrefix(request.Base, "-") || strings.ContainsRune(request.Base, 0) {
			return snapshotState{}, fmt.Errorf("base %q is not a valid local ref", request.Base)
		}
		base, err = s.gitText(ctx, "rev-parse", "--verify", "--end-of-options", request.Base+"^{commit}")
		if err != nil {
			return snapshotState{}, fmt.Errorf("resolving base %q: %w", request.Base, err)
		}
		mergeBase, err = s.gitText(ctx, "merge-base", "--", base, head)
		if err != nil {
			return snapshotState{}, fmt.Errorf("resolving merge-base: %w", err)
		}
	}

	status, err := s.gitBytes(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	if err != nil {
		return snapshotState{}, fmt.Errorf("reading worktree status: %w", err)
	}
	index, err := s.gitBytes(ctx, "ls-files", "-v", "-z", "--", ".")
	if err != nil {
		return snapshotState{}, fmt.Errorf("reading index flags: %w", err)
	}
	unmerged, err := s.gitBytes(ctx, "diff", "--name-only", "--diff-filter=U", "-z", "--", ".")
	if err != nil {
		return snapshotState{}, fmt.Errorf("checking unresolved merges: %w", err)
	}
	if len(unmerged) > 0 {
		return snapshotState{}, fmt.Errorf("repository contains unresolved merge state")
	}

	paths, err := s.snapshotPaths(ctx, index, request.Scope)
	if err != nil {
		return snapshotState{}, err
	}
	records := make([]contentRecord, 0, len(paths))
	for _, path := range paths {
		record, recordErr := s.contentRecord(path)
		if recordErr != nil {
			return snapshotState{}, recordErr
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })

	build, err := s.buildConfig(ctx)
	if err != nil {
		return snapshotState{}, err
	}
	return snapshotState{
		ref: SnapshotRef{
			RepositoryID:    fmt.Sprintf("sha256:%x", repositoryHash[:]),
			Workspace:       workspaceRelative,
			RequestedBase:   request.Base,
			BaseCommit:      base,
			MergeBaseCommit: mergeBase,
			HeadCommit:      head,
			GoVersion:       s.workspace.Toolchain().Version,
			GoplsVersion:    request.Semantic.Version,
			Capabilities:    request.Semantic.Capabilities,
			Build:           build,
			Scope:           request.Scope,
		},
		records: records,
		status:  append([]byte(nil), status...),
		index:   append([]byte(nil), index...),
	}, nil
}

func (s *Snapshotter) snapshotPaths(ctx context.Context, index []byte, scope string) ([]string, error) {
	paths := make(map[string]struct{})
	trackedChanges, err := s.gitBytes(ctx, "diff", "--name-only", "--no-renames", "-z", "--relative", "HEAD", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("reading tracked content changes: %w", err)
	}
	addNULPaths(paths, trackedChanges)
	untracked, err := s.gitBytes(ctx, "ls-files", "--others", "--exclude-standard", "-z", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("reading untracked content: %w", err)
	}
	addNULPaths(paths, untracked)
	ignoredInputs, err := s.gitBytes(ctx, "ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--",
		"go.mod", "go.sum", "go.work", "go.work.sum", ":(glob)**/*.go", ":(glob)**/go.mod", ":(glob)**/go.sum", ":(glob)**/go.work", ":(glob)**/go.work.sum")
	if err != nil {
		return nil, fmt.Errorf("reading ignored Go inputs: %w", err)
	}
	addNULPaths(paths, ignoredInputs)
	for _, entry := range bytes.Split(index, []byte{0}) {
		if len(entry) > 2 && entry[0] >= 'a' && entry[0] <= 'z' && entry[1] == ' ' {
			paths[string(entry[2:])] = struct{}{}
		}
	}
	packageInputs, err := s.goPackageInputs(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, path := range packageInputs {
		paths[path] = struct{}{}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		clean, cleanErr := cleanSnapshotPath(path)
		if cleanErr != nil {
			return nil, cleanErr
		}
		result = append(result, clean)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Snapshotter) goPackageInputs(ctx context.Context, scope string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	result, err := s.runner.Run(ctx, execution.Command{
		Name: s.workspace.Toolchain().Path,
		Args: []string{"list", "-e", "-json", scope},
		Dir:  s.workspace.Root(),
		Env:  map[string]string{"GOTOOLCHAIN": "local", "GOWORK": "auto"},
	}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("listing package inputs: %s", strings.TrimSpace(stderr.String()))
	}
	decoder := json.NewDecoder(&stdout)
	paths := make(map[string]struct{})
	for {
		var pkg struct {
			Dir            string
			GoFiles        []string
			CgoFiles       []string
			IgnoredGoFiles []string
			CFiles         []string
			CXXFiles       []string
			MFiles         []string
			HFiles         []string
			FFiles         []string
			SFiles         []string
			SwigFiles      []string
			SwigCXXFiles   []string
			SysoFiles      []string
			EmbedFiles     []string
			TestGoFiles    []string
			XTestGoFiles   []string
		}
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decoding package inputs: %w", err)
		}
		if pkg.Dir == "" {
			continue
		}
		groups := [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.IgnoredGoFiles, pkg.CFiles, pkg.CXXFiles, pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.SwigCXXFiles, pkg.SysoFiles, pkg.EmbedFiles, pkg.TestGoFiles, pkg.XTestGoFiles}
		for _, group := range groups {
			for _, name := range group {
				absolute := filepath.Join(pkg.Dir, name)
				relative, relativeErr := s.workspace.Relative(absolute)
				if relativeErr == nil {
					paths[relative] = struct{}{}
				}
			}
		}
	}
	resultPaths := make([]string, 0, len(paths))
	for path := range paths {
		resultPaths = append(resultPaths, path)
	}
	sort.Strings(resultPaths)
	return resultPaths, nil
}

func (s *Snapshotter) contentRecord(path string) (contentRecord, error) {
	absolute := filepath.Join(s.workspace.Root(), filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return contentRecord{Path: path, Kind: "deleted", Digest: "sha256:" + strings.Repeat("0", 64)}, nil
	}
	if err != nil {
		return contentRecord{}, fmt.Errorf("inspecting snapshot path %q: %w", path, err)
	}
	kind := "file"
	var content []byte
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
		target, readErr := os.Readlink(absolute)
		if readErr != nil {
			return contentRecord{}, fmt.Errorf("reading snapshot symlink %q: %w", path, readErr)
		}
		resolved, resolveErr := s.workspace.Resolve(path)
		if resolveErr != nil {
			return contentRecord{}, resolveErr
		}
		resolvedContent, readErr := os.ReadFile(resolved)
		if readErr != nil {
			return contentRecord{}, fmt.Errorf("reading snapshot symlink target %q: %w", path, readErr)
		}
		content = append([]byte(target+"\x00"), resolvedContent...)
	} else {
		if !info.Mode().IsRegular() {
			return contentRecord{}, fmt.Errorf("snapshot path %q is not a regular file", path)
		}
		resolved, resolveErr := s.workspace.Resolve(path)
		if resolveErr != nil {
			return contentRecord{}, resolveErr
		}
		content, err = os.ReadFile(resolved)
		if err != nil {
			return contentRecord{}, fmt.Errorf("reading snapshot path %q: %w", path, err)
		}
	}
	digest := sha256.Sum256(content)
	return contentRecord{Path: path, Kind: kind, Digest: fmt.Sprintf("sha256:%x", digest[:])}, nil
}

func (s *Snapshotter) buildConfig(ctx context.Context) (BuildConfig, error) {
	var stdout, stderr bytes.Buffer
	result, err := s.runner.Run(ctx, execution.Command{
		Name: s.workspace.Toolchain().Path,
		Args: []string{"env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK"},
		Dir:  s.workspace.Root(),
		Env:  map[string]string{"GOTOOLCHAIN": "local", "GOWORK": "auto"},
	}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return BuildConfig{}, err
	}
	if result.ExitCode != 0 {
		return BuildConfig{}, fmt.Errorf("reading Go build configuration: %s", strings.TrimSpace(stderr.String()))
	}
	var values struct {
		GOOS       string
		GOARCH     string
		CGOEnabled string `json:"CGO_ENABLED"`
		GOFLAGS    string
		GOWORK     string
	}
	if err := json.Unmarshal(stdout.Bytes(), &values); err != nil {
		return BuildConfig{}, fmt.Errorf("decoding Go build configuration: %w", err)
	}
	workspaceMode := "off"
	if values.GOWORK != "" && values.GOWORK != "off" {
		if relative, relativeErr := s.workspace.Relative(values.GOWORK); relativeErr == nil {
			workspaceMode = relative
		} else {
			workspaceMode = "external"
		}
	}
	return BuildConfig{
		GOOS: values.GOOS, GOARCH: values.GOARCH, CGOEnabled: values.CGOEnabled == "1",
		GOFLAGS: values.GOFLAGS, Tags: parseBuildTags(values.GOFLAGS), Workspace: workspaceMode,
	}, nil
}

func (s *Snapshotter) gitText(ctx context.Context, args ...string) (string, error) {
	output, err := s.gitBytes(ctx, args...)
	return strings.TrimSpace(string(output)), err
}

func (s *Snapshotter) gitBytes(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	result, err := s.runner.Run(ctx, execution.Command{Name: "git", Args: args, Dir: s.workspace.Root()}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = fmt.Sprintf("git exited with status %d", result.ExitCode)
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func normalizeSnapshotRequest(request *SnapshotRequest) error {
	if request.Scope == "" {
		request.Scope = "./..."
	}
	if strings.HasPrefix(request.Scope, "-") || strings.ContainsRune(request.Scope, 0) {
		return fmt.Errorf("package scope %q is invalid", request.Scope)
	}
	if strings.TrimSpace(request.Semantic.Version) == "" {
		return fmt.Errorf("semantic provider version is empty")
	}
	return nil
}

func addNULPaths(destination map[string]struct{}, data []byte) {
	for _, value := range bytes.Split(data, []byte{0}) {
		if len(value) > 0 {
			destination[filepath.ToSlash(string(value))] = struct{}{}
		}
	}
}

func cleanSnapshotPath(path string) (string, error) {
	path = filepath.ToSlash(path)
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("snapshot path %q is not relative", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("snapshot path %q escapes the workspace", path)
	}
	return clean, nil
}

func parseBuildTags(goFlags string) []string {
	fields := strings.Fields(goFlags)
	tags := make([]string, 0)
	for index := 0; index < len(fields); index++ {
		value := ""
		if strings.HasPrefix(fields[index], "-tags=") {
			value = strings.TrimPrefix(fields[index], "-tags=")
		} else if fields[index] == "-tags" && index+1 < len(fields) {
			index++
			value = fields[index]
		}
		for _, tag := range strings.Split(value, ",") {
			tag = strings.TrimSpace(strings.Trim(tag, "'\""))
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	sort.Strings(tags)
	return tags
}

func writeHashPart(hash interface{ Write([]byte) (int, error) }, value string) {
	_, _ = fmt.Fprintf(hash, "%d:%s\x00", len(value), value)
}
