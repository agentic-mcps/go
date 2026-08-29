package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	commandOutputCap = 8 << 20
	gitOutputCap     = 64 << 20
)

// Result records structural and behavioral scoring for one candidate.
type Result struct {
	SchemaVersion   string          `json:"schema_version"`
	TaskID          string          `json:"task_id"`
	Status          string          `json:"status"`
	ChangedPaths    []string        `json:"changed_paths"`
	UnexpectedPaths []string        `json:"unexpected_paths"`
	Commands        []CommandResult `json:"commands"`
	Uncertainties   []string        `json:"uncertainties"`
	Measurements    Measurements    `json:"measurements"`
}

// CommandResult records one bounded acceptance-command execution.
type CommandResult struct {
	Status       string   `json:"status"`
	StdoutSHA256 string   `json:"stdout_sha256"`
	StderrSHA256 string   `json:"stderr_sha256"`
	Argv         []string `json:"argv"`
	ExitCode     int      `json:"exit_code"`
	DurationMS   int64    `json:"duration_ms"`
	StdoutBytes  int64    `json:"stdout_bytes"`
	StderrBytes  int64    `json:"stderr_bytes"`
	Truncated    bool     `json:"truncated"`
}

// Measurements records neutral execution and context-cost measurements.
type Measurements struct {
	ModelTokens *int  `json:"model_tokens,omitempty"`
	DurationMS  int64 `json:"duration_ms"`
	ToolCalls   int   `json:"tool_calls"`
	ResultBytes int64 `json:"result_bytes"`
}

// Score evaluates one candidate without modifying its workspace.
func Score(ctx context.Context, task Task, bundle, workspace string) (Result, error) {
	started := time.Now()
	result := Result{
		SchemaVersion: ResultSchema, TaskID: task.ID, Status: "pass",
		ChangedPaths: []string{}, UnexpectedPaths: []string{}, Commands: []CommandResult{},
		Uncertainties: []string{}, Measurements: Measurements{},
	}
	message, err := commandText(ctx, workspace, nil, "git", "show", "-s", "--format=%B", "HEAD")
	if err != nil || !strings.Contains(message, "Upstream-Commit: "+task.Repository.Base) {
		return result, errors.New("candidate HEAD is not the task fixture base")
	}
	changed, err := candidatePaths(ctx, workspace)
	if err != nil {
		return result, err
	}
	result.ChangedPaths = changed
	allowed := make(map[string]struct{}, len(task.Scope.AllowedPaths))
	for _, path := range task.Scope.AllowedPaths {
		allowed[path] = struct{}{}
	}
	for _, path := range changed {
		if _, ok := allowed[path]; !ok {
			result.UnexpectedPaths = append(result.UnexpectedPaths, path)
		}
	}
	if len(changed) == 0 {
		result.Status = "fail"
		result.Uncertainties = append(result.Uncertainties, "candidate contains no change relative to the fixture base")
	}
	if len(result.UnexpectedPaths) > 0 {
		result.Status = "fail"
	}
	scoringRoot, err := os.MkdirTemp("", "agentic-go-eval-score-")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.RemoveAll(scoringRoot) }()
	clone := filepath.Join(scoringRoot, "workspace")
	if err := Setup(ctx, task, bundle, clone); err != nil {
		return result, fmt.Errorf("set up scoring clone: %w", err)
	}
	if err := applyCandidate(ctx, workspace, clone, changed); err != nil {
		return result, fmt.Errorf("copy candidate change: %w", err)
	}
	if err := overlayOracle(ctx, task, bundle, clone); err != nil {
		return result, fmt.Errorf("overlay oracle: %w", err)
	}
	for _, command := range task.Acceptance {
		commandResult := runAcceptance(ctx, clone, command)
		result.Commands = append(result.Commands, commandResult)
		switch commandResult.Status {
		case "incomplete":
			result.Status = "incomplete"
		case "fail":
			if result.Status != "incomplete" {
				result.Status = "fail"
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	result.Measurements.DurationMS = time.Since(started).Milliseconds()
	return result, nil
}

// WriteResult atomically writes one candidate score.
func WriteResult(path string, result Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o644)
}

func candidatePaths(ctx context.Context, workspace string) ([]string, error) {
	tracked, err := commandBytes(ctx, workspace, "git", "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	untracked, err := commandBytes(ctx, workspace, "git", "ls-files", "--others", "--exclude-standard", "-z", "--")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, data := range [][]byte{tracked, untracked} {
		for _, raw := range bytes.Split(data, []byte{0}) {
			if len(raw) == 0 {
				continue
			}
			path := filepath.ToSlash(string(raw))
			if err := validateRelativePath(path); err != nil {
				return nil, err
			}
			seen[path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func applyCandidate(ctx context.Context, source, destination string, changed []string) error {
	patch, err := commandBytes(ctx, source, "git", "diff", "--binary", "HEAD", "--")
	if err != nil {
		return err
	}
	if len(patch) > 0 {
		cmd := exec.CommandContext(ctx, "git", "apply", "--binary", "--whitespace=nowarn", "-")
		cmd.Dir = destination
		cmd.Stdin = bytes.NewReader(patch)
		if output, applyErr := cmd.CombinedOutput(); applyErr != nil {
			return fmt.Errorf("git apply: %w: %s", applyErr, strings.TrimSpace(string(output)))
		}
	}
	trackedText, err := commandText(ctx, source, nil, "git", "ls-files")
	if err != nil {
		return err
	}
	tracked := make(map[string]struct{})
	for _, path := range strings.Split(trackedText, "\n") {
		tracked[filepath.ToSlash(path)] = struct{}{}
	}
	var copied int64
	for _, path := range changed {
		if _, ok := tracked[path]; ok {
			continue
		}
		sourcePath := filepath.Join(source, filepath.FromSlash(path))
		info, statErr := os.Lstat(sourcePath)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("untracked candidate path is not a regular file: %s", path)
		}
		if info.Size() > 16<<20 || copied+info.Size() > 64<<20 {
			return fmt.Errorf("untracked candidate content exceeds scoring limit")
		}
		data, readErr := os.ReadFile(sourcePath)
		if readErr != nil {
			return readErr
		}
		destinationPath := filepath.Join(destination, filepath.FromSlash(path))
		if err := ensureContained(destination, destinationPath); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destinationPath, data, info.Mode().Perm()); err != nil {
			return err
		}
		copied += info.Size()
	}
	return nil
}

func overlayOracle(ctx context.Context, task Task, bundle, workspace string) error {
	if _, err := commandText(ctx, workspace, nil, "git", "fetch", "--quiet", bundle, "refs/heads/target:refs/eval/target"); err != nil {
		return err
	}
	message, err := commandText(ctx, workspace, nil, "git", "show", "-s", "--format=%B", "refs/eval/target")
	if err != nil || !strings.Contains(message, "Upstream-Commit: "+task.Repository.Target) {
		return errors.New("bundle target provenance does not match task manifest")
	}
	parent, err := commandText(ctx, workspace, nil, "git", "show", "-s", "--format=%P", "refs/eval/target")
	if err != nil {
		return err
	}
	head, err := commandText(ctx, workspace, nil, "git", "rev-parse", "HEAD")
	if err != nil || parent != head {
		return errors.New("bundle target is not adjacent to the fixture base")
	}
	for _, path := range task.Oracle.OverlayPaths {
		data, err := commandBytes(ctx, workspace, "git", "show", "refs/eval/target:"+path)
		if err != nil {
			return err
		}
		modeText, err := commandText(ctx, workspace, nil, "git", "ls-tree", "refs/eval/target", "--", path)
		if err != nil || modeText == "" {
			return fmt.Errorf("read oracle mode for %s: %w", path, err)
		}
		fields := strings.Fields(modeText)
		if len(fields) < 1 {
			return fmt.Errorf("malformed ls-tree output for %s", path)
		}
		modeValue, err := strconv.ParseUint(fields[0], 8, 32)
		if err != nil {
			return err
		}
		destination := filepath.Join(workspace, filepath.FromSlash(path))
		if err := ensureContained(workspace, destination); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, os.FileMode(modeValue)&0o777); err != nil {
			return err
		}
	}
	return nil
}

func runAcceptance(parent context.Context, workspace string, command Command) CommandResult {
	result := CommandResult{Argv: append([]string(nil), command.Argv...), Status: "pass", ExitCode: 0}
	goPath, err := exec.LookPath("go")
	if err != nil {
		result.Status, result.ExitCode = "incomplete", -1
		return result
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(command.TimeoutSeconds)*time.Second)
	defer cancel()
	stdout := newDigestWriter(commandOutputCap)
	stderr := newDigestWriter(commandOutputCap)
	started := time.Now()
	cmd := exec.CommandContext(ctx, goPath, command.Argv[1:]...)
	cmd.Dir = workspace
	cmd.Env = controlledEnvironment(goPath, os.Environ())
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	result.DurationMS = time.Since(started).Milliseconds()
	result.StdoutBytes, result.StderrBytes = stdout.total, stderr.total
	result.StdoutSHA256, result.StderrSHA256 = stdout.digest(), stderr.digest()
	result.Truncated = stdout.truncated || stderr.truncated
	if ctx.Err() != nil || result.Truncated {
		result.Status = "incomplete"
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			if environmentalFailure(stderr.text()) {
				result.Status = "incomplete"
			} else if result.Status != "incomplete" {
				result.Status = "fail"
			}
		} else {
			result.ExitCode = -1
			result.Status = "incomplete"
		}
	}
	return result
}

type digestWriter struct {
	hash      hash.Hash
	prefix    []byte
	mu        sync.Mutex
	total     int64
	limit     int64
	truncated bool
}

func newDigestWriter(limit int64) *digestWriter {
	return &digestWriter{hash: sha256.New(), limit: limit}
}

func (w *digestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.total += int64(len(p))
	_, _ = w.hash.Write(p)
	remaining := int64(16<<10) - int64(len(w.prefix))
	if remaining > 0 {
		keep := int64(len(p))
		if keep > remaining {
			keep = remaining
		}
		w.prefix = append(w.prefix, p[:keep]...)
	}
	if w.total > w.limit {
		w.truncated = true
	}
	return len(p), nil
}

func (w *digestWriter) digest() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return hex.EncodeToString(w.hash.Sum(nil))
}

func (w *digestWriter) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.prefix)
}

func environmentalFailure(stderr string) bool {
	for _, marker := range []string{
		"module lookup disabled by GOPROXY=off",
		"go.mod requires go >=",
		"toolchain not available",
	} {
		if strings.Contains(stderr, marker) {
			return true
		}
	}
	return false
}

func controlledEnvironment(goPath string, base []string) []string {
	goDir := filepath.Dir(goPath)
	pathValue := os.Getenv("PATH")
	out := make([]string, 0, len(base)+4)
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if key == "PATH" {
			pathValue = strings.TrimPrefix(item, "PATH=")
		}
		switch key {
		case "PATH", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB":
			continue
		}
		out = append(out, item)
	}
	return append(out, "PATH="+goDir+string(os.PathListSeparator)+pathValue, "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
}

func commandBytes(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	stdout := &limitedBuffer{limit: gitOutputCap}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("command output exceeds %d bytes", b.limit)
	}
	return b.Buffer.Write(p)
}

var _ io.Writer = (*digestWriter)(nil)
