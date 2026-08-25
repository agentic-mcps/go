# Documentation authority map

Start with [`v0.2.0-release-scope.md`](v0.2.0-release-scope.md). It is the
canonical authority for the current executable implementation. The tagged
[`v0.1.0-release-scope.md`](v0.1.0-release-scope.md) remains the compatibility
baseline. Shared interfaces and invariants live in
[`contracts.md`](contracts.md). The architectural rationale is recorded in
[`decision-memo.md`](decision-memo.md). The future v1 direction is recorded in
[`v1.0.0-roadmap.md`](v1.0.0-roadmap.md); [`plan.md`](plan.md) is the concise
product plan and routing summary.

## v0.2.0 implementation specification

- [`v0.2.0-release-scope.md`](v0.2.0-release-scope.md) — change-aware
  whole-package verification, changed-statement coverage, analyzer baselining,
  risk guidance, and CLI/Action/MCP adapters
- [`schema/verification-report-v1alpha1.json`](schema/verification-report-v1alpha1.json)
  — portable machine-readable report contract
- [`../CONTEXT.md`](../CONTEXT.md) and
  [`adr/0001-verification-report-boundary.md`](adr/0001-verification-report-boundary.md)
  — shared language and the durable product-boundary decision

## v0.3 and v0.4 implementation contracts

- [`v1.0.0-roadmap.md`](v1.0.0-roadmap.md): staged sidecar, semantic,
  continuity, refactor, evaluation, and contract-freeze authority
- [`schema/context-pack-v1alpha1.json`](schema/context-pack-v1alpha1.json):
  compact snapshot-bound semantic context contract
- [`adr/0002-context-pack-boundary.md`](adr/0002-context-pack-boundary.md):
  why Context Packs and the intelligence service, not raw gopls or MCP, form
  the semantic product boundary

## v0.1.0 implementation specifications

- [`phase-1-test-intelligence.md`](phase-1-test-intelligence.md)
- [`phase-2-coverage-benchmark-flake.md`](phase-2-coverage-benchmark-flake.md)
- [`phase-3-gopls-navigation-resources-prompts.md`](phase-3-gopls-navigation-resources-prompts.md)
  — only the resources and prompts selected by the release scope
- [`phase-4a-concurrency.md`](phase-4a-concurrency.md)
- [`phase-4a-errors.md`](phase-4a-errors.md)
- [`phase-6-release-polish.md`](phase-6-release-polish.md)

## Deferred roadmap specifications

The unimplemented v0.5 and later portions of the v1 roadmap remain future
authority. They do not silently expand the current development surface.

- [`phase-4a-index.md`](phase-4a-index.md)
- [`phase-4a-security.md`](phase-4a-security.md)
- [`phase-4a-observability.md`](phase-4a-observability.md)
- [`phase-4a-naming.md`](phase-4a-naming.md)
- [`phase-4a-type-design.md`](phase-4a-type-design.md)
- [`phase-4a-performance.md`](phase-4a-performance.md)
- [`phase-4b-tier-2-tools.md`](phase-4b-tier-2-tools.md)
- [`phase-5-creative-tools.md`](phase-5-creative-tools.md)

[`continuation/v0.2-planning.md`](continuation/v0.2-planning.md) is retained as
a superseded planning record and is not implementation authority. All
filenames are lowercase kebab-case. No implementation step depends on a private
reference checkout or a document outside this repository.
