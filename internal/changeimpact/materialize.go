package changeimpact

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

const (
	maxBaseArchiveBytes = 256 << 20
	maxBaseFiles        = 100_000
	maxBaseSourceBytes  = 512 << 20
)

// MaterializeBase extracts the resolved merge-base into a caller-owned cache
// directory without modifying the target worktree or persistent Git metadata.
func (a *Analyzer) MaterializeBase(ctx context.Context, repository verification.Repository, destination string) (string, error) {
	if repository.MergeBaseCommit == "" {
		return "", fmt.Errorf("merge-base commit is empty")
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", fmt.Errorf("resolving base destination: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspecting base destination: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("base destination is not a directory")
	}
	archivePath := filepath.Join(absolute, "base.tar")
	if _, archiveErr := a.gitBytes(ctx, "archive", "--format=tar", "--output="+archivePath, repository.MergeBaseCommit); archiveErr != nil {
		return "", fmt.Errorf("archiving merge-base: %w", archiveErr)
	}
	defer func() { _ = os.Remove(archivePath) }()
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return "", fmt.Errorf("inspecting base archive: %w", err)
	}
	if archiveInfo.Size() > maxBaseArchiveBytes {
		return "", fmt.Errorf("base archive exceeds %d bytes", maxBaseArchiveBytes)
	}

	tree := filepath.Join(absolute, "tree")
	if mkdirErr := os.Mkdir(tree, 0o700); mkdirErr != nil {
		return "", fmt.Errorf("creating base tree: %w", mkdirErr)
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening base archive: %w", err)
	}
	extractErr := extractBaseArchive(tree, archive)
	closeErr := archive.Close()
	if extractErr != nil {
		return "", extractErr
	}
	if closeErr != nil {
		return "", fmt.Errorf("closing base archive: %w", closeErr)
	}
	workspacePath := tree
	if repository.Workspace != "" && repository.Workspace != "." {
		relative, relativeErr := cleanRelativePath(repository.Workspace)
		if relativeErr != nil {
			return "", fmt.Errorf("validating base workspace path: %w", relativeErr)
		}
		workspacePath = filepath.Join(tree, filepath.FromSlash(relative))
	}
	workspaceInfo, err := os.Stat(workspacePath)
	if err != nil {
		return "", fmt.Errorf("inspecting materialized base workspace: %w", err)
	}
	if !workspaceInfo.IsDir() {
		return "", fmt.Errorf("materialized base workspace is not a directory")
	}
	return workspacePath, nil
}

func extractBaseArchive(root string, source io.Reader) error {
	reader := tar.NewReader(source)
	files := 0
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading base archive: %w", err)
		}
		files++
		if files > maxBaseFiles {
			return fmt.Errorf("base archive exceeds %d entries", maxBaseFiles)
		}
		if header.Size < 0 || total > maxBaseSourceBytes-header.Size {
			return fmt.Errorf("base archive exceeds %d extracted bytes", maxBaseSourceBytes)
		}
		total += header.Size
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name, err := cleanArchivePath(header.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("creating base parent directory: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("creating base directory: %w", err)
			}
		case tar.TypeReg:
			mode := os.FileMode(0o600)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o700
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return fmt.Errorf("creating base file %q: %w", name, err)
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("extracting base file %q: %w", name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("closing base file %q: %w", name, closeErr)
			}
		case tar.TypeSymlink:
			if err := createContainedSymlink(root, target, header.Linkname); err != nil {
				return fmt.Errorf("extracting base symlink %q: %w", name, err)
			}
		default:
			return fmt.Errorf("base archive contains unsupported entry type %d for %q", header.Typeflag, name)
		}
	}
}

func cleanArchivePath(path string) (string, error) {
	path = filepath.ToSlash(path)
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("base archive path %q is invalid", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("base archive path %q escapes the destination", path)
	}
	return clean, nil
}

func createContainedSymlink(root, target, link string) error {
	if link == "" || filepath.IsAbs(filepath.FromSlash(link)) || strings.ContainsRune(link, 0) {
		return fmt.Errorf("symlink target is invalid")
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(link)))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("symlink target escapes the destination")
	}
	if err := os.Symlink(filepath.FromSlash(link), target); err != nil {
		return err
	}
	return nil
}
