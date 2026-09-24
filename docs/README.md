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

## Next-generation Go intelligence

After the contributor instructions, a new agent should start with the
[Astra analysis handoff](continuation/astra-understanding.md): product judgment,
source evidence, confirmed observation defects, deferred work, and the next
decision. Its quick start routes the next task without repeating the broader
investigation. The [continuation handoff](continuation/go-intelligence.md) owns
implementation status and the next action; the
[Go intelligence north star](go-intelligence-north-star.md) owns the approved
architectural direction and acceptance criteria.
`v1.2.1` (tag `67f54b7`) is the latest released baseline. The current
`codex/v1.2-reliability` branch contains unreleased post-v1.2.1 work. Its
product direction is a deterministic, snapshot-bound evidence compiler that
supports the edit, refresh, verify, inspect loop. Slice 1A adds next-action
guidance to existing MCP text, and Slice 1B refines private evidence projection
guidance. Both preserve the frozen public MCP inventory and
`agentic.focus/v1` schema.

Private local Luna evaluations now include a 20-run discoverability pilot and
a 12-run integrated adoption follow-up. In the focus arm, capability delivery
was healthy in 10/10 runs, but no run called `go_context`. All six integrated
runs discovered the shipped skill, called `go_context`, refreshed after
editing, and used the evidence. Integrated median duration was 431,641 ms
versus 223,758 ms for baseline, about 93% higher; median tool calls were 24
versus 26. Astra judged workflow adoption locally demonstrated for the
combined skill and MCP surface, with comparative
product value still unproven. They do not establish MCP-alone causality,
improved correctness, productivity, token efficiency, or speed, statistical
significance, generalization, or production readiness. Both studies used two
scenarios and `gpt-5.6-luna` at max reasoning. The integrated follow-up used
three repetitions per scenario per arm. Reports remain private and are not
tracked. See the
[historical adoption results](../validation/v1.0.0/adoption-results.md) for
older v0.8/v1.0 evidence. The two current studies are regression evidence, not
fresh proof of product superiority. Review of the six integrated traces is
complete: no transcript establishes that context improved the necessary code
edit. In gRPC run 3, refreshed context prompted broader `./...` verification,
which hit the output cap; focused verification later passed after a stale
snapshot rejection. Client-go runs made 3-4 context calls each, and gRPC runs
made 4-5, including ambiguous or unhelpful selections. The bounded guidance
improvement is to narrow ambiguity using returned candidates, finish each batch
of edits and formatting before refreshing, and refresh again after further
edits or stale-snapshot rejection while avoiding redundant refreshes when the
snapshot is unchanged. This observation does not establish causal edit-quality or product
value. External and multi-model evaluation remain pending. Delta refresh
remains deferred. These documents do not override frozen v1 contracts.

A post-guidance regression used six integrated Luna runs, with three
repetitions on each of two scenarios. All six qualified, passed acceptance,
stayed within scope, and required no operator intervention. All six called
`go_context`, refreshed after edits, and recorded evidence use. Five focus
calls failed: three client-go calls returned `invalid_input` for invalid
symbol references, and two calls in grpc-go run 1 returned `stale_snapshot`
because an observed semantic location was absent from the snapshot manifest.
grpc-go runs 2 and 3 had no failed focus calls. This shows workflow adoption
continued while exposing failure categories; it does not establish improved
quality, correctness, productivity, speed, or product value. Before changing
provider behavior, the audit classified the client-go failures as malformed or
reconstructed refs and the gRPC failures as unbound workspace locations. The
provider now omits unbound locations with bounded uncertainty while preserving
strict stale rejection. External and multi-model evaluation remain pending.

After the failure remediation, six integrated Luna reruns completed with 6/6
qualification, 6/6 acceptance, zero scope violations, zero operator
interventions, zero failed focus calls, and complete refresh and evidence-use
signals. The median duration was 411,537 ms and the median tool-call count was
31. This is diagnostic regression evidence only and does not establish product
value or comparative engineering benefit.

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
