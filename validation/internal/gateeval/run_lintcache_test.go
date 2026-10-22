package gateeval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLintRunsWithItsOwnCache pins that every golangci-lint invocation gets a
// fresh cache directory: the shared lint cache returned stale results across
// worktrees with --new-from-rev.
func TestLintRunsWithItsOwnCache(t *testing.T) {
	log := filepath.Join(t.TempDir(), "caches")
	fakeLint(t, "test -d \"$GOLANGCI_LINT_CACHE\" || exit 9\necho \"$GOLANGCI_LINT_CACHE\" >> '"+log+"'\nexit 0\n")
	t.Setenv("GOLANGCI_LINT_CACHE", "/shared/lint/cache")
	for range 2 {
		if result := runCommand(context.Background(), t.TempDir(), time.Minute, "golangci-lint", "run", "./..."); result.err != nil || result.exit != 0 {
			t.Fatalf("lint = %+v, want the cache directory to exist while it runs", result)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	dirs := strings.Fields(string(data))
	if len(dirs) != 2 || dirs[0] == dirs[1] {
		t.Fatalf("cache directories = %v, want two distinct ones", dirs)
	}
	for _, dir := range dirs {
		if dir == "/shared/lint/cache" {
			t.Errorf("lint used the shared cache %q", dir)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("cache directory %s was not removed (stat error %v)", dir, err)
		}
	}
	// Other commands keep the inherited environment.
	other := runCommand(context.Background(), t.TempDir(), time.Minute, "sh", "-c", "echo ${GOLANGCI_LINT_CACHE-unset}")
	if got := strings.TrimSpace(string(other.stdout)); got != "/shared/lint/cache" {
		t.Errorf("non-lint command saw %q", got)
	}
}

func TestLintRefCacheVersionIgnoresPreFixFiles(t *testing.T) {
	if lintRefVersion < 2 {
		t.Fatalf("lintRefVersion = %d: reference files computed with the shared lint cache must be ignored", lintRefVersion)
	}
}
