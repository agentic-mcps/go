package retrievalstudy

import (
	"archive/tar"
	"bytes"
	"testing"
)

func TestLimitedBufferCapsCapturedOutputAndReportsTruncation(t *testing.T) {
	buffer := limitedBuffer{limit: 10}
	if written, err := buffer.Write([]byte("12345678")); err != nil || written != 8 {
		t.Fatalf("first write = (%d, %v), want (8, nil)", written, err)
	}
	if written, err := buffer.Write([]byte("abc")); err != nil || written != 3 {
		t.Fatalf("second write = (%d, %v), want (3, nil)", written, err)
	}
	if got := buffer.Len(); got != 10 {
		t.Fatalf("captured bytes = %d, want cap 10", got)
	}
	if got := string(buffer.Bytes()); got != "12345678ab" {
		t.Fatalf("captured output = %q, want %q", got, "12345678ab")
	}
	if !buffer.truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestLimitedBufferExactlyAtLimitIsNotTruncated(t *testing.T) {
	buffer := limitedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("four")); err != nil || written != 4 {
		t.Fatalf("write = (%d, %v), want (4, nil)", written, err)
	}
	if buffer.truncated {
		t.Fatal("truncated = true, want false when output exactly fits")
	}
}

func TestReadArchiveBoundsTextCandidateCapture(t *testing.T) {
	contents := bytes.Repeat([]byte("x"), maximumTextCandidateFileSize+1)
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{
		Name: "repo/docs/large.md", Mode: 0o600, Size: int64(len(contents)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	expected := map[string]archiveExpected{
		"docs/large.md": {objectID: gitBlobObjectID(contents, 40), size: int64(len(contents))},
	}
	source, err := readArchive(tar.NewReader(&archive), t.TempDir(), expected, Coverage{SourceArchiveComplete: true}, archiveLimits{
		maxSourceBytes: int64(len(contents) + 1), maxFileBytes: int64(len(contents) + 1), indexTextCandidates: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.textCandidateIndex.Status != "partial" || source.textCandidateIndex.IndexedFiles != 0 || source.textCandidateIndex.OmittedFiles != 1 {
		t.Fatalf("text candidate coverage = %+v, want one oversized file omitted", source.textCandidateIndex)
	}
	if source.textCandidateIndex.OmittedBytes != int64(len(contents)) || len(source.textFiles) != 0 {
		t.Fatalf("text capture bytes/files = (%d, %d), want (%d, 0)", source.textCandidateIndex.OmittedBytes, len(source.textFiles), len(contents))
	}
}
