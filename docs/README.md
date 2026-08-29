# Documentation authority map

Start with [`v0.9.0-release-scope.md`](v0.9.0-release-scope.md) for the frozen
v1 contract. [`v0.2.0-release-scope.md`](v0.2.0-release-scope.md) remains the
verification compatibility baseline. The tagged
[`v0.1.0-release-scope.md`](v0.1.0-release-scope.md) remains the earlier
compatibility baseline. Shared interfaces and invariants live in
[`contracts.md`](contracts.md). The completed v1 implementation stages and
their evidence live in
[`v1.0.0-roadmap.md`](v1.0.0-roadmap.md). The architectural rationale is
recorded in [`decision-memo.md`](decision-memo.md); [`plan.md`](plan.md) is the
concise product plan and routing summary.

## Verification report contracts

- [`v0.2.0-release-scope.md`](v0.2.0-release-scope.md) — change-aware
  whole-package verification, changed-statement coverage, analyzer baselining,
  risk guidance, and CLI/Action/MCP adapters
- [`schema/verification-report-v1.json`](schema/verification-report-v1.json)
  - current portable machine-readable report contract
- [`v1-schema-migration.md`](v1-schema-migration.md)
  - pre-freeze schema and private-state migration guide
- [`verification-report-v1beta1-migration.md`](verification-report-v1beta1-migration.md)
  - historical alpha-to-beta migration guide
- [`schema/archive/verification-report-v1alpha1.json`](schema/archive/verification-report-v1alpha1.json)
  - frozen v0.2 report contract
- [`../CONTEXT.md`](../CONTEXT.md) and
  [`adr/0001-verification-report-boundary.md`](adr/0001-verification-report-boundary.md)
  — shared language and the durable product-boundary decision

## v1 implementation contracts

- [`v1.0.0-roadmap.md`](v1.0.0-roadmap.md): staged sidecar, semantic,
  continuity, refactor, evaluation, and contract-freeze authority
- [`schema/context-pack-v1.json`](schema/context-pack-v1.json):
  compact snapshot-bound semantic context contract
- [`schema/change-contract-v1.json`](schema/change-contract-v1.json):
  snapshot-bound Change Contract and Checkpoint contract
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

The v1 roadmap is implemented locally. The following broader phase documents
remain retained research and do not silently expand the frozen surface.

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
