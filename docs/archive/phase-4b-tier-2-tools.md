# Phase 4b — remaining Tier-2 tools

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

Fills the `04-` → `06-` numbering gap. Three tools, none of which are
`go/analysis` passes: `go_dead_code` and `go_test_map` are whole-program
call-graph tools; `go_panic_trace` is a `go test -json` consumer, same family
as `go_flake_finder`/`go_race_report` (Phase 1).

Read `contracts.md` first. All three conform to its canonical tool
skeleton, `internal/finding` types, and error-handling contract. **None of
the three route through `astutil.RegisterRule`/`checker.Analyze`** — there is
no `analysis.Pass` here, so `Finding` values are constructed as literals
directly in the handler. Say so in each tool's own doc comment so a build
agent doesn't go looking for a nonexistent `Analyzer`.

Deliverables:

1. `internal/reach/reach.go` — shared workspace call graph (new).
2. `internal/tools/go_dead_code.go`
3. `internal/tools/go_test_map.go`
4. `internal/tools/go_panic_trace.go`
5. `internal/parser/panic.go` — panic/goroutine-dump text parser (new).
6. One additive field on `internal/parser/testjson.go` (Phase 1) — see
   `go_panic_trace`'s section. Non-breaking; existing callers unaffected.
7. `internal/tools/testdata/fixtures/testing/deadcode.go` +
   `deadcode_test.go` (new). `go_panic_trace` and `go_test_map` reuse Phase
   1's existing `panic.go`/`panic_test.go` and `flaky.go`/`flaky_test.go` —
   no new fixture needed for either.

## Shared: `internal/reach` — workspace call graph

`go_dead_code` and `go_test_map` both need a whole-program static call graph
over the target workspace. Built once, in one package, used by both — do not
construct a second `ssa.Program`/`cha.CallGraph` per tool.

**Why CHA, not `cmd/deadcode`'s RTA.** `golang.org/x/tools/cmd/deadcode` (the
tool the roadmap names as this tool's "backing tool") uses Rapid Type Analysis,
which roots the call graph at `main` function(s) of executable packages
only. Run against a pure library with no `main` in the loaded set, RTA
reports the entire library as dead — "by design," not a bug. Since this
tool's target is arbitrary packages/paths, most of which are libraries, RTA
is the wrong algorithm for the common case, not just a lazier one.
`golang.org/x/tools/go/callgraph/cha` (Class Hierarchy Analysis) needs no
`main` root, is explicitly documented sound on partial programs including
libraries with no `main` or test function, and lives inside the same
`golang.org/x/tools` dependency already on the floor — zero new external
deps, zero new binaries to shell out to. Verified 2026-08-22 against the
package's own documentation; reverify the API against the pinned x/tools
version before implementation.

**Cost of CHA over RTA/VTA.** CHA over-approximates the interface-satisfies
relation — an interface method call site gets an edge to every type in scope
implementing that interface, whether or not that type could actually reach
the call dynamically. Effect differs by consumer:

- `go_dead_code`: over-approximation only pushes findings toward
  under-reporting (fewer false "dead" positives, since more things gain a
  spurious in-edge). Acceptable — a dead-code tool should bias toward "don't
  cry wolf."
- `go_test_map`: over-approximation actively pollutes the signal — a test
  using an interface value will show every concrete implementation in scope
  as "exercised," including ones that test never constructs. This is a named,
  documented limitation (see that tool's section), not silently absorbed.

```go
package reach

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

// Graph is a whole-workspace static call graph, built once per tool
// invocation. Not cached across invocations — each call reloads and
// rebuilds; the process-wide load semaphore and -max-tool-seconds flag
// from CONTRACTS' "Input containment and resource limits" section bound
// cost, no separate per-tool timeout field is added here.
type Graph struct {
	cg   *callgraph.Graph
	fset *token.FileSet
	ws   string // absolute workspace root, for Location.File relativization
}

// Build loads ./... from ws (Tests: true, full CONTRACTS Need* bits),
// builds an ssa.Program, and runs CHA. Returns an error (never a panic) if
// packages.Load reports any package error — a partially-typed program
// produces a call graph too unreliable to trust.
func Build(ctx context.Context, ws string) (*Graph, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     ws,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedTypesSizes | packages.NeedTypesInfo | packages.NeedSyntax,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading workspace %s: %w", ws, err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("loading workspace %s: package errors present", ws)
	}

	prog, _ := ssautil.Packages(pkgs, ssa.BuilderMode(0))
	prog.Build()

	g := cha.CallGraph(prog)
	return &Graph{cg: g, fset: prog.Fset, ws: ws}, nil
}

// Location returns fn's declaration site, relativized to the workspace
// root. ok is false if fn has no position (synthetic/wrapper) or its file
// falls outside ws — per CONTRACTS' Location.File rule, callers must drop
// the function rather than report an absolute path.
func (g *Graph) Location(fn *ssa.Function) (loc finding.Location, ok bool) {
	pos := g.fset.Position(fn.Pos())
	if pos.Filename == "" {
		return finding.Location{}, false
	}
	rel, err := filepath.Rel(g.ws, pos.Filename)
	if err != nil || strings.HasPrefix(rel, "..") {
		return finding.Location{}, false
	}
	return finding.Location{File: filepath.ToSlash(rel), Line: pos.Line}, true
}

// Functions returns every *ssa.Function with a workspace-local Location —
// GOROOT and module-cache functions (fmt.Sprintf, etc.) are excluded at
// this boundary, not filtered piecemeal by every caller.
func (g *Graph) Functions() []*ssa.Function {
	var out []*ssa.Function
	for fn := range g.cg.Nodes {
		if _, ok := g.Location(fn); ok {
			out = append(out, fn)
		}
	}
	return out
}

// Callers returns fn's direct in-workspace callers (Location-filtered).
func (g *Graph) Callers(fn *ssa.Function) []*ssa.Function {
	node := g.cg.Nodes[fn]
	if node == nil {
		return nil
	}
	var out []*ssa.Function
	for _, edge := range node.In {
		if caller := edge.Caller.Func; caller != nil {
			if _, ok := g.Location(caller); ok {
				out = append(out, caller)
			}
		}
	}
	return out
}

// ReachableFrom returns every in-workspace function reachable from fn via
// the call graph, BFS, fn itself excluded. Bounded by workspace size —
// there is no separate depth cap; MaxPackages (CONTRACTS) already bounds
// the workspace this runs against.
func (g *Graph) ReachableFrom(fn *ssa.Function) []*ssa.Function {
	start := g.cg.Nodes[fn]
	if start == nil {
		return nil
	}
	seen := map[*ssa.Function]bool{fn: true}
	queue := []*callgraph.Node{start}
	var out []*ssa.Function
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, edge := range n.Out {
			callee := edge.Callee.Func
			if callee == nil || seen[callee] {
				continue
			}
			seen[callee] = true
			if _, ok := g.Location(callee); ok {
				out = append(out, callee)
			}
			queue = append(queue, edge.Callee)
		}
	}
	return out
}

// IsTestFile reports whether fn is declared in a _test.go file.
func IsTestFile(g *Graph, fn *ssa.Function) bool {
	pos := g.fset.Position(fn.Pos())
	return strings.HasSuffix(pos.Filename, "_test.go")
}
```

`filepath` import omitted above for brevity of the excerpt — the real file
imports it alongside the shown stdlib/x/tools packages.

Progress reporting: `Build`'s `packages.Load` step can dominate wall-clock
time on a large workspace. Call CONTRACTS' `astutil` progress helper once
per package returned by `packages.Load`, same as every audit tool — do not
invent a second progress mechanism for these two tools.

## `go_dead_code`

Input: package or path. Output: exported funcs with zero callers, funcs
only called from tests. Backing: `internal/reach` (CHA), not `cmd/deadcode`.

```go
package tools

type DeadCodeInput struct {
	Package string `json:"package" jsonschema:"import path or relative directory to report on"`
}

// Rule is one of "deadcode-01" (no in-workspace caller) or "deadcode-02"
// (all in-workspace callers are test-only). These are hardcoded literals,
// not looked up via astutil.RegisterRule — there is no domain registry for
// a non-go/analysis tool.
type DeadCodeFinding struct {
	Rule     string           `json:"rule"`
	Function string           `json:"function"` // "pkg/path.FuncName"
	Location finding.Location `json:"location"`
	Exported bool             `json:"exported"`
	Message  string           `json:"message"`
}

type DeadCodeOutput struct {
	Result finding.AuditResult `json:"result"`
}
```

Handler algorithm:

1. `normalizeDeadCodeInput(&in)`.
2. `resolveInWorkspace(in.Package)` (CONTRACTS input-containment validator).
3. `reach.Build(ctx, workspaceRoot)` — whole-module graph, not scoped to
   `in.Package` alone; a function in `in.Package` may only be called from
   elsewhere in the module, and that call must count.
4. `graph.Functions()`, filtered to those declared under `in.Package`'s
   directory.
5. For each candidate `fn`, exclude from consideration entirely (never
   flagged, not even at Info) if it is one of the conventional
   zero-static-caller entry points the `testing` package invokes via
   reflection, not a visible call site: `func init()`; `func main()` in
   `package main`; `func TestXxx(*testing.T)`, `func BenchmarkXxx(*testing.B)`,
   `func FuzzXxx(*testing.F)`, `func ExampleXxx()` matching the stdlib
   signature convention. Skipping this step floods every test file with a
   false "dead" finding on every single test function.
6. `callers := graph.Callers(fn)`.
   - `len(callers) == 0`: `deadcode-01`. Severity `SeverityWarning` for
     unexported `fn`. Severity `SeverityInfo` for exported `fn`, with
     `Message` carrying the caveat explicitly: *"no caller found in this
     workspace — expected if this is a public API consumed by another
     module; confirm before removing."* Whole-program analysis has no
     visibility outside the loaded workspace; this is a permanent scope
     limit, documented here rather than fixed.
   - `len(callers) > 0` and every caller's declaring file is `_test.go`:
     `deadcode-02`, `SeverityInfo` regardless of exported-ness — alive only
     under test, worth a look, not a defect.
   - Otherwise: no finding. `fn` has a genuine in-workspace, non-test caller.
7. Assemble `finding.AuditResult` (`Findings` sorted by `Location.File` then
   `Line`; `Total`/`CountsBySeverity` computed pre-truncation per CONTRACTS).
8. Deferred `recover()` boundary at the top of the handler, per
   go-security.md — SSA construction on malformed/adversarial input is exactly
   the class of AST-adjacent code that rule requires it for.

**Known limitations, documented rather than fixed** (same posture as every
domain's exclusion list): (a) exported-function false positives when the
real caller lives in a downstream module outside this workspace — mitigated
by Info-severity + caveat message, not suppressed; (b) generated code
(`//go:generate` output) is not special-cased — a generated-but-unused
function reports like any other; (c) once any call through an interface
type reaches a concrete method anywhere in the loaded scope, CHA gives that
method an in-edge — an interface-satisfying method is effectively never
`deadcode-01` under this design, which is the same false-negative bias noted
above for the shared `reach` package.

Fixture: `internal/tools/testdata/fixtures/testing/deadcode.go` (new) +
`deadcode_test.go` (new):

```go
package testing

// VIOLATION: deadcode-01 (unexported, zero callers anywhere)
func neverCalled() int { return 42 }

// VIOLATION: deadcode-01 (exported, zero callers anywhere — Info severity)
func UnusedExport() string { return "unused" }

// VIOLATION: deadcode-02 (only called from deadcode_test.go)
func helperForTests() bool { return true }

// COMPLIANT: aliveHelper has a genuine non-test in-workspace caller
// (ComputeSomething, below), so it must produce no finding. ComputeSomething
// itself is exported with zero callers, so it separately produces its own
// deadcode-01/Info finding — that finding is expected and not the assertion
// this fixture exists to make.
func aliveHelper() int { return 1 }

func ComputeSomething() int { return aliveHelper() + 1 }
```

```go
package testing

import "testing"

func TestHelperForTests(t *testing.T) {
	if !helperForTests() {
		t.Fatal("expected true")
	}
}
```

Verification:

- `require.NotEmpty(t, findingsWithRule(result, "deadcode-01"))`; assert
  `neverCalled` is present with `SeverityWarning` and `UnusedExport` is
  present with `SeverityInfo` — the severity split by exported-ness is the
  behavior actually under test, not just presence.
- `require.Len(t, findingsFor(result, "helperForTests"), 1)`; its `Rule` is
  `"deadcode-02"`.
- `require.Empty(t, findingsFor(result, "aliveHelper"))` — this is this
  tool's `_CompliantIsSilent` analog, scoped to the one function the fixture
  designed to be alive. A tree-wide assertion is unsatisfiable under
  one-package-per-rule fixtures; the same issue
  applies here one level down — scope to the specific function, not "no
  finding in the file").
- Assert every `Location.File` is workspace-relative and slash-separated —
  never reduced to `filepath.Base`.

## `go_test_map`

Input: package. Output: test function → production code it exercises (call
graph). Backing: `go test -json` (for test enumeration) + the same
`internal/reach` CHA graph `go_dead_code` uses — do not build a second call
graph.

```go
package tools

type TestMapInput struct {
	Package string `json:"package" jsonschema:"import path or relative directory"`
	// TestNameRegex filters to matching test names; empty means all tests
	// in Package. Anchored the same way `go test -run` anchors.
	TestNameRegex string `json:"test_name_regex,omitempty" jsonschema:"regex filtering to specific test names; default all tests in package"`
}

type ExercisedFunc struct {
	Function string           `json:"function"`
	Location finding.Location `json:"location"`
}

type TestCoverage struct {
	Test      string           `json:"test"`
	Location  finding.Location `json:"location"`
	Exercises []ExercisedFunc  `json:"exercises"` // deduped, sorted by File then Line
}

type TestMapOutput struct {
	Tests []TestCoverage `json:"tests"`
}
```

Handler algorithm:

1. `normalizeTestMapInput(&in)`.
2. `resolveInWorkspace(in.Package)`.
3. `reach.Build(ctx, workspaceRoot)`.
4. From `graph.Functions()`, select test functions: declared in a `_test.go`
   file under `in.Package`'s directory, name matches `^Test` and, if
   `in.TestNameRegex` is set, also matches that regex, signature is
   `func(*testing.T)`.
5. For each selected test `fn`: `exercised := graph.ReachableFrom(fn)`,
   filtered to functions **not** declared in a `_test.go` file (production
   code only — a test calling a shared test helper doesn't count as
   "production code exercised").
6. Map each surviving function to `ExercisedFunc{Function: qualifiedName(fn),
   Location: loc}`, dedupe, sort.
7. Assemble `TestMapOutput`. Deferred `recover()` boundary, same rationale
   as `go_dead_code`.

**Known limitations, documented rather than fixed:**

- **Static over-approximation, not a runtime trace.** `Exercises` lists
  everything statically reachable via CHA's call graph, not everything the
  test actually executed. A test calling one method on an interface value
  will list every concrete implementation of that interface visible in the
  loaded workspace, including ones that specific test never constructs.
  State this in the tool's own description string
  (`"Maps each test to statically-reachable production functions (static
  over-approximation, not a runtime trace)."`) so a caller doesn't over-trust
  the list as measured coverage.
- **Subtests are not distinguished.** A table-driven `TestXxx` with 10
  `t.Run` cases reports one `Exercises` set for the whole outer function —
  CHA sees calls made inside a closure passed to `t.Run` as edges from the
  enclosing `*ssa.Function` regardless of which subtest triggers them at
  runtime. Splitting by subtest needs dynamic instrumentation, out of scope
  for a static tool.

Fixture: none new. Reuses Phase 1's `flaky.go`/`flaky_test.go` and
`panic.go`/`panic_test.go` — both already have a test function calling a
named production function, which is exactly what this tool needs to assert
against.

Verification:

- Run against the `testing` fixture package with no `TestNameRegex`.
- `require.Contains(t, testNames(result), "TestFlaky")`; assert its
  `Exercises` list contains the production function `flaky.go` declares
  (assert by qualified name, not just non-empty length — an empty-but-passing
  assertion here would pass even if `ReachableFrom` returned nothing).
- Assert no entry's `Exercises` list contains a function declared in a
  `_test.go` file — the production-only filter is what's under test.
- Assert every `Location.File` in both `Test.Location` and every
  `ExercisedFunc.Location` is workspace-relative.

## `go_panic_trace`

Input: package, optional test name. Output: structured panic location, call
chain (file:line), goroutine dump. Backing: `go test` panic capture, reusing
`internal/parser/testjson.go`'s NDJSON decode loop (Phase 1) — per that
file's own note, "do not fork a second implementation, both take the same
`testEvent` stream and apply a different reduction on top." That statement
covers the decode *loop*; this tool needs one additive field on the decoder's
result (below), not a second parser.

```go
package tools

type PanicTraceInput struct {
	Package         string `json:"package" jsonschema:"import path or relative directory"`
	Test            string `json:"test,omitempty" jsonschema:"run only this test (anchored, like go test -run); default runs the whole package"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty" jsonschema:"test timeout in seconds; default 60"`
}

type PanicFrame struct {
	Function string           `json:"function"`
	Location finding.Location `json:"location"` // zero value, omitted, if outside the workspace (GOROOT/module cache frames)
}
type PanicTrace struct {
	Test        string       `json:"test"`
	Message     string       `json:"message"`      // e.g. "runtime error: index out of range [5] with length 3"
	Recovered   bool         `json:"recovered"`     // "[recovered]" appeared — a deferred recover() still let it propagate
	GoroutineID int          `json:"goroutine_id"`
	CallChain   []PanicFrame `json:"call_chain"`    // workspace frames only, innermost first
	TraceExcerpt string      `json:"trace_excerpt"` // bounded excerpt with paths outside the workspace redacted
}

type PanicTraceOutput struct {
	Panics   []PanicTrace `json:"panics"` // empty, not nil, if the run produced no panic
	TestsRun int          `json:"tests_run"`
}
```

**Extends `internal/parser/testjson.go` (Phase 1) — additive, non-breaking.**
A panic aborts the test binary mid-run: `test2json` still relays every
output line up to the crash, but no clean terminal pass/fail/skip action
ever arrives for the panicking test, so Phase 1's existing accumulator never
flushes a `TestCase` for it — that per-test output buffer holds exactly the
panic text this tool needs, just under a key nothing currently exposes. Add
one field to the decoder's result type:

```go
// IncompleteTests holds the accumulated "output"-action text, keyed by
// Package+"\x00"+Test, for every test whose stream ended without a
// terminal pass/fail/skip action — i.e. it panicked, or the process was
// killed. Existing callers (go_test_structured, go_flake_finder,
// go_race_report) are unaffected: they only ever read the terminal
// TestCase list, never this field.
IncompleteTests map[string]string
```

No other change to `testjson.go`'s state machine, buffering, or malformed-
line handling.

`internal/parser/panic.go` (new) parses one `IncompleteTests` buffer,
following `race.go`'s established pattern for this file family — concatenate
first, parse the concatenated text afterward, never per-event:

1. Locate the `panic: ` line → `Message` (strip the `panic: ` prefix; if the
   line ends `[recovered]`, set `Recovered = true` and strip that suffix; if
   a `[recovered]` line is immediately followed by a second, more specific
   `panic: ` line — the standard double-panic-line form — take the second
   line's message, since it's the one that actually terminated the process).
2. Locate `goroutine (\d+) \[.+\]:` → `GoroutineID`.
3. Walk alternating (call line, `\s+(\S+\.go):(\d+)` location line) pairs
   into `PanicFrame`s until a `created by ` line or a blank line ends the
   block.
4. `RawTrace` is the full, unmodified buffer. `CallChain` is the same frame
   list filtered to workspace-relative `Location.File` only (GOROOT
   `testing.go`/`runtime/panic.go` frames dropped from the structured chain,
   preserved in `RawTrace`).

Handler algorithm:

1. `normalizePanicTraceInput(&in)` (`TimeoutSeconds` default 60, matching
   `go_test_structured`'s existing default).
2. `resolveInWorkspace(in.Package)`.
3. `exec.CommandContext(ctx, "go", "test", "-json", "-timeout", ..., in.Package)`
   plus `-run=^`+regexp.QuoteMeta(in.Test)+`$` if `in.Test` is set — context
   derived from the inbound handler `ctx`, never `context.Background()`, per
   go-security.md.
4. Decode stdout through `testjson`'s existing loop unchanged.
5. For each key in `result.IncompleteTests`, run `panic.Parse` on the
   buffer. A buffer with no `panic: ` line (e.g. the process was killed by
   the timeout, not a panic) produces no `PanicTrace` entry — not every
   incomplete test is a panic.
6. Assemble `PanicTraceOutput`; `TestsRun` from `testjson`'s existing
   terminal-TestCase count plus `len(result.IncompleteTests)`.
7. Deferred `recover()` boundary around the parse step — adversarial or
   truncated panic text is untrusted subprocess output, exactly the class
   go-security.md's recover-boundary rule targets.

Fixture: none new. Reuses Phase 1's `panic.go`/`panic_test.go` verbatim — it
already produces a genuine `panic: runtime error: index out of range`.

Verification:

- Run against the `testing` fixture package with `Test: "TestPanic"` (exact
  existing Phase 1 test name).
- `require.Len(t, output.Panics, 1)`; assert `Message` contains
  `"index out of range"`; assert `Recovered` is `false` (Phase 1's fixture
  panics without a recovering deferred call — confirm against the actual
  fixture source before asserting `Recovered`'s value, since a fixture using
  `defer recover()` internally would flip this to `true`).
- Assert `CallChain` contains at least one frame whose `Location.File` is
  under `internal/tools/testdata/fixtures/testing/` — the workspace-only
  filter is what's under test, not just "some frames exist."
- Assert `RawTrace` contains a `goroutine` line that `CallChain` itself
  omits from its filtered frames (proves `RawTrace` retains what `CallChain`
  drops, rather than the two fields being redundant copies).
- Run again with no `Test` set, against a package with no panicking test:
  assert `output.Panics` is empty (`[]`, not `nil`) and `TestsRun` matches
  the package's real test count — the "clean run" path must not be mistaken
  for "tool didn't run."
