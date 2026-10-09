# Agentic-go contributor contract

## Start here

`agentic-go check` is the product: a done-gate that runs when a coding agent
tries to stop (or on pre-push and in CI) and blocks only on problems the change
introduced. Read [`docs/design/check-gate.md`](docs/design/check-gate.md) before
changing gate behavior, and [`validation/gate/README.md`](validation/gate/README.md)
before changing the evaluation, its baselines, or any claim about the gate.

The MCP server (`go_context`, Change Contracts, guarded refactor, retrieval,
source views) is maintenance-only. Its frozen v1 surface stays as documented in
[`docs/contracts.md`](docs/contracts.md) and
[`docs/v0.9.0-release-scope.md`](docs/v0.9.0-release-scope.md): fix defects,
add no tools, resources, prompts, or schema fields.

For an analyzer rule change, read the matching archived specification:
[`docs/archive/phase-4a-concurrency.md`](docs/archive/phase-4a-concurrency.md) or
[`docs/archive/phase-4a-errors.md`](docs/archive/phase-4a-errors.md).

## Invariants

- The gate reports problems the change introduced. A failure it can attribute
  to the base (fails there too) or to flakiness (passes on rerun) is a warning,
  never a block.
- When evidence is incomplete (time budget, closure too large, tooling failure)
  the verdict is `unknown`, never `pass`. Blocking items found before the
  evidence ran out still block.
- In hook mode the process always exits 0 and speaks only through the hook's
  JSON. A gate failure never blocks an agent. A repeated stop with an unchanged
  fingerprint is allowed and disclosed to the human, never blocked again.
- Text output and hook reasons stay within 2048 bytes, keep whole lines and
  valid UTF-8, and rank blocking entries and their fixes above detail and notes.
- Integrity rules (deleted, skipped, hidden, or weakened tests; stubs; gate
  configuration edits) favor precision. A rule ships with a positive fixture,
  a meaningful near miss, and a stated limitation. A block on a common
  legitimate refactor is a defect.
- Claims about the gate come only from recorded runs of the pre-registered
  evaluation. Do not claim model parity, speedups, or real-world prevalence the
  evidence does not show. Held-out evaluation variants must not inform detector
  changes before a run is recorded; if they do, disclose it in the results.
- The `agentic.verify/v1` report schema stays frozen. Engine changes made for
  the gate keep `agentic-go verify` output compatible.
- All filesystem access stays within the configured, symlink-resolved
  workspace. Subprocess work shares cancellation, deadlines, concurrency limits,
  and bounded output. Describe these controls as containment, never as
  sandboxing. The gate compiles and runs trusted target-repository code.
- Protocol errors fail loudly. Clean results use non-nil empty collections.

## Work loop

1. Locate the smallest relevant implementation and its governing contract.
2. State the behavior and failure modes the change must preserve.
3. Make one coherent change; widen a public surface only when the design
   document calls for it.
4. Inspect the diff and run the smallest relevant verification first.
5. Before handoff, run `go build ./...`, `go test -race ./...`, `go vet ./...`,
   `golangci-lint run` (the version pinned in `.github/workflows/verify.yml`),
   and `git diff --check` when the environment supports them.

Use short Conventional Commit subjects. Preserve the configured Git author.
Create no tag, release, or remote push without explicit maintainer approval.
