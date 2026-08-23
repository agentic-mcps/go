// Package workspace validates and contains access to a configured Go workspace.
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Workspace is a validated, symlink-resolved Go workspace root.
type Workspace struct {
	root string
}

// Open validates root as a directory in an active Go module or workspace.
func Open(ctx context.Context, root string) (*Workspace, error) {
	if root == "" {
		root = "."
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace symlinks: %w", err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("inspecting workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace %q is not a directory", root)
	}

	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json")
	cmd.Dir = resolved
	cmd.Env = withEnv(os.Environ(), "GOWORK", "auto")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("validating Go workspace: %s", message)
	}
	if !containsMainModule(output) {
		return nil, fmt.Errorf("validating Go workspace: no go.mod or active go.work module found")
	}

	return &Workspace{root: resolved}, nil
}

func containsMainModule(output []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var module struct {
			Path  string
			GoMod string
			Main  bool
		}
		if err := decoder.Decode(&module); err != nil {
			return false
		}
		if module.Main && module.Path != "command-line-arguments" && module.GoMod != "" {
			return true
		}
	}
}

// Root returns the absolute, symlink-resolved workspace root.
func (w *Workspace) Root() string {
	return w.root
}

// Resolve resolves an existing path and rejects anything outside the workspace.
func (w *Workspace) Resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(w.root, candidate)
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving path %q: %w", path, err)
	}

	rel, err := filepath.Rel(w.root, resolved)
	if err != nil {
		return "", fmt.Errorf("checking path containment: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the workspace", path)
	}

	return resolved, nil
}

// Relative returns an existing contained path relative to the workspace root.
func (w *Workspace) Relative(path string) (string, error) {
	resolved, err := w.Resolve(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(w.root, resolved)
	if err != nil {
		return "", fmt.Errorf("making path workspace-relative: %w", err)
	}
	return filepath.ToSlash(relative), nil
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}
