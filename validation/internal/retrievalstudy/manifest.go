package retrievalstudy

import (
	"bytes"
	"encoding/json"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode"
)

var fullCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var repositoryIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.:-]*/[a-z0-9_.-]+(/[a-z0-9_.-]+)*$`)

var evidenceTypes = map[string]struct{}{
	"declaration": {},
	"caller":      {},
	"test":        {},
	"documentation": {},
	"configuration": {},
}

// LoadManifest decodes a strict, versioned JSON manifest and validates all
// fields that do not depend on the archived source tree.
func LoadManifest(filename string) (Manifest, string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("opening manifest: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	closeErr := file.Close()
	if err != nil {
		return Manifest{}, "", fmt.Errorf("reading manifest: %w", err)
	}
	if closeErr != nil {
		return Manifest{}, "", fmt.Errorf("closing manifest: %w", closeErr)
	}
	if len(data) > 4<<20 {
		return Manifest{}, "", fmt.Errorf("manifest exceeds 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("decoding manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, "", fmt.Errorf("manifest must contain exactly one JSON value")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, "", err
	}
	digest := sha256.Sum256(data)
	return manifest, hex.EncodeToString(digest[:]), nil
}

// Validate checks the manifest fields and their source-independent constraints.
func (manifest Manifest) Validate() error {
	if manifest.Version != ManifestVersion {
		return fmt.Errorf("manifest version must be %q", ManifestVersion)
	}
	if !validRepositoryID(manifest.RepositoryID) {
		return fmt.Errorf("repository_id must be a canonical host/owner/repository identifier")
	}
	if !fullCommitPattern.MatchString(manifest.Commit) {
		return fmt.Errorf("commit must be a full lowercase 40- or 64-character Git object ID")
	}
	if !fullCommitPattern.MatchString(manifest.Tree) {
		return fmt.Errorf("tree must be a full lowercase 40- or 64-character Git object ID")
	}
	switch manifest.Stratum {
	case "small", "medium", "large":
	default:
		return fmt.Errorf("stratum must be small, medium, or large")
	}
	if len(manifest.Queries) == 0 {
		return fmt.Errorf("manifest must contain at least one query")
	}
	seen := make(map[string]struct{}, len(manifest.Queries))
	for queryIndex, query := range manifest.Queries {
		if strings.TrimSpace(query.ID) == "" || len(query.ID) > 128 {
			return fmt.Errorf("queries[%d].id must be non-empty and at most 128 bytes", queryIndex)
		}
		if _, exists := seen[query.ID]; exists {
			return fmt.Errorf("duplicate query id %q", query.ID)
		}
		seen[query.ID] = struct{}{}
		if strings.TrimSpace(query.Text) == "" || len(query.Text) > 4096 || len(Tokenize(query.Text)) == 0 {
			return fmt.Errorf("queries[%d].query must contain searchable text of at most 4096 bytes", queryIndex)
		}
		if len(query.GoplsQuery) > 4096 {
			return fmt.Errorf("queries[%d].gopls_query exceeds the 4096-byte limit", queryIndex)
		}
		if len(Tokenize(query.Text)) > 64 {
			return fmt.Errorf("queries[%d].query exceeds the 64-token limit", queryIndex)
		}
		if len(query.Gold) == 0 {
			return fmt.Errorf("queries[%d].gold must contain at least one reviewed evidence span", queryIndex)
		}
		for spanIndex, span := range query.Gold {
			if _, ok := evidenceTypes[span.Type]; !ok {
				return fmt.Errorf("queries[%d].gold[%d].type must be one of declaration, caller, test, documentation, configuration", queryIndex, spanIndex)
			}
			if !fs.ValidPath(span.Path) || path.IsAbs(span.Path) || strings.Contains(span.Path, "\\") {
				return fmt.Errorf("queries[%d].gold[%d].path must be a slash-separated repository-relative path", queryIndex, spanIndex)
			}
			if span.StartLine < 1 || span.EndLine < span.StartLine {
				return fmt.Errorf("queries[%d].gold[%d] has an invalid one-based line range", queryIndex, spanIndex)
			}
		}
	}
	return nil
}

func validRepositoryID(value string) bool {
	if !repositoryIDPattern.MatchString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\?#@") {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return false
	}
	if parts[0] == "local" && len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".git") {
			return false
		}
	}
	return strings.ToLower(value) == value
}

// Tokenize intentionally mirrors internal/intelligence/retrieval's current
// Unicode and camel-case tokenizer. Keep this copy versioned with the study:
// changing it changes the fixed native rg baseline.
func Tokenize(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	runes := []rune(value)
	result := make([]string, 0, len(runes)/2)
	start := -1
	flush := func(end int) {
		if start < 0 || end <= start {
			start = -1
			return
		}
		result = append(result, strings.ToLower(string(runes[start:end])))
		start = -1
	}
	for index, current := range runes {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) && current != '_' {
			flush(index)
			continue
		}
		if start < 0 {
			start = index
			continue
		}
		previous := runes[index-1]
		if current == '_' {
			flush(index)
			continue
		}
		if unicode.IsUpper(current) && unicode.IsLower(previous) {
			flush(index)
			start = index
		}
	}
	flush(len(runes))
	return result
}
