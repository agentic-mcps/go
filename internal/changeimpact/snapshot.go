package changeimpact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

func (a *Analyzer) snapshot(ctx context.Context, options Options) (Analysis, error) {
	if strings.HasPrefix(options.Base, "-") || strings.ContainsRune(options.Base, 0) {
		return Analysis{}, fmt.Errorf("base %q is not a valid local ref", options.Base)
	}

	base, err := a.gitText(ctx, "rev-parse", "--verify", "--end-of-options", options.Base+"^{commit}")
	if err != nil {
		return Analysis{}, fmt.Errorf("resolving base %q: %w", options.Base, err)
	}
	head, err := a.gitText(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Analysis{}, fmt.Errorf("resolving HEAD: %w", err)
	}
	mergeBase, err := a.gitText(ctx, "merge-base", "--", base, head)
	if err != nil {
		return Analysis{}, fmt.Errorf("resolving merge-base: %w", err)
	}
	repositoryRoot, err := a.gitText(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return Analysis{}, fmt.Errorf("resolving repository root: %w", err)
	}
	repositoryRoot, err = filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return Analysis{}, fmt.Errorf("resolving repository-root symlinks: %w", err)
	}
	workspaceRelative, err := filepath.Rel(repositoryRoot, a.workspace.Root())
	if err != nil || workspaceRelative == ".." || strings.HasPrefix(workspaceRelative, ".."+string(filepath.Separator)) {
		return Analysis{}, fmt.Errorf("workspace is outside the Git repository")
	}
	workspaceRelative = filepath.ToSlash(workspaceRelative)
	if workspaceRelative == "" {
		workspaceRelative = "."
	}

	rawOutput, err := a.gitBytes(ctx, "diff", "--raw", "-z", "--find-renames", "--relative", mergeBase, "--", ".")
	if err != nil {
		return Analysis{}, fmt.Errorf("reading tracked changes: %w", err)
	}
	rawChanges, err := ParseRawChanges(rawOutput)
	if err != nil {
		return Analysis{}, fmt.Errorf("parsing tracked changes: %w", err)
	}
	untrackedOutput, err := a.gitBytes(ctx, "ls-files", "--others", "--exclude-standard", "--full-name", "-z", "--", ".")
	if err != nil {
		return Analysis{}, fmt.Errorf("reading untracked changes: %w", err)
	}
	untracked, err := parseUntracked(untrackedOutput, workspaceRelative)
	if err != nil {
		return Analysis{}, err
	}

	files := make([]File, 0, len(rawChanges)+len(untracked))
	trackedPaths := make(map[string]struct{}, len(rawChanges))
	for _, raw := range rawChanges {
		if raw.Status == 'U' {
			return Analysis{}, fmt.Errorf("repository contains an unresolved merge at %q", raw.Path)
		}
		path, cleanErr := cleanRelativePath(raw.Path)
		if cleanErr != nil {
			return Analysis{}, fmt.Errorf("validating changed path: %w", cleanErr)
		}
		oldPath := path
		if raw.OldPath != "" {
			oldPath, cleanErr = cleanRelativePath(raw.OldPath)
			if cleanErr != nil {
				return Analysis{}, fmt.Errorf("validating previous path: %w", cleanErr)
			}
		}
		trackedPaths[path] = struct{}{}

		kind, kindErr := rawChangeKind(raw.Status)
		if kindErr != nil {
			return Analysis{}, kindErr
		}
		file := File{Change: verification.ChangedFile{
			Path:          path,
			Change:        kind,
			BaseRanges:    make([]verification.LineRange, 0),
			CurrentRanges: make([]verification.LineRange, 0),
		}}
		if kind == verification.ChangeRenamed {
			file.Change.PreviousPath = oldPath
		}
		if kind != verification.ChangeAdded {
			repoPath := repositoryPath(workspaceRelative, oldPath)
			file.BaseContent, err = a.gitObject(ctx, mergeBase, repoPath)
			if err != nil {
				return Analysis{}, fmt.Errorf("reading base content for %q: %w", oldPath, err)
			}
		}
		if kind != verification.ChangeDeleted {
			file.CurrentContent, err = a.readContained(path)
			if err != nil {
				return Analysis{}, fmt.Errorf("reading current content for %q: %w", path, err)
			}
		}
		patchArgs := []string{"diff", "--no-ext-diff", "--unified=0", "--no-color", "--find-renames", mergeBase, "--"}
		if oldPath != path {
			patchArgs = append(patchArgs, oldPath)
		}
		patchArgs = append(patchArgs, path)
		patch, patchErr := a.gitBytes(ctx, patchArgs...)
		if patchErr != nil {
			return Analysis{}, fmt.Errorf("reading line changes for %q: %w", path, patchErr)
		}
		hunks, parseErr := ParseHunks(patch)
		if parseErr != nil {
			return Analysis{}, fmt.Errorf("parsing line changes for %q: %w", path, parseErr)
		}
		file.Change.BaseRanges, file.Change.CurrentRanges = hunkRanges(hunks)
		file.Edits = lineEdits(hunks)
		files = append(files, file)
	}

	for _, path := range untracked {
		if _, exists := trackedPaths[path]; exists {
			continue
		}
		content, readErr := a.readContained(path)
		if readErr != nil {
			return Analysis{}, fmt.Errorf("reading untracked content for %q: %w", path, readErr)
		}
		currentRanges := make([]verification.LineRange, 0, 1)
		if lines := sourceLineCount(content); lines > 0 {
			currentRanges = append(currentRanges, verification.LineRange{Start: 1, End: lines})
		}
		files = append(files, File{
			Change: verification.ChangedFile{
				Path:          path,
				Change:        verification.ChangeUntracked,
				BaseRanges:    make([]verification.LineRange, 0),
				CurrentRanges: currentRanges,
			},
			CurrentContent: content,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Change.Path < files[j].Change.Path })

	declarations, err := a.changedDeclarations(files)
	if err != nil {
		return Analysis{}, err
	}
	changedFiles := make([]verification.ChangedFile, len(files))
	for index := range files {
		changedFiles[index] = files[index].Change
	}
	statusOutput, err := a.gitBytes(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=normal", "--", ".")
	if err != nil {
		return Analysis{}, fmt.Errorf("reading final worktree status: %w", err)
	}
	if err := a.verifySnapshotStable(ctx, mergeBase, rawOutput, untrackedOutput, files); err != nil {
		return Analysis{}, err
	}
	repository := verification.Repository{
		RequestedBase:   options.Base,
		BaseCommit:      base,
		MergeBaseCommit: mergeBase,
		HeadCommit:      head,
		Workspace:       workspaceRelative,
		Dirty:           len(statusOutput) > 0,
	}
	repository.SnapshotID = snapshotIdentity(repository, files)
	return Analysis{
		Repository:    repository,
		Change:        verification.Change{Files: changedFiles, Declarations: declarations},
		Impact:        verification.Impact{Packages: make([]verification.ImpactedPackage, 0)},
		Files:         files,
		Packages:      make([]Package, 0),
		Uncertainties: make([]verification.Uncertainty, 0),
		Complete:      true,
	}, nil
}

func (a *Analyzer) gitText(ctx context.Context, args ...string) (string, error) {
	output, err := a.gitBytes(ctx, args...)
	return strings.TrimSpace(string(output)), err
}

func (a *Analyzer) gitBytes(ctx context.Context, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	result, err := a.runner.Run(ctx, execution.Command{Name: "git", Args: args, Dir: a.workspace.Root()}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = fmt.Sprintf("git exited with status %d", result.ExitCode)
		}
		return nil, fmt.Errorf("%s", message)
	}
	return stdout.Bytes(), nil
}

func (a *Analyzer) gitObject(ctx context.Context, commit, repoPath string) ([]byte, error) {
	return a.gitBytes(ctx, "show", commit+":"+repoPath)
}

func (a *Analyzer) verifySnapshotStable(ctx context.Context, mergeBase string, rawBefore, untrackedBefore []byte, files []File) error {
	rawAfter, err := a.gitBytes(ctx, "diff", "--raw", "-z", "--find-renames", "--relative", mergeBase, "--", ".")
	if err != nil {
		return fmt.Errorf("rechecking tracked snapshot: %w", err)
	}
	untrackedAfter, err := a.gitBytes(ctx, "ls-files", "--others", "--exclude-standard", "--full-name", "-z", "--", ".")
	if err != nil {
		return fmt.Errorf("rechecking untracked snapshot: %w", err)
	}
	if !bytes.Equal(rawBefore, rawAfter) || !bytes.Equal(untrackedBefore, untrackedAfter) {
		return fmt.Errorf("working tree changed during analysis; retry verification")
	}
	for _, file := range files {
		if file.Change.Change == verification.ChangeDeleted {
			if _, statErr := os.Lstat(filepath.Join(a.workspace.Root(), filepath.FromSlash(file.Change.Path))); statErr == nil || !os.IsNotExist(statErr) {
				return fmt.Errorf("working tree changed during analysis; retry verification")
			}
			continue
		}
		current, readErr := a.readContained(file.Change.Path)
		if readErr != nil || !bytes.Equal(current, file.CurrentContent) {
			return fmt.Errorf("working tree changed during analysis; retry verification")
		}
	}
	return nil
}

func (a *Analyzer) readContained(path string) ([]byte, error) {
	resolved, err := a.workspace.Resolve(filepath.FromSlash(path))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file")
	}
	return os.ReadFile(resolved)
}

func rawChangeKind(status byte) (verification.ChangeKind, error) {
	switch status {
	case 'A', 'C':
		return verification.ChangeAdded, nil
	case 'M', 'T':
		return verification.ChangeModified, nil
	case 'D':
		return verification.ChangeDeleted, nil
	case 'R':
		return verification.ChangeRenamed, nil
	default:
		return "", fmt.Errorf("unsupported Git change status %q", status)
	}
}

func hunkRanges(hunks []Hunk) ([]verification.LineRange, []verification.LineRange) {
	base := make([]verification.LineRange, 0, len(hunks))
	current := make([]verification.LineRange, 0, len(hunks))
	for _, hunk := range hunks {
		if hunk.OldCount > 0 {
			base = append(base, verification.LineRange{Start: hunk.OldStart, End: hunk.OldStart + hunk.OldCount - 1})
		}
		if hunk.NewCount > 0 {
			current = append(current, verification.LineRange{Start: hunk.NewStart, End: hunk.NewStart + hunk.NewCount - 1})
		}
	}
	return base, current
}

func lineEdits(hunks []Hunk) []verification.LineEdit {
	edits := make([]verification.LineEdit, 0, len(hunks))
	for _, hunk := range hunks {
		edits = append(edits, verification.LineEdit{
			BaseStart: hunk.OldStart, BaseCount: hunk.OldCount,
			CurrentStart: hunk.NewStart, CurrentCount: hunk.NewCount,
		})
	}
	return edits
}

func parseUntracked(data []byte, workspaceRelative string) ([]string, error) {
	parts := bytes.Split(data, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		path := filepath.ToSlash(string(part))
		if workspaceRelative != "." {
			prefix := strings.TrimSuffix(workspaceRelative, "/") + "/"
			if !strings.HasPrefix(path, prefix) {
				return nil, fmt.Errorf("untracked path %q is outside the workspace", path)
			}
			path = strings.TrimPrefix(path, prefix)
		}
		clean, err := cleanRelativePath(path)
		if err != nil {
			return nil, fmt.Errorf("validating untracked path: %w", err)
		}
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	return paths, nil
}

func cleanRelativePath(path string) (string, error) {
	path = filepath.ToSlash(path)
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("path %q is not a relative path", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q escapes the workspace", path)
	}
	return clean, nil
}

func repositoryPath(workspaceRelative, path string) string {
	if workspaceRelative == "." {
		return path
	}
	return strings.TrimSuffix(workspaceRelative, "/") + "/" + path
}

func sourceLineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

func snapshotIdentity(repository verification.Repository, files []File) string {
	hash := sha256.New()
	for _, value := range []string{repository.BaseCommit, repository.MergeBaseCommit, repository.HeadCommit, repository.Workspace} {
		_, _ = fmt.Fprintf(hash, "%d:%s\x00", len(value), value)
	}
	for _, file := range files {
		for _, value := range []string{file.Change.Path, file.Change.PreviousPath, string(file.Change.Change)} {
			_, _ = fmt.Fprintf(hash, "%d:%s\x00", len(value), value)
		}
		_, _ = fmt.Fprintf(hash, "%d:", len(file.BaseContent))
		hash.Write(file.BaseContent)
		_, _ = fmt.Fprintf(hash, "%d:", len(file.CurrentContent))
		hash.Write(file.CurrentContent)
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil))
}
