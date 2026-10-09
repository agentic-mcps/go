# Documentation authority map

Start with [`v0.9.0-release-scope.md`](v0.9.0-release-scope.md) for the frozen
v1 contract. [`v0.2.0-release-scope.md`](v0.2.0-release-scope.md) remains the
verification compatibility baseline. The tagged
[`v0.1.0-release-scope.md`](v0.1.0-release-scope.md) remains the earlier
compatibility baseline. Shared interfaces and invariants live in
[`contracts.md`](contracts.md). The completed v1 implementation stages and
their evidence live in
[`v1.0.0-roadmap.md`](v1.0.0-roadmap.md). The architectural rationale is
recorded in the historical [`decision-memo.md`](decision-memo.md);
[`plan.md`](plan.md) is the current product summary and routing guide.

## Next-generation Go intelligence

After the contributor instructions, read the
[Go engineering north star](go-intelligence-north-star.md) for product direction,
model/skill requirements, the next delivery cycle, and acceptance criteria.
Then read the [continuation handoff](continuation/go-intelligence.md) for
implemented behavior, current evidence, and the exact unfinished step.
The [Astra source review](continuation/astra-understanding.md) is dated
historical background, not current implementation sequencing.

The first customer is a Go engineer using coding agents on real repositories.
The intended workflow covers understanding, implementation, debugging,
refresh, verification, and review/resume. The next product cycle makes one
cross-package API/interface change dependable from start to handoff, using the
existing deterministic evidence compiler and concise shared skills.

The user-directed aspiration for install-time, branch-aware repository
indexing and repeatable retrieval is recorded separately in the
[codebase indexing research note](research/codebase-indexing-retrieval.md).
The current development branch adds an exact branch source-view preview and
records the first retrieval screen. It does not yet provide persistent
indexing or demonstrate comparative product value.

`v1.2.1` (tag `67f54b7`) remains the released baseline. The current
`codex/agentic-go-retrieval-2026-09-27` topic branch contains unreleased work.
The frozen v1 registry
remains 14 tools, seven resources, one template, and six prompts; the existing
additive `go_context` brings the server to 15 tools. The revised plan does not
change that inventory, schemas, or strict freshness behavior.

| Read for | Authority |
| --- | --- |
| Customer, model independence, skills, and workflow outcomes | [North star](go-intelligence-north-star.md) |
| Current implementation, remediation, and next action | [Continuation handoff](continuation/go-intelligence.md) |
| Branch source-view preview, retrieval findings, and next gates | [Codebase indexing research](research/codebase-indexing-retrieval.md) |
| Current campaign's implementation/check status | [Selector remediation record](../validation/v1.0.0/adoption-remediation-2026-09-24.md) |
| Older adoption experiments | [Historical results](../validation/v1.0.0/adoption-results.md) |
| Compatibility and execution invariants | [v1 freeze](v0.9.0-release-scope.md) and [contracts](contracts.md) |

Keep the current canonical Luna matrix separate from older integrated
diagnostics. Workflow use and task acceptance have been observed; comparative
engineer value and broad model/host compatibility remain unproven. The plan
requires comparison against equipped native Go/gopls workflows, plus actual
review/rework effort. It does not promise equal competence across models or an
unreproducible capability advantage. Full-replacement refresh remains current;
delta delivery stays deferred.

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
- [`schema/focus-v1.json`](schema/focus-v1.json):
  additive change, focused evidence, refresh, and verification-applicability contract
- [`adr/0002-context-pack-boundary.md`](adr/0002-context-pack-boundary.md):
  why Context Packs and the intelligence service, not raw gopls or MCP, form
  the semantic product boundary

## v0.1.0 implementation specifications

- [`phase-1-test-intelligence.md`](archive/phase-1-test-intelligence.md)
- [`phase-2-coverage-benchmark-flake.md`](archive/phase-2-coverage-benchmark-flake.md)
- [`phase-3-gopls-navigation-resources-prompts.md`](archive/phase-3-gopls-navigation-resources-prompts.md)
  — only the resources and prompts selected by the release scope
- [`phase-4a-concurrency.md`](archive/phase-4a-concurrency.md)
- [`phase-4a-errors.md`](archive/phase-4a-errors.md)
- [`phase-6-release-polish.md`](archive/phase-6-release-polish.md)

## Deferred roadmap specifications

The v1 roadmap is implemented locally. The following broader phase documents
remain retained research and do not silently expand the frozen surface.

- [`phase-4a-index.md`](archive/phase-4a-index.md)
- [`phase-4a-security.md`](archive/phase-4a-security.md)
- [`phase-4a-observability.md`](archive/phase-4a-observability.md)
- [`phase-4a-naming.md`](archive/phase-4a-naming.md)
- [`phase-4a-type-design.md`](archive/phase-4a-type-design.md)
- [`phase-4a-performance.md`](archive/phase-4a-performance.md)
- [`phase-4b-tier-2-tools.md`](archive/phase-4b-tier-2-tools.md)
- [`phase-5-creative-tools.md`](archive/phase-5-creative-tools.md)

[`continuation/v0.2-planning.md`](continuation/v0.2-planning.md) is retained as
a superseded planning record and is not implementation authority. All
filenames are lowercase kebab-case. No implementation step depends on a private
reference checkout or a document outside this repository.
