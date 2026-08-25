// Package releasebundle creates reproducible agentic-go release artifacts.
package releasebundle

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// File maps one local file into the root of a release archive.
type File struct {
	Source string
	Name   string
	Mode   int64
}

// WriteArchive atomically creates a deterministically ordered tar.gz archive.
func WriteArchive(destination string, files []File) error {
	if len(files) == 0 {
		return fmt.Errorf("release archive has no files")
	}
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	seen := make(map[string]struct{}, len(sorted))
	for _, file := range sorted {
		if err := validateArchiveName(file.Name); err != nil {
			return err
		}
		if _, exists := seen[file.Name]; exists {
			return fmt.Errorf("duplicate release archive path %q", file.Name)
		}
		seen[file.Name] = struct{}{}
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("creating release output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".agentic-go-archive-*")
	if err != nil {
		return fmt.Errorf("creating release archive: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	gzipWriter, err := gzip.NewWriterLevel(temporary, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("creating gzip stream: %w", err)
	}
	gzipWriter.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range sorted {
		if err := appendFile(tarWriter, file); err != nil {
			if closeErr := tarWriter.Close(); closeErr != nil {
				return err
			}
			if closeErr := gzipWriter.Close(); closeErr != nil {
				return err
			}
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("closing release tar stream: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("closing release gzip stream: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("syncing release archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing release archive: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("publishing release archive: %w", err)
	}
	committed = true
	return nil
}

// WriteChecksums atomically records SHA-256 checksums in filename order.
func WriteChecksums(destination string, artifacts []string) error {
	if len(artifacts) == 0 {
		return fmt.Errorf("checksum manifest has no artifacts")
	}
	sorted := append([]string(nil), artifacts...)
	sort.Slice(sorted, func(i, j int) bool { return filepath.Base(sorted[i]) < filepath.Base(sorted[j]) })
	var contents strings.Builder
	for _, artifact := range sorted {
		file, err := os.Open(artifact)
		if err != nil {
			return fmt.Errorf("opening release artifact %s: %w", artifact, err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, bufio.NewReader(file))
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("hashing release artifact %s: %w", artifact, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("closing release artifact %s: %w", artifact, closeErr)
		}
		contents.WriteString(hex.EncodeToString(hash.Sum(nil)))
		contents.WriteString("  ")
		contents.WriteString(filepath.Base(artifact))
		contents.WriteByte('\n')
	}
	return atomicWrite(destination, []byte(contents.String()), 0o644)
}

func appendFile(writer *tar.Writer, file File) (resultErr error) {
	source, err := os.Open(file.Source)
	if err != nil {
		return fmt.Errorf("opening release file %s: %w", file.Source, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, source.Close())
	}()
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("inspecting release file %s: %w", file.Source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release file %s is not regular", file.Source)
	}
	header := &tar.Header{
		Name:       file.Name,
		Mode:       file.Mode,
		Size:       info.Size(),
		ModTime:    time.Unix(0, 0).UTC(),
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
		Typeflag:   tar.TypeReg,
		Format:     tar.FormatUSTAR,
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("writing release header %s: %w", file.Name, err)
	}
	if _, err := io.Copy(writer, source); err != nil {
		return fmt.Errorf("writing release file %s: %w", file.Name, err)
	}
	return nil
}

func validateArchiveName(name string) error {
	if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == "." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("unsafe release archive path %q", name)
	}
	return nil
}

func atomicWrite(destination string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".agentic-go-manifest-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, destination); err != nil {
		return err
	}
	committed = true
	return nil
}
