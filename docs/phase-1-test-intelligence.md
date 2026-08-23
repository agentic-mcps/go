# Phase 1 — Test Intelligence (v0.1)

> v0.1 contract: module `github.com/ashwingopalsamy/agentic-go`, exactly 7
> tools, 4 resources, and 4 prompts over stdio only. There is no gopls or
> navigation implementation, cache, HTTP transport, doctor command, or SARIF
> output in v0.1. The executable supports `--workspace` (default: current
> directory) and `--version`. Trusted target-code execution requires workspace
> containment, deadlines, bounded concurrency, and output caps. Long-running
> subprocess tools report event-driven MCP progress when a progress token is
> present. Trace files are stored under
> `os.UserCacheDir()/agentic-go/runs`. The five execution tools are annotated
> non-read-only, potentially destructive, non-idempotent, and open-world; the
> two audit tools are annotated read-only, non-destructive, idempotent, and
> closed-world.

Read `docs/contracts.md` first. This file is self-contained beyond that.

## Deliverables
1. `cmd/agentic-go/main.go` — server bootstrap, flags, transport selection.
2. `internal/tools/go_test_structured.go`
3. `internal/tools/go_race_report.go`
4. `internal/parser/testjson.go`
5. `internal/parser/race.go`
6. `internal/trace/trace.go`
7. `internal/tools/testdata/fixtures/testing/` fixture package (shared by Phase 1
   and Phase 2's self-check tests — see `docs/contracts.md` fixture layout).
8. `README.md` — quickstart, `go install` line, minimal Claude Code
   `.mcp.json`/`.claude.json` config snippet (stdio transport).
9. `.github/workflows/ci.yml` — see CI contract in `docs/contracts.md`. Create
   now; later phases only add steps if the contract's step list grows (it
   doesn't — all 6 steps are already fully specified).

## `cmd/agentic-go/main.go`

Flags (stdlib `flag` package, not a CLI framework — no dependency justifies one):

| Flag | Default | Meaning |
|---|---|---|
| `--workspace` | `.` | Go workspace root used to resolve relative package paths in tool arguments |
| `--log-level` | `info` | Stderr lifecycle log level: `debug` or `info`; tool arguments are never logged |
| `--max-concurrent-loads` | `4` | Process-wide ceiling for Go subprocesses and package loads |
| `--max-tool-seconds` | `300` | Global tool subprocess deadline; accepted range 1–300 seconds |
| `--version` | (none) | Print the server version and exit |

Startup sequence:
1. Parse flags.
2. Install the stderr `slog.Logger` per `docs/contracts.md`'s server-lifecycle
   carve-out, level from `--log-level`. Every subsequent step — including
   preflight — logs through this logger, never `fmt.Println` or
   `log.Default()`.
3. Create the process context with
   `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
4. **Startup preflight** — fatal (`logger.Error(...)` then `os.Exit(1)`,
   never a silent `os.Exit` with no logged reason) if any of:
   - `--workspace` does not resolve to a directory, or that directory has no
     `go.mod` reachable from it (nor a `go.work` workspace file covering it).
   This is the predictable top support burden this project can eliminate for
   free: cheaper to fail loud once at startup than to have every tool
   independently discover that this is not a Go workspace on its first call.
5. Resolve `--workspace` to an absolute path; `os.Chdir` is NOT done — pass the
   absolute path explicitly to every tool call instead. (Chdir in a
   long-lived server is a footgun the moment any tool call is concurrent.)
6. Construct the server with deprecated MCP logging disabled:
   `server := mcp.NewServer(&mcp.Implementation{Name: "agentic-go", Version: "0.1.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})`.
7. Call every `Register<Name>(server, runtime)` function through one explicit
   flat `tools.RegisterAll` list,
   ordered by phase/tier (Tier 1 first). This list is the single place that
   proves which tools exist — each new tool adds one line here and never
   touches this file for anything else.
8. Initialize trace under `os.UserCacheDir()/agentic-go/runs`, inject it into
   tool constructors, and run `server.Run(ctx, &mcp.StdioTransport{})`.
9. Run with graceful shutdown on Ctrl-C/SIGTERM, propagated into every
   in-flight tool call.

## `go_test_structured`

**Input:**
```go
type TestStructuredInput struct {
    Package string `json:"package" jsonschema:"Go package import path or ./relative/path"`
    Race    bool   `json:"race,omitempty" jsonschema:"enable the race detector; default false"`
    Verbose bool   `json:"verbose,omitempty" jsonschema:"include passing and skipped test output; default false"`
    TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"test timeout in seconds; default 60, maximum 300"`
}
```

**Output types** (phase-specific, not in `docs/contracts.md` — these are test
results, not audit findings, deliberately a different shape):
```go
type TestCase struct {
    Name     string  `json:"name"`
    Package  string  `json:"package"`
    Status   string  `json:"status"`      // "pass" | "fail" | "skip"
    ElapsedS float64 `json:"elapsed_s"`
    Output   string  `json:"output,omitempty"` // failures always; passing/skipped only when Verbose
}
type PackageSummary struct {
    Status  string `json:"status"` // "ok" | "FAIL"
    Passed  int    `json:"passed"`
    Failed  int    `json:"failed"`
    Skipped int    `json:"skipped"`
    Output  string `json:"output,omitempty"` // package-level build/test output on failure
}
type TestStructuredOutput struct {
    Packages   map[string]PackageSummary `json:"packages"`
    Tests      []TestCase                `json:"tests"`
    Passed     int   `json:"passed"`
    Failed     int   `json:"failed"`
    Skipped    int   `json:"skipped"`
    DurationMS int64 `json:"duration_ms"`
}
```

**Handler algorithm:**
1. Resolve the package with bounded `go list -json -mod=readonly`; reject every
   matched package whose directory is outside the configured workspace.
2. Submit `go test -json` to the shared execution runner. Append `-race`
   if `Race`, `-v` if `Verbose`, `-timeout`, `fmt.Sprintf("%ds", TimeoutSeconds)`,
   then the validated package pattern. `Dir` = resolved workspace.
3. Connect the runner's bounded stdout writer to the parser through an
   `io.Pipe`; do not
   buffer the whole output into memory first, packages can produce megabytes
   of `-v` output.
4. A non-zero process exit is expected and NOT the handler's error when tests
   simply failed (`go test` exits non-zero on test failure) — only surface it
   as a handler error if the parser found zero valid JSON events at all
   (indicates `go test` failed to even start, e.g. bad package path).
5. Return the accumulated `TestStructuredOutput`.

## `internal/parser/testjson.go`

`go test -json` emits one JSON object per line (NDJSON) with shape:
```go
type testEvent struct {
    Time    time.Time `json:"Time"`
    Action  string    `json:"Action"`  // "run","pause","cont","bench","output","pass","fail","skip"
    Package string    `json:"Package"`
    Test    string    `json:"Test,omitempty"` // empty = package-level event
    Elapsed float64   `json:"Elapsed,omitempty"` // seconds, present on pass/fail/skip
    Output  string    `json:"Output,omitempty"`
}
```

**Algorithm:**
1. Scan one bounded line at a time (8 MiB maximum event) and `json.Unmarshal`
   each complete line. A stream-level `json.Decoder` cannot recover after a
   malformed line, so it does not satisfy the skip-and-continue contract.
2. Maintain `map[string]*strings.Builder` keyed by `Package+"\x00"+Test` for
   accumulating `output` action lines per test (only test-level events, i.e.
   `Test != ""`).
3. On `Action == "pass"|"fail"|"skip"` with `Test != ""`: finalize a `TestCase`
   using the accumulated builder content. Keep failures always; keep passing
   and skipped output only when `Verbose` is true.
4. On `Action == "pass"|"fail"` with `Test == ""`: this is the package-level
   terminal event — set `PackageSummary.Status = "ok"` or `"FAIL"` for that
   package and retain package-level output only for failure diagnostics.
5. Increment top-level `Passed`/`Failed`/`Skipped` counters as each test-level
   terminal event is seen.
6. Malformed/non-JSON lines on stdout (shouldn't happen with `-json`, but a
   corrupted toolchain or a test that writes raw bytes to stdout via `os.Stdout`
   directly can produce one) are counted and skipped, not fatal. If no valid
   events exist, return a parser error.

This exact streaming decoder is reused
unchanged by `go_flake_finder` (Phase 2) and `go_panic_trace` (Phase 4) — do
not fork a second implementation. Each tool applies its own reduction to the
same `TestEvent` stream.

## `go_race_report`

**Input:** `{Package string, TimeoutSeconds int}` — race is always on for this
tool (no `Race bool` field, unlike `go_test_structured` — a tool named
`go_race_report` that might not enable the race detector is a design smell).

**Output:**
```go
type RaceAccess struct {
    Kind        string          `json:"kind"` // "read" | "write"
    Address     string          `json:"address"`
    GoroutineID int             `json:"goroutine_id"`
    Function    string          `json:"function"`
    Location    tools.Location  `json:"location"`
    State       string          `json:"state,omitempty"` // "running" | "finished" — only on creation-site entries
}
type RaceConflict struct {
    Current           RaceAccess   `json:"current"`
    Previous          RaceAccess   `json:"previous"`
    GoroutineCreation []RaceAccess `json:"goroutine_creation"` // where each involved goroutine was spawned; 0-2 entries
}
type RaceReportOutput struct {
    Conflicts []RaceConflict `json:"conflicts"` // empty slice, never nil, if no race found
    RawBlocksFound int       `json:"raw_blocks_found"` // sanity cross-check: count of "WARNING: DATA RACE" markers seen
}
```

**Handler:** runs `go test -race -json <package>`, feeds stdout through
`testjson` parser AND through `race.Parse` in parallel (the race detector's
text output arrives interleaved inside `output` action `Output` fields — it
is NOT separately delimited JSON). Concretely: accumulate every `output`
action's `Output` string, concatenated in event order, into one buffer per
package; after the test run completes, run `race.Parse(buffer)` on that
buffer. Race detector text is multi-line and line-buffering per JSON event
can split a race block across several `output` events — concatenation before
parsing, not per-event parsing, is required.

## `internal/parser/race.go`

Go's race detector text format (stable since Go 1.1, unaffected by the
1.25-1.27 toolchain range):

```
WARNING: DATA RACE
Write at 0x00c0000a4010 by goroutine 7:
  path/to/pkg.Function()
      /abs/path/file.go:42 +0x1a3

Previous read at 0x00c0000a4010 by goroutine 6:
  path/to/pkg.OtherFunction()
      /abs/path/file.go:38 +0x27

Goroutine 7 (running) created at:
  path/to/pkg.Spawner()
      /abs/path/file.go:30 +0x9c

Goroutine 6 (running) created at:
  path/to/pkg.OtherSpawner()
      /abs/path/file.go:26 +0x4f
==================
```

**Parsing algorithm:**
1. Split the full buffer on the literal line `==================` — each
   segment containing `WARNING: DATA RACE` is one conflict block. There may
   be zero, one, or many blocks in a single run (the detector does not halt
   on first race by default — do not set `GORACE=halt_on_error=1`, we want
   every race in one pass).
2. Within a block, regex `^(Write|Read) at (0x[0-9a-f]+) by goroutine (\d+):$`
   matches the "current" access line. The next non-blank line is the function
   name (trim whitespace). The line after that matches
   `^\s+(\S+\.go):(\d+)(?: \+0x[0-9a-f]+)?$` for file:line (discard the `+0x...`
   offset suffix).
3. Regex `^Previous (write|read) at (0x[0-9a-f]+) by goroutine (\d+):$` matches
   the "previous" access, same function/location extraction as step 2.
4. Zero or more blocks matching `^Goroutine (\d+) \((running|finished)\) created at:$`
   followed by the same function/location pair — these populate
   `GoroutineCreation`, matched to `Current`/`Previous` by goroutine ID.
5. If a block's regexes don't all match (format drift in a future Go version),
   do not panic and do not silently drop the block — emit one `RaceConflict`
   with only the fields successfully extracted and the raw block text in
   `Current.Function` prefixed `"UNPARSED: "`, so the caller still sees
   *something* rather than a silently short `Conflicts` slice. Increment
   `RawBlocksFound` regardless of parse success, so a mismatch between
   `RawBlocksFound` and `len(Conflicts)` with fully-populated fields is itself
   the signal that the parser needs a version-format update.

## `internal/trace/trace.go`

Implements the trace contract from `docs/contracts.md` exactly. Public surface:
```go
func Init() (*Tracer, error) // reads AGENTIC_GO_TRACE once; disabled tracer if unset
func (t *Tracer) Record(event Event) error
func (t *Tracer) Close() error // flushes and closes the JSONL file, called from main's shutdown path
```
`Record` on a no-op `Tracer` (env var unset) does nothing and allocates
nothing — check the bool first, return before touching `args` (which would
otherwise cost a `json.Marshal` + `sha256.Sum` on every call for no reason).
Enabled initialization reports cache-directory failures instead of silently
disabling a trace the user explicitly requested. `Event` accepts only bounded
aggregate fields and an `ErrorKind` enum; it has no raw-error field.

## Fixture: `internal/tools/testdata/fixtures/testing/`

One package, `package testingfixture`, these files:
- `flaky.go` + `flaky_test.go`: a function whose behavior depends on
  `time.Now().UnixNano()%2`, test asserts a fixed outcome — fails
  non-deterministically. `// VIOLATION: flaky_test_time_seeded`.
- `panic.go` + `panic_test.go`: a function that indexes a slice out of bounds
  under a specific input; test calls it with that input. `// VIOLATION: panic_test`.
- `stable_test.go`: one unconditionally-passing test, one `t.Skip()` test.
- `bench_test.go`: one `func BenchmarkX(b *testing.B)` doing trivial work
  (needed by Phase 2's `go_benchmark_diff` self-check, specified there).

## Verification (this phase's own gate)
`internal/tools/go_test_structured_test.go` and `go_race_report_test.go`,
each targeting the `testing` fixture:
- `go_test_structured`: running against the fixture returns
  `Passed >= 1, Failed >= 1` (panic test fails, stable test passes), and the
  failed test's `Output` is non-empty while the passed test's is empty.
- `go_race_report`: running against the fixture with the flaky/race-inducing
  goroutine present returns `len(Conflicts) >= 1` with a non-empty
  `Current.Function` and a valid `Location.File`/`Location.Line`. NOTE: the
  `testing` fixture as described above does not yet contain a genuine data
  race — add one: a third file `race.go` + `race_test.go` with two goroutines
  incrementing a shared `int` without synchronization, test launches both and
  waits via `time.Sleep` (acceptable here — this is fixture code meant to
  race, not production code; `go-testing.md`'s "no `time.Sleep` in tests" rule
  governs assertions about async completion, not deliberately-racy fixtures).
