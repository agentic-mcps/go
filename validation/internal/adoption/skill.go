package adoption

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// SkillName identifies the shipped model-invoked skill used by the
// integrated adoption diagnostic.
const SkillName = "agentic-go-context"

var skillFiles = []string{"SKILL.md", filepath.Join("agents", "openai.yaml")}

// Skill is the validated, content-addressed skill payload.
//
//nolint:govet // Files remain grouped with the root and digest for the contract.
type Skill struct {
	Root   string
	Digest string
	Files  map[string][]byte
}

// LoadSkill validates the minimal shipped skill and computes a deterministic
// digest over its two instruction files.
func LoadSkill(root string) (Skill, error) {
	if root == "" {
		return Skill{}, fmt.Errorf("skill root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Skill{}, fmt.Errorf("resolve skill root: %w", err)
	}
	rootInfo, err := os.Lstat(abs)
	if err != nil {
		return Skill{}, fmt.Errorf("stat skill root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return Skill{}, fmt.Errorf("skill root is not a directory")
	}
	files := make(map[string][]byte, len(skillFiles))
	for _, rel := range skillFiles {
		path := filepath.Join(abs, rel)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return Skill{}, fmt.Errorf("read skill file %s: %w", rel, readErr)
		}
		files[rel] = data
	}
	err = filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(abs, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill contains symlink %s", rel)
		}
		if entry.IsDir() {
			if rel != "agents" {
				return fmt.Errorf("skill contains unexpected directory %s", rel)
			}
			return nil
		}
		for _, expected := range skillFiles {
			if filepath.Clean(rel) == filepath.Clean(expected) {
				return nil
			}
		}
		return fmt.Errorf("skill contains unexpected file %s", rel)
	})
	if err != nil {
		return Skill{}, err
	}
	h := sha256.New()
	ordered := append([]string(nil), skillFiles...)
	sort.Strings(ordered)
	for _, rel := range ordered {
		data := files[rel]
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		_, _ = h.Write(data)
	}
	return Skill{Root: abs, Digest: hex.EncodeToString(h.Sum(nil)), Files: files}, nil
}

// Install copies the validated skill to an isolated Codex home. The returned
// path is outside the candidate workspace and is suitable for CODEX_HOME.
func (s Skill) Install(home string) (string, error) {
	if s.Digest == "" || len(s.Files) != len(skillFiles) {
		return "", fmt.Errorf("skill is not loaded")
	}
	if home == "" {
		return "", fmt.Errorf("codex home is empty")
	}
	target := filepath.Join(home, "skills", SkillName)
	if err := os.MkdirAll(filepath.Join(target, "agents"), 0o755); err != nil {
		return "", fmt.Errorf("create isolated skill home: %w", err)
	}
	for rel, data := range s.Files {
		path := filepath.Join(target, rel)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", fmt.Errorf("install skill file %s: %w", rel, err)
		}
	}
	return target, nil
}
