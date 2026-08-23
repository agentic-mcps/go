# Documentation authority map

Start with [`v0.1.0-release-scope.md`](v0.1.0-release-scope.md). It is the
canonical authority for the current release and wins when a roadmap document
describes a broader surface. Shared interfaces and invariants live in
[`contracts.md`](contracts.md). The architectural rationale is recorded in
[`decision-memo.md`](decision-memo.md), while [`plan.md`](plan.md) describes
the complete roadmap toward v1.0.0.

## v0.1.0 implementation specifications

- [`phase-1-test-intelligence.md`](phase-1-test-intelligence.md)
- [`phase-2-coverage-benchmark-flake.md`](phase-2-coverage-benchmark-flake.md)
- [`phase-3-gopls-navigation-resources-prompts.md`](phase-3-gopls-navigation-resources-prompts.md)
  — only the resources and prompts selected by the release scope
- [`phase-4a-concurrency.md`](phase-4a-concurrency.md)
- [`phase-4a-errors.md`](phase-4a-errors.md)
- [`phase-6-release-polish.md`](phase-6-release-polish.md)

## Deferred roadmap specifications

- [`phase-4a-index.md`](phase-4a-index.md)
- [`phase-4a-security.md`](phase-4a-security.md)
- [`phase-4a-observability.md`](phase-4a-observability.md)
- [`phase-4a-naming.md`](phase-4a-naming.md)
- [`phase-4a-type-design.md`](phase-4a-type-design.md)
- [`phase-4a-performance.md`](phase-4a-performance.md)
- [`phase-4b-tier-2-tools.md`](phase-4b-tier-2-tools.md)
- [`phase-5-creative-tools.md`](phase-5-creative-tools.md)

All filenames are lowercase kebab-case. No implementation step depends on a
private reference checkout or a document outside this repository.
