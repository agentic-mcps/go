# Phase 2 — Coverage, Benchmark Diff, Flake Finder (v0.1)

This phase completes the trusted target-code execution tools. v0.1 has no
cache: every result is fresh. Subprocesses require containment, deadlines,
bounded concurrency, output caps, and event-driven MCP progress when a
progress token is supplied.

Read `docs/contracts.md` first. Reuses `internal/parser/testjson.go` and the
`testing` fixture from `phase-1-test-intelligence.md` — read that file's
"Fixture" section too, do not re-derive a second fixture.

## Deliverables
1. `internal/tools/go_coverage_gaps.go`
2. `internal/tools/go_benchmark_diff.go`
3. `internal/tools/go_flake_finder.go`
4. `internal/parser/coverage.go`
5. `internal/parser/benchmark.go`
6. Fixture additions: `internal/tools/testdata/fixtures/testing/bench_test.go`
   (already listed in Phase 1, defined here), plus one uncovered branch added
   to `panic.go` for `go_coverage_gaps` to find.

All three tools in this phase are never cached. The former cache design is
deferred beyond v0.1; no cache package or registry is part of this phase.

<!-- The former internal/cache design is intentionally omitted in v0.1. -->

## `go_coverage_gaps`

**Input:**
```go
type CoverageGapsInput struct {
    Package string `json:"package" jsonschema:"Go package import path or ./relative/path"`
}
```

**Output:**
```go
type CoverageGap struct {
    File       string `json:"file"`
    StartLine  int    `json:"start_line"`
    StartCol   int    `json:"start_col"`
    EndLine    int    `json:"end_line"`
    EndCol     int    `json:"end_col"`
    Statements int    `json:"statements"`
}
type FileCoverage struct {
    File    string        `json:"file"`
    Percent float64       `json:"percent"`
    Gaps    []CoverageGap `json:"gaps"` // empty, not nil, when file is 100% covered
}
type CoverageGapsOutput struct {
    Files          []FileCoverage `json:"files"`
    OverallPercent float64        `json:"overall_percent"`
}
```

**Handler:**
1. Create a unique run directory under
   `os.UserCacheDir()/agentic-go/runs/coverage-*`; remove it on return. The
   profile is capped at 8 MiB before parsing and never touches the target
   worktree.
2. `go test -coverprofile=<tmp> -covermode=atomic <package>` via the shared
   bounded execution runner. A non-zero test exit is a tool error: coverage
   from an incomplete suite must not be presented as authoritative.
   `atomic` (not `count`) — matches the SLA-sensitive/concurrent-code context
   this whole project lives in; `atomic` is race-detector-safe if the caller
   also passes `-race` in a future extension, `count` is not. Fixed choice,
   no input field for mode — one correct default beats a knob nobody sets
   correctly.
3. Parse the profile file (below) into per-file gap lists and percentages.
   Convert profile import paths to slash-separated paths relative to the
   configured workspace; fail if any file cannot be contained and mapped.
4. `OverallPercent` = `sum(covered_statements_all_files) / sum(total_statements_all_files) * 100` —
   computed from the SAME parsed block list as per-file percentages, never by
   shelling out a second `go tool cover -func` call. One source of truth for
   both numbers; two independently-parsed text formats agreeing by coincidence
   is a bug waiting to happen the day either format changes.

## `internal/parser/coverage.go`

Go coverage profile text format (stable, documented in `go help testflag` /
`cmd/cover`):
```
mode: atomic
github.com/ashwingopalsamy/agentic-go/internal/tools/go_x.go:12.34,15.2 3 1
github.com/ashwingopalsamy/agentic-go/internal/tools/go_x.go:17.2,19.3 1 0
```
Line 1: `mode: <atomic|count|set>` — read once, discard (we always request `atomic`,
a mismatch here would mean a caller's toolchain override, not our bug — trace
it at debug level, don't error).
Subsequent lines: regex `^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$` captures
file, startLine, startCol, endLine, endCol, numStmt, count.

**Algorithm:**
1. `bufio.Scanner` over the profile file, one regex match per line.
2. Group matches by `file`.
3. Per file: `Percent = 100 * sum(numStmt where count>0) / sum(numStmt)`.
4. Per file: `Gaps` = every match where `count == 0`, mapped directly to
   `CoverageGap` (field names line up 1:1 with the regex captures).
5. Files with zero total statements (shouldn't occur — cover only emits
   instrumented files) are skipped, not a zero-division crash — defensive
   only because a hand-edited or corrupted profile is possible input, not
   because normal operation produces this.

## `go_benchmark_diff`

**Input:**
```go
type BenchmarkDiffInput struct {
    Package          string  `json:"package" jsonschema:"Go package import path or ./relative/path"`
    Baseline         string  `json:"baseline" jsonschema:"git ref to compare against, e.g. HEAD~1 or main"`
    BenchRegex       string  `json:"bench_regex,omitempty" jsonschema:"regex filter for -bench; default is all benchmarks"`
    Count            int     `json:"count,omitempty" jsonschema:"repetitions per revision; default 6"`
    ThresholdPercent float64 `json:"threshold_percent,omitempty" jsonschema:"regression threshold in percent; default 10"`
}
```

**Output:**
```go
type BenchmarkComparison struct {
    Name         string  `json:"name"`
    BaselineNsOp float64 `json:"baseline_ns_op"`
    CurrentNsOp  float64 `json:"current_ns_op"`
    DeltaPercent float64 `json:"delta_percent"`
    Regression   bool    `json:"regression"`
}
type BenchmarkDiffOutput struct {
    Comparisons []BenchmarkComparison `json:"comparisons"`
    Regressions int                   `json:"regressions"`
}
```

**Safety design decision (stated explicitly, this is deliberate, not an
oversight):** never `git checkout` the caller's actual working tree to reach
`Baseline` — that mutates state a human or another process may be mid-edit
on. Use `git worktree add <tmpdir> <Baseline>`
instead: an isolated checkout, zero interaction with the real working tree,
`git worktree remove <tmpdir>` (or `defer os.RemoveAll` + `git worktree prune`
if `remove` fails on a dirty tmp dir) cleans up unconditionally via `defer`.

**Handler algorithm:**
1. Reject a baseline beginning with `-`, then validate it as a commit with
   `git rev-parse --verify --end-of-options <Baseline>^{commit}` — handler
   error (not empty-result) if it doesn't, this is a caller input mistake, not
   an execution-failure-vs-zero-findings ambiguity (no findings concept here).
2. Create a unique parent with `os.MkdirTemp("", "agentic-go-bench-")`,
   set `tmpdir` to a nonexistent child beneath it, and run
   `git worktree add --detach <tmpdir> <Baseline>`.
3. Run `go test -run=^$ -bench=<BenchRegex> -benchtime=1s -count=<Count> <Package>`
   in `tmpdir` — capture stdout, parse (below) → baseline medians per name.
4. Run the identical command in the real workspace dir (current code) →
   current medians per name.
5. `git worktree remove <tmpdir>` (defer, unconditional).
6. For each benchmark name present in both runs:
   `DeltaPercent = 100 * (current - baseline) / baseline`;
   `Regression = DeltaPercent > ThresholdPercent`.
   A name present in only one revision (added/removed benchmark) is reported
   with the missing side's `*NsOp` as `0` and `Regression = false` — a
   structural diff, not a performance regression, don't conflate the two in
   the same boolean.
7. `Regressions` = count where `Regression == true`.

## `internal/parser/benchmark.go`

`go test -bench` output line format (one line per completed benchmark run,
repeated `Count` times per name when `-count=N`):
```
BenchmarkParseJSONL-8   	 1000000	   123.4 ns/op	    45 B/op	    2 allocs/op
```
Regex: `^(Benchmark\S+)-\d+\s+\d+\s+([\d.]+) ns/op`. Only `ns/op` is used —
`B/op`/`allocs/op` are ignored for the diff (the roadmap's tool scope is
latency regression detection, not allocation regression; adding a second
metric axis nobody asked for is scope creep, note it as a possible future
`--metric=allocs` flag, do not build it now).

**Median, not mean:** with `-count=N`, each benchmark name appears N times in
the output; collect all N `ns/op` values per name, sort, take the middle
value (or average of the two middle values if N is even). Median is robust to
one noisy-neighbor outlier run without needing `golang.org/x/perf/benchstat`'s
full statistical machinery — contracts.md's dependency floor is exactly two
modules; benchstat is not one of them and does not become one for this.

## `go_flake_finder`

**Input:**
```go
type FlakeFinderInput struct {
    Package        string `json:"package" jsonschema:"Go package import path or ./relative/path"`
    Runs           int    `json:"runs,omitempty" jsonschema:"repetitions; default 20"`
    TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"per-run timeout in seconds; default 120"`
}
```

**Output:**
```go
type FlakeResult struct {
    Test      string  `json:"test"`
    Package   string  `json:"package"`
    Runs      int     `json:"runs"`
    Passes    int     `json:"passes"`
    Failures  int     `json:"failures"`
    FlakeRate float64 `json:"flake_rate"` // failures / runs
}
type FlakeFinderOutput struct {
    Flaky         []FlakeResult `json:"flaky"` // only tests with >=1 pass AND >=1 fail across Runs
    TotalTestsRun int           `json:"total_tests_run"`
}
```

**Handler:** `go test -json -count=<Runs> -timeout=<TimeoutSeconds>s <Package>`.
`-count=N` re-executes the compiled test binary N times within this single
`go test` invocation (documented `go help testflag` behavior) — this is the
correct, single-subprocess way to get N independent runs; do NOT loop
`exec.Command` N times in application code, that reruns the build step N
times for no benefit and multiplies process-spawn overhead for nothing.

Feed the one JSON stream through the **same** `testjson` parser from Phase 1
(no second implementation — see Phase 1's explicit reuse note). Difference in
reduction: instead of discarding per-test state after each terminal event,
accumulate a `map[string]*FlakeResult` keyed by `Package+"\x00"+Test` across
ALL terminal events seen (there will be up to `Runs` terminal events per test
name, not one). After the stream ends, filter to names with `Passes > 0 &&
Failures > 0` → `Flaky`.

## Fixture: `bench_test.go` (completes Phase 1's fixture package)
```go
func BenchmarkTrivial(b *testing.B) {
    for i := 0; i < b.N; i++ {
        _ = fmt.Sprintf("%d", i) // COMPLIANT: deliberately allocates, gives go_benchmark_diff a non-zero, non-trivial-to-optimize-away ns/op to diff against itself (baseline == current, same commit) — self-check only needs DeltaPercent ≈ 0, not a real regression
    }
}
```
Add one intentionally-uncovered branch to `panic.go` (from Phase 1) behind an
unreachable-in-tests condition, so `go_coverage_gaps` has ≥1 real gap to find
without inventing a second fixture file just for this tool.

## Verification (this phase's own gate)
`internal/tools/go_coverage_gaps_test.go`:
- Running against the `testing` fixture returns `OverallPercent < 100` and at
  least one `FileCoverage.Gaps` entry pointing at the deliberately-uncovered
  branch's exact line.

`internal/tools/go_benchmark_diff_test.go`:
- `Baseline: "HEAD"` (comparing the fixture against itself, same commit) on
  the fixture's `BenchmarkTrivial` returns `len(Comparisons) == 1` and
  `Regression == false` (same code both sides — asserting `DeltaPercent`
  is exactly 0 would be flaky given real scheduler noise even at identical
  commits; assert `math.Abs(DeltaPercent) < ThresholdPercent` instead, which
  is the actual invariant this tool exists to check, not a coincidence of
  measurement noise happening to land near zero).

`internal/tools/go_flake_finder_test.go`:
- Running against `flaky.go`/`flaky_test.go` (Phase 1 fixture, `%2`-seeded)
  with `Runs: 20` returns `len(Flaky) >= 1` containing that test name, with
  `0 < FlakeRate < 1` (would fail if the flake logic broke into an
  always-fails-the-same-way stub — this is the WHY-not-WHAT check `go-testing.md`
  requires: a stub returning "always flaky" or "never flaky" fails this
  assertion, a stub returning a fixed `FlakeRate` value fails the exact-fixture
  cross-check too since the seeded fixture's real rate is ~50%, not fixed).
