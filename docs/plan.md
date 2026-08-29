# agentic-go product plan

## Product thesis

Agentic-go is source-grounded Go change intelligence for coding agents. Its
currently shipped workflow is language-native change verification:

> Given a local base and the final worktree, explain what changed, what may be
> affected, what evidence was executed, which findings appear introduced, and
> what remains uncertain.

Go is the reference implementation. The durable product boundary is the
versioned verification report, not MCP and not a count of tools. The CLI,
GitHub Action, and MCP server are adapters over one engine. Agentic-go remains
deterministic developer tooling; it embeds no LLM and performs no agent
orchestration.

The personal v1 module path is `github.com/ashwingopalsamy/agentic-go`. A later
`github.com/agentic-mcps/go` repository is an independent module identity with
an explicit migration, not a transfer or alias.

## Release authorities

- [`v0.2.0-release-scope.md`](v0.2.0-release-scope.md) is the executable v0.2
  specification.
- [`v0.1.0-release-scope.md`](v0.1.0-release-scope.md) is the compatibility
  baseline.
- [`contracts.md`](contracts.md) owns shared protocol and execution
  invariants.
- [`v1.0.0-roadmap.md`](v1.0.0-roadmap.md) owns the staged v0.3 through v1
  direction without retroactively changing the v0.2 contract.
- [`../CONTEXT.md`](../CONTEXT.md) defines the domain language.
- [`adr/0001-verification-report-boundary.md`](adr/0001-verification-report-boundary.md)
  records why the report is the durable boundary.

Broader phase documents are design material only. They do not add release
scope merely because a possible tool or rule is described there.

## Architecture

```text
Change Request
  -> Change Snapshot
  -> Affected Package Closure
  -> Verification Plan
  -> Executed Evidence
  -> Verification Report
```

- `internal/verification` owns portable types, policy, orchestration, and
  report assembly.
- `internal/changeimpact` owns Go, Git, module, package, declaration, and diff
  discovery behind the verification interface.
- workspace, execution, parser, audit, and analysis packages are infrastructure
  adapters.
- `cmd/agentic-go`, the root Action, and MCP tools adapt the same report. MCP
  types, workflow concepts, and adapter names do not enter the engine.

The report began at `agentic.verify/v1alpha1` for v0.2 and is frozen as
`agentic.verify/v1`. Go-specific entities use
namespaced kinds such as `go.package`; top-level concepts remain portable so a
future TypeScript implementation can produce the same semantics. Extraction
into an organization-level specification waits until a second implementation
exists and proves the common boundary.

In v0.7 the CLI and MCP adapters invoke the same unified report path. The
report adds semantic and compiler diagnostics, exact snapshot lineage,
bounded context and refactor provenance, provider capabilities, and optional
Change Contract compliance. These additions preserve the existing result and
exit-status semantics.

## v0.1 trust seed

The compatibility baseline provides seven stdio MCP tools, four resources,
four prompts, and `agentic-go-vet`. Its active concurrency and error rules were
calibrated against a pinned ten-repository corpus. That evidence is
corpus-specific, not a universal precision guarantee.

Those analyzers remain the only v0.2 policy-finding domains. A new or changed
predicate requires a positive fixture, a meaningful near miss, a documented
limitation, production-path coverage, reviewed external findings, and an
acceptable false-positive rate.

## v0.2 usage seed

The primary workflow is:

```sh
agentic-go verify --base origin/main
```

It provides:

- a final-worktree snapshot covering committed, staged, unstaged, renamed,
  deleted, and untracked changes;
- changed Go declarations and module/workspace/embed metadata;
- directly changed packages plus transitive reverse importers within scope;
- one whole-package test and changed-statement coverage run, with optional race
  detection;
- base/current comparison for calibrated analyzer findings;
- source-grounded risk facts and targeted review guidance;
- explicit uncertainty for generated code, build constraints, cgo, external
  consumers, generated inputs, and unmodelled non-Go behavior; and
- a deterministic report with `pass`, `findings`, or `incomplete` automation
  status. `pass` is never a safety verdict.

Delivery order is CLI first, a thin advisory GitHub Action second, and one
approval-aware MCP operation for coding agents third. The existing seven MCP
tools remain compatible, but clients should use `go_verify_change` when they
want the complete workflow.

Selective tests, call-graph reachability claims, SSA/VTA, `test_regex`, SARIF,
HTTP, `doctor`, automatic toolchain installation, and Windows support claims do
not ship in v0.2.

## Evidence before publication

The v0.2 release record must contain:

- golden contract reports;
- local CLI and ephemeral stdio MCP dogfood against agentic-go;
- three reviewed historical changes from already-cloned projects showing
  reverse impact, changed coverage, and analyzer baselining;
- commands, pinned commits, timings, limitations, and observed usefulness;
- the Go 1.25/1.26/1.27 release matrix, race/vet/build/static analysis,
  four-target cross-builds, release configuration checks, signatures, history,
  and a clean worktree.

This is self-serve implementation evidence, not an adoption or product-market
fit claim.

## Expansion rule

Security, observability, API design, naming/maintainability, and performance
remain relevant review lenses. In v0.2 they report only change-grounded facts
and guidance. They become analyzers only after repeated repository evidence
shows that an actionable defect class can be detected precisely enough to pass
the same calibration gate as the existing rules.

Navigation, profiling, build analysis, fuzz orchestration, and other ideas are
also proposals, not a promised catalog. Expansion follows demonstrated user
pain and retained workflow value; it does not aim at a predetermined MCP tool
count.

## Multi-language direction

Future repositories may use paths such as `github.com/agentic-mcps/go` and an
equivalent TypeScript package, but each language implementation owns its native
change discovery and evidence execution. They share only semantics proven
portable in practice: snapshots, impacted units, checks, evidence, findings,
risks, uncertainty, and policy status.

The personal Go module remains `github.com/ashwingopalsamy/agentic-go` for its
v1 release. A later `github.com/agentic-mcps/go` implementation starts from the
same source lineage but remains a separate repository and module identity. The
two paths are not interchangeable and no automatic mirroring is implied.

## v1 direction

The v0.2 compatibility authority remains
[`v0.2.0-release-scope.md`](v0.2.0-release-scope.md). The implemented product
direction is [`v1.0.0-roadmap.md`](v1.0.0-roadmap.md):
source-grounded Go change intelligence combining semantic navigation, compact
context, persistent change continuity, guarded deterministic refactoring, and
verification with explicit provenance and uncertainty.

The roadmap was staged to prove useful workflows and reliable contracts before
v1. The implemented surface and evidence do not establish a universal
model-reliability or token-saving claim.
