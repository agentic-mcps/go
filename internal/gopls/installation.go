package gopls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// SupportedVersion is the only gopls version covered by the current LSP
// contract suite and release bundle.
const SupportedVersion = "v0.21.0"

var goplsVersionPattern = regexp.MustCompile(`\bv[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?\b`)

// Installation describes the effective semantic sidecar.
type Installation struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Bundled bool   `json:"bundled"`
}

// Locate resolves and probes the pinned companion. An explicit override is
// authoritative; otherwise the sibling release binary wins over PATH.
func Locate(ctx context.Context, hostExecutable, override string) (Installation, error) {
	if hostExecutable == "" {
		resolved, err := os.Executable()
		if err != nil {
			return Installation{}, fmt.Errorf("locating agentic-go executable: %w", err)
		}
		hostExecutable = resolved
	}
	if override != "" {
		candidate, err := resolveCandidate(override)
		if err != nil {
			return Installation{}, fmt.Errorf("locating configured gopls companion: %w", err)
		}
		version, err := probeVersion(ctx, candidate)
		if err != nil {
			return Installation{}, err
		}
		return Installation{Path: candidate, Version: version}, nil
	}

	sibling := filepath.Join(filepath.Dir(hostExecutable), "agentic-go-gopls")
	type candidate struct {
		path    string
		bundled bool
	}
	candidates := []candidate{{path: sibling, bundled: true}}
	if path, err := exec.LookPath("agentic-go-gopls"); err == nil && path != sibling {
		candidates = append(candidates, candidate{path: path})
	}
	if path, err := exec.LookPath("gopls"); err == nil {
		candidates = append(candidates, candidate{path: path})
	}

	var diagnostics []string
	for _, option := range candidates {
		path, err := resolveCandidate(option.path)
		if err != nil {
			continue
		}
		version, err := probeVersion(ctx, path)
		if err != nil {
			diagnostics = append(diagnostics, err.Error())
			continue
		}
		return Installation{Path: path, Version: version, Bundled: option.bundled}, nil
	}
	message := fmt.Sprintf("pinned gopls companion agentic-go-gopls %s was not found beside agentic-go or on PATH", SupportedVersion)
	if len(diagnostics) > 0 {
		message += ": " + strings.Join(diagnostics, "; ")
	}
	return Installation{}, errors.New(message)
}

func resolveCandidate(candidate string) (string, error) {
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	return absolute, nil
}

func probeVersion(ctx context.Context, path string) (string, error) {
	command := exec.CommandContext(ctx, path, "version")
	command.Env = replaceEnv(os.Environ(), "GOTOOLCHAIN", "local")
	command.Env = replaceEnv(command.Env, "GOTELEMETRY", "off")
	output := &boundedBuffer{limit: 64 << 10}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("probing gopls companion %s: %w: %s", path, err, output.String())
	}
	versionOutput := output.String()
	version := goplsVersionPattern.FindString(versionOutput)
	if version == "" {
		return "", fmt.Errorf("probing gopls companion %s: malformed version output %q; require %s", path, versionOutput, SupportedVersion)
	}
	if version != SupportedVersion {
		return "", fmt.Errorf("gopls companion %s reports %s; require %s", path, version, SupportedVersion)
	}
	return version, nil
}
