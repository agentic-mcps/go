# contracts.md — canonical, load once, reference from every phase

Authoritative for: module setup, SDK call shapes, shared types, error/logging
conventions, fixture layout, CI, trace, cache, naming. Every phase document
assumes this file has been read first and does not repeat it.
If a phase file conflicts with this file, this file wins — file an inconsistency
note instead of silently picking one.

Grounded facts (verified against the local official SDK checkout at
`v1.7.0-27-g3d6450f` on 2026-08-23; production pins stable v1.7.0):
- `github.com/modelcontextprotocol/go-sdk` confirmed API: `mcp.NewServer`,
  `mcp.AddTool(server, *mcp.Tool, handlerFunc)`, handler signature
  `func(ctx context.Context, req *mcp.CallToolRequest, in Input) (*mcp.CallToolResult, Output, error)`.
  Returning `nil` for `*mcp.CallToolResult` on success is correct — the SDK
  marshals `Output` into the result's structured content automatically. Do not
  hand-build `CallToolResult{Content: ...}` for normal tool returns; only
  construct it explicitly for protocol features that require one. No v0.1.0
  tool uses elicitation.
- `server.AddResource(*mcp.Resource, mcp.ResourceHandler)`,
  `server.AddResourceTemplate(*mcp.ResourceTemplate, mcp.ResourceHandler)`,
  `server.AddPrompt(*mcp.Prompt, mcp.PromptHandler)` confirmed.
- `mcp.StdioTransport{}` is the only transport through v1. Streamable HTTP
  and legacy SSE exist in the SDK but remain roadmap-only and must not leak
  into released flags, documentation, or tests.
- The SDK still defaults to advertising MCP logging for compatibility, but
  logging is deprecated in protocol `2026-07-28`. Construct the server with
  an explicit empty `ServerCapabilities` value so v0.1.0 does not advertise
  deprecated logging; lifecycle diagnostics remain on stderr.
- Go 1.27 is current stable. The SDK requires Go 1.25, so the module floor is
  1.25 and CI explicitly tests Go 1.25, 1.26, and 1.27.
- Server startup resolves `go` through the MCP process `PATH` and checks it
  with `GOTOOLCHAIN=local` before serving stdio. The selected toolchain must
  compare at or above stable Go 1.25 and satisfy the highest Go version
  required by the active workspace modules. A valid newer development
  toolchain may pass preflight but remains outside the explicit Go 1.25–1.27
  support claim. Missing, malformed, older, or insufficient toolchains fail
  startup with an actionable diagnostic; v0.1 does not download toolchains or
  expose a separate Go-binary flag.
- `golang.org/x/tools/go/analysis/checker` confirmed API (2026-08-22):
  `checker.Analyze(analyzers []*analysis.Analyzer, pkgs []*packages.Package, opts *checker.Options) (*checker.Graph, error)`.
  Requires `pkgs` loaded with the `packages.LoadAllSyntax`-equivalent `Need*`
  bits (see the pass skeleton below). `Graph.Roots` holds one `*Action` per
  analyzer×package; each root `Action` carries `Diagnostics []analysis.Diagnostic`
  and `Duration time.Duration` directly as struct fields — no separate accessor.
  `Action.Result` is populated only when `IsRoot`.

## Verification report boundary

For v0.2, `agentic.verify/v1alpha1` is the durable product contract. The
adapter-independent verification engine assembles one deterministic report from
the change snapshot, affected-package closure, verification plan, executed
evidence, findings, risk facts, and explicit uncertainties. The CLI, composite
GitHub Action, and MCP server transport or render that report; they must not
duplicate policy or ask a caller to reconstruct it from low-level results.
The checked-in [JSON Schema](schema/verification-report-v1alpha1.json) is the
machine-readable authority. Collections are non-null and deterministically
ordered, source locations are workspace-relative, and reports contain no
absolute workspace or run-cache paths. Result precedence is `incomplete`, then
`findings`, then `pass`; `pass` means only that requested checks completed
without policy-blocking evidence. Bounded collections expose an integer total
and boolean truncated field adjacent to the collection. The display caps are
15 changed files, 5 base/current ranges per changed file, 20 changed
declarations, 20 impacted packages, 20 check targets, 20 test package
summaries, 20 nonpassing tests, 50 findings, 5 locations per risk or
uncertainty, and 20 uncovered coverage ranges. Policy evaluates the complete
evidence and findings, and the full impacted closure drives planning and
execution, before display truncation; adapters must expose totals and
truncation and must not infer safety from omitted records.

The v0.2 MCP inventory is eight tools, four resources, and four prompts. The
additive `go_verify_change` tool is the MCP adapter for the report; the seven
v0.1 tool schemas and behavior remain compatible. The tool's
`structuredContent` is authoritative; its text content is a concise status
fallback rather than a duplicate JSON serialization, so coding clients do not
spend their output budget twice. MCP remains stdio-only.

Verification may compile and run trusted repository code with the caller's
privileges. Workspace containment, symlink resolution, cancellation,
deadlines, concurrency limits, and bounded output constrain scope and resource
use, but provide no sandbox or hostile-code isolation. The Action has the same
trust boundary through its runner. SARIF, HTTP, automatic toolchain
installation, and Windows support claims remain deferred.

## v0.3 semantic sidecar foundation

Release archives contain the server, analyzer CLI, and an exact gopls v0.21.0
companion named `agentic-go-gopls`. The managed adapter communicates through a
long-lived stdio LSP session, records capabilities from `initialize`, uses
UTF-16 LSP positions, sends cancellation, bounds frames and stderr, and
disables gopls telemetry. A terminal sidecar failure may replay one explicitly
idempotent read after restart; mutation is never replayed. Agentic-go rejects
an external companion unless it reports the exact pinned version.

The v0.3 public additions are `agentic-go doctor` and `agentic-go mcp-config`.
The config command prints generic JSON, Codex TOML, or Claude JSON and never
edits client settings. These additions do not change the v0.2 MCP inventory.
The semantic sidecar is infrastructure until the v0.4 Context Pack operations
become public; raw LSP and the upstream experimental gopls MCP surface are not
agentic-go contracts.

## v0.4 Context Pack boundary

The v0.4 development surface adds `go_workspace_brief`, `go_search`, and
`go_symbol_context`. The v0.4 milestone inventory is 11 tools, five fixed
resources, one artifact resource template, and four prompts. The three
intelligence tools are read-only, non-destructive, idempotent, and closed-world.
Existing eight tool contracts remain unchanged.

`agentic.context/v1alpha1` is the compact semantic contract. The checked-in
[JSON Schema](schema/context-pack-v1alpha1.json) is authoritative. Every result
records its observed Snapshot Ref, uses workspace-relative locations with
one-based UTF-8 byte columns, keeps collections non-null and deterministic, and
reports omitted external or dynamic relationships as uncertainty. LSP UTF-16
positions exist only inside the pinned-gopls adapter and opaque Symbol Refs.
Stale Snapshot Refs, Symbol Refs, and continuation cursors fail loudly.

Workspace briefs default to 8 KiB, symbol context to 16 KiB, and search to 20
matches with a maximum of 100. Overflow detail is written atomically to the
private user cache as a content-addressed artifact. The
`agentic-go://artifact/{id}` template accepts the opaque cursor returned by a
compact result and returns bounded UTF-8-safe chunks with another cursor until
complete. `agentic-go://capabilities` reports the effective negotiated semantic
manifest without exposing the sidecar path.

The intelligence module owns orchestration and neutral domain types. Git, Go,
gopls, LSP, MCP, subprocess, and cache mechanics remain infrastructure
adapters. Raw gopls responses are never public product types.

## v0.5 Change Contract boundary

The v0.5 development surface adds `go_begin_change` and
`go_checkpoint_change`, the fixed
`agentic-go://change-contract/current` resource, and the `understand-change`
and `resume-change` prompts. The live inventory is 13 tools, six fixed
resources, one artifact resource template, and six prompts. Existing tool,
resource, and prompt contracts remain additive and compatible.

`agentic.change/v1alpha1` is the private continuity contract. The checked-in
[JSON Schema](schema/change-contract-v1alpha1.json) is authoritative. A Change
Contract records the caller's opaque goal, local base, package scope, focused
paths, packages and Symbol Refs, optional allowed paths, structural policies,
decisions, unresolved questions, exact Snapshot Ref lineage, and latest
verification reference. Goal, decision, and question prose is retained exactly
for handoff and is never interpreted or semantically enforced.

Normal persistence is atomic private state under
`os.UserCacheDir()/agentic-go/contracts/<repository-id>/<contract-id>.json`.
Directories use 0700 and files use 0600. Contracts contain no source contents,
absolute workspace paths, or telemetry. Resume is supported across fresh
processes running as the same user on the same machine and requires the same
repository identity. `agentic-go contract export --output <path>` is an
explicit CLI-only handoff operation. The destination must be a new contained
workspace-relative file in an existing directory, must not enter Git metadata,
uses 0600, and is never overwritten. Normal contract operations do not modify
the worktree.

Every checkpoint names the contract ID and exact latest snapshot ID. A stale
lineage, invalid input, corrupt state, containment failure, cancellation, or
deadline is an error. A successful checkpoint records affected packages,
focused diagnostics, structural policy observations, uncertainty, decisions,
questions, and the immutable snapshot transition. It does not run tests or
claim that the change is correct. The two Change Contract tools persist private
state but do not edit source. They are non-idempotent, closed-world, and
truthfully annotated as not read-only and not destructive.

Default structural policy is `forbid` outside explicit allowed paths and for
generated-file modification; it is `warn` outside focus, for exported API
shape changes, dependency or workspace changes, cross-module impact, and test
deletion. An affected-package closure above 200 is incomplete rather than
silently narrowed. Exported API checks compare declaration shape, ignore
function and method body-only edits, and report `exported_api_unknown`
uncertainty when available source cannot classify an exported declaration.
That evidence is structural guidance, not a universal Go compatibility proof.

## Module

```
module github.com/ashwingopalsamy/agentic-go
go 1.25
```
CI matrix tests `{1.25, 1.26, 1.27}` — floor stays 1.25 for adoption reach,
CI proves forward-compat. `go.mod` `toolchain` directive: omit (let CI pick).

Dependencies — exactly these three in production code, nothing else, per plan's own
dependency table:
- `github.com/modelcontextprotocol/go-sdk`
- `golang.org/x/tools` (gopls libs + `go/analysis` + `go/analysis/checker`
  + `go/analysis/passes/inspect` + `go/analysis/passes/fieldalignment`)
- `golang.org/x/mod/modfile` for parsing active `go.mod` and `go.work`
  requirements during the offline startup toolchain preflight.

**Fixture-only exception.** A handful of Phase 4 audit-domain fixtures must compile against
the *real* third-party type their predicate resolves via `pass.TypesInfo` — a local mock type
would compile but never trigger the real analyzer (whose predicate checks the real import
path), making the fixture a false regression test. These packages are permitted as
**test-only** `go.mod` requirements, never imported from any `internal/` production file:
`github.com/aws/aws-sdk-go-v2/service/dynamodb` + `github.com/aws/aws-sdk-go-v2/aws`
(security-10 fixture), `go.opentelemetry.io/otel/trace` + `go.opentelemetry.io/otel/attribute`
(security-12 fixture), `github.com/prometheus/client_golang` (observability-07 fixture). CI's
test/cross-compile matrix must stay green with these present — they widen the module's
`go test ./...` dependency graph only; `go build ./cmd/agentic-go` never reaches `testdata/`
and its import graph is unaffected.

## SDK usage — canonical tool shape

Every tool file follows this exact skeleton. `<Name>` is PascalCase tool name
without `go_` (e.g. `TestStructured` for `go_test_structured`).

```go
package tools

import "github.com/ashwingopalsamy/agentic-go/internal/finding"

type <Name>Input struct {
    // jsonschema tag is the ONLY documentation the MCP client sees for this field.
    // Write it as a spec, not a comment: constraints, defaults, units.
    Package string `json:"package" jsonschema:"Go package import path or ./relative/path"`
}

type <Name>Output struct {
    Result finding.AuditResult `json:"result"` // or tool-specific shape — see per-tool spec
}

func <Name>Handler(ctx context.Context, req *mcp.CallToolRequest, in <Name>Input) (*mcp.CallToolResult, <Name>Output, error) {
    if err := normalize<Name>Input(&in); err != nil {
        return nil, <Name>Output{}, fmt.Errorf("validating input: %w", err)
    }
    // ... do the work, deriving ctx deadline from req's ctx, never context.Background() ...
    return nil, <Name>Output{Result: result}, nil
}

func Register<Name>(server *mcp.Server) {
    mcp.AddTool(server, &mcp.Tool{
        Name:        "go_<snake_name>",
        Description: "<one imperative sentence, <120 chars, states input+output shape>",
    }, <Name>Handler)
}
```

`main.go` calls every `Register<Name>(server)` explicitly in one flat list —
no reflection-based auto-discovery. Deterministic, greppable, matches the
"don't define abstraction until 2nd need" bias. New tool = new file + one new
line in the registration list.

Note the `Input` struct carries **no** `,required` in its jsonschema tag —
see "Input schema and defaults" below for why, and note it here
because this skeleton is the one every domain file's §4 copies verbatim.

## Input schema and defaults

`jsonschema` struct tags are **description prose only** — never a validation
mechanism. Do not write `jsonschema:"required"`, `jsonschema:"default:200"`,
or `jsonschema:"enum:error,warning,info"` anywhere in this codebase:
`jsonschema.For[T]` generates the wire schema from the struct shape and tag
*text*, not from parsed sub-tokens inside that text, so a `default:200`
fragment is never applied by the SDK and only misleads a human reading the
source. Two real mechanisms, used instead:

- **Optional vs. required** is the struct tag's own `,omitempty` — present
  on every field with a sane zero-value default, absent only on fields with
  no reasonable default (`Package string` on every audit tool; there is no
  default package to audit).
- **Defaults** are set programmatically, once, at server startup:
  ```go
  schema, _ := jsonschema.For[Audit<Domain>Input]()
  schema.Properties["max_findings"].Default = json.RawMessage(`200`)
  ```
  so the *advertised* schema (what an MCP client's UI shows before the tool
  is even called) matches the *behavioral* default, not just the latter.
- **Belt-and-braces:** every handler's first line is
  `normalize<Name>Input(&in)` (or `validate<Name>Input` when the field needs
  real rejection, not just defaulting — see "Input containment" below). A
  client that ignores the advertised schema default and sends a zero-value
  field still gets the correct default applied inside the handler: the
  schema default documents intent, the normalize call enforces it.

`go_rename_symbol`'s destructive flag is `Apply bool` (`,omitempty`), not
`DryRun bool`. The Go zero value (`false`) must mean "do not touch the
caller's source" — a `DryRun` field means the opposite: an omitted or
zero-value field silently defaults to *writing*, which is exactly backwards
for the one tool in this server capable of mutating a caller's repository.

## Canonical shared types — `internal/finding` (leaf package)

Every Tier-2/3/4 tool's `Output` embeds or returns one of these. A bare
`[]string` or raw text return anywhere is a spec violation — it defeats the
product's structured-output thesis.

`internal/finding` has zero imports from any other `internal/...` package —
enforced by `depguard` in Phase 6's linter config. Both `internal/tools` and
every `internal/analysis/<domain>` subpackage import `internal/finding`;
`internal/finding` imports neither. This is the fix for the import cycle a
naive split creates: put these types in `internal/tools` (as tool-output
shapes) while `internal/analysis` also needs them (as pass-output shapes),
and `analysis` is forced to import `tools` — but `tools` already imports
`analysis`/`audit` to invoke passes. A shared leaf package has no such
problem, by construction, not by discipline.

```go
package finding

type Location struct {
    File string `json:"file"` // always slash-separated, workspace-relative — see rule below
    Line int    `json:"line"`
    Col  int    `json:"col,omitempty"`
}

type Severity string

const (
    SeverityError   Severity = "error"
    SeverityWarning Severity = "warning"
    SeverityInfo    Severity = "info"
)

// Rule is "<domain>-<NN>", e.g. "concurrency-01" — matches
// phase-4a-index.md item 1 verbatim and every fixture's
// `// VIOLATION: <rule>` comment. Stable across releases — AI callers and CI
// dashboards key off this string, never off Message text.
type Finding struct {
    Rule       string   `json:"rule"`
    RuleName   string   `json:"rule_name,omitempty"` // human slug, e.g. "fire_and_forget_goroutine" — descriptive only, never parsed by callers
    Severity   Severity `json:"severity"`
    Location   Location `json:"location"`
    Message    string   `json:"message"`              // noun-phrase, lowercase start, no trailing period, no "failed to"
    Suggestion string   `json:"suggestion,omitempty"`  // imperative one-liner, e.g. "wrap in sync.WaitGroup or pass ctx"
}

type AuditResult struct {
    Findings         []Finding        `json:"findings"`
    Total            int              `json:"total"`              // len(Findings) BEFORE truncation — never recomputed after clamping
    Truncated        bool             `json:"truncated"`          // true iff MaxFindings clamped Findings below Total
    CountsBySeverity map[Severity]int `json:"counts_by_severity"` // computed BEFORE truncation, same reason
    FilesScanned     int              `json:"files_scanned"`
    DurationMS       int64            `json:"duration_ms"`
}

func ValidateSeverity(value Severity) error
func Filter(result AuditResult, min Severity, max int) AuditResult
```

`AuditResult.Findings` is never nil on success — always `[]Finding{}` for a
clean scan. A nil slice serializes to JSON `null`, and "null findings" reads
as "audit didn't run" to a caller, not "zero findings." This is a fail-loud
requirement from the user's own error-handling doctrine. `Total` and
`CountsBySeverity` follow the identical rule: a truncated response reading
`Total: 340, Truncated: true, len(Findings): 200` must never let a caller
believe only 200 problems exist — computing these fields from the
already-truncated slice would silently reintroduce exactly that failure mode.
`Filter` applies `MinSeverity` first, then computes `Total` and
`CountsBySeverity`, and only then clamps the visible `Findings` slice to
`MaxFindings`.

### `Location.File` rule

`Location.File` is always relative to the workspace root passed into the
tool, slash-separated regardless of host OS (`filepath.ToSlash`). A finding
whose position resolves outside the workspace root (a vendored dependency, a
GOROOT file pulled in transitively by `packages.Load`) is **dropped, not
reported** with an absolute path — the audit's contract is "problems in the
package you asked about," and an absolute-path finding is either noise or,
worse, a workspace-root path leaking into a report a caller might paste
elsewhere. Same posture for `go_generics_map`/`go_module_risk` when they walk
dependency source: filter to the workspace, never report on a GOPATH/module-
cache path.

## Error handling contract

- Handler returns a non-nil `error` **only** for tool-execution failure:
  invalid input, subprocess spawn failure, or context deadline exceeded.
  Deferred integrations add their own specific execution failures when they
  ship. A clean scan with zero findings is `nil` error +
  `Findings: []Finding{}`.
- Wrap with noun-phrase context, most-specific innermost:
  `fmt.Errorf("running go test -json for %s: %w", pkg, err)`. Never
  `"failed to run..."`.
- Never log AND return the same error. Tool handlers have no ambient logger —
  the only log sink is the trace writer, gated behind `AGENTIC_GO_TRACE`. If a
  handler needs to explain a failure, that explanation IS the returned error's
  message; do not also `slog.Error` it.
- Every outbound subprocess call (`os/exec`) derives its context from `ctx`
  (the handler's inbound `context.Context`, itself derived from `req`). Never
  `context.Background()` inside a handler — this is the exact violation
  go-security.md's parsing-safety rule calls out, and it's how a client
  disconnect leaks an orphaned `go test` process.
- Any function iterating AST nodes, bitmaps-equivalent (here: `go/analysis`
  passes), or subprocess output parsing MUST have a deferred recover boundary
  at the tool-handler level (not deeper) that converts a panic into a returned
  error with the panic value in the message, never a struct that could carry
  raw secrets or other sensitive payloads (none flow through this server, but
  the recover-boundary discipline transfers).
- **Carve-out, server-lifecycle only:** `main.go` installs one `slog.Logger`
  writing to stderr, used exclusively for process-lifecycle events —
  startup, listener bind, shutdown signal, fatal preflight failure before
  any tool handler exists. This does not violate "never log and return the
  same error" above: lifecycle logging happens where there is no error to
  return (nothing is listening for a return value from `main`), and every
  tool handler's own contract is unchanged — still returns errors, still
  never logs the ones it returns. Lifecycle diagnostics remain stderr-only;
  v0.1.0 explicitly does not advertise the deprecated MCP logging capability.

## go/analysis pass skeleton — canonical, all Phase 4 passes use this

Driver: `golang.org/x/tools/go/analysis/checker` — the only option, no ladder.
It replaces the CLI-only `singlechecker` (calls `os.Exit`, unusable inside a
long-lived MCP server process) and a hand-rolled `packages.Load` + manual
`pass.Run` loop (which can silently drop `TypesSizes`, get `Fset` construction
wrong, and reinvent per-action timing; `performance-02` requires non-nil
type-size information).
Every domain's analysis subpackage imports `golang.org/x/tools/go/analysis`
directly, never a stdlib `go/analysis` — that stdlib package does not exist.

### Analysis subpackage — one per domain, `internal/analysis/<domain>/`

```go
package <domain> // e.g. package concurrency — never package analysis (see Naming below for why)

import (
    "go/ast"

    "golang.org/x/tools/go/analysis"
    "golang.org/x/tools/go/analysis/passes/inspect"
    "golang.org/x/tools/go/ast/inspector"

    "github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
    "github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func init() {
    astutil.RegisterRule("<domain>-01", "<rule_one_slug>", finding.SeverityWarning)
    // one RegisterRule call per rule in this domain, matching §2's table exactly —
    // Report (below) panics at test/analyzer-init time on an unregistered rule ID,
    // by design: a typo'd rule ID must fail loud in `go test ./...`, never ship
    // silently as a Finding with an empty Severity.
}

var Analyzer = &analysis.Analyzer{
    Name:     "<domain>",
    Doc:      "<one line>",
    Run:      run,
    Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
    insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
    insp.Preorder([]ast.Node{ /* node types this pass cares about */ }, func(n ast.Node) {
        // per-rule detection predicate; on match:
        astutil.Report(pass, n.Pos(), "<domain>-NN", "<message template %s>", arg)
    })
    return nil, nil // findings are collected via pass.Report -> Action.Diagnostics, not the return value
}
```

`Analyzer` is the **only** exported symbol from a domain subpackage. No
domain declares its own `Run<Domain>` / `run<Domain>Pass` / `mustRun<Domain>`
wrapper, entry point, or driver — see Orchestration below.

### Orchestration — `internal/audit/` (package `audit`), one call site for every domain

```go
package audit

import (
    "context"
    "fmt"

    "golang.org/x/tools/go/analysis"
    "golang.org/x/tools/go/analysis/checker"
    "golang.org/x/tools/go/packages"

    "github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func Run(ctx context.Context, ws, pattern string, analyzers []*analysis.Analyzer) (result finding.AuditResult, err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("analyzing %s: panic in analyzer predicate: %v", pattern, r)
        }
    }()
    cfg := &packages.Config{
		Context:    ctx,
		Dir:        ws,
		Env:        closedWorldEnv(os.Environ()), // GOPROXY=off, GOSUMDB=off, GOTOOLCHAIN=local
		BuildFlags: []string{"-mod=readonly"},
        Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
            packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
            packages.NeedTypesSizes | packages.NeedTypesInfo | packages.NeedSyntax,
        Tests: true,
    }
    pkgs, err := packages.Load(cfg, pattern)
    if err != nil {
        return finding.AuditResult{}, fmt.Errorf("loading packages for %s: %w", pattern, err)
    }
    pkgs = dedupeTestVariants(pkgs) // see below
    // Sequential keeps analyzer predicates on this goroutine so the recovery
    // boundary above can actually intercept a panic. With the default parallel
    // driver, a predicate panic occurs in a worker goroutine and crashes the
    // server before Run's defer can observe it.
    graph, err := checker.Analyze(analyzers, pkgs, &checker.Options{Sequential: true})
    if err != nil {
        return finding.AuditResult{}, fmt.Errorf("running analysis for %s: %w", pattern, err)
    }
    return collect(graph, ws, pkgs), nil
}
```

Audit package loading is operationally closed-world, not merely annotated
that way: it may use the configured workspace, standard library, and the
existing local module cache, but it never downloads a module, checksum entry,
or Go toolchain. A missing local dependency fails the audit with a clear load
error.

The deferred `recover()` above is this project's analog of `go-security.md`'s
"every function that touches bitmap iteration, binary decoding, or field
extraction must have a recover boundary" rule. There, the risk is a nil deref
during binary field extraction on untrusted input; here, it's a nil deref
inside any one of the 7 domains' AST predicates walking a target repo's
(externally-controlled, potentially malformed) syntax tree during
`checker.Analyze`. `Run` is the single call site for all 7 analyzers (see
above), so one recover here — converting a panic into a returned error rather
than crashing the MCP server process — covers every domain, not one per
handler. A per-handler recover would be redundant and, worse, would recover at
a point after `checker.Analyze` already corrupted its shared `graph` state
across the other 6 analyzers in the same call; recovering at the top of `Run`
discards the whole `Run` invocation cleanly instead.

`Mode` above is the explicit `Need*` set equivalent to the deprecated
`packages.LoadAllSyntax` constant that `checker.Analyze`'s own doc requires —
spelled out field-by-field so nothing is silently missing. `NeedTypesSizes`
is mandatory here for **every** domain, not just performance's — its absence
is what panics `performance-02` (`types.Sizes.Sizeof` on a nil `Sizes`), and
a mode correct for 6 domains and wrong for 1 is exactly the drift this pass
exists to kill. `Tests: true` is mandatory for all 7 — some domains' rules
only fire inside `_test.go` files (e.g. flaky-test and table-test patterns
under Tier-2), and a mode that only sometimes sees test files is a silent
no-op for whichever rules rely on it.

`dedupeTestVariants` — `Tests: true` makes `packages.Load` return both a
package and its `[pkg.test]` synthetic test-binary variant; the two share
every non-`_test.go` file. Running the same analyzer on both doubles every
finding located in a non-test file. Rule, applied once here rather than
copy-pasted into 7 domain files: keep the `[pkg.test]` variant's findings
**only** for positions inside a `_test.go` file; keep the base variant's
findings for everything else; if a domain has no test-file-only rules, the
`[pkg.test]` variant contributes nothing and is dropped entirely.

`collect(graph, ws, pkgs)` — for each `*checker.Action` in `graph.Roots`
(each is one analyzer × one package), reads `.Diagnostics`, resolves each
`analysis.Diagnostic.Pos` to a `finding.Location` via that action's
package's `Fset` (relative to `ws`, slash-separated, dropped if outside
`ws` — see the `Location.File` rule above), maps `Diagnostic.Category` (the
rule ID passed to `astutil.Report`) through `astutil.RuleSeverity`/
`astutil.RuleName`, and builds the final `finding.AuditResult` — `Total`,
`CountsBySeverity`, and `FilesScanned` (via `astutil.FilesScanned(pkgs)`) are
computed here, before any `MaxFindings` truncation, which the caller (the
tool handler, not `audit.Run`) applies afterward.

### Rule → AST-pattern conversion recipe (used identically by all 7 Phase 4 passes)

For every numbered rule in the source reference `.md` file:
1. Quote the exact rule sentence.
2. Name the minimal AST node type(s) that can even syntactically match
   (`*ast.GoStmt`, `*ast.AssignStmt`, `*ast.StructType`, `*ast.InterfaceType`,
   `*ast.FuncDecl`, `*ast.CallExpr`, ...).
3. State the positive detection predicate as a boolean condition over that
   node (walk children via `ast.Inspect`, resolve types via
   `pass.TypesInfo.TypeOf`/`.Uses`/`.Defs` when the syntactic shape alone is
   ambiguous).
4. State exclusion conditions that prevent false positives. This step is
   mandatory: an under-specified exclusion set produces a noisy audit tool.
5. State the `<domain>-NN` rule ID (registered via `astutil.RegisterRule` in
   the domain's `init()`, never inline at the call site), `Severity`, and
   `Message` template with placeholders for identifier/file:line.
6. Provide one fixture snippet that violates the rule and one that is the
   correct/compliant equivalent (false-positive regression pair), placed per
   the fixture layout below.

## Testdata fixture layout — one isolated package per rule

```
internal/tools/testdata/fixtures/
  audit-concurrency/rule01/  audit-concurrency/rule02/  ...
  audit-errors/rule01/       audit-errors/rule02/       ...
  audit-security/rule01/     audit-security/rule02/     ...
  audit-observability/rule01/  ...
  audit-naming/rule01/       ...
  audit-typedesign/rule01/   ...
  audit-performance/rule01/  ...
  testing/       flaky test, passing test, skipped test, panic test, data race,
                 uncovered error branch, one benchmark
```

One Go package per rule (`rule<NN>`, zero-padded to 2 digits, matching the
rule ID's own `-NN` suffix — `audit-concurrency/rule01` backs `concurrency-01`).
Each package compiles cleanly and contains exactly one violating file and,
where a false-positive risk exists, one compliant counterpart — never a
mixed file with both. This makes `require.Len(t, findings, 1)` true by
construction: the fixture package for a rule cannot accidentally also
trigger a sibling rule, because no sibling rule's trigger pattern is present
in that directory at all. Strictly stronger than one-package-per-domain: that
only bounds which domain a stray finding belongs to; this bounds it to a
single rule.

Every subdirectory still lives under `testdata/`, so `go build ./...` and
`go vet ./...` skip it under Go's wildcard-exclusion convention; audit
self-check tests target each `rule<NN>` path explicitly by string, which
still works — `testdata` exclusion only applies to `...` wildcard expansion,
not to an explicit import/file-path argument.

Path form, canonical, no variation permitted:
`internal/tools/testdata/fixtures/audit-<domain>/rule<NN>/`. (An earlier draft
of this file disagreed with itself: the tree above used `audit-<domain>/`,
the closing section used a bare `<domain>/` with no prefix. `audit-<domain>/`
wins; the closing section below is corrected to match.)

Every fixture file gets a `// VIOLATION: <rule>` comment directly above the
offending line, and every compliant counterpart gets `// COMPLIANT: <rule>`
— `<rule>` is the `<domain>-<NN>` ID, not a slug. This is the only place `//`
comments referencing a rule are acceptable in this codebase; production code
comments still follow the no-narration rule.

## Trace contract — `internal/trace`

Env var: `AGENTIC_GO_TRACE=true`. Checked once at server startup, stored as a bool
on the server wrapper struct — zero-cost (no allocation, no syscall) when unset.

When set: writes JSONL to `os.UserCacheDir()/agentic-go/runs/<run-id>/trace.jsonl`.
`run-id` = UTC server start time formatted `20060102T150405Z` + `-` + PID.
One line per tool call:
```json
{
  "ts": "2026-08-22T10:00:00Z",
  "tool": "go_race_report",
  "args_hash": "sha256:...",
  "duration_ms": 142,
  "packages_load_ms": 38,
  "analysis_ms": 96,
  "findings_by_severity": {"error": 1, "warning": 2, "info": 0},
  "analyzer_durations_ms": {"concurrency": 6},
  "result_summary": "3 findings",
  "error": false,
  "error_kind": ""
}
```
Never log raw `args` — hash them (`sha256` of the JSON-marshaled input) so
traces stay diffable without leaking workspace paths verbatim into a file a
user might paste into a bug report. `result_summary` is the aggregate the
`AuditResult`/tool-specific output would show a human — never the full
payload. `analyzer_durations_ms` is per-analyzer wall time aggregated from
`checker.Graph` actions and is empty for non-audit tools. Traces never store
raw returned errors because subprocess errors can contain absolute workspace
paths or target output. `error_kind` is a bounded category such as
`invalid_input`, `cancelled`, `deadline`, `subprocess`, `analysis`, or
`internal`; `error` is its boolean companion. A trace line with both
`error: true` and a non-empty `result_summary` is a handler bug.

Resource `agentic-go://trace-summary` (added to the inventory below):
aggregates the current run's trace file into per-tool call counts, p50/p99
duration, and error count — read-only, computed on read,
never persisted separately from the JSONL it summarizes.

## Cache contract — `internal/cache`

Generic `TTLCache[K comparable, V any]` (Go 1.18+ generics). Cache **key**:

```
sha256(tool_name, workspace_abs_path, go_version, gopls_version,
       GOOS, GOARCH, build_tags, json.Marshal(args), source_digest)
```

`source_digest` = a sorted hash over `(relPath, size, mtime)` for every
`.go` file `packages.Load` resolves into the target package (and its
same-module deps, for tools that walk deps — `go_module_risk`,
`go_generics_map`). This is the actual correctness fix: `args` alone is not
a valid cache key for anything that reads source — editing a file and
re-running the identical tool call with an unmodified `Package` string must
be a cache **miss**, not a stale hit. `go_version`/`gopls_version`/`GOOS`/
`GOARCH`/`build_tags` are in the key because a diagnostic set is not stable
across any of them, and this server runs as a long-lived process where the
underlying toolchain or target platform can differ between two calls in the
same run (a client proxying calls for two different target platforms, or a
`gopls` restart after a version bump).

TTL is a **memory backstop, not the correctness mechanism** — it exists only
to bound the cache's size and time-based blast radius (a hover result from a
workspace state that no longer exists on disk should not live forever), not
to decide whether a result is still valid. The `source_digest` component of
the key is what decides validity. Default TTL table:

| Tool | TTL |
|---|---|
| `go_diagnostics` | 30s |
| `go_hover` | 60s |
| `go_workspace_symbols` | 300s |
| `go_definition`, `go_references` | 10s |
| every test/analysis tool (Tier 1, 2, 4 fuzz/pprof) | 0 (never cached) |

Override: repeatable flag `-cache-ttl tool=duration`, e.g.
`-cache-ttl go_diagnostics=10s -cache-ttl go_hover=5s`, parsed via
`flag.Func` accumulating into a `map[string]time.Duration` that overlays the
default table. No global single-value override — per-tool only, matches the
table's own granularity.

## Progress reporting

`internal/progress` exposes one helper:
```go
func Report(ctx context.Context, req *mcp.CallToolRequest, pct float64, msg string)
```
No-op if the inbound request carries no MCP progress token — checked once,
cheaply, never a source of latency for a client that didn't ask.

v0.1.0 reports only real milestones: subprocess started/completed for test,
race, coverage, and flake tools; baseline/current completion for benchmark
diff; and package completion for audit tools. Fractions come from the known
number of phases or packages. There are no polling timers, invented elapsed-
time percentages, or progress-only goroutines. Errors from progress
notifications are ignored because progress is advisory and must never turn a
successful tool operation into a failure.

## Tool annotations

Every `mcp.Tool` registration sets all four behavioral hints explicitly.
In SDK v1.7.0, `ReadOnlyHint` and `IdempotentHint` are `bool` fields while
`DestructiveHint` and `OpenWorldHint` are `*bool`; use package-level bool
values or a small `boolPtr` helper for the pointer fields. These values are
client guidance, not a security boundary.

The tools package defines the helper once:

```go
func boolPtr(value bool) *bool { return &value }
```

| Field | Default posture | Named exceptions |
|---|---|---|
| `ReadOnlyHint` | `true` | `false` for test-oriented tools and `go_rename_symbol`; audit tools remain `true` |
| `DestructiveHint` | `false` | `true` for test-oriented tools and `go_rename_symbol` |
| `IdempotentHint` | `true` | `false` for test-oriented tools and `go_rename_symbol` with `Apply: true` |
| `OpenWorldHint` | `false` | `true` for test-oriented tools and `go_module_risk` |

Describe executable tools reassuringly and plainly: they run trusted target
repository code with the server process's privileges and may therefore write
files or access external systems. Do not claim sandboxing, and do not add
alarmist language beyond the concrete boundary.

## Input containment and resource limits

`resolveInWorkspace(pkg string) (string, error)` — `internal/tools/workspace.go`,
package `tools` — is the first filesystem-affecting call in every handler,
after zero-value defaults and scalar validation. Resolves `pkg` (an import
path or `./relative` path) against
the server's configured workspace root, rejects:

- absolute paths outside the workspace root,
- `..` traversal that escapes the workspace root,
- symlinks that resolve outside the workspace root (`filepath.EvalSymlinks`
  after the join, checking the resolved result — not just the unresolved path).

Numeric/count fields are clamped, not merely defaulted — a client-supplied
`10000` must not reach a subprocess or an unbounded slice:

| Field | Ceiling |
|---|---|
| `MaxPackages` (module-risk, generics tools) | 500 |
| `Runs` (flake/fuzz repetitions) | 200 |
| `Count` (benchmark repetitions) | 20 |
| `TimeoutSeconds` / `DurationSeconds` | 300 |
| Other result-count limits | 50 |
| `TopN` (pprof/benchmark ranking size) | 100 |

Each subprocess stream is capped at 8 MiB per stdout/stderr channel. Crossing
the cap cancels the subprocess and returns a tool-execution error that names
the exceeded limit; silently truncating parser input could manufacture a
plausible but incomplete result.

A process-wide counting semaphore — a `chan struct{}` buffered to
`--max-concurrent-loads` (default 4), acquired before and released after
every `packages.Load` call and every subprocess spawn — bounds concurrent
work server-wide. Buffer size >1 here is the semaphore pattern itself, not
unbounded-buffer drift: the capacity IS the concurrency ceiling, and every
acquire site is documented inline
(`// sem: bounded by --max-concurrent-loads, see internal/execution/runner.go`).
No new dependency — a stdlib `chan struct{}` is sufficient, and the
production dependency list (go-sdk + x/tools) stays exactly two.
Audit handlers reserve the same runner semaphore with `Runner.Permit` around
`audit.Run`; this prevents in-process package loading from bypassing the
server-wide ceiling.

Global `--max-tool-seconds` flag (default 300, accepted range 1–300) wraps every handler in a
`context.WithTimeout` derived from the inbound `ctx` — belt-and-braces
against a tool whose own per-call limits (table above) are individually
sane but whose subprocess hangs anyway (a `go test` that deadlocks instead
of timing out on its own).

## Naming and file layout

- One file per tool: `internal/tools/go_<snake_name>.go`. Package `tools`.
- One file per parser: `internal/parser/<format>.go` (`testjson.go`, `race.go`,
  `coverage.go`, `benchmark.go`, `pprof.go`).
- One subpackage per analysis domain: `internal/analysis/<domain>/`
  (`concurrency/`, `errors/`, `security/`, `observability/`, `naming/`,
  `typedesign/`, `performance/`), each exporting exactly one symbol:
  `Analyzer *analysis.Analyzer`. Package name matches the directory
  (`package concurrency`, never `package analysis`).
- Shared AST/report helpers: `internal/analysis/astutil/` (package
  `astutil`) — the single home for every helper any domain needs. See the
  conformance block below for its full exported surface; a domain file
  declaring its own copy of an astutil symbol is a spec violation.
- Orchestration: `internal/audit/` (package `audit`) — `run.go` (`audit.Run`,
  see above) and `registry.go` (`audit.All []*analysis.Analyzer`, one entry
  per domain, consumed by `go_audit_all`). **Not** `internal/analysis/*.go` —
  a package literally named `analysis`, living in a directory that must also
  `import "golang.org/x/tools/go/analysis"`, self-collides on the import
  name. Caught during this pass, not by the original review; recorded here
  so it is never rediscovered as a build error.
- Canonical types: `internal/finding/` (package `finding`), zero internal
  imports (see above).
- `Register<Name>` is the only exported symbol per tool file besides the
  Input/Output types and Handler. Everything else in the file is unexported.

## Conformance block — copy verbatim into every domain file's §4

This is not sample code. Every domain file's §4 (tool file spec) reproduces
this block byte-for-byte, substituting only `<Domain>`/`<domain>`/`<NN>`.
The consolidation sweep catches any domain that drifts from it.

### `astutil` exported surface — declared exactly once, `internal/analysis/astutil/`

```go
package astutil

// Rule registry — populated by each domain's init(), read by Report and by tests.
func RegisterRule(rule, name string, severity finding.Severity)
func RuleSeverity(rule string) finding.Severity // panics if rule was never registered
func RuleName(rule string) string
func RulesInDomain(domain string) []string // sorted; backs the per-domain total-count test

// Reporting — the only path from a domain's run() to a Finding.
func Report(pass *analysis.Pass, pos token.Pos, rule, tmpl string, args ...any)

// AST predicates shared across domains. Two, not one, for "is this a call to
// pkg.Func" — a package-level function call and a method call on a named
// type are different questions, and forcing one helper to answer both is
// exactly what produced 4 divergent single-purpose names in the pre-fix tree.
func IsPkgFunc(pass *analysis.Pass, call *ast.CallExpr, pkgPath, name string) bool
func IsMethodOn(pass *analysis.Pass, call *ast.CallExpr, pkgPath, typeName, methodName string) bool
func ExprStmtCall(n ast.Node) (*ast.CallExpr, bool)
func StringLit(e ast.Expr) (string, bool)
func FuncName(decl *ast.FuncDecl) string // receiver-qualified for methods: "(*T).Method"
func FilesScanned(pkgs []*packages.Package) int
func TypeString(pass *analysis.Pass, e ast.Expr) string // static type of e, or "" if unresolved

// Test-only helpers — internal/analysis/astutil/testhelpers.go, same package.
func RunFixture(t *testing.T, analyzer *analysis.Analyzer, fixtureRelPath string) []finding.Finding
func FindingsForRule(findings []finding.Finding, rule string) []finding.Finding
```

No domain file declares `isPkgDotFunc`, `isQualifiedCall`, `isPkgIdent`,
`calleeName`, `unquote`, `runInspectDependency`, `inspectAnalyzer`,
`filesScannedFrom`, `findingsForRule`, `extractFuncName`, `mustRunNaming`,
`runConcurrencyAudit`, `runTypedesign`, `runPerformanceAudit`, `typeString`, or
`resolvedTypeString` — every one
of those names is either the astutil symbol above, the real
`golang.org/x/tools/go/analysis/passes/inspect.Analyzer` (imported directly,
never wrapped), or the single `audit.Run` entry point. Any domain file still
referencing one of these old names has drifted and must be fixed to match
this block, not the other way around.

`normalizeAudit<Domain>Input`/`validateAudit<Domain>Input` (see below) are
**not** in astutil — they are per-tool, declared once in each tool file,
because each domain's `Input` struct has different fields to validate.

### Analysis subpackage and orchestration call site

Reproduced from the pass skeleton above, unchanged — see "Analysis
subpackage" and "Orchestration" there. Every domain's §4 pastes that block
verbatim, substituting `<domain>`.

### Tool file — `internal/tools/go_audit_<domain>.go`

```go
package tools

import (
    "context"
    "fmt"

    "golang.org/x/tools/go/analysis"

    "github.com/ashwingopalsamy/agentic-go/internal/analysis/<domain>"
    "github.com/ashwingopalsamy/agentic-go/internal/audit"
    "github.com/ashwingopalsamy/agentic-go/internal/finding"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Audit<Domain>Input struct {
    Package     string           `json:"package" jsonschema:"Go package import path or ./relative/path"`
    MinSeverity finding.Severity `json:"min_severity,omitempty" jsonschema:"lowest severity to include; default error+warning+info"`
    MaxFindings int              `json:"max_findings,omitempty" jsonschema:"clamp on returned findings; default 200, max 1000"`
}

type Audit<Domain>Output struct {
    Result finding.AuditResult `json:"result"`
}

func Audit<Domain>Handler(ctx context.Context, req *mcp.CallToolRequest, in Audit<Domain>Input) (*mcp.CallToolResult, Audit<Domain>Output, error) {
    if err := normalizeAudit<Domain>Input(&in); err != nil {
        return nil, Audit<Domain>Output{}, fmt.Errorf("validating input: %w", err)
    }
    ws, err := resolveInWorkspace(in.Package)
    if err != nil {
        return nil, Audit<Domain>Output{}, fmt.Errorf("resolving package: %w", err)
    }
    result, err := audit.Run(ctx, ws, in.Package, []*analysis.Analyzer{<domain>.Analyzer})
    if err != nil {
        return nil, Audit<Domain>Output{}, fmt.Errorf("running <domain> audit: %w", err)
    }
	result = finding.Filter(result, in.MinSeverity, in.MaxFindings)
    return nil, Audit<Domain>Output{Result: result}, nil
}

func RegisterAudit<Domain>(server *mcp.Server) {
    mcp.AddTool(server, &mcp.Tool{
        Name:        "go_audit_<domain>",
        Description: "<one imperative sentence>",
        Annotations: &mcp.ToolAnnotations{
            ReadOnlyHint:    true,
            DestructiveHint: boolPtr(false),
            IdempotentHint:  true,
            OpenWorldHint:   boolPtr(false),
        },
    }, Audit<Domain>Handler)
}

func normalizeAudit<Domain>Input(in *Audit<Domain>Input) error {
	if in.Package == "" {
		return fmt.Errorf("package is required")
	}
	if in.MaxFindings < 0 {
		return fmt.Errorf("max_findings must not be negative")
	}
    if in.MaxFindings == 0 {
        in.MaxFindings = 200
    }
    if in.MaxFindings > 1000 {
        in.MaxFindings = 1000
    }
    if in.MinSeverity == "" {
        in.MinSeverity = finding.SeverityInfo
    }
	return finding.ValidateSeverity(in.MinSeverity)
}
```

`resolveInWorkspace` is defined once in the project-wide input-containment
section — never redeclared per domain.
`mcp.ToolAnnotations` fields above are the read-only-audit defaults; a tool
that isn't read-only overrides them explicitly per the Tool annotations
section, never by omission.

### §5 verification — per-domain, per-rule

```go
func TestAudit<Domain>_Rule<NN>(t *testing.T) {
    findings := astutil.RunFixture(t, <domain>.Analyzer, "audit-<domain>/rule<NN>")
    require.Len(t, findings, 1)
    f := findings[0]
    assert.Equal(t, "<domain>-<NN>", f.Rule)
    assert.Equal(t, finding.Severity<X>, f.Severity)
    assert.Equal(t, "fixtures/audit-<domain>/rule<NN>/violation.go", f.Location.File)
    assert.Equal(t, <line>, f.Location.Line)
}

func TestAudit<Domain>_Rule<NN>_CompliantIsSilent(t *testing.T) {
    findings := astutil.RunFixture(t, <domain>.Analyzer, "audit-<domain>/rule<NN>")
    for _, f := range findings {
        assert.NotEqual(t, "<domain>-<NN>", f.Rule, "compliant.go must not trigger its own rule")
    }
}

func TestAudit<Domain>_TotalRuleCount(t *testing.T) {
    assert.Len(t, astutil.RulesInDomain("<domain>"), <N>) // catches a rule silently dropped or added
}
```

`f.Location.File` is asserted as a real workspace-relative path in every
single-rule test above — never `filepath.Base(f.Location.File)`, never
omitted in favor of an "a finding occurred" or "at that line" prose claim.
This is non-negotiable per-rule; the domain-wide count test above is the
only place a bare count assertion is acceptable, because it is checking
"how many rules exist," not "did the right rule fire in the right file."

## Live protocol inventory

Release inventories come from their canonical scope and are asserted against
`internal/tools.RegisterAll`; a roadmap count is never a product goal.
Tagged v0.1.0 has seven tools, four resources, and four prompts. The v0.2.0
target adds `go_verify_change`, v0.4 adds three semantic context tools, and v0.5
adds two continuity tools. The current local development inventory is 13 tools,
six fixed resources, one resource template, and six prompts.

| Tool | Release |
|---|---|
| `go_test_structured` | v0.1.0 |
| `go_race_report` | v0.1.0 |
| `go_coverage_gaps` | v0.1.0 |
| `go_benchmark_diff` | v0.1.0 |
| `go_flake_finder` | v0.1.0 |
| `go_audit_concurrency` | v0.1.0 |
| `go_audit_errors` | v0.1.0 |
| `go_verify_change` | v0.2.0 target |
| `go_workspace_brief` | v0.4 development |
| `go_search` | v0.4 development |
| `go_symbol_context` | v0.4 development |
| `go_begin_change` | v0.5 development |
| `go_checkpoint_change` | v0.5 development |

The live resources are `agentic-go://module`, `agentic-go://packages`,
`agentic-go://analysis-rules`, `agentic-go://trace-summary`,
`agentic-go://capabilities`, and `agentic-go://change-contract/current`. The
`agentic-go://artifact/{id}` resource template reads snapshot-bound overflow
detail. The live prompts are `audit-package`, `pre-commit-check`,
`bisect-flake`, `verify-change`, `understand-change`, and `resume-change`.

The phase documents retain possible navigation, audit, profiling, and creative
tools as design material. None is committed release scope. A future tool ships
only when repeated user pain justifies a public surface and, for analyzers, the
fixture and external-calibration gates are satisfied.

## Repository verification workflow — `.github/workflows/verify.yml`

Matrix: `go-version: [1.25, 1.26, 1.27]` × `os: [ubuntu-latest, macos-latest]`
(covers linux/amd64 + darwin/arm64 directly; darwin/amd64 + linux/arm64 covered
by build-only cross-compilation). Each matrix job builds, runs race-enabled
tests, and vets the repository. Separate jobs run Staticcheck and
golangci-lint, then cross-build both binaries for `darwin/arm64`,
`darwin/amd64`, `linux/amd64`, and `linux/arm64`.

MCP protocol tests use the SDK's in-memory transport and run under step 2;
v0.1.0 has no gopls dependency or separate gopls CI step.

Any red step blocks merge. No `--no-verify`-equivalent skip flags anywhere in
this repo's CI config.

## v0.1.0 release scope — `docs/v0.1.0-release-scope.md`

The canonical v0.1.0 release is exactly 7 tools, 4 resources, 4 prompts, and
the `agentic-go-vet` binary. It is stdio-only; possible tools in phase
documents are not release scope. Deprecated MCP logging capability is disabled:
lifecycle logs go to stderr only. Long-running subprocess tools report MCP
progress when a progress token is supplied.

This file remains the canonical contracts reference for every phase, while
`docs/v0.1.0-release-scope.md` defines what shipped and what remained deferred.

Three additions in the v0.1.0 scope doc are not defined elsewhere in this
contracts file and are called out here as nice additions worth noting:

- **`cmd/agentic-go-vet/` multichecker binary.** Wraps the same
  `concurrency.Analyzer` and `errors.Analyzer` values this file's
  `go/analysis` pass skeleton defines, exposed as a `go vet`-compatible
  CLI via `multichecker.Main`. Widens the audience beyond MCP clients to
  anyone running `go vet`. The `os.Exit` concern that drove the
  `checker.Analyze` (not `singlechecker`) decision above does not apply:
  this is a CLI entry point where `os.Exit` is correct. See the scope doc
  for the full spec, smoke test, and `.goreleaser.yml` entry.

- **`verify-change` prompt.** A lightweight post-edit verification prompt
  (tests + race + 2 audits, no coverage) added to the 3 prompts adapted
  from `phase-3-gopls-navigation-resources-prompts.md`. Registered alongside the other prompts in
  `internal/tools/prompts.go` via the same `server.AddPrompt` pattern. This
  is the high-frequency loop the existing prompt set doesn't cover. See
  the scope doc for the template, registration code, and why-it's-a-prompt
  rationale.

- **False-positive validation step.** Before tagging v0.1.0, run
  `agentic-go-vet` against at least ten prominent OSS Go projects, classify each
  finding (true positive / false positive / style preference), calculate
  the FP rate, and publish it in the README if low. This is a release gate,
  not a CI step. Use at least ten prominent repositories cloned and pinned
  locally by the user. See the scope doc for the full process, target project
  list, CSV format, and `validation/v0.1.0/` directory layout.

Everything else in the v0.1.0 scope doc is a subset of contracts defined
here or in the phase files it references. The scope doc does not redefine
types, error handling, fixture layout, or the pass skeleton — it points
back to this file for each.

## v0.2.0 release scope — `docs/v0.2.0-release-scope.md`

The canonical v0.2.0 target is exactly 8 tools, 4 resources, 4 prompts, and
the `agentic-go-vet` binary. It adds the adapter-independent verification
engine, `agentic-go verify`, the root composite Action, and
`go_verify_change`. Verification runs whole-package checks for the complete
bounded affected closure; selective tests, reachability analysis, SSA/VTA,
`test_regex`, and the former plan-only `go_change_impact` proposal do not ship.
The tagged v0.1.0 scope remains the compatibility baseline.

`docs/v0.2.0-release-scope.md` owns the complete public types, defaults,
evidence model, uncertainty model, containment behavior, and focused release
gate for these additions. Broader roadmap documents do not widen that scope.

## What every phase file assumes you already did

1. Read this file fully.
2. Created the module/dependency skeleton above if not already present.
3. Will use `finding.Finding`/`finding.AuditResult`/`finding.Location` for
   any tool whose output is a set of code findings — never invent a parallel
   shape.
4. Will place fixtures under `internal/tools/testdata/fixtures/audit-<domain>/rule<NN>/`
   as specified above, never a shared mega-fixture and never a bare `<domain>/`
   or `testdata/<domain>/` prefix.
5. Read the target release scope before implementation:
   `docs/v0.2.0-release-scope.md` for v0.2 work and
   `docs/v0.1.0-release-scope.md` for the tagged compatibility baseline. Skip
   cache and HTTP/SSE; progress reporting remains part of the live contract.
