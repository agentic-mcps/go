package gateeval

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Identity of every commit the evaluation creates.
const (
	evalIdentity = "gateeval"
	evalEmail    = "gateeval@example.invalid"
)

// commitDate is the fixed author and committer date of every evaluation commit,
// so regenerating a variant yields the same commit hash.
const commitDate = "2026-01-01T00:00:00Z"

// errStepTimeout marks a command that ran out of time.
var errStepTimeout = errors.New("timed out")

// evalEnv is the environment of every command the generator and selector run:
// the process environment without CI (a CI variable changes what the code
// under test does, for example a skip guarded by CI), plus the evaluation's Go
// settings.
func evalEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if entry == "CI" || strings.HasPrefix(entry, "CI=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
}

// runStep runs one command in dir under its own timeout with evalEnv.
func runStep(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) stepResult {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	cmd.Dir = dir
	cmd.Env = evalEnv()
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	result := stepResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), duration: time.Since(start)}
	if err == nil {
		return result
	}
	label := name + " " + strings.Join(args, " ")
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		result.err = fmt.Errorf("%s: canceled: %w", label, ctx.Err())
	case cmdCtx.Err() != nil:
		result.err = fmt.Errorf("%s: %w after %s", label, errStepTimeout, timeout)
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		result.exit = exitErr.ExitCode()
	default:
		result.err = fmt.Errorf("%s: %w", label, err)
	}
	return result
}

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
	commit := exec.CommandContext(ctx, "git",
		"-c", "user.name="+evalIdentity, "-c", "user.email="+evalEmail,
		"-c", "commit.gpgsign=false", "commit", "--quiet", "--no-verify", "-m", message)
	commit.Dir = dir
	commit.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+commitDate, "GIT_COMMITTER_DATE="+commitDate)
	if out, commitErr := commit.CombinedOutput(); commitErr != nil {
		return "", fmt.Errorf("git commit: %w: %s", commitErr, strings.TrimSpace(string(out)))
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(head)), nil
}
