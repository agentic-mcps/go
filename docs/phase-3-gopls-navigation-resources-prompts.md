# Phase 3 — gopls Navigation, Resources, Prompts (roadmap; deferred in v0.1)

Navigation implementation is deferred beyond v0.1. Preserve the sections
below as the roadmap, but do not register or ship these tools in v0.1. The
v0.1 server is stdio-only, has no cache, and exposes exactly 7 tools, 4
resources, and 4 prompts. The v0.1 resources are module, packages,
analysis-rules, and the trace-summary resource defined in `docs/contracts.md`;
the prompts are `audit-package`, `pre-commit-check`, `bisect-flake`, and
`verify-change`.

Read `docs/contracts.md` first.

## Grounded fact (verified 2026-08-22, https://github.com/golang/tools/blob/master/gopls/doc/command-line.md)
The gopls CLI is experimental, not a stable compatibility contract. It exposes
most navigation operations directly
as subcommands, confirmed present: `definition`, `references`, `implementation`,
`rename`, `prepare_rename`, `call_hierarchy`, `check`. Position syntax:
`file.go:line:col` (1-based, matches `docs/contracts.md`'s `Location` struct
exactly, zero translation needed) or `file.go:#byteoffset`. `gopls help` also
lists `mcp` — gopls can itself run as an MCP server; this is the "gopls MCP"
competitor already named in the roadmap's comparison table, not new
information, just confirming that table's accuracy.

**Design decision: prefer gopls CLI subcommands over a hand-rolled LSP client,
per-tool, wherever a subcommand exists.** A hand-rolled JSON-RPC/LSP client
(Content-Length framing, id-multiplexed request/response, initialize
handshake, didOpen/didClose document lifecycle) is real complexity that
duplicates what gopls's own maintainers already test and ship. Shelling out
to a documented CLI subcommand per call is simpler, and more robust to gopls
version drift (the CLI is the maintained public contract; LSP wire nuances
are not something we should reverse-engineer when a subcommand already does
the job). This is the ladder-first choice, not a shortcut — it happens to
also be correct engineering independent of any brevity goal.

**The confirmed gap is `go_hover`: gopls has no `hover` CLI subcommand.
`workspace_symbol` exists in gopls v0.21.0.** Use the workspace-symbol CLI
operation when implementing against the pinned future gopls version. The
gopls-provided MCP server also overlaps the navigation tools planned here;
that overlap is an explicit compatibility and scope consideration.

## Deferred roadmap deliverables
1. `internal/gopls/cli.go` — thin subprocess wrapper, one function per
   confirmed subcommand, shared error handling.
2. `internal/gopls/lspclient.go` — **built only if step-1 discovery finds a
   gap.** Minimal LSP client: stdio JSON-RPC 2.0, `initialize`/`initialized`
   handshake, `didOpen`/`didClose` around each query, request/response only
   (no notification handling needed — `go_diagnostics` uses `gopls check`,
   not LSP push notifications, see below). One exported method per tool that
   needs it (`Hover`, `WorkspaceSymbols`), not a general-purpose LSP library —
   build exactly the two RPCs required, nothing framework-shaped.
3. `internal/tools/go_definition.go`
4. `internal/tools/go_references.go`
5. `internal/tools/go_hover.go`
6. `internal/tools/go_workspace_symbols.go`
7. `internal/tools/go_diagnostics.go`
8. `internal/tools/go_rename_symbol.go`
9. `internal/tools/go_call_hierarchy.go`
10. `internal/tools/resources.go` — future navigation resource registrations.
11. `internal/tools/prompts.go` — future navigation prompt registrations.

## `internal/gopls/cli.go`

```go
package gopls

type Client struct {
    binPath   string
    workspace string // absolute path, passed as cwd for every subprocess
}

func New(binPath, workspace string) *Client

func (c *Client) run(ctx context.Context, args ...string) (stdout string, err error) {
    cmd := exec.CommandContext(ctx, c.binPath, args...)
    cmd.Dir = c.workspace
    var out, errBuf bytes.Buffer
    cmd.Stdout, cmd.Stderr = &out, &errBuf
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("running gopls %s: %w: %s", args[0], err, errBuf.String())
    }
    return out.String(), nil
}
```
Every tool method (`Definition`, `References`, `Implementation`, `Rename`,
`PrepareRename`, `CallHierarchy`, `Check`) is a thin wrapper: build the
`file.go:line:col` position string, call `run`, parse the subcommand's own
text output. **Exact output format per subcommand is not asserted here —
confirm empirically against the pinned future gopls version** (`gopls <subcommand> -h`, then
one real invocation against a known file/symbol in this repo's own source,
inspect the actual bytes) **before writing the parser regex.** This is a
five-minute empirical check per subcommand, safer than transcribing a format
from memory that may have drifted between gopls releases. Record the
confirmed format as a doc comment directly above each parser function once
verified — that comment becomes the ground truth for future gopls upgrades
(if a parser test starts failing after the pinned future gopls version is
updated, the comment is exactly what to re-verify first).

## Tools 1-2: `go_definition`, `go_references`

**Shared input shape** (both take a source position):
```go
type PositionInput struct {
    File string `json:"file" jsonschema:"path relative to workspace root"`
    Line int    `json:"line" jsonschema:"1-based"`
    Col  int    `json:"col" jsonschema:"1-based"`
}
```
**`go_definition` output:**
```go
type DefinitionOutput struct {
    Location tools.Location `json:"location"`
}
```
**`go_references` output:**
```go
type ReferencesOutput struct {
    References []tools.Location `json:"references"` // empty, not nil, if unused (dead code — itself a useful signal, don't error on zero)
}
```
Future cache policy: unspecified; v0.1 has no cache.
Cache key includes `File+Line+Col` — a workspace edit between two calls at the
same position within the TTL window returning stale data is an accepted,
documented tradeoff (10s window, matching the roadmap table, not revisited
here).

## Tool 3: `go_hover`

**Input:** `PositionInput` (reused, same shape).
**Output:**
```go
type HoverOutput struct {
    Signature string `json:"signature"` // e.g. "func Parse(data []byte) (*Message, error)"
    Doc       string `json:"doc,omitempty"`
}
```
If `gopls help` (checked at implementation time, see above) reveals a `hover`
subcommand: use it, parse its output the same empirical-first way as the
other CLI wrappers. If not: implement via `lspclient.go`:
1. `initialize` (rootUri = `file://` + workspace, capabilities = empty object
   is valid — we don't need any optional LSP capability negotiated).
2. `initialized` notification.
3. `didOpen` for the target file (read file content from disk, send as
   `textDocument/didOpen` with full text — required before gopls will answer
   position queries with up-to-date content).
4. `textDocument/hover` request at the given position.
5. `didClose` immediately after (do not keep documents open across calls —
   a long-lived MCP server processing many distinct files must not accumulate
   open-document state indefinitely; open-query-close per call is the
   correct lifecycle for a stateless-per-call tool, at the cost of gopls
   re-parsing the file each time — acceptable for the deferred roadmap.
`hover` LSP result is `{contents: MarkupContent{value: string}}` (markdown) —
split the first code-fenced block as `Signature`, remainder as `Doc`
(gopls's hover markdown convention: signature in a ` ```go ` fence, then a
blank line, then doc comment prose — confirm this shape against one real
response before finalizing the split logic, same empirical-first rule).
Future cache policy: unspecified; v0.1 has no cache.

## Tool 4: `go_workspace_symbols`

**Input:**
```go
type WorkspaceSymbolsInput struct {
    Query string `json:"query" jsonschema:"symbol name or substring to search for"`
}
```
**Output:**
```go
type SymbolMatch struct {
    Name     string          `json:"name"`
    Kind     string          `json:"kind"` // "function","struct","interface","method","const","var","type"
    Location tools.Location  `json:"location"`
}
type WorkspaceSymbolsOutput struct {
    Symbols []SymbolMatch `json:"symbols"` // empty, not nil, on no match
}
```
Use the `workspace_symbol` operation available in gopls v0.21.0. LSP fallback
uses
`workspace/symbol` (a workspace-wide request, no `didOpen` needed — it
queries the server's whole-workspace index directly, this is the one
navigation RPC that genuinely doesn't need a document lifecycle). LSP
`SymbolKind` is a numeric enum (1=File, 5=Class, 6=Method, 12=Function, ...
per the LSP spec) — map to the lowercase string kinds above via a fixed
`map[int]string` table, confirm the exact numeric-to-Go-construct mapping
gopls actually emits (it approximates Go constructs onto LSP's
class-oriented enum — e.g. Go `struct` likely reports as LSP `Class` or
`Struct` depending on gopls version) empirically before finalizing the table.
Future cache policy: unspecified; v0.1 has no cache.

## Tool 5: `go_diagnostics`

**Design decision: does NOT use the LSP push-notification model at all,
even though `textDocument/publishDiagnostics` is the "normal" LSP way to get
diagnostics.** Push notifications require the client to hold a document open
and wait for an async server-initiated message — awkward to map onto a
synchronous request/response MCP tool call, and gopls's own `check`
subcommand already does this synchronously: run it, get diagnostics back
directly, no waiting, no notification-handling code needed anywhere in this
codebase. This sidesteps the single most complex part of the LSP protocol
for a tool that doesn't need it.

**Input:** `{File string}` (whole-file, not position-based — diagnostics are
reported per-file).
**Output:** reuses `tools.AuditResult` from `docs/contracts.md` directly — this
tool's shape is a Finding list like the Phase 4 audit tools, not a novel
type. `Finding.Rule` = the gopls diagnostic's source analyzer name (e.g.
`"unusedparams"`, `"printf"` — confirm the exact `gopls check` output format
includes an analyzer-name field empirically; if it doesn't, set
`Finding.Rule = "gopls"` uniformly rather than inventing a name).
Future cache policy: no cache — v0.1 has no cache and diagnostics-like/analysis
tools in the never-cache bucket; a tool whose entire purpose is "what's wrong
right now" returning 30-second-stale answers is actively misleading.

## Tool 6: `go_rename_symbol`

**Input:**
```go
type RenameSymbolInput struct {
    PositionInput
    NewName string `json:"new_name" jsonschema:"replacement identifier"`
    Apply   bool   `json:"apply,omitempty" jsonschema:"write edits to disk instead of returning a diff; default false"`
}
```
**Output:**
```go
type FileEdit struct {
    File  string `json:"file"`
    Diff  string `json:"diff"` // unified diff text
}
type RenameSymbolOutput struct {
    Edits   []FileEdit `json:"edits"`
    Applied bool       `json:"applied"` // true only when Apply was set AND the write actually happened
}
```
**`Apply` defaults to `false` — the zero value is the safe one, matching
every other bool input in this spec: an omitted field can never mean
"write to the caller's source." This tool writes to the caller's actual
source files on disk only when `Apply == true`; it must never overwrite
uncommitted changes without a clean preflight. Handler behavior:
- `Apply == false` (default, and every call with the field omitted): call
  `gopls rename` in its diff-only mode (no `-w`), return the computed `Edits`
  with `Applied: false`. Never touches disk. This is the only path a caller
  gets without opting in explicitly — no separate "preview" flag needed,
  since the safe default already is a preview.
- `Apply == true`: before ever calling `gopls rename` with `-w`, run
  `git status --porcelain <file>` for every file the rename would touch
  (`gopls rename` without `-w` already reports the full affected-file list,
  so get that list first, THEN decide whether to write) and refuse (handler
  error, not a silent partial apply) if any affected file has uncommitted
  changes. The safety check is not skippable by any input combination other
  than the explicit `Apply: true` itself — there is no separate "force" flag;
  `Apply: true` on a clean worktree is the only path to a real write,
  matching this project's confirmation-hierarchy tier-2 requirement
  translated into tool-level input validation since an MCP tool call has no
  interactive prompt to ask the human mid-call. On success, `Applied: true`.
Not cached (mutating tool, caching would be actively wrong for the same
reason `go_diagnostics` isn't cached, doubly so since this one writes files).

## Tool 7: `go_call_hierarchy`

**Input:** `PositionInput`.
**Output:**
```go
type CallHierarchyNode struct {
    Function string `json:"function"`
    Location tools.Location `json:"location"`
}
type CallHierarchyOutput struct {
    Target  CallHierarchyNode   `json:"target"`
    Callers []CallHierarchyNode `json:"callers"` // empty, not nil
    Callees []CallHierarchyNode `json:"callees"` // empty, not nil
}
```
`gopls call_hierarchy` CLI subcommand is confirmed to exist (grounded fact
above) — use it directly, no LSP fallback needed for this tool. Confirm its
exact output text format empirically (same rule as every other CLI wrapper)
before writing the parser; it likely reports callers and callees as two
labeled sections.
Not cached — call graphs shift with every edit to a hot-path file in this
project's own processing pipeline, a stale call hierarchy
during active refactoring is worse than a redundant gopls invocation.

## Resources (`internal/tools/resources.go`)

The four v0.1 resources are computed fresh on every read (no caching layer — resource reads
are already infrequent relative to tool calls, and staleness in a "what does
my environment look like right now" resource is a correctness bug, not a
performance one).

1. **`agentic-go://module`** — runs `go mod edit -json` in the resolved
   workspace and reduces its structured output to the fields below. This uses
   the Go command's own parser, correctly handles block and single-line
   directives, performs no network access, and does not modify `go.mod`.
   Returns JSON:
   `{module: string, go_version: string, requires: []{path, version}}`.
2. **`agentic-go://packages`** — `go list -json ./...`, reduced per package to
   `{import_path, name, go_files: int, test_go_files: int}` (drop the dozens
   of other fields `go list -json` emits — token discipline, callers of this
   resource want a package inventory, not the full build-list schema).
3. **`agentic-go://analysis-rules`** — static, hand-maintained manifest:
   one entry per `Finding.Rule` value from the concurrency and errors audit
   tools, `{rule: string, domain: string, severity: string, source_doc: string}`
   where `source_doc` names the golang skill reference file the rule came
   from (e.g. `"concurrency.md#L42"` — exact rule slugs and line refs are
   defined by the Phase 4 domain files, this resource is assembled last,
   after Phase 4 is written, by literally listing every `Finding.Rule` slug
   those files define — do not hand-invent rule names here that don't match
   Phase 4's actual output).
4. **`agentic-go://trace-summary`** — summarizes recent trace runs from
   `os.UserCacheDir()/agentic-go/runs`, with bounded output and no raw target
   arguments or source contents.

Deferred roadmap resources, not registered in v0.1.0:

5. **`agentic-go://config`** — effective server configuration after roadmap
   cache and navigation configuration exists.
6. **`agentic-go://cache-stats`** — bounded cache counters after a cache is
   implemented. It must not exist before then.

## Prompts (`internal/tools/prompts.go`)

Each prompt: `server.AddPrompt(&mcp.Prompt{Name, Description, Arguments}, handler)`
where handler returns one user-role `PromptMessage` built via `text/template`
(stdlib, no new dependency) substituting the declared arguments. The v0.1
prompts are the first four below; the roadmap adds two more, for six total.
Each names the exact tool-call sequence a calling agent should follow — these
are workflow shortcuts over tools already specified above, not new logic:

1. **`audit-package`** — args `{package}`. Template: call exactly two audit
   tools against `{{.package}}`: `go_audit_concurrency` and
   `go_audit_errors`. Merge their findings explicitly into one result, combine
   their severity counts, and list every error-severity finding's `Location`
   and `Message` verbatim.
2. **`pre-commit-check`** — args `{package, coverage_threshold}`. Template:
   run `go_test_structured`, `go_race_report`, `go_coverage_gaps` against
   `{{.package}}`; fail (state explicitly, do not soften) if any test
   failed, any race conflict found, or `OverallPercent < {{.coverage_threshold}}`.
3. **`bisect-flake`** — args `{package, runs}`. Template: run
   `go_flake_finder` with `runs={{.runs}}`; for each name in `Flaky`, follow
   up with `go_race_report` on the same package and cross-reference whether
   any reported `RaceConflict.Current.Function` matches the flaky test's
   package — state the correlation explicitly if found, state "no race
   correlation found" explicitly if not (never omit the negative result,
   per this project's own "fail loud" convention — a flake with no detected
   race cause is a real, reportable outcome, not a non-answer).
4. **`verify-change`** — args `{package}`. Template: run the applicable
   v0.1 audit and verification tools for `{{.package}}`, report each result,
   and state explicitly whether the change is ready for review; do not imply
   that deferred navigation tools were run.
5. **`benchmark-regression-gate`** — args `{package, baseline, threshold_percent}`.
   Template: run `go_benchmark_diff` with those args; report `Regressions`
   count and list every `BenchmarkComparison` where `Regression == true`
   with its `DeltaPercent`.
6. **`explain-symbol`** — args `{file, line, col}`. Template: run `go_hover`
   then `go_definition` then `go_references` at that position, synthesize
   one paragraph: what the symbol is (from hover), where it's declared (from
   definition), how many places use it and where (from references' count
   and locations).

## Verification (this phase's own gate)
`internal/gopls/cli_test.go`: one test per confirmed CLI wrapper, run against
this project's own source tree once it exists (self-hosting: `go_definition`
on a symbol in `internal/tools/common.go` finds its own declaration — this is
a real, always-available fixture, no separate `testdata` needed for
navigation tools since the whole repo is a valid Go workspace).
`internal/tools/go_rename_symbol_test.go`: explicitly verifies the
uncommitted-changes refusal — create a temp git repo fixture with one dirty
file, assert the handler returns an error (not a silent no-op, not a partial
apply) when `Apply: true` is requested against it. This is the one test in
this phase that would fail if the safety check were stubbed out entirely
(the WHY this test exists — `go-testing.md`'s intent rule — is exactly this
safety property, not "does rename work" in the abstract).
