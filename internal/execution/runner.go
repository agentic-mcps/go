// Package execution runs bounded subprocesses inside a validated workspace.
package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agentic-mcps/go/internal/workspace"
)

const (
	defaultMaxConcurrent = 4
	defaultTimeout       = 300 * time.Second
	defaultOutputLimit   = 8 << 20
)

// ErrOutputLimit indicates that stdout or stderr crossed its configured cap.
var ErrOutputLimit = errors.New("subprocess output limit exceeded")

// Config controls process-wide subprocess limits.
type Config struct {
	MaxConcurrent int
	Timeout       time.Duration
	OutputLimit   int64
}

// Command describes one subprocess invocation.
//
//nolint:govet // Keep invocation fields in their natural call-site reading order.
type Command struct {
	Name string
	Args []string
	Dir  string
	Env  map[string]string
}

// Streams receive bounded subprocess output. Nil writers discard that stream.
type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Result describes a completed subprocess, including ordinary non-zero exits.
type Result struct {
	ExitCode int
	Duration time.Duration
}

// Runner owns workspace containment, deadlines, concurrency, and output caps.
type Runner struct {
	workspace   *workspace.Workspace
	semaphore   chan struct{}
	timeout     time.Duration
	outputLimit int64
}

// New returns a runner with validated defaults for every zero-valued limit.
func New(ws *workspace.Workspace, config Config) (*Runner, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = defaultMaxConcurrent
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.OutputLimit == 0 {
		config.OutputLimit = defaultOutputLimit
	}
	if config.MaxConcurrent < 1 {
		return nil, fmt.Errorf("max concurrent processes must be positive")
	}
	if config.Timeout < 0 {
		return nil, fmt.Errorf("timeout must not be negative")
	}
	if config.OutputLimit < 1 {
		return nil, fmt.Errorf("output limit must be positive")
	}

	return &Runner{
		workspace:   ws,
		semaphore:   make(chan struct{}, config.MaxConcurrent),
		timeout:     config.Timeout,
		outputLimit: config.OutputLimit,
	}, nil
}

// ForWorkspace returns a runner contained to another validated workspace while
// sharing this runner's process budget. It is used for server-created worktrees.
func (r *Runner) ForWorkspace(ws *workspace.Workspace) (*Runner, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	return &Runner{
		workspace:   ws,
		semaphore:   r.semaphore,
		timeout:     r.timeout,
		outputLimit: r.outputLimit,
	}, nil
}

// Run executes command inside the workspace. A normal non-zero process exit is
// returned in Result; containment, cancellation, spawn, and output failures are
// returned as errors.
func (r *Runner) Run(ctx context.Context, command Command, streams Streams) (Result, error) {
	if command.Name == "" {
		return Result{}, fmt.Errorf("command name is empty")
	}

	dir := r.workspace.Root()
	if command.Dir != "" {
		resolved, err := r.workspace.Resolve(command.Dir)
		if err != nil {
			return Result{}, fmt.Errorf("resolving command directory: %w", err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return Result{}, fmt.Errorf("inspecting command directory: %w", err)
		}
		if !info.IsDir() {
			return Result{}, fmt.Errorf("command directory %q is not a directory", command.Dir)
		}
		dir = resolved
	}

	callCtx, cancel := r.Deadline(ctx)
	defer cancel()

	select {
	case r.semaphore <- struct{}{}:
		defer func() { <-r.semaphore }()
	case <-callCtx.Done():
		return Result{}, callCtx.Err()
	}

	processCtx, stop := context.WithCancel(callCtx)
	defer stop()

	cmd := exec.CommandContext(processCtx, command.Name, command.Args...)
	cmd.Dir = dir
	cmd.Env = mergedEnv(command.Env)
	configureProcess(cmd)

	stdout := newCappedWriter("stdout", streams.Stdout, r.outputLimit, stop)
	stderr := newCappedWriter("stderr", streams.Stderr, r.outputLimit, stop)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	err := cmd.Run()
	result := Result{ExitCode: exitCode(cmd.ProcessState), Duration: time.Since(started)}

	if streamErr := errors.Join(stdout.Err(), stderr.Err()); streamErr != nil {
		return Result{}, streamErr
	}
	if callCtx.Err() != nil {
		return Result{}, callCtx.Err()
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return result, nil
	}
	return Result{}, fmt.Errorf("starting %s: %w", command.Name, err)
}

// Deadline applies the process-wide maximum tool duration to a handler. Tool
// handlers call it once so package resolution and execution share one budget.
func (r *Runner) Deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.timeout)
}

// Permit reserves one slot from the process-wide load budget for expensive
// in-process work such as package loading and static analysis.
func (r *Runner) Permit(ctx context.Context) (func(), error) {
	select {
	case r.semaphore <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-r.semaphore }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func exitCode(state *os.ProcessState) int {
	if state == nil {
		return -1
	}
	return state.ExitCode()
}

func mergedEnv(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}

	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := os.Environ()
	for _, key := range keys {
		prefix := key + "="
		filtered := env[:0]
		for _, entry := range env {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		env = append(filtered, prefix+overrides[key])
	}
	return env
}

//nolint:govet // Writer state is kept grouped by ownership and synchronization.
type cappedWriter struct {
	name      string
	dst       io.Writer
	limit     int64
	cancel    context.CancelFunc
	mu        sync.Mutex
	written   int64
	err       error
	cancelled bool
}

func newCappedWriter(name string, dst io.Writer, limit int64, cancel context.CancelFunc) *cappedWriter {
	if dst == nil {
		dst = io.Discard
	}
	return &cappedWriter{name: name, dst: dst, limit: limit, cancel: cancel}
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if int64(len(p)) > w.limit-w.written {
		w.err = fmt.Errorf("%w: %s exceeded %d bytes", ErrOutputLimit, w.name, w.limit)
		if !w.cancelled {
			w.cancelled = true
			w.cancel()
		}
		return 0, w.err
	}
	n, err := w.dst.Write(p)
	w.written += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = fmt.Errorf("writing subprocess %s: %w", w.name, err)
		if !w.cancelled {
			w.cancelled = true
			w.cancel()
		}
	}
	return n, err
}

func (w *cappedWriter) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}
