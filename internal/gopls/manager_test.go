package gopls

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagerRetriesOneIdempotentReadAfterCrash(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "starts")
	lifecycle, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	manager, err := NewManager(lifecycle, Config{
		Command:       os.Args[0],
		Args:          []string{"-test.run=TestGoplsHelperProcess", "--", "restart", statePath},
		Workspace:     t.TempDir(),
		ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var result struct {
		Instance int `json:"instance"`
	}
	if err := manager.Request(ctx, "test/read", nil, &result, true); err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if result.Instance != 2 {
		t.Fatalf("result instance = %d, want 2", result.Instance)
	}
	if starts := readStartCount(t, statePath); starts != 2 {
		t.Fatalf("sidecar starts = %d, want 2", starts)
	}
}

func TestManagerNeverRetriesMutationAfterCrash(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "starts")
	lifecycle, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	manager, err := NewManager(lifecycle, Config{
		Command:       os.Args[0],
		Args:          []string{"-test.run=TestGoplsHelperProcess", "--", "restart", statePath},
		Workspace:     t.TempDir(),
		ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Request(ctx, "test/write", nil, nil, false); err == nil {
		t.Fatal("Request() error = nil, want sidecar failure")
	}
	if starts := readStartCount(t, statePath); starts != 1 {
		t.Fatalf("sidecar starts = %d, want 1", starts)
	}
}

func TestManagerExplicitRestartReplacesSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "starts")
	lifecycle, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	manager, err := NewManager(lifecycle, Config{
		Command:       os.Args[0],
		Args:          []string{"-test.run=TestGoplsHelperProcess", "--", "restart", statePath},
		Workspace:     t.TempDir(),
		ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Restart(ctx); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if starts := readStartCount(t, statePath); starts != 2 {
		t.Fatalf("sidecar starts = %d, want 2", starts)
	}
}

func readStartCount(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil {
		t.Fatal(err)
	}
	return count
}
