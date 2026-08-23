# Decision memo — focused v0.1.0, complete v1.0.0 roadmap

## Decision

Build agentic-go as a deterministic, local-agent-first Go intelligence MCP
server. Ship a focused v0.1.0 before completing the broader v1.0.0 roadmap.
The canonical v0.1.0 boundary is
[`v0.1.0-release-scope.md`](v0.1.0-release-scope.md); shared implementation
contracts live in [`contracts.md`](contracts.md).

v0.1.0 contains seven MCP tools, four resources, four prompts, and the
`agentic-go-vet` binary. It proves the product thesis with structured test
intelligence plus high-precision concurrency and error analysis. Navigation,
the remaining audit domains, and creative tools remain specified roadmap work.

## Why

The original plan had a strong technical spine but coupled the first release
to thirty tools. That maximized surface area before schemas, parser behavior,
and analyzer precision had been exercised by real users. A smaller release is
not a lower-quality release: every shipped capability keeps the same typed
output, containment, fixture, and verification standards.

Four findings shaped the executable specification:

1. Phase documents need one shared contract and explicit release authority.
2. Static-analysis prose must map to exact AST predicates and near-miss
   fixtures; vague heuristics are not shippable.
3. Audit fixtures must be isolated by rule so one failure cannot corrupt
   another rule's evidence.
4. Analyzer precision outranks catalog breadth. A noisy rule is disabled,
   even when it was planned.

## v0.1.0 execution order

1. Normalize the specification corpus and repository identity.
2. Build the module, server foundation, workspace containment, progress, and
   tracing.
3. Add structured tests and race reports.
4. Add coverage gaps, benchmark comparison, and flake detection.
5. Add concurrency and error analyzers plus their MCP wrappers.
6. Add `agentic-go-vet`, resources, and prompts.
7. Add release artifacts and run the complete verification matrix.
8. Validate analyzer findings against at least ten prominent, pinned local Go
   repositories before tagging v0.1.0.

Each implementation milestone is a small local commit. Before the first push,
unpublished commits may be split, squashed, reordered, and retitled while
preserving truthful authorship and provenance.

## Product boundaries

- Go only; no embedded LLM or agent orchestration.
- Stdio only in v0.1.0.
- Target-repository tests are trusted code executed with the server process's
  privileges; the product does not claim sandboxing.
- Analysis tools type-check source without executing target code.
- No IDE plugin, hosted service, Homebrew tap, doctor command, or SARIF output
  in v0.1.0.
- Public claims require runnable evidence.

## Verification principle

Implementation is incomplete until the relevant scoped build, tests, race
tests, vet, static analysis, cross-compilation, and MCP protocol checks pass.
Before release, any analyzer rule with more than five percent observed false
positives or a repeatable systemic false-positive pattern is disabled.
