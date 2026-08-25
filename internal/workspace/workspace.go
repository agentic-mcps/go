// Package workspace validates and contains access to a configured Go workspace.
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	goversion "go/version"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

const minimumGoVersion = "go1.25"

// Workspace is a validated, symlink-resolved Go workspace root.
type Workspace struct {
	root       string
	toolchain  Toolchain
	requiredGo string
}

// Toolchain is the effective Go executable selected from the process PATH.
type Toolchain struct {
	Path    string `json:"path"`
	Version string `json:"version"`
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
	selectedGo, err := checkGoToolchain(ctx, resolved)
	if err != nil {
		return nil, err
	}
	requiredGo, err := workspaceGoRequirement(resolved)
	if err != nil {
		return nil, fmt.Errorf("validating Go workspace requirements: %w", err)
	}
	if requiredGo != "" && goversion.Compare(selectedGo.Version, requiredGo) < 0 {
		return nil, fmt.Errorf("validating Go workspace: workspace requires Go %s but %s provides %s; configure the MCP client to launch with a supported Go toolchain", strings.TrimPrefix(requiredGo, "go"), selectedGo.Path, selectedGo.Version)
	}

	cmd := exec.CommandContext(ctx, selectedGo.Path, "list", "-m", "-json")
	cmd.Dir = resolved
	cmd.Env = withEnv(withEnv(os.Environ(), "GOWORK", "auto"), "GOTOOLCHAIN", "local")
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

	return &Workspace{
		root:       resolved,
		toolchain:  Toolchain(selectedGo),
		requiredGo: requiredGo,
	}, nil
}

type goToolchain struct {
	Path    string
	Version string
}

func checkGoToolchain(ctx context.Context, root string) (goToolchain, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return goToolchain{}, fmt.Errorf("locating go on the MCP process PATH: %w; agentic-go requires Go 1.25 or newer", err)
	}
	cmd := exec.CommandContext(ctx, goPath, "version")
	cmd.Dir = root
	cmd.Env = withEnv(os.Environ(), "GOTOOLCHAIN", "local")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return goToolchain{}, ctxErr
		}
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return goToolchain{}, fmt.Errorf("checking Go toolchain %s: %s; agentic-go requires Go 1.25 or newer", goPath, message)
	}

	selected, err := parseGoVersion(output)
	if err != nil {
		return goToolchain{}, fmt.Errorf("checking Go toolchain %s: %w; agentic-go requires Go 1.25 or newer", goPath, err)
	}
	if goversion.Compare(selected, minimumGoVersion) < 0 {
		return goToolchain{}, fmt.Errorf("go toolchain %s at %s is unsupported; agentic-go requires Go 1.25 or newer; check the MCP client's PATH", selected, goPath)
	}
	return goToolchain{Path: goPath, Version: selected}, nil
}

func parseGoVersion(output []byte) (string, error) {
	for _, field := range strings.Fields(string(output)) {
		if strings.HasPrefix(field, "go1.") && goversion.IsValid(field) {
			return field, nil
		}
	}
	return "", fmt.Errorf("unexpected go version output %q", strings.TrimSpace(string(output)))
}

func workspaceGoRequirement(root string) (string, error) {
	workPath, found, err := findUp(root, "go.work")
	if err != nil {
		return "", err
	}
	if found {
		data, readErr := os.ReadFile(workPath)
		if readErr != nil {
			return "", fmt.Errorf("reading %s: %w", workPath, readErr)
		}
		work, parseErr := modfile.ParseWork(workPath, data, nil)
		if parseErr != nil {
			return "", fmt.Errorf("parsing %s: %w", workPath, parseErr)
		}
		required, versionErr := normalizedGoVersion(work.Go)
		if versionErr != nil {
			return "", fmt.Errorf("reading %s: %w", workPath, versionErr)
		}
		for _, use := range work.Use {
			moduleRoot := use.Path
			if !filepath.IsAbs(moduleRoot) {
				moduleRoot = filepath.Join(filepath.Dir(workPath), moduleRoot)
			}
			moduleGo, moduleErr := moduleGoRequirement(filepath.Join(moduleRoot, "go.mod"))
			if moduleErr != nil {
				return "", moduleErr
			}
			required = maxGoVersion(required, moduleGo)
		}
		return required, nil
	}

	modulePath, found, err := findUp(root, "go.mod")
	if err != nil || !found {
		return "", err
	}
	return moduleGoRequirement(modulePath)
}

func moduleGoRequirement(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	module, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	required, err := normalizedGoVersion(module.Go)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return required, nil
}

func normalizedGoVersion(statement *modfile.Go) (string, error) {
	if statement == nil || statement.Version == "" {
		return "", nil
	}
	candidate := "go" + statement.Version
	if !goversion.IsValid(candidate) {
		return "", fmt.Errorf("invalid Go version %q", statement.Version)
	}
	return candidate, nil
}

func maxGoVersion(left, right string) string {
	if left == "" || (right != "" && goversion.Compare(right, left) > 0) {
		return right
	}
	return left
}

func findUp(root, name string) (string, bool, error) {
	for current := root; ; current = filepath.Dir(current) {
		candidate := filepath.Join(current, name)
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", false, fmt.Errorf("%s is not a regular file", candidate)
			}
			return candidate, true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("inspecting %s: %w", candidate, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
	}
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

// Toolchain returns the immutable effective Go toolchain recorded at preflight.
func (w *Workspace) Toolchain() Toolchain {
	return w.toolchain
}

// RequiredGo returns the highest Go version required by the active workspace.
func (w *Workspace) RequiredGo() string {
	return w.requiredGo
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
