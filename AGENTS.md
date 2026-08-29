# Agentic-go contributor contract

## Start here

Read [`docs/v0.2.0-release-scope.md`](docs/v0.2.0-release-scope.md) before
changing v0.2 behavior. It is the target release authority and overrides
broader roadmap documents. The tagged
[`v0.1.0 release scope`](docs/v0.1.0-release-scope.md) remains the compatibility
baseline. Read [`docs/contracts.md`](docs/contracts.md) when changing protocol
types, execution boundaries, findings, tracing, or analyzer wiring.
Read [`docs/v1.0.0-roadmap.md`](docs/v1.0.0-roadmap.md) before changing the
v0.3+ sidecar, intelligence, snapshot, contract, refactor, or distribution
work. The roadmap adds staged scope without weakening v0.2 compatibility.
Read [`docs/v0.8.0-evaluation-scope.md`](docs/v0.8.0-evaluation-scope.md)
before changing the historical task corpus, scorer, replay format, or model
evaluation claims.
Read [`docs/v0.9.0-release-scope.md`](docs/v0.9.0-release-scope.md) before
changing frozen schemas, MCP interfaces, cached-state upgrades, or v1 release
evidence.

For a rule change, also read the matching domain specification:
[`docs/phase-4a-concurrency.md`](docs/phase-4a-concurrency.md) or
[`docs/phase-4a-errors.md`](docs/phase-4a-errors.md). For release work, read
[`docs/phase-6-release-polish.md`](docs/phase-6-release-polish.md).

## Invariants

- Tagged v0.1.0 is seven tools, four resources, four prompts, and the
  `agentic-go-vet` binary. v0.2 adds `go_verify_change`. The frozen v1
  development surface is 14 tools, seven fixed resources, one artifact resource
  template, and six prompts. `internal/tools.RegisterAll` is the live inventory.
  Change Contracts are private same-machine user-cache state with exact
  snapshot lineage and stale rejection. Goal and decision prose is never
  semantically enforced.
- Guarded refactor preview is non-mutating and content-addressed. Apply requires
  the exact snapshot and every exact preimage, changes only existing contained
  non-generated files, and journals before writing. Recovery fails closed when
  a target diverges. Refactoring never mutates Git state or history.
- The v0.2 verification report is the durable product boundary shared by the
  CLI, GitHub Action, and MCP adapter. It reports conservative impact,
  executed evidence, findings, risk facts, and explicit uncertainty; it never
  claims to prove that omitted code is safe.
- v0.3 release bundles pair `agentic-go` with the exact
  `agentic-go-gopls` companion. Managed sessions negotiate capabilities,
  disable telemetry, use bounded stdio LSP, and replay only an explicitly
  idempotent read after terminal failure.
- v0.4 intelligence is bound to immutable Snapshot Refs. Public locations use
  one-based UTF-8 byte columns; LSP UTF-16 positions stay inside the gopls
  adapter and opaque Symbol Refs. Stale refs fail instead of being re-resolved.
- `agentic.context/v1` Context Packs are compact, deterministic, and
  source-grounded. Complete overflow detail stays in private content-addressed
  artifacts addressed by opaque cursors. MCP and LSP types do not enter
  `internal/intelligence` domain contracts.
- The server is stdio-only and helps an external coding agent make decisions.
  It does not embed an LLM or become an agent framework.
- All filesystem access stays within the configured, symlink-resolved
  workspace. Subprocess and analyzer work share cancellation, deadlines,
  concurrency limits, and bounded output. Describe these controls as
  containment, never as sandboxing.
- Execution and verification tools may compile and run trusted
  target-repository code. Audit tools remain read-only and closed-world.
- A rule ships only with a positive fixture, a meaningful near miss, a stated
  limitation, and integration coverage through the production audit path.
  External validation is a release gate, not a claim to infer from fixtures.
- Protocol errors fail loudly. Clean results use non-nil empty collections.

## Work loop

1. Locate the smallest relevant implementation and its governing contract.
2. State the behavior and failure modes the change must preserve.
3. Make one coherent change without widening the public surface.
4. Inspect the diff and run the smallest relevant verification first.
5. Before handoff, run `go test ./...`, `go test -race ./...`, `go vet ./...`,
   `go build ./...`, and `git diff --check` when the environment supports them.

Use short Conventional Commit subjects. Preserve the configured Git author.
Create no tag, release, or remote push without explicit maintainer approval.
