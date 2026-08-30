package tools

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/progress"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/agentic-mcps/go/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BenchmarkDiffInput configures a current-versus-baseline benchmark run.
//
//nolint:govet // field order is the public JSON schema order.
type BenchmarkDiffInput struct {
	Package          string   `json:"package" jsonschema:"Go package import path or ./relative/path"`
	Baseline         string   `json:"baseline" jsonschema:"git ref to compare against, e.g. HEAD~1 or main"`
	BenchRegex       string   `json:"bench_regex,omitempty" jsonschema:"regex filter for -bench; default all benchmarks"`
	Count            int      `json:"count,omitempty" jsonschema:"repetitions per revision; default 6, maximum 20"`
	ThresholdPercent *float64 `json:"threshold_percent,omitempty" jsonschema:"regression threshold percent; default 10"`
}

// BenchmarkComparison compares one benchmark across revisions.
type BenchmarkComparison struct {
	Name         string  `json:"name"`
	BaselineNsOp float64 `json:"baseline_ns_op"`
	CurrentNsOp  float64 `json:"current_ns_op"`
	DeltaPercent float64 `json:"delta_percent"`
	Regression   bool    `json:"regression"`
}

// BenchmarkDiffOutput contains deterministic benchmark comparisons.
type BenchmarkDiffOutput struct {
	Comparisons []BenchmarkComparison `json:"comparisons"`
	Regressions int                   `json:"regressions"`
}

// RegisterBenchmarkDiff registers go_benchmark_diff with execution hints.
func RegisterBenchmarkDiff(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_benchmark_diff",
		Description: "Runs trusted workspace benchmarks in an isolated baseline worktree and reports latency changes.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
			OpenWorldHint:   boolPtr(true),
		},
	}, runtime.benchmarkDiff)
}

func (r *Runtime) benchmarkDiff(ctx context.Context, request *mcp.CallToolRequest, input BenchmarkDiffInput) (*mcp.CallToolResult, BenchmarkDiffOutput, error) {
	ctx, cancel := r.runner.Deadline(ctx)
	defer cancel()
	started := time.Now()
	if err := normalizeBenchmarkDiffInput(&input); err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, trace.ErrorInvalidInput)
		return nil, BenchmarkDiffOutput{}, fmt.Errorf("validating input: %w", err)
	}

	progress.Report(ctx, request, 0, 5, "validating package and baseline")
	pattern, err := r.resolvePackage(ctx, input.Package)
	if err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, classifyTraceError(err))
		return nil, BenchmarkDiffOutput{}, fmt.Errorf("resolving package: %w", err)
	}
	commit, repoRoot, err := r.resolveBenchmarkBaseline(ctx, input.Baseline)
	if err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, classifyTraceError(err))
		return nil, BenchmarkDiffOutput{}, err
	}

	progress.Report(ctx, request, 1, 5, "creating baseline worktree")
	worktree, err := r.createBenchmarkWorktree(ctx, repoRoot, commit)
	if err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, classifyTraceError(err))
		return nil, BenchmarkDiffOutput{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = worktree.close(ctx)
		}
	}()

	arguments := benchmarkArguments(pattern, input)
	progress.Report(ctx, request, 2, 5, "running baseline benchmarks")
	baseline, err := runBenchmarks(ctx, worktree.runner, arguments)
	if err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, classifyTraceError(err))
		return nil, BenchmarkDiffOutput{}, fmt.Errorf("running baseline benchmarks: %w", err)
	}

	progress.Report(ctx, request, 3, 5, "running current benchmarks")
	current, err := runBenchmarks(ctx, r.runner, arguments)
	if err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, classifyTraceError(err))
		return nil, BenchmarkDiffOutput{}, fmt.Errorf("running current benchmarks: %w", err)
	}

	progress.Report(ctx, request, 4, 5, "cleaning baseline worktree")
	if err := worktree.close(ctx); err != nil {
		r.recordBenchmarkTrace(input, started, BenchmarkDiffOutput{}, trace.ErrorInternal)
		return nil, BenchmarkDiffOutput{}, fmt.Errorf("cleaning baseline worktree: %w", err)
	}
	closed = true

	output := compareBenchmarks(baseline, current, *input.ThresholdPercent)
	progress.Report(ctx, request, 5, 5, "benchmark comparison completed")
	r.recordBenchmarkTrace(input, started, output, trace.ErrorNone)
	return nil, output, nil
}

func normalizeBenchmarkDiffInput(input *BenchmarkDiffInput) error {
	if input.Package == "" {
		return fmt.Errorf("package is required")
	}
	if input.Baseline == "" {
		return fmt.Errorf("baseline is required")
	}
	if strings.TrimSpace(input.Baseline) != input.Baseline || strings.HasPrefix(input.Baseline, "-") || strings.ContainsRune(input.Baseline, '\x00') || len(input.Baseline) > 512 {
		return fmt.Errorf("baseline is invalid")
	}
	if input.BenchRegex == "" {
		input.BenchRegex = "."
	}
	if _, err := regexp.Compile(input.BenchRegex); err != nil {
		return fmt.Errorf("bench_regex is invalid: %w", err)
	}
	if input.Count < 0 {
		return fmt.Errorf("count must not be negative")
	}
	if input.Count == 0 {
		input.Count = 6
	}
	if input.Count > 20 {
		input.Count = 20
	}
	if input.ThresholdPercent == nil {
		input.ThresholdPercent = float64Ptr(10)
	}
	if math.IsNaN(*input.ThresholdPercent) || math.IsInf(*input.ThresholdPercent, 0) || *input.ThresholdPercent < 0 {
		return fmt.Errorf("threshold_percent must be a finite non-negative number")
	}
	return nil
}

func float64Ptr(value float64) *float64 { return &value }

func (r *Runtime) resolveBenchmarkBaseline(ctx context.Context, baseline string) (string, string, error) {
	var commitOut bytes.Buffer
	result, err := r.runner.Run(ctx, execution.Command{Name: "git", Args: []string{"rev-parse", "--verify", "--end-of-options", baseline + "^{commit}"}}, execution.Streams{Stdout: &commitOut})
	if err != nil {
		return "", "", fmt.Errorf("resolving baseline commit: %w", err)
	}
	if result.ExitCode != 0 {
		return "", "", fmt.Errorf("baseline %q does not resolve to a commit", baseline)
	}
	commit := strings.TrimSpace(commitOut.String())
	if len(commit) < 40 || len(commit) > 64 {
		return "", "", fmt.Errorf("git returned an invalid baseline commit")
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{40,64}$`).MatchString(commit) {
		return "", "", fmt.Errorf("git returned an invalid baseline commit")
	}

	var rootOut bytes.Buffer
	result, err = r.runner.Run(ctx, execution.Command{Name: "git", Args: []string{"rev-parse", "--show-toplevel"}}, execution.Streams{Stdout: &rootOut})
	if err != nil {
		return "", "", fmt.Errorf("locating git repository: %w", err)
	}
	if result.ExitCode != 0 {
		return "", "", fmt.Errorf("workspace is not inside a git repository")
	}
	repoRoot, err := filepath.EvalSymlinks(strings.TrimSpace(rootOut.String()))
	if err != nil {
		return "", "", fmt.Errorf("resolving git repository root: %w", err)
	}
	return commit, repoRoot, nil
}

type benchmarkWorktree struct {
	runner *execution.Runner
	owner  *execution.Runner
	path   string
	runDir string
}

func (r *Runtime) createBenchmarkWorktree(ctx context.Context, repoRoot, commit string) (*benchmarkWorktree, error) {
	relativeWorkspace, err := filepath.Rel(repoRoot, r.workspace.Root())
	if err != nil || relativeWorkspace == ".." || strings.HasPrefix(relativeWorkspace, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("configured workspace is outside its git repository")
	}
	runDir, err := createRunTempDir("benchmark")
	if err != nil {
		return nil, fmt.Errorf("creating benchmark run: %w", err)
	}
	baselineRoot := filepath.Join(runDir, "baseline")
	result, err := r.runner.Run(ctx, execution.Command{Name: "git", Args: []string{"worktree", "add", "--detach", baselineRoot, commit}}, execution.Streams{})
	if err != nil || result.ExitCode != 0 {
		_ = os.RemoveAll(runDir)
		if err != nil {
			return nil, fmt.Errorf("creating baseline worktree: %w", err)
		}
		return nil, fmt.Errorf("creating baseline worktree: git exited %d", result.ExitCode)
	}
	baselineWorkspace := filepath.Join(baselineRoot, relativeWorkspace)
	ws, err := workspace.Open(ctx, baselineWorkspace)
	if err != nil {
		worktree := &benchmarkWorktree{owner: r.runner, path: baselineRoot, runDir: runDir}
		_ = worktree.close(ctx)
		return nil, fmt.Errorf("opening baseline workspace: %w", err)
	}
	runner, err := r.runner.ForWorkspace(ws)
	if err != nil {
		worktree := &benchmarkWorktree{owner: r.runner, path: baselineRoot, runDir: runDir}
		_ = worktree.close(ctx)
		return nil, fmt.Errorf("creating baseline runner: %w", err)
	}
	return &benchmarkWorktree{runner: runner, owner: r.runner, path: baselineRoot, runDir: runDir}, nil
}

func (w *benchmarkWorktree) close(ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	result, runErr := w.owner.Run(cleanupCtx, execution.Command{Name: "git", Args: []string{"worktree", "remove", "--force", w.path}}, execution.Streams{})
	removeErr := os.RemoveAll(w.runDir)
	if runErr != nil {
		return runErr
	}
	if result.ExitCode != 0 {
		_, _ = w.owner.Run(cleanupCtx, execution.Command{Name: "git", Args: []string{"worktree", "prune"}}, execution.Streams{})
		return fmt.Errorf("git worktree remove exited %d", result.ExitCode)
	}
	return removeErr
}

func benchmarkArguments(pattern string, input BenchmarkDiffInput) []string {
	return []string{"test", "-run=^$", "-bench=" + input.BenchRegex, "-benchtime=1s", "-count=" + strconv.Itoa(input.Count), pattern}
}

func runBenchmarks(ctx context.Context, runner *execution.Runner, arguments []string) (parser.BenchmarkReport, error) {
	var stdout bytes.Buffer
	result, err := runner.Run(ctx, execution.Command{Name: "go", Args: arguments, Env: map[string]string{"GOWORK": "auto"}}, execution.Streams{Stdout: &stdout})
	if err != nil {
		return parser.BenchmarkReport{}, err
	}
	if result.ExitCode != 0 {
		return parser.BenchmarkReport{}, fmt.Errorf("go test exited %d", result.ExitCode)
	}
	return parser.ParseBenchmarks(&stdout)
}

func compareBenchmarks(baseline, current parser.BenchmarkReport, threshold float64) BenchmarkDiffOutput {
	base := benchmarkMedians(baseline)
	now := benchmarkMedians(current)
	names := make([]string, 0, len(base)+len(now))
	seen := make(map[string]struct{}, len(base)+len(now))
	for name := range base {
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range now {
		if _, ok := seen[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	output := BenchmarkDiffOutput{Comparisons: make([]BenchmarkComparison, 0, len(names))}
	for _, name := range names {
		baselineValue, baselineOK := base[name]
		currentValue, currentOK := now[name]
		comparison := BenchmarkComparison{Name: name, BaselineNsOp: baselineValue, CurrentNsOp: currentValue}
		if baselineOK && currentOK {
			comparison.DeltaPercent = 100 * (currentValue - baselineValue) / baselineValue
			comparison.Regression = comparison.DeltaPercent > threshold
			if comparison.Regression {
				output.Regressions++
			}
		}
		output.Comparisons = append(output.Comparisons, comparison)
	}
	return output
}

func benchmarkMedians(report parser.BenchmarkReport) map[string]float64 {
	medians := make(map[string]float64, len(report.Benchmarks))
	for _, benchmark := range report.Benchmarks {
		medians[benchmark.Name] = benchmark.Median
	}
	return medians
}

func (r *Runtime) recordBenchmarkTrace(input BenchmarkDiffInput, started time.Time, output BenchmarkDiffOutput, kind trace.ErrorKind) {
	summary := ""
	if kind == trace.ErrorNone {
		summary = fmt.Sprintf("%d benchmarks, %d regressions", len(output.Comparisons), output.Regressions)
	}
	_ = r.tracer.Record(trace.Event{Tool: "go_benchmark_diff", Args: input, Duration: time.Since(started), ResultSummary: summary, ErrorKind: kind})
}
