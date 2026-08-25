// Command releasepack builds the four supported agentic-go release bundles.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/gopls"
	"github.com/ashwingopalsamy/agentic-go/internal/releasebundle"
)

var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

type target struct {
	OS   string
	Arch string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "agentic-go releasepack: %v\n", err)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string) error {
	flags := flag.NewFlagSet("releasepack", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	version := flags.String("version", "", "release version without a leading v")
	output := flags.String("output", "dist", "artifact output directory")
	root := flags.String("root", ".", "agentic-go repository root")
	targetsInput := flags.String("targets", "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64", "comma-separated release targets")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if !releaseVersionPattern.MatchString(*version) {
		return fmt.Errorf("invalid --version %q", *version)
	}
	repository, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolving repository root: %w", err)
	}
	if err := requireRegularFile(filepath.Join(repository, "go.mod")); err != nil {
		return fmt.Errorf("validating repository root: %w", err)
	}
	outputRoot, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("resolving output directory: %w", err)
	}
	targets, err := parseTargets(*targetsInput)
	if err != nil {
		return err
	}
	goCommand, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("locating go: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	goplsSource, err := downloadGopls(ctx, goCommand, repository)
	if err != nil {
		return err
	}
	working, err := os.MkdirTemp("", "agentic-go-releasepack-*")
	if err != nil {
		return fmt.Errorf("creating release workspace: %w", err)
	}
	defer os.RemoveAll(working)
	licensesPath := filepath.Join(working, "gopls-dependencies.txt")
	if err := generateGoplsLicenses(ctx, goCommand, goplsSource, licensesPath); err != nil {
		return err
	}

	artifacts := make([]string, 0, len(targets))
	for _, releaseTarget := range targets {
		archive, err := buildTarget(ctx, goCommand, repository, goplsSource, working, outputRoot, *version, releaseTarget, licensesPath)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, archive)
	}
	if err := releasebundle.WriteChecksums(filepath.Join(outputRoot, "checksums.txt"), artifacts); err != nil {
		return fmt.Errorf("writing checksums: %w", err)
	}
	return nil
}

func buildTarget(ctx context.Context, goCommand, repository, goplsSource, working, output, version string, releaseTarget target, licensesPath string) (string, error) {
	targetRoot := filepath.Join(working, releaseTarget.OS+"-"+releaseTarget.Arch)
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return "", err
	}
	environment := targetEnvironment(releaseTarget)
	agenticGo := filepath.Join(targetRoot, "agentic-go")
	if err := runCommand(ctx, repository, environment, goCommand, "build", "-mod=readonly", "-trimpath", "-ldflags=-s -w -X main.version="+version, "-o", agenticGo, "./cmd/agentic-go"); err != nil {
		return "", fmt.Errorf("building agentic-go for %s/%s: %w", releaseTarget.OS, releaseTarget.Arch, err)
	}
	agenticVet := filepath.Join(targetRoot, "agentic-go-vet")
	if err := runCommand(ctx, repository, environment, goCommand, "build", "-mod=readonly", "-trimpath", "-ldflags=-s -w", "-o", agenticVet, "./cmd/agentic-go-vet"); err != nil {
		return "", fmt.Errorf("building agentic-go-vet for %s/%s: %w", releaseTarget.OS, releaseTarget.Arch, err)
	}
	sidecar := filepath.Join(targetRoot, "agentic-go-gopls")
	if err := runCommand(ctx, goplsSource, environment, goCommand, "build", "-mod=readonly", "-trimpath", "-ldflags=-s -w -X main.version="+gopls.SupportedVersion, "-o", sidecar, "."); err != nil {
		return "", fmt.Errorf("building agentic-go-gopls for %s/%s: %w", releaseTarget.OS, releaseTarget.Arch, err)
	}

	archive := filepath.Join(output, fmt.Sprintf("agentic-go_%s_%s_%s.tar.gz", version, releaseTarget.OS, releaseTarget.Arch))
	files := []releasebundle.File{
		{Source: agenticGo, Name: "agentic-go", Mode: 0o755},
		{Source: sidecar, Name: "agentic-go-gopls", Mode: 0o755},
		{Source: agenticVet, Name: "agentic-go-vet", Mode: 0o755},
		{Source: filepath.Join(repository, "LICENSE"), Name: "LICENSE", Mode: 0o644},
		{Source: filepath.Join(repository, "THIRD_PARTY_NOTICES.md"), Name: "THIRD_PARTY_NOTICES.md", Mode: 0o644},
		{Source: filepath.Join(goplsSource, "LICENSE"), Name: "LICENSES/gopls-BSD.txt", Mode: 0o644},
		{Source: licensesPath, Name: "LICENSES/gopls-dependencies.txt", Mode: 0o644},
	}
	if err := releasebundle.WriteArchive(archive, files); err != nil {
		return "", fmt.Errorf("packaging %s/%s: %w", releaseTarget.OS, releaseTarget.Arch, err)
	}
	return archive, nil
}

func downloadGopls(ctx context.Context, goCommand, repository string) (string, error) {
	command := exec.CommandContext(ctx, goCommand, "mod", "download", "-json", "golang.org/x/tools/gopls@"+gopls.SupportedVersion)
	command.Dir = repository
	command.Env = releaseEnvironment(nil)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("downloading pinned gopls: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var module struct {
		Path    string
		Version string
		Dir     string
		Error   *struct{ Err string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &module); err != nil {
		return "", fmt.Errorf("decoding pinned gopls module: %w", err)
	}
	if module.Error != nil {
		return "", fmt.Errorf("downloading pinned gopls: %s", module.Error.Err)
	}
	if module.Path != "golang.org/x/tools/gopls" || module.Version != gopls.SupportedVersion || module.Dir == "" {
		return "", fmt.Errorf("resolved unexpected gopls module %s %s", module.Path, module.Version)
	}
	return module.Dir, nil
}

func generateGoplsLicenses(ctx context.Context, goCommand, source, destination string) error {
	command := exec.CommandContext(ctx, goCommand, "run", "-mod=readonly", "-trimpath", "-ldflags=-X main.version="+gopls.SupportedVersion, ".", "licenses")
	command.Dir = source
	command.Env = releaseEnvironment(nil)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("generating gopls license notices: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() == 0 {
		return fmt.Errorf("generating gopls license notices: empty output")
	}
	if err := os.WriteFile(destination, stdout.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing gopls license notices: %w", err)
	}
	return nil
}

func runCommand(ctx context.Context, directory string, environment []string, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = releaseEnvironment(environment)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

func parseTargets(value string) ([]target, error) {
	var targets []target
	seen := make(map[string]struct{})
	for _, raw := range strings.Split(value, ",") {
		parts := strings.Split(strings.TrimSpace(raw), "/")
		if len(parts) != 2 || (parts[0] != "darwin" && parts[0] != "linux") || (parts[1] != "amd64" && parts[1] != "arm64") {
			return nil, fmt.Errorf("unsupported release target %q", raw)
		}
		key := parts[0] + "/" + parts[1]
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate release target %q", key)
		}
		seen[key] = struct{}{}
		targets = append(targets, target{OS: parts[0], Arch: parts[1]})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no release targets configured")
	}
	return targets, nil
}

func targetEnvironment(releaseTarget target) []string {
	return []string{"GOOS=" + releaseTarget.OS, "GOARCH=" + releaseTarget.Arch, "CGO_ENABLED=0"}
}

func releaseEnvironment(overrides []string) []string {
	values := append([]string(nil), os.Environ()...)
	values = setEnvironment(values, "GOTOOLCHAIN", "local")
	values = setEnvironment(values, "GOTELEMETRY", "off")
	for _, override := range overrides {
		key, value, found := strings.Cut(override, "=")
		if found {
			values = setEnvironment(values, key, value)
		}
	}
	return values
}

func setEnvironment(values []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(values)+1)
	for _, entry := range values {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}
