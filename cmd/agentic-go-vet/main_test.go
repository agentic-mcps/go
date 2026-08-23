package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const smokePackage = "./rule01"

func TestVetBinarySmoke(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	binPath := filepath.Join(t.TempDir(), "agentic-go-vet")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/agentic-go-vet")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agentic-go-vet: %v\n%s", err, output)
	}
	fixtureRoot := filepath.Join(repoRoot, "internal", "analysis", "concurrency", "testdata")

	plain := exec.CommandContext(ctx, binPath, smokePackage)
	plain.Dir = fixtureRoot
	output, err := plain.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("plain diagnostic exit = %v, want 3\n%s", err, output)
	}
	if !strings.Contains(string(output), "goroutine spawned") {
		t.Fatalf("plain output missing expected diagnostic:\n%s", output)
	}

	machine := exec.CommandContext(ctx, binPath, "-json", smokePackage)
	machine.Dir = fixtureRoot
	output, err = machine.CombinedOutput()
	if err != nil {
		t.Fatalf("JSON diagnostic run: %v\n%s", err, output)
	}
	var report map[string]map[string][]struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode JSON diagnostics: %v\n%s", err, output)
	}
	if !hasCategory(report, "concurrency", "concurrency-01") {
		t.Fatalf("JSON output missing concurrency-01 category:\n%s", output)
	}
}

func hasCategory(report map[string]map[string][]struct {
	Category string `json:"category"`
}, analyzer, category string) bool {
	for _, analyzers := range report {
		for _, diagnostic := range analyzers[analyzer] {
			if diagnostic.Category == category {
				return true
			}
		}
	}
	return false
}
