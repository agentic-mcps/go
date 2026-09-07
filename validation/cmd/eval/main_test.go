package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluationRunsRequireExplicitSourceRoot(t *testing.T) {
	for _, command := range []string{"adoption-run", "pilot-run"} {
		t.Run(command, func(t *testing.T) {
			if err := run(context.Background(), []string{command}); err == nil || err.Error() != "-source-root is required" {
				t.Fatalf("run error = %v, want explicit source-root error", err)
			}
		})
	}
}

func TestResolveAdoptionRootsMakesPreparationPathsAbsolute(t *testing.T) {
	source, output, err := resolveAdoptionRoots("relative-source", "relative-output")
	if err != nil {
		t.Fatalf("resolve adoption roots: %v", err)
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(output) {
		t.Fatalf("resolved roots are not absolute: source=%q output=%q", source, output)
	}
}

func TestCopyCodexAuthCopiesOnlyAuthenticationIntoIsolatedHome(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "auth.json"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyCodexAuth(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "auth.json"))
	if err != nil || string(data) != "secret" {
		t.Fatalf("copied auth = %q, err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("config.toml unexpectedly copied, err=%v", err)
	}
}
