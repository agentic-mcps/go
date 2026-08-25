package releasebundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteArchiveIsDeterministicAndOrdered(t *testing.T) {
	root := t.TempDir()
	writeReleaseFixture(t, filepath.Join(root, "binary"), "binary")
	writeReleaseFixture(t, filepath.Join(root, "notice"), "notice")
	files := []File{
		{Source: filepath.Join(root, "notice"), Name: "LICENSES/notice.txt", Mode: 0o644},
		{Source: filepath.Join(root, "binary"), Name: "agentic-go", Mode: 0o755},
	}
	first := filepath.Join(root, "first.tar.gz")
	second := filepath.Join(root, "second.tar.gz")
	if err := WriteArchive(first, files); err != nil {
		t.Fatal(err)
	}
	if err := WriteArchive(second, files); err != nil {
		t.Fatal(err)
	}
	if releaseHash(t, first) != releaseHash(t, second) {
		t.Fatal("archives differ for identical inputs")
	}

	archive, err := os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = archive.Close() })
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
	}
	if strings.Join(names, ",") != "LICENSES/notice.txt,agentic-go" {
		t.Fatalf("archive order = %v", names)
	}
}

func TestWriteArchiveRejectsUnsafeAndDuplicateNames(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	writeReleaseFixture(t, source, "data")
	for _, files := range [][]File{
		{{Source: source, Name: "../escape", Mode: 0o644}},
		{{Source: source, Name: "same", Mode: 0o644}, {Source: source, Name: "same", Mode: 0o644}},
	} {
		if err := WriteArchive(filepath.Join(t.TempDir(), "archive.tar.gz"), files); err == nil {
			t.Fatalf("WriteArchive(%#v) accepted invalid names", files)
		}
	}
}

func TestWriteChecksumsUsesSortedBasenames(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "a.tar.gz")
	zulu := filepath.Join(root, "z.tar.gz")
	writeReleaseFixture(t, alpha, "alpha")
	writeReleaseFixture(t, zulu, "zulu")
	manifest := filepath.Join(root, "checksums.txt")
	if err := WriteChecksums(manifest, []string{zulu, alpha}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "  a.tar.gz") || !strings.HasSuffix(lines[1], "  z.tar.gz") {
		t.Fatalf("manifest = %q", contents)
	}
}

func writeReleaseFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func releaseHash(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(contents)
}
