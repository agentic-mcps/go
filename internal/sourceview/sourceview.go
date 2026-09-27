// Package sourceview creates exact, visible Git worktree views for a selected
// branch without changing the source checkout's branch or worktree.
package sourceview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/workspace"
)

const (
	SchemaVersion      = "agentic.branch-view/v1"
	metadataConfigKey  = "agentic-go.branch-view"
	metadataRef        = "refs/worktree/agentic-go/source-view"
	maximumOverlaySize = 8 << 20
)

var (
	ErrStale = errors.New("branch source view is stale")
)

// Marker is private per-worktree provenance used to reject a moved branch ref.
// It is stored in the linked worktree's Git metadata, not in repository files.
//
//nolint:govet // Field order matches the serialized source-view identity.
type Marker struct {
	SchemaVersion   string `json:"schema_version"`
	Ref             string `json:"ref"`
	Commit          string `json:"commit"`
	Tree            string `json:"tree"`
	OverlayIncluded bool   `json:"overlay_included"`
	OverlayDigest   string `json:"overlay_digest,omitempty"`
}

// Request selects a branch source view and an optional exact dirty overlay.
type Request struct {
	Branch        string
	OutputPath    string
	IncludeDirty  bool
}

// Result identifies the materialized source tree and any known checkout gaps.
//
//nolint:govet // Field order matches the CLI JSON response.
type Result struct {
	SchemaVersion      string   `json:"schema_version"`
	RequestedBranch    string   `json:"requested_branch"`
	Branch             string   `json:"branch"`
	Ref                string   `json:"ref"`
	Commit             string   `json:"commit"`
	Tree               string   `json:"tree"`
	ViewPath           string   `json:"view_path"`
	OverlayIncluded    bool     `json:"overlay_included"`
	OverlayDigest      string   `json:"overlay_digest,omitempty"`
	CheckoutComplete  bool     `json:"checkout_complete"`
	CheckoutLimitations []string `json:"checkout_limitations"`
}

type selectedBranch struct {
	requested string
	name      string
	ref       string
	commit    string
	tree      string
}

type overlayFile struct {
	path    string
	mode    fs.FileMode
	content []byte
}

type overlay struct {
	patch []byte
	files []overlayFile
	digest string
}

// Create resolves a branch to an exact commit, creates a detached worktree at
// the requested new path, and records the ref so later observations can reject
// the view if the branch moves.
func Create(ctx context.Context, runner *execution.Runner, source *workspace.Workspace, request Request) (result Result, returnErr error) {
	if runner == nil {
		return Result{}, errors.New("source-view runner is nil")
	}
	if source == nil {
		return Result{}, errors.New("source-view workspace is nil")
	}
	repositoryRoot, err := gitText(ctx, runner, "rev-parse", "--show-toplevel")
	if err != nil {
		return Result{}, fmt.Errorf("resolving repository root: %w", err)
	}
	repositoryRoot, err = filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("resolving repository root: %w", err)
	}
	if repositoryRoot != source.Root() {
		return Result{}, errors.New("source-view workspace must be the Git repository root")
	}

	outputPath, err := validateOutputPath(repositoryRoot, request.OutputPath)
	if err != nil {
		return Result{}, err
	}
	branch, err := resolveBranch(ctx, runner, request.Branch)
	if err != nil {
		return Result{}, err
	}
	limitations, err := checkoutLimitations(ctx, runner, branch.commit)
	if err != nil {
		limitations = []string{"Git could not determine whether the selected tree contains submodules"}
	}

	var captured overlay
	if request.IncludeDirty {
		captured, err = captureOverlay(ctx, runner, source.Root(), branch.commit)
		if err != nil {
			return Result{}, fmt.Errorf("capturing exact dirty overlay: %w", err)
		}
	}

	created := false
	complete := false
	defer func() {
		if complete || !created {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanupErr := removeWorktree(cleanupCtx, runner, outputPath); cleanupErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("cleaning incomplete source view: %w", cleanupErr))
		}
	}()

	if _, err := gitBytes(ctx, runner, "worktree", "add", "--detach", "--", outputPath, branch.commit); err != nil {
		return Result{}, fmt.Errorf("creating detached branch view: %w", err)
	}
	created = true

	viewWorkspace, err := workspace.Open(ctx, outputPath)
	if err != nil {
		return Result{}, fmt.Errorf("validating selected branch workspace: %w", err)
	}
	viewRunner, err := runner.ForWorkspace(viewWorkspace)
	if err != nil {
		return Result{}, fmt.Errorf("creating selected branch runner: %w", err)
	}
	if request.IncludeDirty {
		if len(captured.patch) > 0 {
			if err := applyPatch(ctx, viewRunner, outputPath, captured.patch); err != nil {
				return Result{}, fmt.Errorf("applying exact tracked overlay: %w", err)
			}
		}
		if err := copyOverlayFiles(outputPath, captured.files); err != nil {
			return Result{}, fmt.Errorf("copying untracked overlay: %w", err)
		}
		current, err := captureOverlay(ctx, runner, source.Root(), branch.commit)
		if err != nil {
			return Result{}, fmt.Errorf("rechecking source overlay: %w", err)
		}
		if current.digest != captured.digest {
			return Result{}, errors.New("source checkout changed while the dirty overlay was captured")
		}
	}

	if err := writeMarker(ctx, viewRunner, Marker{
		SchemaVersion: SchemaVersion, Ref: branch.ref, Commit: branch.commit,
		Tree: branch.tree, OverlayIncluded: request.IncludeDirty,
		OverlayDigest: captured.digest,
	}); err != nil {
		return Result{}, fmt.Errorf("recording selected branch identity: %w", err)
	}
	if err := Validate(ctx, viewRunner, branch.commit); err != nil {
		return Result{}, fmt.Errorf("validating selected branch identity: %w", err)
	}

	result = Result{
		SchemaVersion: SchemaVersion, RequestedBranch: branch.requested,
		Branch: branch.name, Ref: branch.ref, Commit: branch.commit, Tree: branch.tree,
		ViewPath: outputPath, OverlayIncluded: request.IncludeDirty,
		OverlayDigest: captured.digest, CheckoutComplete: len(limitations) == 0,
		CheckoutLimitations: limitations,
	}
	complete = true
	return result, nil
}

// Validate rejects an invalid or moved branch-view marker. An ordinary
// checkout has no marker and is left unchanged.
func Validate(ctx context.Context, runner *execution.Runner, currentHead string) error {
	marker, found, err := ReadMarker(ctx, runner)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if !validObjectID(marker.Commit) || !validObjectID(marker.Tree) || !validBranchRef(marker.Ref) || marker.OverlayIncluded && !validDigest(marker.OverlayDigest) || !marker.OverlayIncluded && marker.OverlayDigest != "" {
		return errors.New("branch source view metadata is malformed")
	}
	if marker.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported branch source view schema %q", marker.SchemaVersion)
	}
	if currentHead != marker.Commit {
		return fmt.Errorf("%w: workspace HEAD is %s, expected %s", ErrStale, currentHead, marker.Commit)
	}
	commit, found, err := resolveCommit(ctx, runner, marker.Ref)
	if err != nil {
		return fmt.Errorf("checking branch source view ref: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: ref %s no longer resolves", ErrStale, marker.Ref)
	}
	if commit != marker.Commit {
		return fmt.Errorf("%w: ref %s moved from %s to %s", ErrStale, marker.Ref, marker.Commit, commit)
	}
	tree, err := gitText(ctx, runner, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil {
		return fmt.Errorf("checking branch source view tree: %w", err)
	}
	if tree != marker.Tree {
		return fmt.Errorf("%w: ref %s now identifies a different tree", ErrStale, marker.Ref)
	}
	return nil
}

// ReadMarker returns per-worktree provenance, if present.
func ReadMarker(ctx context.Context, runner *execution.Runner) (Marker, bool, error) {
	if runner == nil {
		return Marker{}, false, errors.New("branch-view runner is nil")
	}
	gitDir, err := gitText(ctx, runner, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return Marker{}, false, fmt.Errorf("resolving worktree Git directory: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		return Marker{}, false, errors.New("Git returned a non-absolute worktree directory")
	}
	configPath := filepath.Join(gitDir, "config.worktree")
	value, exitCode, err := gitRun(ctx, runner, "config", "--file", configPath, "--get", metadataConfigKey)
	if err != nil {
		return Marker{}, false, fmt.Errorf("reading branch source view metadata: %w", err)
	}
	if exitCode == 1 {
		markerCommit, found, markerErr := resolveCommit(ctx, runner, metadataRef)
		if markerErr != nil {
			return Marker{}, false, fmt.Errorf("checking branch source view sentinel: %w", markerErr)
		}
		if found {
			return Marker{}, false, fmt.Errorf("branch source view marker is missing for private ref %s", markerCommit)
		}
		return Marker{}, false, nil
	}
	if exitCode != 0 {
		return Marker{}, false, fmt.Errorf("reading branch source view metadata: git exited with status %d", exitCode)
	}
	encoded := strings.TrimSpace(string(value))
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Marker{}, false, fmt.Errorf("decoding branch source view metadata: %w", err)
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return Marker{}, false, fmt.Errorf("parsing branch source view metadata: %w", err)
	}
	markerCommit, found, err := resolveCommit(ctx, runner, metadataRef)
	if err != nil {
		return Marker{}, false, fmt.Errorf("checking branch source view sentinel: %w", err)
	}
	if !found || markerCommit != marker.Commit {
		return Marker{}, false, errors.New("branch source view metadata and private sentinel disagree")
	}
	return marker, true, nil
}

func resolveBranch(ctx context.Context, runner *execution.Runner, requested string) (selectedBranch, error) {
	branchName := strings.TrimSpace(requested)
	if branchName != requested {
		return selectedBranch{}, errors.New("branch name must not contain leading or trailing whitespace")
	}
	ref := ""
	if branchName == "" {
		branchName = "main"
		ref = "refs/heads/main"
		if _, found, err := resolveCommit(ctx, runner, ref); err != nil {
			return selectedBranch{}, err
		} else if !found {
			remoteHead, exitCode, symbolicErr := gitRun(ctx, runner, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
			if symbolicErr != nil {
				return selectedBranch{}, fmt.Errorf("resolving configured default branch: %w", symbolicErr)
			}
			if exitCode != 0 || !strings.HasPrefix(strings.TrimSpace(string(remoteHead)), "refs/remotes/") {
				return selectedBranch{}, errors.New("branch main does not exist and origin/HEAD is not configured; select a branch explicitly")
			}
			ref = strings.TrimSpace(string(remoteHead))
			branchName = strings.TrimPrefix(ref, "refs/remotes/")
		}
	} else if strings.HasPrefix(branchName, "refs/heads/") || strings.HasPrefix(branchName, "refs/remotes/") {
		ref = branchName
	} else if strings.HasPrefix(branchName, "refs/") || strings.HasPrefix(branchName, "-") || strings.ContainsRune(branchName, 0) {
		return selectedBranch{}, errors.New("branch must be a local branch name or a full refs/heads or refs/remotes ref")
	} else {
		ref = "refs/heads/" + branchName
	}
	if err := checkBranchRef(ctx, runner, ref); err != nil {
		return selectedBranch{}, err
	}
	commit, found, err := resolveCommit(ctx, runner, ref)
	if err != nil {
		return selectedBranch{}, err
	}
	if !found {
		return selectedBranch{}, fmt.Errorf("branch ref %s does not resolve to a commit", ref)
	}
	tree, err := gitText(ctx, runner, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil || !validObjectID(tree) {
		if err == nil {
			err = errors.New("Git returned an invalid tree object ID")
		}
		return selectedBranch{}, fmt.Errorf("resolving branch tree: %w", err)
	}
	canonicalName := strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/")
	return selectedBranch{requested: branchName, name: canonicalName, ref: ref, commit: commit, tree: tree}, nil
}

func checkBranchRef(ctx context.Context, runner *execution.Runner, ref string) error {
	if !validBranchRef(ref) {
		return fmt.Errorf("branch ref %q is invalid", ref)
	}
	_, exitCode, err := gitRun(ctx, runner, "check-ref-format", ref)
	if err != nil {
		return fmt.Errorf("validating branch ref: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("branch ref %q is invalid", ref)
	}
	return nil
}

func validBranchRef(ref string) bool {
	return strings.HasPrefix(ref, "refs/heads/") || strings.HasPrefix(ref, "refs/remotes/")
}

func resolveCommit(ctx context.Context, runner *execution.Runner, ref string) (string, bool, error) {
	output, exitCode, err := gitRun(ctx, runner, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", false, err
	}
	if exitCode != 0 {
		return "", false, nil
	}
	commit := strings.TrimSpace(string(output))
	if !validObjectID(commit) {
		return "", false, errors.New("Git returned an invalid commit object ID")
	}
	return commit, true, nil
}

func validateOutputPath(repositoryRoot, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", errors.New("--output is required")
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolving output path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolving output parent: %w", err)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return "", errors.New("output parent must be an existing directory")
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	if _, err := os.Lstat(absolute); err == nil {
		return "", fmt.Errorf("output path %q already exists", absolute)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("checking output path: %w", err)
	}
	if inside, err := pathWithin(repositoryRoot, absolute); err != nil || inside {
		return "", errors.New("output path must be outside the source repository")
	}
	if contains, err := pathWithin(absolute, repositoryRoot); err != nil || contains {
		return "", errors.New("output path must not contain the source repository")
	}
	return absolute, nil
}

func pathWithin(root, candidate string) (bool, error) {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, err
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
}

func checkoutLimitations(ctx context.Context, runner *execution.Runner, commit string) ([]string, error) {
	output, err := gitText(ctx, runner, "ls-tree", "-r", "--format=%(objectmode)", commit)
	if err != nil {
		return nil, err
	}
	modules := 0
	for _, mode := range strings.Fields(output) {
		if mode == "160000" {
			modules++
		}
	}
	if modules == 0 {
		return []string{}, nil
	}
	return []string{fmt.Sprintf("%d Git submodule entries are not initialized in this branch view", modules)}, nil
}

func captureOverlay(ctx context.Context, runner *execution.Runner, sourceRoot, commit string) (overlay, error) {
	head, err := gitText(ctx, runner, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return overlay{}, fmt.Errorf("resolving source checkout HEAD: %w", err)
	}
	if head != commit {
		return overlay{}, fmt.Errorf("source checkout HEAD %s does not match selected branch commit %s", head, commit)
	}
	unmerged, err := gitText(ctx, runner, "ls-files", "-u", "-z", "--")
	if err != nil {
		return overlay{}, fmt.Errorf("checking unresolved source merges: %w", err)
	}
	if unmerged != "" {
		return overlay{}, errors.New("source checkout has unresolved merge entries")
	}
	patch, err := gitBytes(ctx, runner, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--no-renames", commit, "--")
	if err != nil {
		return overlay{}, fmt.Errorf("capturing tracked source changes: %w", err)
	}
	listed, err := gitBytes(ctx, runner, "ls-files", "--others", "--exclude-standard", "--full-name", "-z", "--")
	if err != nil {
		return overlay{}, fmt.Errorf("listing untracked source files: %w", err)
	}
	paths := make([]string, 0)
	for _, raw := range bytes.Split(listed, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := filepath.ToSlash(string(raw))
		if !fs.ValidPath(path) || !utf8.ValidString(path) || strings.HasPrefix(path, ".git/") || path == ".git" {
			return overlay{}, fmt.Errorf("untracked source path %q is unsupported", path)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]overlayFile, 0, len(paths))
	contentBytes := len(patch)
	sourceDirectory, err := os.OpenRoot(sourceRoot)
	if err != nil {
		return overlay{}, fmt.Errorf("opening source workspace root: %w", err)
	}
	defer sourceDirectory.Close()
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return overlay{}, err
		}
		info, err := sourceDirectory.Lstat(path)
		if err != nil {
			return overlay{}, fmt.Errorf("inspecting untracked file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return overlay{}, fmt.Errorf("untracked path %q is not a regular file; symlink and special-file overlays are unsupported", path)
		}
		if info.Size() < 0 || info.Size() > int64(maximumOverlaySize-contentBytes) {
			return overlay{}, fmt.Errorf("dirty overlay exceeds the %d-byte limit", maximumOverlaySize)
		}
		opened, err := sourceDirectory.Open(path)
		if err != nil {
			return overlay{}, fmt.Errorf("reading untracked file %q: %w", path, err)
		}
		content, readErr := io.ReadAll(io.LimitReader(opened, int64(maximumOverlaySize-contentBytes)+1))
		closeFileErr := opened.Close()
		if readErr != nil {
			return overlay{}, fmt.Errorf("reading untracked file %q: %w", path, readErr)
		}
		if closeFileErr != nil {
			return overlay{}, fmt.Errorf("closing untracked file %q: %w", path, closeFileErr)
		}
		contentBytes += len(content)
		if contentBytes > maximumOverlaySize {
			return overlay{}, fmt.Errorf("dirty overlay exceeds the %d-byte limit", maximumOverlaySize)
		}
		mode := fs.FileMode(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		files = append(files, overlayFile{path: path, mode: mode, content: content})
	}
	digest := digestOverlay(patch, files)
	return overlay{patch: patch, files: files, digest: digest}, nil
}

func digestOverlay(patch []byte, files []overlayFile) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("agentic-go/branch-overlay/v1\x00"))
	writeHashField(hash, patch)
	for _, file := range files {
		writeHashField(hash, []byte(file.path))
		var mode [4]byte
		binary.BigEndian.PutUint32(mode[:], uint32(file.mode.Perm()))
		_, _ = hash.Write(mode[:])
		writeHashField(hash, file.content)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeHashField(hash interface{ Write([]byte) (int, error) }, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write(value)
}

func applyPatch(ctx context.Context, runner *execution.Runner, viewPath string, patch []byte) error {
	file, err := os.CreateTemp(viewPath, ".agentic-go-overlay-*.patch")
	if err != nil {
		return fmt.Errorf("creating temporary overlay patch: %w", err)
	}
	patchPath := file.Name()
	defer func() { _ = os.Remove(patchPath) }()
	if _, err := file.Write(patch); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing temporary overlay patch: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing temporary overlay patch: %w", err)
	}
	_, err = gitBytes(ctx, runner, "apply", "--binary", "--whitespace=nowarn", patchPath)
	return err
}

func copyOverlayFiles(root string, files []overlayFile) error {
	viewRoot, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("opening selected worktree root: %w", err)
	}
	defer viewRoot.Close()
	for _, file := range files {
		if err := makeContainedParents(viewRoot, file.path); err != nil {
			return err
		}
		if _, err := viewRoot.Lstat(file.path); err == nil {
			return fmt.Errorf("untracked path %q collides with selected branch content", file.path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("checking overlay destination %q: %w", file.path, err)
		}
		created, err := viewRoot.OpenFile(file.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.mode)
		if err != nil {
			return fmt.Errorf("creating overlay file %q: %w", file.path, err)
		}
		_, writeErr := created.Write(file.content)
		closeErr := created.Close()
		if writeErr != nil {
			return fmt.Errorf("writing overlay file %q: %w", file.path, writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("closing overlay file %q: %w", file.path, closeErr)
		}
	}
	return nil
}

func makeContainedParents(root *os.Root, relative string) error {
	if !fs.ValidPath(relative) {
		return fmt.Errorf("overlay path %q is invalid", relative)
	}
	parts := strings.Split(path.Dir(relative), "/")
	current := ""
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := root.Mkdir(current, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("creating overlay directory: %w", err)
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("inspecting overlay directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("overlay path %q traverses a non-directory or symlink", relative)
		}
	}
	return nil
}

func writeMarker(ctx context.Context, runner *execution.Runner, marker Marker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	gitDir, err := gitText(ctx, runner, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(gitDir) {
		return errors.New("Git returned a non-absolute worktree directory")
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if _, err := gitBytes(ctx, runner, "config", "--file", filepath.Join(gitDir, "config.worktree"), "--replace-all", metadataConfigKey, encoded); err != nil {
		return err
	}
	_, err = gitBytes(ctx, runner, "update-ref", metadataRef, marker.Commit)
	return err
}

func removeWorktree(ctx context.Context, runner *execution.Runner, path string) error {
	_, err := gitBytes(ctx, runner, "worktree", "remove", "--force", "--", path)
	return err
}

func gitText(ctx context.Context, runner *execution.Runner, args ...string) (string, error) {
	output, err := gitBytes(ctx, runner, args...)
	return strings.TrimSpace(string(output)), err
}

func gitBytes(ctx context.Context, runner *execution.Runner, args ...string) ([]byte, error) {
	output, exitCode, err := gitRun(ctx, runner, args...)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("git %s exited with status %d", args[0], exitCode)
	}
	return output, nil
}

func gitRun(ctx context.Context, runner *execution.Runner, args ...string) ([]byte, int, error) {
	var stdout, stderr bytes.Buffer
	result, err := runner.Run(ctx, execution.Command{Name: "git", Args: args}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return stdout.Bytes(), 0, err
	}
	return stdout.Bytes(), result.ExitCode, nil
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
