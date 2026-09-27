package retrievalstudy

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/agentic-mcps/go/internal/intelligence/retrieval"
)

const (
	maximumTextCandidateFiles     = 100_000
	maximumTextCandidateFileSize  = 1 << 20
	maximumTextCandidateBytes     = 64 << 20
	maximumTextCandidateFragments = retrieval.MaximumTextLineFragments
)

type sourceMeta struct {
	lines  int
	goFile bool
}

//nolint:govet // Keep extracted source, file inventory, and coverage together.
type archivedSource struct {
	workspace          string
	goFiles            []retrieval.File
	textFiles          []retrieval.File
	textCandidateIndex TextCandidateIndexCoverage
	meta               map[string]sourceMeta
	expected           map[string]archiveExpected
	coverage           Coverage
}

type archiveExpected struct {
	objectID string
	size     int64
}

type archiveLimits struct {
	maxSourceBytes      int64
	maxFileBytes        int64
	timeout             time.Duration
	indexTextCandidates bool
}

// exportCommit uses git archive for the exact manifest commit. It never reads
// from the checkout's working tree, index, or current branch.
func exportCommit(ctx context.Context, repositoryPath, workspace string, manifest Manifest, limits archiveLimits) (Repository, archivedSource, error) {
	repositoryRoot, err := gitText(ctx, limits.timeout, repositoryPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repository{}, archivedSource{}, fmt.Errorf("locating Git repository: %w", err)
	}
	repositoryRoot = strings.TrimSpace(repositoryRoot)
	remotes, err := gitText(ctx, limits.timeout, repositoryRoot, "remote")
	if err != nil {
		return Repository{}, archivedSource{}, fmt.Errorf("reading Git remotes: %w", err)
	}
	hasOrigin := false
	for _, remoteName := range strings.Fields(remotes) {
		if remoteName == "origin" {
			hasOrigin = true
			break
		}
	}
	repositoryID := ""
	if hasOrigin {
		remote, remoteErr := gitText(ctx, limits.timeout, repositoryRoot, "remote", "get-url", "origin")
		if remoteErr != nil {
			return Repository{}, archivedSource{}, fmt.Errorf("reading origin repository identity: %w", remoteErr)
		}
		repositoryID, err = canonicalRepositoryID(strings.TrimSpace(remote))
		if err != nil {
			return Repository{}, archivedSource{}, err
		}
		if repositoryID != manifest.RepositoryID {
			return Repository{}, archivedSource{}, fmt.Errorf("manifest repository_id %q does not match origin %q", manifest.RepositoryID, repositoryID)
		}
	} else if strings.HasPrefix(manifest.RepositoryID, "local/") && filepath.Base(repositoryRoot) == strings.TrimPrefix(manifest.RepositoryID, "local/") {
		repositoryID = manifest.RepositoryID
	} else {
		return Repository{}, archivedSource{}, fmt.Errorf("repository has no origin; repository_id must use local/<exact-repository-basename>")
	}
	resolvedCommit, err := gitText(ctx, limits.timeout, repositoryRoot, "rev-parse", "--verify", manifest.Commit+"^{commit}")
	if err != nil {
		return Repository{}, archivedSource{}, fmt.Errorf("resolving pinned commit: %w", err)
	}
	resolvedCommit = strings.TrimSpace(resolvedCommit)
	if resolvedCommit != manifest.Commit {
		return Repository{}, archivedSource{}, fmt.Errorf("manifest commit did not resolve to the exact object id")
	}
	tree, err := gitText(ctx, limits.timeout, repositoryRoot, "rev-parse", "--verify", manifest.Commit+"^{tree}")
	if err != nil {
		return Repository{}, archivedSource{}, fmt.Errorf("resolving pinned tree: %w", err)
	}
	resolvedTree := strings.TrimSpace(tree)
	if resolvedTree != manifest.Tree {
		return Repository{}, archivedSource{}, fmt.Errorf("manifest tree did not match the exact pinned commit tree")
	}
	repository := Repository{ID: repositoryID, Commit: resolvedCommit, Tree: resolvedTree}
	patterns, coverage, expected, err := archiveSelection(ctx, repositoryRoot, manifest.Commit, limits.timeout)
	if err != nil {
		return Repository{}, archivedSource{}, err
	}
	source, err := extractArchive(ctx, repositoryRoot, workspace, manifest.Commit, patterns, expected, coverage, limits)
	if err != nil {
		return Repository{}, archivedSource{}, err
	}
	return repository, source, nil
}

func canonicalRepositoryID(remote string) (string, error) {
	var host, repositoryPath string
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Host == "" {
			return "", fmt.Errorf("origin URL cannot be normalized to host/owner/repository")
		}
		host, repositoryPath = parsed.Host, parsed.Path
	} else {
		colon := strings.Index(remote, ":")
		if colon < 0 {
			return "", fmt.Errorf("origin must be an HTTPS, SSH, or SCP-style remote URL")
		}
		hostPart := remote[:colon]
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		host, repositoryPath = hostPart, remote[colon+1:]
	}
	host = strings.ToLower(strings.TrimSpace(host))
	repositoryPath = strings.Trim(strings.TrimSpace(repositoryPath), "/")
	repositoryPath = strings.TrimSuffix(repositoryPath, ".git")
	if host == "" || repositoryPath == "" {
		return "", fmt.Errorf("origin URL cannot be normalized to host/owner/repository")
	}
	return strings.ToLower(host + "/" + repositoryPath), nil
}

func gitText(parent context.Context, timeout time.Duration, repositoryPath string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repositoryPath}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("git %s failed: %w", strings.Join(arguments, " "), err)
	}
	return string(output), nil
}

func archiveSelection(parent context.Context, repositoryRoot, commit string, timeout time.Duration) ([]string, Coverage, map[string]archiveExpected, error) {
	output, err := gitText(parent, timeout, repositoryRoot, "ls-tree", "-r", "-l", "-z", "--full-tree", commit)
	if err != nil {
		return nil, Coverage{}, nil, fmt.Errorf("enumerating pinned Git tree: %w", err)
	}
	patterns := make(map[string]struct{})
	expected := make(map[string]archiveExpected)
	coverage := Coverage{SourceArchiveComplete: true}
	for _, record := range bytes.Split([]byte(output), []byte{0}) {
		if len(record) == 0 {
			continue
		}
		separator := bytes.IndexByte(record, '\t')
		if separator < 0 {
			return nil, Coverage{}, nil, fmt.Errorf("git tree entry has an invalid record")
		}
		metadata := strings.Fields(string(record[:separator]))
		if len(metadata) < 3 {
			return nil, Coverage{}, nil, fmt.Errorf("git tree entry has incomplete metadata")
		}
		mode, objectType := metadata[0], metadata[1]
		name := string(record[separator+1:])
		if mode == "120000" {
			if supportedTextPath(name) {
				coverage.SymlinksNotIndexed++
				markArchiveIncomplete(&coverage, "one or more supported source paths are symlinks and were not indexed")
			}
			continue
		}
		if objectType != "blob" || (mode != "100644" && mode != "100755") {
			if supportedTextPath(name) {
				coverage.OtherArchiveEntriesIgnored++
				markArchiveIncomplete(&coverage, "one or more supported source paths are non-regular Git entries and were not indexed")
			}
			continue
		}
		if len(metadata) < 4 {
			return nil, Coverage{}, nil, fmt.Errorf("git tree entry has no blob size")
		}
		size, parseErr := strconv.ParseInt(metadata[3], 10, 64)
		if parseErr != nil || size < 0 {
			return nil, Coverage{}, nil, fmt.Errorf("git tree entry has an invalid blob size")
		}
		coverage.TrackedRegularFiles++
		coverage.TrackedRegularBytes += size
		if !supportedTextPath(name) {
			continue
		}
		if !utf8.ValidString(name) {
			coverage.RejectedTextFiles++
			coverage.RejectedTextBytes += size
			markArchiveIncomplete(&coverage, "one or more supported source paths are not valid UTF-8 and were not indexed")
			continue
		}
		coverage.ExpectedSupportedSourceFiles++
		coverage.ExpectedSupportedSourceBytes += size
		expected[name] = archiveExpected{objectID: metadata[2], size: size}
		patterns[archivePattern(name)] = struct{}{}
	}
	result := make([]string, 0, len(patterns))
	for pattern := range patterns {
		result = append(result, pattern)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, Coverage{}, nil, fmt.Errorf("pinned Git tree contains no supported Go or text source files")
	}
	return result, coverage, expected, nil
}

func archivePattern(name string) string {
	extension := path.Ext(name)
	if extension != "" && extension != ".gitignore" && extension != ".editorconfig" && extension != ".gitattributes" {
		return "(glob)**/*" + extension
	}
	return "(glob)**/" + path.Base(name)
}

func extractArchive(parent context.Context, repositoryRoot, workspace, commit string, patterns []string, expected map[string]archiveExpected, coverage Coverage, limits archiveLimits) (archivedSource, error) {
	ctx, cancel := context.WithTimeout(parent, limits.timeout)
	defer cancel()
	arguments := []string{"-C", repositoryRoot, "archive", "--format=tar", "--prefix=repo/", commit, "--"}
	for _, pattern := range patterns {
		arguments = append(arguments, ":"+pattern)
	}
	command := exec.CommandContext(ctx, "git", arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return archivedSource{}, fmt.Errorf("opening git archive stream: %w", err)
	}
	var stderr limitedBuffer
	stderr.limit = 64 << 10
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return archivedSource{}, fmt.Errorf("starting git archive: %w", err)
	}
	source, extractErr := readArchive(tar.NewReader(stdout), workspace, expected, coverage, limits)
	if extractErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if extractErr != nil {
		return archivedSource{}, extractErr
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return archivedSource{}, ctx.Err()
		}
		return archivedSource{}, fmt.Errorf("git archive failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return source, nil
}

func readArchive(reader *tar.Reader, workspace string, expected map[string]archiveExpected, coverage Coverage, limits archiveLimits) (archivedSource, error) {
	source := archivedSource{workspace: workspace, goFiles: []retrieval.File{}, meta: make(map[string]sourceMeta), expected: expected, coverage: coverage}
	if limits.indexTextCandidates {
		source.textFiles = []retrieval.File{}
		source.textCandidateIndex = TextCandidateIndexCoverage{
			Status: "complete", MaximumFiles: maximumTextCandidateFiles,
			MaximumFileBytes: maximumTextCandidateFileSize, MaximumTotalBytes: maximumTextCandidateBytes,
			MaximumFragments: maximumTextCandidateFragments,
		}
	}
	seen := make(map[string]bool, len(expected))
	var extractedSourceBytes int64
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return archivedSource{}, fmt.Errorf("reading Git archive: %w", nextErr)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name, isDirectory, pathErr := archivePath(header.Name)
		if errors.Is(pathErr, errArchivePathEncoding) {
			continue
		}
		if pathErr != nil {
			return archivedSource{}, pathErr
		}
		if name == "" && isDirectory {
			continue
		}
		if isDirectory || header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(filepath.Join(workspace, filepath.FromSlash(name)), 0o700); err != nil {
				return archivedSource{}, fmt.Errorf("creating archive directory: %w", err)
			}
			continue
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if !supportedTextPath(name) {
			continue
		}
		expectedFile, selected := expected[name]
		if !selected {
			source.coverage.OtherArchiveEntriesIgnored++
			markArchiveIncomplete(&source.coverage, "git archive yielded a supported source path absent from the pinned tree selection")
			continue
		}
		seen[name] = true
		if header.Size < 0 || header.Size > limits.maxFileBytes {
			return archivedSource{}, fmt.Errorf("source file %q exceeds the per-file byte limit", name)
		}
		if extractedSourceBytes+header.Size > limits.maxSourceBytes {
			return archivedSource{}, fmt.Errorf("supported source exceeds the configured byte limit")
		}
		extractedSourceBytes += header.Size
		contents, readErr := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if readErr != nil {
			return archivedSource{}, fmt.Errorf("reading source file %q: %w", name, readErr)
		}
		if int64(len(contents)) != header.Size {
			return archivedSource{}, fmt.Errorf("archive source file %q has an inconsistent size", name)
		}
		if gitBlobObjectID(contents, len(expectedFile.objectID)) != expectedFile.objectID {
			source.coverage.ArchiveTransformedSourceFiles++
			source.coverage.ArchiveTransformedSourceBytes += expectedFile.size
			markArchiveIncomplete(&source.coverage, "git archive transformed one or more source blobs; transformed bytes were excluded")
			continue
		}
		if !utf8.Valid(contents) || containsNUL(contents) {
			source.coverage.RejectedTextFiles++
			source.coverage.RejectedTextBytes += header.Size
			markArchiveIncomplete(&source.coverage, "one or more supported source blobs are binary or not valid UTF-8 and were not indexed")
			continue
		}
		filename := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			return archivedSource{}, fmt.Errorf("creating source directory: %w", err)
		}
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return archivedSource{}, fmt.Errorf("creating archived source file: %w", err)
		}
		_, copyErr := file.Write(contents)
		closeErr := file.Close()
		if copyErr != nil {
			return archivedSource{}, fmt.Errorf("writing archived source file: %w", copyErr)
		}
		if closeErr != nil {
			return archivedSource{}, fmt.Errorf("closing archived source file: %w", closeErr)
		}
		lineCount := countLines(contents)
		source.coverage.SupportedSourceFiles++
		source.coverage.SupportedSourceBytes += header.Size
		if strings.HasSuffix(strings.ToLower(name), ".go") {
			digest := sha256.Sum256(contents)
			source.goFiles = append(source.goFiles, retrieval.File{Path: name, Digest: hex.EncodeToString(digest[:]), Contents: contents})
			source.meta[name] = sourceMeta{lines: lineCount, goFile: true}
			source.coverage.GoFiles++
			source.coverage.GoBytes += header.Size
		} else {
			source.meta[name] = sourceMeta{lines: lineCount}
			source.coverage.SupportedTextFiles++
			source.coverage.SupportedTextBytes += header.Size
			if limits.indexTextCandidates {
				source.textCandidateIndex.SupportedFiles++
				source.textCandidateIndex.SupportedBytes += header.Size
				if source.textCandidateIndex.Status == "complete" &&
					len(source.textFiles) < maximumTextCandidateFiles &&
					header.Size <= maximumTextCandidateFileSize &&
					source.textCandidateIndex.IndexedBytes+header.Size <= maximumTextCandidateBytes {
					digest := sha256.Sum256(contents)
					source.textFiles = append(source.textFiles, retrieval.File{
						Path: name, Digest: hex.EncodeToString(digest[:]), Contents: contents,
					})
					source.textCandidateIndex.IndexedFiles++
					source.textCandidateIndex.IndexedBytes += header.Size
				} else {
					source.textCandidateIndex.Status = "partial"
					source.textCandidateIndex.OmittedFiles++
					source.textCandidateIndex.OmittedBytes += header.Size
					if source.textCandidateIndex.Reason == "" {
						source.textCandidateIndex.Reason = "bounded text candidate capture reached a file-count, per-file, or total-byte limit"
					}
				}
			}
		}
	}
	for name, expectedFile := range expected {
		if seen[name] {
			continue
		}
		source.coverage.ArchiveOmittedSourceFiles++
		source.coverage.ArchiveOmittedSourceBytes += expectedFile.size
	}
	if source.coverage.ArchiveOmittedSourceFiles > 0 {
		markArchiveIncomplete(&source.coverage, "git archive omitted one or more supported source blobs, possibly due to export-ignore attributes")
	}
	if limits.indexTextCandidates && !source.coverage.SourceArchiveComplete && source.textCandidateIndex.Status == "complete" {
		source.textCandidateIndex.Status = "partial"
		source.textCandidateIndex.Reason = source.coverage.SourceArchiveIncompleteReason
	}
	sortGoFiles(source.goFiles)
	return source, nil
}

var errArchivePathEncoding = errors.New("git archive path is not valid UTF-8")

func archivePath(headerName string) (string, bool, error) {
	if headerName == "repo/" || headerName == "repo" {
		return "", true, nil
	}
	if !strings.HasPrefix(headerName, "repo/") {
		return "", false, fmt.Errorf("git archive entry %q escaped its expected prefix", headerName)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(headerName, "repo/"), "/")
	if name == "" {
		return "", true, nil
	}
	if !filepath.IsLocal(filepath.FromSlash(name)) || path.Clean(name) != name {
		return "", false, fmt.Errorf("git archive contains an unsafe path")
	}
	if !utf8.ValidString(name) {
		return "", false, errArchivePathEncoding
	}
	if !isRegularPath(name) {
		return name, true, nil
	}
	return name, false, nil
}

func gitBlobObjectID(contents []byte, objectIDLength int) string {
	var digest interface {
		Write([]byte) (int, error)
		Sum([]byte) []byte
	}
	if objectIDLength == 64 {
		digest = sha256.New()
	} else {
		digest = sha1.New()
	}
	_, _ = fmt.Fprintf(digest, "blob %d\x00", len(contents))
	_, _ = digest.Write(contents)
	return hex.EncodeToString(digest.Sum(nil))
}

func markArchiveIncomplete(coverage *Coverage, reason string) {
	coverage.SourceArchiveComplete = false
	if coverage.SourceArchiveIncompleteReason == "" {
		coverage.SourceArchiveIncompleteReason = reason
	}
}

func isRegularPath(name string) bool {
	return name != "" && path.Base(name) != "." && path.Base(name) != ".."
}

func supportedTextPath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".go", ".md", ".rst", ".txt", ".yaml", ".yml", ".json", ".toml", ".proto", ".mod", ".sum", ".work":
		return true
	}
	switch path.Base(name) {
	case "Makefile", "GNUmakefile", "Dockerfile", "Containerfile", ".gitignore", ".editorconfig", ".gitattributes":
		return true
	default:
		return false
	}
}

func countLines(contents []byte) int {
	if len(contents) == 0 {
		return 0
	}
	count := 0
	for _, current := range contents {
		if current == '\n' {
			count++
		}
	}
	if contents[len(contents)-1] != '\n' {
		count++
	}
	return count
}

func containsNUL(contents []byte) bool {
	for _, current := range contents {
		if current == 0 {
			return true
		}
	}
	return false
}

func sortGoFiles(files []retrieval.File) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	mu        sync.Mutex
	limit     int
	truncated bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.limit <= 0 || buffer.buffer.Len() >= buffer.limit {
		buffer.truncated = true
		return len(data), nil
	}
	remaining := buffer.limit - buffer.buffer.Len()
	if len(data) > remaining {
		_, _ = buffer.buffer.Write(data[:remaining])
		buffer.truncated = true
		return len(data), nil
	}
	return buffer.buffer.Write(data)
}

func (buffer *limitedBuffer) Len() int {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Len()
}

func (buffer *limitedBuffer) Bytes() []byte {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func (buffer *limitedBuffer) String() string {
	return string(buffer.Bytes())
}
