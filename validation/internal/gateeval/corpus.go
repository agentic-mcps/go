package gateeval

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Identity of every commit the evaluation creates.
const (
	evalIdentity = "gateeval"
	evalEmail    = "gateeval@example.invalid"
)

// LoadCorpus reads the corpus CSV (header project,url,pinned) in file order.
func LoadCorpus(path string) ([]Project, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening corpus %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading corpus %s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("corpus %s is empty", path)
	}
	header := rows[0]
	if len(header) != 3 || header[0] != "project" || header[1] != "url" || header[2] != "pinned" {
		return nil, fmt.Errorf("corpus %s: header must be project,url,pinned", path)
	}
	projects := make([]Project, 0, len(rows)-1)
	seen := map[string]bool{}
	for n, row := range rows[1:] {
		if len(row) != 3 {
			return nil, fmt.Errorf("corpus %s: row %d has %d fields, want 3", path, n+2, len(row))
		}
		project := Project{Name: strings.TrimSpace(row[0]), URL: strings.TrimSpace(row[1]), Pinned: strings.TrimSpace(row[2])}
		if project.Name == "" || project.URL == "" || project.Pinned == "" {
			return nil, fmt.Errorf("corpus %s: row %d has an empty field", path, n+2)
		}
		if seen[project.Name] {
			return nil, fmt.Errorf("corpus %s: duplicate project %q", path, project.Name)
		}
		seen[project.Name] = true
		projects = append(projects, project)
	}
	return projects, nil
}

// absWorkDir resolves the work directory, which every later command depends on
// because commands run in other directories.
func absWorkDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("work directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving work directory: %w", err)
	}
	return abs, nil
}

// clonePath is where a project's full clone lives.
func clonePath(workDir, project string) string {
	return filepath.Join(workDir, "clones", project)
}

// ensureClone returns the project's clone under workDir/clones, creating it
// with a full-history git clone when missing.
func ensureClone(ctx context.Context, workDir string, project Project) (string, error) {
	path := clonePath(workDir, project.Name)
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return path, nil
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", fmt.Errorf("creating %s: %w", parent, err)
	}
	if _, err := git(ctx, parent, "clone", "--quiet", project.URL, path); err != nil {
		return "", fmt.Errorf("cloning %s: %w", project.Name, err)
	}
	return path, nil
}

// scratchWorktree returns workDir/work/<purpose>-<project>, a detached
// worktree of the project's clone, creating it when missing.
func scratchWorktree(ctx context.Context, workDir, purpose, project string) (string, error) {
	path := filepath.Join(workDir, "work", purpose+"-"+project)
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return path, nil
	}
	clone := clonePath(workDir, project)
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		return "", fmt.Errorf("clone of %s: %w", project, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("creating work directory: %w", err)
	}
	if _, err := git(ctx, clone, "worktree", "prune"); err != nil {
		return "", err
	}
	if _, err := git(ctx, clone, "worktree", "add", "--quiet", "--detach", path); err != nil {
		return "", err
	}
	return path, nil
}

// commitPaths stages exactly paths (additions, edits, and deletions) in the
// worktree and commits them with the fixed evaluation identity. It returns the
// new commit.
func commitPaths(ctx context.Context, dir string, paths []string, message string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("nothing to commit")
	}
	args := append([]string{"add", "-A", "-f", "--"}, paths...)
	if _, err := git(ctx, dir, args...); err != nil {
		return "", err
	}
	staged, err := git(ctx, dir, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(staged)) == "" {
		return "", errors.New("edit changed nothing")
	}
	if _, err = git(ctx, dir,
		"-c", "user.name="+evalIdentity, "-c", "user.email="+evalEmail,
		"-c", "commit.gpgsign=false", "commit", "--quiet", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(head)), nil
}
