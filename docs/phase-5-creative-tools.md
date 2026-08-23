# Phase 5 — Tier-4 creative tools

> **Release status:** deferred beyond v0.1.0. This remains the executable
> specification for the roadmap implementation.

Read `contracts.md` first. Deferral changes scheduling, not rigor: every
tool below retains the same exact-struct-shape, exact-algorithm, and
verification-gate treatment as Phases 1-4 when implementation begins.

## Deliverables
1. `internal/tools/go_fuzz_orchestrate.go`
2. `internal/tools/go_build_matrix.go`
3. `internal/tools/go_module_risk.go`
4. `internal/tools/go_generics_map.go` — instantiation tracking via
   `go/types.Info.Instances`, not structural clustering (Tool 4 below).
5. `internal/tools/go_generics_candidates.go` — structural near-duplicate
   clustering; this is what an earlier draft of this file called
   `go_generics_map` before this pass split the two concerns apart (Tool 5
   below).
6. `internal/tools/go_pprof_analyze.go`
7. `internal/tools/go_field_alignment.go`
8. `internal/tools/testdata/fixtures/tier4/` fixture additions (see per-tool).

## Tool 1: `go_fuzz_orchestrate`

**Input:**
```go
type FuzzOrchestrateInput struct {
    Package         string `json:"package" jsonschema:"Go package import path or ./relative/path"`
    FuzzFunc        string `json:"fuzz_func" jsonschema:"target fuzz function name, e.g. FuzzParseMessage"`
    DurationSeconds int    `json:"duration_seconds,omitempty" jsonschema:"fuzz run duration in seconds; default 30"`
}
```
**Output:**
```go
type FuzzCrasher struct {
    CorpusFile string         `json:"corpus_file"` // path under testdata/fuzz/<FuzzFunc>/
    Panic      string         `json:"panic"`       // first line of the panic/failure message
    Location   tools.Location `json:"location"`
}
type FuzzOrchestrateOutput struct {
    Crashers         []FuzzCrasher `json:"crashers"` // empty, not nil
    DurationS        float64       `json:"duration_s"`
    CorpusEntriesAdded int         `json:"corpus_entries_added"`
}
```
**Algorithm:** run `go test -run=^$ -fuzz=<FuzzFunc> -fuzztime=<N>s <Package>`
(`-run=^$` skips ordinary tests, matches nothing, so only the fuzz target
executes — required, otherwise every non-fuzz test in the package also runs
first). Capture combined stdout+stderr. On a crash, gopls's own fuzzer prints
a line matching `Failing input written to (testdata/fuzz/\S+)` followed by
the failure output (typically a panic trace or a `--- FAIL:` block) —
**confirm this exact line format empirically** (run one real fuzz target
against a deliberately-crashing fixture function, inspect actual output)
before finalizing the regex, per this project's established empirical-first
rule for any subprocess text format not already nailed down by a Go spec.
`CorpusEntriesAdded`: count files under `testdata/fuzz/<FuzzFunc>/` before
and after the run, report the delta (`os.ReadDir`, no parsing needed — this
is a filesystem fact, not a text-parsing one). If zero crashers and the
process exits 0, `Crashers = []FuzzCrasher{}` (fail-loud: empty slice, never
nil, matches every other tool's list-output convention).
**Not cached** — a fuzz run's entire purpose is fresh exploration; caching
"no crashers found" for even 10 seconds actively hides a crasher found one
second after a cached response was served.
**Timeout discipline:** `context.WithTimeout(ctx, time.Duration(DurationSeconds+10)*time.Second)`
wrapping the subprocess — the `+10` is slack for process startup/shutdown
overhead beyond the fuzzer's own `-fuzztime` budget, not a silent scope
change to the requested duration.

## Tool 2: `go_build_matrix`

**Input:**
```go
type BuildMatrixInput struct {
    Package string   `json:"package" jsonschema:"required"`
    Targets []string `json:"targets" jsonschema:"default from CI matrix: linux/amd64,darwin/arm64,linux/arm64"`
}
```
**Output:**
```go
type BuildResult struct {
    GOOS      string `json:"goos"`
    GOARCH    string `json:"goarch"`
    Success   bool   `json:"success"`
    SizeBytes int64  `json:"size_bytes,omitempty"` // omitted when Success=false
    Error     string `json:"error,omitempty"`      // omitted when Success=true
}
type BuildMatrixOutput struct {
    Results []BuildResult `json:"results"` // one entry per target, always — never skip a failed target
}
```
**Algorithm:** for each target, run `go build -o <tmpfile> <Package>` with
`GOOS`/`GOARCH` set via `cmd.Env` (append to `os.Environ()`, don't replace
it wholesale — a replaced env drops `GOPATH`/`GOCACHE`/`GOMODCACHE` and
breaks the build for reasons unrelated to cross-compilation). Sequential,
not parallel goroutines per target — cross-compiles share `GOCACHE` and
concurrent writes to the build cache from multiple `go build` invocations
racing on the same module are a real, documented class of flakiness; this
tool's own runtime is bounded by target count × build time, acceptable for
a Tier-4 tool with no sub-second SLA. On failure, `Error` = stderr, truncated
to the first 500 bytes (a full Go compile error dump is not what a caller
needs — the first error line(s) are). On success, `SizeBytes` via
`os.Stat(tmpfile).Size()`, then `os.Remove(tmpfile)` (defer, unconditional —
never leave built binaries in `/tmp`).
**Not cached** — a build matrix result changes with every source edit;
this tool's own execution cost (several `go build` invocations) already
dwarfs anything a cache would save, and a stale "build succeeded" is exactly
the failure mode a build-checking tool must never produce.

## Tool 3: `go_module_risk`

**Input:** `{Package string}` — note this reads the whole module's `go.mod`,
`Package` here selects which module (in a multi-module workspace) to
analyze, defaulting to the module containing that import path.
**Output:**
```go
type ModuleRiskFinding struct {
    Module   string `json:"module"`
    Version  string `json:"version"`
    Risk     string `json:"risk"`     // rule ID, see below
    Detail   string `json:"detail"`
}
type ModuleRiskOutput struct {
    Findings []ModuleRiskFinding `json:"findings"` // empty, not nil
}
```
**Risk rules** (each a distinct, mechanically-detectable signal — no
subjective "is this maintainer trustworthy" heuristics, which would require
data this tool has no access to and shouldn't guess at):
- `module-risk-pseudo-version`: dependency version matches Go's
  pseudo-version format (`v0.0.0-<timestamp>-<commit>`, detect via regex
  `^v0\.0\.0-\d{14}-[0-9a-f]{12}$`) instead of a tagged semver release —
  means the dependency has never cut an actual release, a real
  supply-chain and stability signal. Detected via `go list -m -json all`
  parsed for the `Version` field per module.
- `module-risk-major-zero`: dependency major version is `v0` (regex
  `^v0\.`) — Go modules make no compatibility promise below v1, a `v0`
  dependency can break on any minor bump.
- `module-risk-update-available`: `go list -m -u -json all` reports a
  non-empty `Update` field — a newer version exists upstream. This is the
  one signal in this tool requiring a module-proxy network round trip
  (`go list -u` checks the configured `GOPROXY`) — document this plainly in
  the tool's description string surfaced via MCP (`mcp.AddTool`'s
  `Description` field) so a calling agent knows this specific tool makes a
  network call, unlike every other tool in this spec which is fully local.
  This is a read-only GET against the module proxy (the same one `go build`
  itself already trusts), not a side-effecting call — does not trigger this
  project's own production-hard-stop tier, but the transparency disclosure
  stands regardless.
- `module-risk-indirect-only-no-go-sum-entry`: an indirect dependency (`//
  indirect` comment in `go.mod`, or absent from the direct-require block)
  with no corresponding `go.sum` hash line — a real integrity gap (Go
  normally guarantees every resolved module has a sum entry; a missing one
  after `go mod verify` would itself fail, so this rule is really a
  cross-check that `go.sum` and `go.mod` agree, catching a manually-edited
  or corrupted `go.sum`).
- `module-risk-local-replace`: a `replace` directive in `go.mod` whose
  right-hand side is a filesystem path (`./`, `../`, or an absolute path)
  rather than a module path with a version. Detect via
  `golang.org/x/mod/modfile.Parse` (already a transitive dependency of
  `x/tools`, no new import) and check each `Replace.New.Version == ""` — a
  real module-to-module replace always carries a version; a filesystem
  replace never does, which is exactly how `modfile` itself distinguishes
  the two cases, so this needs no path-string heuristics of its own. Purely
  static (parses `go.mod` only, no `go list`), so it runs alongside the
  other three `go.mod`-only rules, not the network-touching update-check.
  Risk: a local replace pins the build to a path that exists only on the
  author's machine, silently breaking the build for every other clone and
  CI runner, and can mask a deliberate vendored security patch that was
  never meant to ship past a local branch.
**Not cached** — dependency risk data (especially the network-touching
update-check) should never be served stale from an in-memory TTL cache when
the whole point is "what's true right now."

## Tool 4: `go_generics_map`

**Input:** `{Package string}`.

**Output:**
```go
type GenericInstantiation struct {
    Location tools.Location `json:"location"`  // instantiation site
    TypeArgs []string       `json:"type_args"` // stringified, declaration order
}
type GenericDeclaration struct {
    Name           string                 `json:"name"`         // bare name, no brackets (e.g. "Map")
    Kind           string                 `json:"kind"`         // "func" or "type"
    DeclLocation   tools.Location         `json:"decl_location"`
    Instantiations []GenericInstantiation `json:"instantiations"` // empty, not nil
}
type GenericsMapOutput struct {
    Declarations []GenericDeclaration `json:"declarations"` // empty, not nil
}
```
**Algorithm (instantiation tracking via `go/types`, not structural
clustering — clustering is `go_generics_candidates`, Tool 5 below; the two
were one tool in an earlier draft of this file and were split apart because
they answer different questions: "how is this generic actually used" vs "do
these non-generic functions look like they should be one generic"):**
1. `packages.Load(Package)` with this project's standard `Need*` bits.
   `NeedTypesInfo` is the one this tool depends on — `go/packages` populates
   `types.Info.Instances` automatically once type-checking runs with that bit
   set; no manual `types.Info{}` construction needed.
2. Walk `pkg.TypesInfo.Instances` (`map[*ast.Ident]types.Instance`, confirmed
   against the `go/types` doc before committing to this spec:
   `Instance{TypeArgs *types.TypeList, Type types.Type}`). Every key is an
   `*ast.Ident` naming a generic function or type at an instantiation site
   (explicit type args or type-inferred).
3. For each `ident`, resolve `pkg.TypesInfo.Uses[ident]` to the underlying
   `types.Object` being instantiated (`*types.Func` for a generic function,
   `*types.TypeName` for a generic type) — this is the *declaration* the
   instantiation site refers to, distinct from the site itself.
4. Group instantiation sites by declaration object identity (map keyed by
   `types.Object`, never by name string — two distinct generic functions
   both named `Map` in different packages must never merge into one group).
5. Per group: `Name = obj.Name()`; `Kind = "func"` for `*types.Func`, `"type"`
   for `*types.TypeName`; `DeclLocation` from `pkg.Fset.Position(obj.Pos())`,
   converted to the workspace-relative form `contracts.md`'s
   `Location.File` rule requires. Per instantiation site: `Location` from
   `pkg.Fset.Position(ident.Pos())`; `TypeArgs[i] =
   types.TypeString(instance.TypeArgs.At(i), types.RelativeTo(pkg.Types))`
   for `i` over `instance.TypeArgs.Len()` — `RelativeTo` is what makes `int`
   print as `int` rather than over-qualifying with a package path it doesn't
   need.
6. Sort `Declarations` by `Name`; sort each `Instantiations` slice by
   `Location.File` then `Line` — matches every other list-output tool's
   determinism rule in this spec.

**Explicit ceiling — scope is `Package` only, not the whole module:** this
tool only sees instantiation sites inside `Package`'s own source files.
`packages.Load(Package)` loads `Package`'s dependencies, not its dependents,
so a generic exported from `Package` and instantiated by every package that
imports it shows none of those external sites here. Scanning the whole
module for external instantiation sites is a workspace-wide operation
(comparable in cost to `internal/reach.Build`'s whole-workspace CHA graph,
`phase-4b-tier-2-tools.md`) and is out of scope for v0.1.0 — do not word
this tool's MCP `Description` string to imply whole-module coverage it
doesn't provide. A later version could extend `internal/reach.Graph` to
index instantiation sites while it already walks every loaded package,
rather than giving this tool a second, duplicate whole-workspace load.

Also: a generic declaration with zero instantiations anywhere in `Package`'s
source is invisible to this tool by construction — it can only ever surface
as a value grouped under an instantiation-site key, so a declaration nobody
calls has no key to discover it from. That's `go_dead_code`'s job, not this
tool's: an uninstantiated generic function is just an uncalled function to
CHA, no special-casing required. Do not add a second, parallel
"declared-but-never-instantiated" scan here — that would duplicate
`go_dead_code`'s exclusion-list logic for a case CHA already covers.

**Fixture:** `internal/tools/testdata/fixtures/tier4/geninstances/generic.go`
declaring `func Map[T, U any](xs []T, f func(T) U) []U` and
`type Box[T any] struct { V T }`, plus `usage.go` calling `Map[int,
string](...)` once and `Map[string, int](...)` once (two instantiations of
the same declaration with different type args) and constructing one
`Box[int]{}`. Verification asserts: exactly one `GenericDeclaration` for
`Map` with `len(Instantiations) == 2`, whose two entries' `TypeArgs` are
`["int","string"]` and `["string","int"]` respectively (order follows the
sort rule above, not source call order — a stub returning source-order would
fail this assertion); exactly one `GenericDeclaration` for `Box` with
`len(Instantiations) == 1` and `TypeArgs == ["int"]`.
**Not cached** — matches every other tool in this file.

## Tool 5: `go_generics_candidates`

**Input:**
```go
type GenericsCandidatesInput struct {
    Package       string  `json:"package" jsonschema:"Go package import path or ./relative/path"`
    MinClusterSize int    `json:"min_cluster_size,omitempty" jsonschema:"minimum near-duplicate cluster size to report; default 2"`
}
```
**Output:**
```go
type GenericCandidate struct {
    Functions          []tools.Location `json:"functions"`
    Similarity         float64          `json:"similarity"` // 0.0-1.0
    SuggestedSignature string           `json:"suggested_signature"` // best-effort, see below
}
type GenericsCandidatesOutput struct {
    Candidates []GenericCandidate `json:"candidates"` // empty, not nil
}
```
**Algorithm (structural near-duplicate detection, not semantic
understanding — be explicit about this ceiling, don't oversell it):**
1. Parse every function declaration in `Package` via `go/parser`.
2. For each function body, produce a **normalized AST fingerprint**: walk
   the body with `ast.Inspect`, emit a token stream of node *kinds* only
   (e.g. `IfStmt`, `BinaryExpr`, `CallExpr`, `Ident`) — **erase all
   identifier names and all concrete type names**, replacing each with a
   positional placeholder (`IDENT_1`, `IDENT_2`, ..., reset per function) so
   that two functions differing only in variable names or the one type they
   operate over produce identical or near-identical fingerprints.
3. Hash each fingerprint stream (e.g. `fnv.New64a` over the joined token
   list — a fast non-cryptographic hash is correct here, this is similarity
   bucketing, not security).
4. Group functions by exact fingerprint hash first (pass 1: exact
   structural matches only, `Similarity = 1.0`). This alone catches the
   canonical case this tool exists for: `func SumInts(xs []int) int` and
   `func SumFloats(xs []float64) float64` with identical bodies differing
   only in the element type — a textbook generic-consolidation candidate.
5. **Explicit ceiling, do not attempt in v0.1.0:** near-miss (non-exact)
   structural similarity scoring (e.g. edit distance between fingerprint
   token streams for functions that are *almost* identical) is real
   complexity with real false-positive risk (unrelated functions that
   happen to share control-flow shape) — ship exact-fingerprint clustering
   only (`Similarity` is therefore always `0.0` or `1.0` in this version,
   document this plainly rather than implying a continuous score that
   doesn't exist). If the exact-match approach produces too few or too many
   candidates against real code, that's a v0.2.0 tuning decision, not a
   v0.1.0 blocker.
6. `SuggestedSignature`: best-effort string built from the first function
   in the cluster with its concrete type replaced by a fresh type parameter
   name (`T any`) — e.g. `func Sum[T int | float64](xs []T) T`. Constraint
   inference is naive: union the concrete types actually observed across
   the cluster's functions (via `go/types` on each function's parameter/
   return types) into a type-set constraint; if the union would require an
   interface method set instead of a type-set union (i.e. the types share
   no common underlying kind), emit `"T any // manual constraint review needed"`
   rather than fabricating a plausible-looking but wrong constraint.
Only report clusters with `len(Functions) >= MinClusterSize`.
**Fixture:** `internal/tools/testdata/fixtures/tier4/generics/` with
`sumints.go` (`SumInts([]int) int`) and `sumfloats.go` (`SumFloats([]float64) float64`,
structurally identical body) as the true-positive pair, plus
`unrelated.go` (a function with a different control-flow shape) as the
near-miss that must NOT cluster with the pair.
**Not cached.**

## Tool 6: `go_pprof_analyze`

**Input:**
```go
type PprofAnalyzeInput struct {
    Package     string `json:"package" jsonschema:"Go package import path or ./relative/path"`
    BenchRegex  string `json:"bench_regex,omitempty" jsonschema:"regex filter for -bench; default is all benchmarks"`
    ProfileType string `json:"profile_type,omitempty" jsonschema:"one of cpu, mem; default cpu"`
    TopN        int    `json:"top_n,omitempty" jsonschema:"number of top entries to report; default 10"`
}
```
**Output:**
```go
type PprofEntry struct {
    Function    string  `json:"function"`
    FlatPercent float64 `json:"flat_percent"`
    CumPercent  float64 `json:"cum_percent"`
}
type PprofAnalyzeOutput struct {
    TopFunctions []PprofEntry `json:"top_functions"`
}
```
**Algorithm:** create a temp dir (`os.MkdirTemp`, deferred `os.RemoveAll`,
unconditional). Run `go test -bench=<BenchRegex> -benchtime=1x -run=^$
-<ProfileType>profile=<tmpdir>/profile.pprof <Package>` (`-cpuprofile` or
`-memprofile` depending on `ProfileType`; `-benchtime=1x` keeps this tool
fast — one iteration is enough for a hot-function ranking, this is not a
benchmark-accuracy tool, that's `go_benchmark_diff`'s job in Phase 2, don't
duplicate it). Then run `go tool pprof -top -nodecount=<TopN> <binary>
<tmpdir>/profile.pprof` and parse its text table output (columns: flat,
flat%, sum%, cum, cum%, function name — **confirm exact column layout
empirically**, `go tool pprof -top` output has shifted formatting across Go
versions before, same empirical-first rule as every other subprocess-text
parser in this spec). `go tool pprof` requires the compiled test binary path
as well as the profile — capture it via `go test -c -o <tmpdir>/test.bin
<Package>` run once before the profiled bench run, reuse that binary path
for the pprof call.
**Not cached** — profiling reflects current code; the roadmap comparison
table already lists this as a differentiator over static-only competitors,
serving a stale profile would quietly undercut that claim.

## Tool 7: `go_field_alignment`

**Simplest of the seven — this is a thin wrapper, not new analysis logic.**
`golang.org/x/tools/go/analysis/passes/fieldalignment` already exists as a
real, shipped analyzer inside the `golang.org/x/tools` module this project
already depends on per `contracts.md`'s two-dependency floor — **import
and run it directly via `analysis.Analyzer.Run`, do not reimplement struct
field alignment logic from scratch.** This is the correct application of
this project's own ladder-first bias: an already-available dependency
solves the entire problem, adding custom AST work here would be duplicated,
worse-tested logic for something the Go toolchain team already maintains.

**Input:** `{Package string}`.
**Output:** `tools.AuditResult` directly (reuses the Phase 4 shared type —
this tool's shape is identical to every `go_audit_*` tool, it just delegates
to a pre-built analyzer instead of a hand-written one). `Finding.Rule =
"fieldalignment"`, `Finding.Message` = the analyzer's own diagnostic text
verbatim (it already reports the byte-savings-worthy reordering
suggestion), `Finding.Severity = tools.SeverityInfo` (a layout optimization,
never a correctness issue — justify against this project's own severity
convention: this is the one rule in the entire tool catalog that can never
reach `SeverityError` by construction, state that explicitly rather than
leaving severity assignment ambiguous).
**Wiring:** use `singlechecker`-style invocation but driven programmatically
(not as a standalone CLI) — construct the `analysis.Pass` for the target
package's `go/packages.Load` result and call `fieldalignment.Analyzer.Run(pass)`
directly, collecting diagnostics into `Finding`s via the same adapter
pattern `contracts.md`'s canonical pass skeleton already uses for custom
passes (this tool proves that skeleton generalizes to third-party analyzers
too, not just hand-written ones).
**Not cached** — matches every other audit tool in this spec.
**Fixture:** `internal/tools/testdata/fixtures/tier4/fieldalign/misaligned.go`
— one struct with fields deliberately ordered smallest-to-largest (e.g.
`bool`, then `int64`, then `string`) to trigger the analyzer, and
`aligned.go` — the same fields in the analyzer's preferred order, which must
produce zero findings (this is the near-miss case: proves the wrapper
correctly surfaces "no finding" rather than always firing).

## Verification (this phase's own gate)
One test file per tool (`go_fuzz_orchestrate_test.go` through
`go_field_alignment_test.go`), each asserting the WHY, not just the WHAT,
per this project's global testing rule:
- `go_fuzz_orchestrate`: fixture package ships a deliberately-crashing fuzz
  target (a function that panics on a specific byte pattern reachable
  within a short `-fuzztime`); assert `len(Crashers) >= 1` — a stub that
  always returns zero crashers must fail this test.
- `go_build_matrix`: assert `len(Results) == len(Targets)` always (even
  with a deliberately-invalid target, e.g. `GOOS=bogus`, in the input list)
  and that the invalid target's `Success == false` with a non-empty `Error`
  — a stub that silently drops failed targets from the results list must
  fail this test.
- `go_module_risk`: fixture `go.mod` with one deliberately-pinned
  pseudo-version dependency; assert exactly one `module-risk-pseudo-version`
  finding referencing that exact module path. A second fixture `go.mod`
  carries one `replace example.com/foo => ../local/foo` directive alongside
  one ordinary versioned `replace`; assert exactly one
  `module-risk-local-replace` finding, referencing the filesystem-replaced
  module only — the versioned replace must not also fire.
- `go_generics_map`: per the `geninstances` fixture above, assert `Map`'s
  group has exactly 2 `Instantiations` with `TypeArgs` `["int","string"]` and
  `["string","int"]` respectively, and `Box`'s group has exactly 1 with
  `TypeArgs == ["int"]` — a stub that returns the declaration without walking
  `Instances` must fail this test since it can't distinguish one call site
  from two.
- `go_generics_candidates`: per the fixture above, assert the
  `SumInts`/`SumFloats` pair clusters together with `Similarity == 1.0` AND
  that `unrelated.go`'s function does not appear in that cluster.
- `go_pprof_analyze`: fixture benchmark with one deliberately dominant
  hot function (e.g. a tight busy-loop) and one trivially cheap function;
  assert the hot function appears in `TopFunctions` at a higher
  `FlatPercent` than the cheap one — a stub returning a fixed, input-blind
  list must fail this test since it can't reflect which function is
  actually hot.
- `go_field_alignment`: per the fixture above, `misaligned.go` produces
  exactly one finding, `aligned.go` produces zero.
