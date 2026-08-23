# agentic-go: Go Intelligence MCP Server

## Context

Existing Go-oriented MCP surfaces concentrate on navigation or generic command
execution. Agentic-go occupies a narrower boundary: it turns Go test output and
high-precision static analysis into compact, typed evidence that improves an
external coding agent's judgment. It does not embed an LLM, plan work, or become
an agent framework.

The product differentiates on structured race reports, benchmark comparison,
coverage gaps, flake evidence, and custom concurrency/error findings. Any
public comparison must be revalidated against pinned competitor versions before
release; stars, activity claims, and moving feature counts do not belong in the
canonical plan.

Module path: `github.com/ashwingopalsamy/agentic-go`
Go version: 1.25+ (matches official SDK)
SDK: `github.com/modelcontextprotocol/go-sdk` (official, not mark3labs)

**v0.1.0 ships exactly 7 tools, 4 resources, 4 prompts, and the `agentic-go-vet` binary.** The later roadmap reaches 30 tools, 6 resources, and 6 prompts (the five original roadmap prompts plus `verify-change`). The audit tools are the thesis: agentic-go packages narrow concurrency and error-handling rules as structured, AI-consumable findings instead of trying to duplicate gopls navigation.

## Architecture

```
Claude Code / Cursor / Windsurf / any MCP client
        ↓ (MCP over stdio)
agentic-go (single Go binary)
    ├── cmd/agentic-go/      # entry point, server setup, transport selection
    ├── internal/tools/      # MCP tool implementations (one file per tool)
    ├── internal/parser/     # test, race, coverage, and benchmark parsers
    ├── internal/analysis/   # concurrency and error go/analysis passes
    └── internal/trace/      # bounded opt-in JSONL tracing in the user cache
        ↓
Go toolchain: go test and go vet-compatible analysis
```

The later roadmap adds `internal/gopls`, the remaining analysis domains,
additional parsers, and a cache only when their owning milestones ship.

### SDK usage pattern (from official example)

```go
server := mcp.NewServer(
    &mcp.Implementation{Name: "agentic-go", Version: "0.1.0"},
    &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}},
)

type args struct {
    Package string `json:"package" jsonschema:"Go package path to test"`
    Race    bool   `json:"race" jsonschema:"enable race detector"`
}

mcp.AddTool(server, &mcp.Tool{
    Name:        "go_test_structured",
    Description: "Run go test -json and return structured pass/fail/skip per test",
}, func(ctx context.Context, req *mcp.CallToolRequest, args args) (*mcp.CallToolResult, TestResult, error) {
    // ... run go test -json, parse, return structured result
})

server.Run(ctx, &mcp.StdioTransport{})
```

### Transport

Stdio only in v0.1.0 (for Claude Code). HTTP transports are roadmap work. The stdio transport passes through `args` and `env` to subprocesses.

### Deferred gopls integration for navigation tools

After v0.1.0, gopls runs as a bounded subprocess managed by the server. The
`internal/gopls` package uses verified CLI operations where available and a
narrow LSP client only for confirmed gaps. Navigation tools return structured
results and propagate cancellation and deadlines.

This is the minimal navigation surface, not a full LSP passthrough. Only the tools that 90% of Go development needs.

### Tracing

`AGENTIC_GO_TRACE=true` writes bounded JSONL traces beneath
`os.UserCacheDir()/agentic-go/runs/<run-id>/trace.jsonl`. One environment
variable enables it; disabled tracing creates no trace files. Records contain
tool identity, duration, and bounded summaries, never raw source or arguments.

### Deferred per-tool cache

v0.1.0 has no cache. A later navigation milestone may introduce TTL caches;
test and analysis tools remain uncached because their results are run-specific.
The provisional roadmap policy is:

| Tool | TTL | Reason |
|---|---|---|
| `go_diagnostics` | 30s | gopls diagnostics change on file save |
| `go_hover` | 60s | Type info rarely changes |
| `go_workspace_symbols` | 300s | Symbol table is stable |
| `go_definition`, `go_references` | 10s | Changes immediately on edit |
| All test/analysis tools | 0 (never) | Results are run-specific |

## Tool catalog

**30 tools total toward v1.0.0** (the inventory in `docs/contracts.md`) + 6 resources + 6 prompts. v0.1.0 is the first seven tools below; later phases add the remaining inventory. The audit tools are the differentiator: they expose the repository's deliberately narrow analyzer rules as stable structured findings.

### Skill reference coverage map

The rule corpus reproduced in the Phase 4 specifications is operationalized into tools:

| Reference file | Tools that implement its rules |
|---|---|
| `testing.md` | `go_test_structured`, `go_race_report`, `go_coverage_gaps`, `go_benchmark_diff`, `go_flake_finder`, `go_fuzz_orchestrate`, `go_test_map`, `go_panic_trace` |
| `concurrency.md` | `go_audit_concurrency` |
| `error-handling.md` | `go_audit_errors` |
| `security-observability.md` | later audit tools in the contracts inventory |
| `type-design.md` | `go_type_design_audit`, `go_generics_map`, `go_field_alignment` |
| `performance.md` | `go_performance_audit`, `go_field_alignment`, `go_pprof_analyze` |
| `naming.md` | `go_naming_audit` |
| `style.md` | `go_naming_audit` (overlapping), `go_type_design_audit` (overlapping), `go vet`/`staticcheck` via `go_diagnostics` |
| `tooling.md` | `go_module_risk` (govulncheck), CI setup (race detector, staticcheck, goleak), `go_diagnostics` (gopls) |
| `proverbs.md` | Design philosophy, not a tool. It informs the later audit rules: “share memory by communicating” and “bigger interface weaker abstraction”. |

### Tier 1: Test Intelligence (MVP, ship first)

| Tool | Input | Output | Backing tool |
|---|---|---|---|
| `go_test_structured` | package, race flag, verbose flag, timeout | structured: per-test pass/fail/skip, timing, package grouping, failure output | `go test -json` |
| `go_race_report` | package, timeout | structured: goroutine IDs, conflicting addresses, source locations, access types (read/write), conflict scenario as structured data | `go test -race` |
| `go_coverage_gaps` | package, threshold | structured: functions with zero coverage, functions missing error paths, untested branches with file:line | `go test -coverprofile` |
| `go_benchmark_diff` | package, bench name, baseline ref | structured: per-benchmark ns/op, allocs, delta vs baseline, regression flag | `go test -bench` + in-process delta computation |
| `go_flake_finder` | package, test name, iterations, timeout | structured: failure count, failure timing, failure patterns | `go test -count=N` |

### Tier 2: Code Intelligence (differentiators)

| Tool | Input | Output | Backing tool |
|---|---|---|---|
| `go_panic_trace` | package, test name | structured: panic location, call chain with file:line, goroutine dump | `go test` panic capture |
| `go_test_map` | package | structured: test function to production code it exercises (call graph) | `go test -json` + AST analysis |
| `go_audit_concurrency` | package/path | structured concurrency findings | custom `go/analysis` pass |
| `go_audit_errors` | package/path | structured error-handling findings | custom `go/analysis` pass |
| `go_dead_code` | package/path | structured: exported funcs with zero callers, funcs only called from tests | `deadcode` analyzer + workspace call graph |
| `go_security_audit` | package/path | structured: unmasked sensitive structs (no `Stringer`/`LogValue`), `math/rand` used for tokens/nonces, `==` on secrets (not `subtle.ConstantTimeCompare`), custom crypto instead of `crypto/` stdlib, string-concatenated SQL/DynamoDB expressions, secrets in error messages or log fields, missing input validation at boundaries | custom `go/analysis` pass (from `security-observability.md` rules 1-10) |
| `go_observability_audit` | package/path | structured: `fmt.Printf`/`log.Println` instead of `slog`, missing context in log calls (`slog.Info` vs `slog.InfoContext`), high-cardinality metric labels (user ID, request ID, raw error string), liveness probe checking downstream deps, missing Prometheus metrics for RED/USE, whole-struct slog values instead of specific fields | custom `go/analysis` pass (from `security-observability.md` rules 13-20) |
| `go_naming_audit` | package/path | structured: package-name stutter (`parser.NewParser`), `this`/`self` receivers, `Get` prefixes on getters, inconsistent acronym casing (`Xml` vs `XML`), `util`/`helper`/`common` package names, snake_case identifiers, shadowed builtins/stdlib packages, missing `-er` suffix on single-method interfaces | custom `go/analysis` pass (from `naming.md` rules 1-16) |
| `go_type_design_audit` | package/path | structured: interfaces defined where implemented (not where used), interface with single implementation and no test need, embedding in public structs (promotes API irreversibly), zero-value enums (`iota` starting at 0), pointer-to-interface parameters, nil pointer returned as interface (typed-nil trap), missing compile-time interface check (`var _ Iface = (*T)(nil)`), `interface{}`/`any` in business-logic signatures | custom `go/analysis` pass (from `type-design.md` rules 1-20) |
| `go_performance_audit` | package/path | structured: missing preallocation (`make([]T, 0)` with known size), string concatenation in loops (not `strings.Builder`), `fmt.Sprintf` for primitive conversions on hot paths (should use `strconv`), missing `sync.Pool` for reusable allocations, slice operations sharing backing arrays (missing copy/three-index slice), `reflect.DeepEqual` where `cmp.Diff` or type-specific comparison works | custom `go/analysis` pass (from `performance.md` rules) |

### Tier 3: Navigation (bare-minimum, from gopls)

| Tool | Input | Output | Backing tool |
|---|---|---|---|
| `go_definition` | file, line, col | structured: definition file, line, col, symbol name | gopls LSP `textDocument/definition` |
| `go_references` | file, line, col | structured: list of reference locations (file, line, col, surrounding context) | gopls LSP `textDocument/references` |
| `go_hover` | file, line, col | structured: type info, documentation, signature | gopls LSP `textDocument/hover` |
| `go_workspace_symbols` | query | structured: symbol name, file, line, kind (func/type/const/var) | gopls LSP `workspace/symbol` |
| `go_diagnostics` | file(s) or package | structured: errors, warnings, hints with file:line and quick-fix suggestions | gopls LSP `textDocument/publishDiagnostics` |
| `go_rename_symbol` | file, line, col, new name | structured: list of affected files and locations (preview, does not apply) | gopls LSP `textDocument/rename` |
| `go_call_hierarchy` | file, line, col, direction | structured: incoming or outgoing call tree | gopls LSP `callHierarchy/incomingCalls` |

### Tier 4: Creative Go-Specific (name-makers)

| Tool | Input | Output | Backing tool |
|---|---|---|---|
| `go_fuzz_orchestrate` | package, fuzz target, duration | structured: corpus growth, crashes found, crash-reproducing inputs | `go test -fuzz` |
| `go_build_matrix` | path | structured: per GOOS/GOARCH, files included/excluded by build constraints | `go/build` constraint parsing |
| `go_module_risk` | module path | structured: transitive depth, known CVEs, staleness, license | `go mod why` + `govulncheck` |
| `go_generics_map` | package | structured: generic type/function to concrete instantiations with file:line | `go/types` instantiation tracking |
| `go_pprof_analyze` | profile file, top N | structured: top N functions by CPU/mem, allocation hotspots | `go tool pprof` |
| `go_field_alignment` | path | structured: structs with padding waste, bytes saved by reordering, suggested field order | `fieldalignment` analyzer |

### Resources and Prompts (MCP protocol features both competitors lack)

AgenticGoKit and mcp-navigator-go both implement tools-only MCP. The 2025-06-18 spec adds resources and prompts as first-class primitives. We expose both.

**MCP Resources** (read-only context the AI can pull without a tool call):

| Resource | URI | Content |
|---|---|---|
| `module` | `agentic-go://module` | module metadata and Go version |
| `packages` | `agentic-go://packages` | workspace package inventory |
| `analysis-rules` | `agentic-go://analysis-rules` | available audit rules |
| `config` | `agentic-go://config` | effective server configuration |
| `cache-stats` | `agentic-go://cache-stats` | cache metrics (later roadmap) |
| `trace-summary` | `agentic-go://trace-summary` | aggregate trace summary (later roadmap) |

**MCP Prompts** (pre-built prompt templates the AI can use):

| Prompt | Description |
|---|---|
| `audit-package` | Run the available audits for a package and summarize findings |
| `pre-commit-check` | Run the pre-commit quality gates |
| `bisect-flake` | Investigate a suspected flaky test |
| `benchmark-regression-gate` | Evaluate benchmark changes against a regression threshold |
| `explain-symbol` | Explain a symbol using navigation context (later roadmap) |
| `verify-change` | Run tests, race detection, and the two v0.1.0 audits after an edit |

## Build sequence

The build is staged around the canonical release boundary; dates are intentionally omitted.

### v0.1.0 milestones

1. **Foundation:** preserve the corrected module path, add the official SDK, establish the stdio server, structured results, stderr-only lifecycle logging, workspace resolution, bounded subprocess execution, and shared error contracts.
2. **Test intelligence:** ship `go_test_structured`, `go_race_report`, `go_coverage_gaps`, `go_benchmark_diff`, and `go_flake_finder` with their parsers and fixtures.
3. **Thesis audits:** implement `go_audit_concurrency` and `go_audit_errors` on shared `go/analysis` analyzers, then expose the same analyzers through `cmd/agentic-go-vet/`.
4. **Protocol surfaces:** register exactly `agentic-go://module`, `agentic-go://packages`, `agentic-go://analysis-rules`, and `agentic-go://trace-summary`; register `audit-package`, `pre-commit-check`, `bisect-flake`, and `verify-change`.
5. **Release gate:** add CI, cross-build both binaries, run the documented fixture and false-positive validation, and publish only when the seven-tool/four-resource/four-prompt contract is met.

### Post-v0.1.0 roadmap

1. Add the remaining tools in the 30-tool inventory in `docs/contracts.md`, in dependency-aware phases: navigation, the remaining audit domains and consolidation tools, then creative and profiling tools.
2. Add the two deferred resources, `agentic-go://config` and `agentic-go://cache-stats`, and then `agentic-go://trace-summary` if it was not delivered in v0.1.0; keep the six-URI inventory authoritative.
3. Complete the five original roadmap prompts (`audit-package`, `pre-commit-check`, `bisect-flake`, `benchmark-regression-gate`, `explain-symbol`) and retain `verify-change` as the sixth prompt.
4. Introduce gopls-backed navigation only after the v0.1.0 boundary; its subprocess lifecycle, compatibility, and cache behavior are separate milestones. HTTP/SSE and cache policy are also deferred.

## Key design decisions

1. **Official SDK over mark3labs.** `modelcontextprotocol/go-sdk` is Google-collaborated, 5K stars, tracks the spec canonically, pushed yesterday. AgenticGoKit uses a v0.0.2 personal library for MCP and is already patching around its limitations with shadow transports. We don't repeat that mistake.

2. **gopls as subprocess, not import.** gopls is not designed as a library. Spawning it as an LSP subprocess is what hloiseau does, what gopls's own MCP does, and what GoLand does internally. Reuse, don't reinvent.

3. **Custom `go/analysis` passes grounded in the skill references.** The v0.1.0 concurrency and error passes are the first thesis slice; later audit tools extend the same model. Every rule is grounded in a production note rather than an invented heuristic.

4. **Structured output on every tool.** Every tool returns typed structured output via the SDK's `CallToolResult` with `StructuredContent`. AI agents receive bounded, domain-specific data rather than raw command output. Competing gopls MCP servers also expose structured tool results, so the differentiator is the shape and precision of agentic-go's test evidence and custom audits, not structured output by itself.

5. **Resources and prompts, not just tools.** Both AgenticGoKit and mcp-navigator-go are tools-only. The MCP spec defines resources and prompts as first-class primitives. We expose the scoped v0.1.0 surfaces first, then complete the six-resource/six-prompt roadmap without tool-call overhead.

6. **In-process benchmark diff, not benchstat dependency.** Parse benchmark output directly and compute deltas in-process. One less dependency, one fewer install step. (Ponytail: if the in-process diff logic gets complex, switch to shelling out to `benchstat`.)

7. **Tool naming: `go_` prefix.** Consistent, immediately recognizable as Go-specific. Different server namespace from gopls MCP's `go_` tools, so no collision in practice.

8. **Documentation where examples compile.** Both competitor repos have broken documentation (mcp-navigator-go's `NewMCPClient` doesn't exist, AgenticGoKit's `NewSequentialWorkflow` signature is wrong). Our README examples are tested in CI as part of the `examples/` directory, each with its own `go.mod` (stolen from AgenticGoKit's good practice).

9. **CI that is green.** mcp-navigator-go's CI has been red for 7 months (broken `go test`). Our CI runs `go test -race ./...` (the race detector is mandatory per the tooling rules), `go vet`, `staticcheck`, and build matrix on every push.

10. **Honest claims.** AgenticGoKit claims "70% memory reduction" with an empty `benchmarks/` directory. mcp-navigator-go claims "Production Ready" with broken tests. We make no performance claims without runnable benchmarks in `benchmarks/`.

## What this project does NOT do

- No IDE integration. No JetBrains plugin. No VS Code extension. Pure standalone binary.
- No generic code navigation in v0.1.0. Later navigation is limited to the seven gopls-backed tools in the contracts inventory.
- No multi-language support. Go only. This is the specialization that makes it better than generic tools.
- No LLM-in-the-loop. All analysis is deterministic Go tooling. No AI calls in the server itself. (Aligns with the LLM-vs-deterministic boundary rule.)
- No agent orchestration. AgenticGoKit does agent workflows, streaming, memory/RAG. We don't. That's a different product. We are a Go intelligence server, not an agent framework.

## Verification

Each tool ships with a self-check test using a minimal Go fixture project in `testdata/`:

```
testdata/
  fixture/
    main.go           # has a race condition, an ignored error, a fire-and-forget goroutine, embedded sync.Mutex,
                      # a math/rand token, an == on a secret, a fmt.Printf log, a Get-prefix getter, a util package,
                      # an interface defined where implemented, a zero-value enum, a string concat in a loop
    main_test.go      # has a flaky test, a passing test, a skipped test, a panic test
    go.mod
```

Tests verify:
- `go_test_structured`: parses the fixture's `go test -json` output correctly, returns expected pass/fail/skip counts
- `go_race_report`: detects the race in the fixture, returns structured conflict data with correct goroutine IDs and source locations
- `go_coverage_gaps`: identifies the untested function in the fixture
- `go_audit_concurrency`: flags the concurrency findings in the fixture
- `go_audit_errors`: flags the ignored-error finding in the fixture

CI verification: GitHub Actions runs `go test -race ./...` on every push, plus `go vet` and `staticcheck`. Build matrix: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64.

Manual verification: install both binaries from a tagged release, configure the stdio server in an MCP client, run the seven v0.1.0 tools against a representative local Go workspace, and confirm structured output and analyzer findings.

## Dependencies (minimal)

| Dependency | Purpose | Justification |
|---|---|---|
| `github.com/modelcontextprotocol/go-sdk` | MCP protocol | Official SDK, required |
| `golang.org/x/tools` | `go/analysis` framework and deferred navigation support | Already required by the SDK, and imported directly for the custom analyzers |

No other runtime dependencies. Everything else uses Go stdlib (`go test`, `go vet`, `os/exec` for subprocess management, `go/parser` and `go/types` for AST analysis, `encoding/json` for structured output, `log/slog` for structured logging).

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| gopls LSP client complexity | Start with the 7 navigation tools only. Use hloiseau's `pkg/lsp/client` as reference (not copy, learn the pattern). gopls's own MCP server is also reference. |
| `go/analysis` custom passes are non-trivial (7 passes) | Phase 4 is the hardest and largest. Ship in order: concurrency + errors first (highest incident value), then security + observability, then naming + type-design + performance. If passes take too long, ship Tier 1-3 first (they don't need custom analysis), then add audit tools incrementally. Each pass is independent, so partial delivery is fine. |
| Race report parsing is fragile (format changes between Go versions) | Pin the parser to the current race detector format (Go 1.22+). Document the Go version assumption. Add a version check at startup. |
| Adoption is slow (competing with gopls-based MCP servers) | Lead with the narrow concurrency/error audits and bounded test evidence. Some competitors already expose tests or coverage, while the built-in gopls MCP concentrates on editor intelligence. Describe the verified gap, not the overlap. |
| Name collision with future official tools | `agentic-go` is distinctive. If Google ships an official `go-mcp`, the differentiation (test intelligence, custom analysis, resources/prompts) still holds. |
| Scope creep into agent framework territory | AgenticGoKit is an agent framework. We are not. Resist adding agent orchestration, memory, RAG, or LLM provider adapters. Stay focused on Go intelligence. |
