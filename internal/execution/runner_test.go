package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/workspace"
)

func TestRunnerReturnsOrdinaryExit(t *testing.T) {
	runner := newTestRunner(t, Config{})
	var stderr bytes.Buffer

	result, err := runner.Run(context.Background(), helperCommand("exit"), Streams{Stderr: &stderr})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 3 {
		t.Fatalf("Run() exit code = %d, want 3", result.ExitCode)
	}
	if !strings.Contains(stderr.String(), "expected failure") {
		t.Fatalf("Run() stderr = %q", stderr.String())
	}
}

func TestRunnerCapsEachOutputStream(t *testing.T) {
	runner := newTestRunner(t, Config{OutputLimit: 32})

	_, err := runner.Run(context.Background(), helperCommand("output"), Streams{Stdout: &bytes.Buffer{}})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("Run() error = %v, want ErrOutputLimit", err)
	}
}

func TestRunnerHonorsTimeout(t *testing.T) {
	runner := newTestRunner(t, Config{Timeout: 20 * time.Millisecond})

	_, err := runner.Run(context.Background(), helperCommand("sleep"), Streams{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want deadline exceeded", err)
	}
}

func TestRunnerBoundsConcurrency(t *testing.T) {
	runner := newTestRunner(t, Config{MaxConcurrent: 1, Timeout: time.Second})
	ready := make(chan struct{})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()

	var once sync.Once
	firstDone := make(chan error, 1)
	go func() {
		_, err := runner.Run(firstCtx, helperCommand("sleep"), Streams{Stdout: writerFunc(func(p []byte) (int, error) {
			once.Do(func() { close(ready) })
			return len(p), nil
		})})
		firstDone <- err
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("first subprocess did not start")
	}

	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelSecond()
	if _, err := runner.Run(secondCtx, helperCommand("exit"), Streams{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Run() error = %v, want deadline while waiting for semaphore", err)
	}

	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Run() error = %v, want cancellation", err)
	}
}

func TestRunnerRejectsDirectoryEscape(t *testing.T) {
	runner := newTestRunner(t, Config{})
	command := helperCommand("exit")
	command.Dir = ".."

	if _, err := runner.Run(context.Background(), command, Streams{}); err == nil {
		t.Fatal("Run() succeeded outside workspace")
	}
}

func TestForWorkspaceSharesConcurrencyLimit(t *testing.T) {
	primary := newTestRunner(t, Config{MaxConcurrent: 1, Timeout: time.Second})
	secondaryRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondaryRoot, "go.mod"), []byte("module example.com/secondary\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondaryWorkspace, err := workspace.Open(context.Background(), secondaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	secondary, err := primary.ForWorkspace(secondaryWorkspace)
	if err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	var once sync.Once
	go func() {
		_, err := primary.Run(firstCtx, helperCommand("sleep"), Streams{Stdout: writerFunc(func(p []byte) (int, error) {
			once.Do(func() { close(ready) })
			return len(p), nil
		})})
		firstDone <- err
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("primary subprocess did not start")
	}

	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelSecond()
	if _, err := secondary.Run(secondCtx, helperCommand("exit"), Streams{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("child Run() error = %v, want shared-semaphore deadline", err)
	}
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("primary Run() error = %v, want cancellation", err)
	}
}

func TestPermitSharesConcurrencyLimitWithRun(t *testing.T) {
	runner := newTestRunner(t, Config{MaxConcurrent: 1, Timeout: time.Second})
	release, err := runner.Permit(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := runner.Run(waitCtx, helperCommand("exit"), Streams{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want deadline while permit is held", err)
	}

	release()
	release()
	if _, err := runner.Run(context.Background(), helperCommand("exit"), Streams{}); err != nil {
		t.Fatalf("Run() after release error = %v", err)
	}
}

func TestHelperProcess(_ *testing.T) {
	if os.Getenv("AGENTIC_GO_HELPER_PROCESS") != "1" {
		return
	}

	switch os.Getenv("AGENTIC_GO_HELPER_MODE") {
	case "exit":
		fmt.Fprintln(os.Stderr, "expected failure")
		os.Exit(3)
	case "output":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 128))
		os.Exit(0)
	case "sleep":
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(time.Second)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func newTestRunner(t *testing.T, config Config) *Runner {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/runner\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(ws, config)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func helperCommand(mode string) Command {
	return Command{
		Name: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess"},
		Env: map[string]string{
			"AGENTIC_GO_HELPER_PROCESS": "1",
			"AGENTIC_GO_HELPER_MODE":    mode,
		},
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}
