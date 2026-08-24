# Agentic-go contributor contract

## Start here

Read [`docs/v0.2.0-release-scope.md`](docs/v0.2.0-release-scope.md) before
changing v0.2 behavior. It is the target release authority and overrides
broader roadmap documents. The tagged
[`v0.1.0 release scope`](docs/v0.1.0-release-scope.md) remains the compatibility
baseline. Read [`docs/contracts.md`](docs/contracts.md) when changing protocol
types, execution boundaries, findings, tracing, or analyzer wiring.

For a rule change, also read the matching domain specification:
[`docs/phase-4a-concurrency.md`](docs/phase-4a-concurrency.md) or
[`docs/phase-4a-errors.md`](docs/phase-4a-errors.md). For release work, read
[`docs/phase-6-release-polish.md`](docs/phase-6-release-polish.md).

## Invariants

- Tagged v0.1.0 is exactly seven tools, four resources, four prompts, and the
  `agentic-go-vet` binary. The v0.2.0 target adds only `go_change_impact`, for
  eight tools total. `internal/tools.RegisterAll` is always the live inventory.
- `go_change_impact` returns conservative verification candidates with
  evidence and explicit uncertainty. It never claims to prove that omitted
  code is safe and never executes its proposed plan.
- The server is stdio-only and helps an external coding agent make decisions.
  It does not embed an LLM or become an agent framework.
- All filesystem access stays within the configured, symlink-resolved
  workspace. Subprocess and analyzer work shares cancellation, deadlines,
  concurrency limits, and bounded output. Describe these controls as
  containment, never as sandboxing.
- Execution tools may compile and run trusted target-repository code. Audit
  tools remain read-only and closed-world.
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
