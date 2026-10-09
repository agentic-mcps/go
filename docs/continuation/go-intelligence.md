# Go intelligence continuation handoff

## Purpose

This document preserves the current product understanding for a future agent
working in another account or session. It is independent of conversation
history, account identity, private memory, and previous tool output.

The first customer is a Go engineer using coding agents on real repositories.
The product should support understanding, implementation, debugging, refresh,
verification, and an inspectable handoff across supported models and hosts.
Its deterministic, snapshot-bound evidence compiler should reduce reliance on
model memory and repeated engineer investigation. Comparative benefit remains
unproven. Model-independent contracts do not guarantee equal model competence
or prevent an agent from ignoring evidence.

The full approved architectural direction is in the canonical
[Go intelligence north-star plan](../go-intelligence-north-star.md). Approval
of the direction does not freeze the proposed interface or establish that it
is implemented. This handoff points to the plan instead of duplicating it.

## Reading order

1. Read [the north-star plan](../go-intelligence-north-star.md).
2. Read the applicable repository contributor instructions and the
   [v0.9 frozen interface contract](../v0.9.0-release-scope.md). Consult only
   relevant sections of [contracts](../contracts.md); read the
   [v0.2 release scope](../v0.2.0-release-scope.md) when touching verification.
3. Follow the source pointers below for the current milestone. Read the
   [Context Pack rationale](../adr/0002-context-pack-boundary.md) if changing
   the intelligence interface.

Do not repeat broad documentation exploration unless a source fact below has
become stale.

The [Astra source review](astra-understanding.md) is historical background from
2026-09-05, not current sequencing authority. Read it only for a relevant
source rationale; its old next-slice instructions must not restart completed
observation work.

## Current status

`v1.2.1` (tag `67f54b7`) is the latest released baseline. This branch,
`codex/v1.2-reliability`, contains unreleased work after that release, including
commits `8e65ce1`, `71099dc`, and `7b5111c`. Do not describe this branch as a
release candidate or assign a new release label before a reliability milestone
passes.

The 2026-09-24 north-star revision selects a complete cross-package API or
interface change as the first product workflow: understand obligations, edit
and debug, refresh, verify, and hand back current evidence. Offering focused
declaration candidates from the observed diff and completing the review
handoff are proposed next increments. They are not implemented by this
documentation change. The existing system does not decide task completion.

At this revision's inspection, HEAD was `7b5111c` and separate uncommitted
selector-remediation work was already present. Its
[campaign record](../../validation/v1.0.0/adoption-remediation-2026-09-24.md)
reports focused checks complete, with full post-change gates and evaluation
reruns pending. Recheck that record and the worktree before continuing; do not
overwrite the existing work or infer that a documented plan has been executed.

The frozen v1 registry remains 14 tools, seven resources, one template, and six
prompts. The existing additive `go_context` brings the server to 15 tools;
`agentic.focus/v1` and the frozen v1 schemas remain unchanged. Slice 1A
adds `next_action` to existing MCP text using the existing structured
`Verification.NextAction`. Slice 1B refines the private projection with
cause-specific guidance for stale or unavailable evidence, truncation, budget
omission, advisory findings, and passing checks with limits. Slice 1A changes
existing MCP text rendering; Slice 1B changes private projection guidance.
Neither changes inventory, structured fields, or schemas.

Implemented in v1.1.0:

- `Snapshotter.observe` performs the existing two-pass capture, rejects drift,
  retains the exact manifest, and returns captured source bytes for the
  request. `Capture` remains a compatibility wrapper and `Validate` continues
  to recapture and reject stale references.
- `snapshotObservation` owns the snapshot, manifest records, bounded Go source
  contents, package discovery, and a release-once manifest lease. `Core.Brief`,
  `Core.Search`, and `Core.Symbol` consume that observation. Release is installed
  immediately after acquisition, before position resolution or other fallible
  post-acquisition work.
- Manifest retention is bounded to 32 entries and 8 MiB of manifest metadata;
  active entries are pinned, ordinary eviction skips them, and admission fails
  closed when all capacity is active. Existing-ID replacement accounts for
  the new retained size, evicts only inactive entries as needed, and leaves the
  prior entry intact when admission fails. Captured source retained for one
  observation is bounded to 4 MiB.
- The managed gopls provider pins the manifest for each serialized semantic
  read, preserving existing notification, restart, ordering, and stale
  rejection behavior.
- Observation-aware source reads prefer captured bytes. A retention-cap miss
  rereads a contained file and compares its kind and digest with the observation
  manifest before using those same bytes; missing, deleted, or mismatched inputs
  fail explicitly. Brief inventory, diagnostics, guidance, source positions,
  semantic locations, and uncertainty inspection use this policy. Guidance paths
  are included in snapshot identity. Legacy non-observation helpers retain their
  compatibility behavior.
- Manifest retention and lease acquisition are atomic, so a newly retained
  observation cannot be evicted between admission and pinning.

Focused correctness tests cover captured-source precedence, same-size and
A→B→A rewrites on a source-cap miss, guidance identity and mismatch rejection,
Brief observation forwarding, Symbol position-error lease release, active
manifest protection, fail-closed admission, and replacement byte accounting.
Those checks qualify the historical v1.1.0 work; they do not qualify later
changes. The v0.8 task, adoption, and pilot records below are historical. Two
private local Luna studies provide bounded instruction-use and workflow
adoption observations; comparative product value remains unproven.

In the v1.1.0 implementation, Stage 2 added no public interface or general
derived cache. The post-v1 focus and refresh capabilities described below were
added later; the frozen v1 contracts remain unchanged.

## Current reliability work

The post-v1.2.1 branch work improves how agents interpret existing evidence.
The four private projection outcomes are `verification_needed`,
`finding_inspection_needed`, `evidence_unavailable`, and
`requested_checks_passed_with_limits`. Stale, mismatched, legacy, missing, or
policy-incompatible reports do not yield current repair targets. Findings are
actionable for inspection only when their source locations remain valid.
Incomplete, cancelled, truncated, provider-failed, and unknown evidence stays
unavailable. A passing requested check is not a declaration that the task is
complete. The follow-up guidance is bounded, provenance-linked, and
non-mutating.

## Historical source inspection

Use symbol names to relocate sections if line numbers change. These are
findings from the earlier architecture review. Several proposed improvements
in this table are now implemented in focus or observation, as recorded below.
Do not use the table as a list of unfinished work or as a current runtime audit.

| Source and entry point | Observed behavior | Consequence for the plan |
| --- | --- | --- |
| [core.go](../../internal/intelligence/core.go), `Core.Brief` | Initializes `Symbols` empty; includes package inventory and optional change context. Diagnostics sample the first 32 sorted Go files, with uncertainty. | A bounded overview is not yet anchor-directed context. |
| [core.go](../../internal/intelligence/core.go), `Core.Search`, `normalizeSymbolMatches` | Captures state, requests workspace symbols, normalizes/deduplicates/sorts, and pages stored results. | Existing search is the starting point; task relevance has not been established. |
| [core.go](../../internal/intelligence/core.go), `Core.Symbol` | Collects hover, definitions, references, implementations, diagnostics, and optional type/call facets. Related tests are reference locations ending in `_test.go`. | Resolve enclosing test declarations and select relevant facets before expansion. |
| [core.go](../../internal/intelligence/core.go), `boundSymbolContext` | Stores full overflow, then removes calls, references, implementations, type definitions, definitions, related tests, diagnostics, hover, and finally uncertainties as needed. | Fixed category removal can discard useful relationships; preserve essential uncertainty in the new interface. |
| [snapshot.go](../../internal/intelligence/snapshot.go), `Capture`, `Validate`, `readState` | Capture performs two observations; validation recaptures. Discovery includes Git state, package/build inputs, and file-content hashes. | Reuse internal observation work without weakening content-based freshness. |
| [snapshot.go](../../internal/intelligence/snapshot.go), `remember` | Retains 32 manifests in memory. Semantic reads require the manifest to remain available. | Pin active observations rather than allowing ordinary eviction to fail active reads. |
| [inventory.go](../../internal/intelligence/inventory.go), `inventoryPackages` and inventory assembly | Separately invokes `go list`; parses active source for exports and discovers guidance. | Share discovery and cache derived content conservatively. |
| [change_continuity.go](../../internal/intelligence/change_continuity.go), checkpoint path | Captures state to locate the contract, captures at contract scope, gathers observations, validates, then saves lineage. | Carry coherent state through helpers; contracts should remain optional for navigation. |
| [semantic_gopls.go](../../internal/intelligence/semantic_gopls.go), `goplsProvider.Read`, `mustRestartGopls` | Serializes semantic reads, sends changed-file notifications, and restarts for HEAD, build/provider, or module/workspace configuration changes. | Incremental synchronization already exists; preserve ordering and restart rules. |
| [intelligence_tools.go](../../internal/tools/intelligence_tools.go) | Existing brief/search/symbol text responses return counts and point to canonical structured content. | New text rendering must actually expose useful evidence for text-only consumers. |
| [types.go](../../internal/intelligence/types.go), `Service`, context types | Intelligence types are independent of MCP/LSP; `agentic.context/v1` is frozen. | Add the planned focus contract separately rather than silently redefining v1. |

The live tool inventory is owned by `internal/tools.RegisterAll`. The
[frozen interface scope](../v0.9.0-release-scope.md) records 14 tools, seven
fixed resources, one artifact resource template, and six prompts. Those counts
describe the baseline, not a target for feature growth.

### Documentation review and strategy correction

The review covered the README, documentation authority map, product plan,
decision memo, Context Pack ADR, v1 roadmap, relevant shared contracts, semantic
dogfood, and evaluation/release records. Their strongest foundation is compact
source-grounded context over gopls, snapshot lineage, guarded edits, and executed
verification with explicit limits.

[Semantic dogfood](../../validation/v0.4.0/semantic-dogfood.md) records bounded
responses and snapshot consistency on four workspaces. Its reported aggregate
durations include cold initialization; it explicitly does not establish search
relevance, warm p95 latency, adoption, model accuracy, or token savings.
[Evaluation scope](../v0.8.0-evaluation-scope.md) separates deterministic server
replay from model outcomes. The reviewed [v1 release evidence](../../validation/v1.0.0/release-evidence.md)
does not establish a paid model pilot or comparative agent advantage. These are
historical document statements, not checks rerun in this session.

The original reliability work did not require comparative product claims.
Its implemented foundations remain useful regardless of later evaluation.
The revised north star separately requires evidence of engineer value before
widening usefulness or model-support claims. Reliability release gates and
comparative product decisions must not be conflated.

These are repository observations and design opportunities, not claims that
the current runtime is fast, relevant, or superior to other tools. Those
properties have not been verified in this documentation task.

## Important limits

The following boundaries apply to the implemented foundations and the next
product cycle. Resolve additional design decisions within the slice that
needs them; do not silently expand a frozen contract.

| Area | Current boundary and remaining decision |
| --- | --- |
| Observation and execution | Request-scoped observations, retained manifests, and final validation are implemented. gopls and verification commands still observe the live workspace. Require stability during checks; endpoint equality does not establish isolation from transient concurrent edits. |
| Context entry | Query/ref/position/file/package selectors are implemented. At `7b5111c`, `focusContext` returns no focused context without an explicit selector, even though `Core.Focus` separately computes the diff. Current declaration candidates from that diff are proposed. |
| Implementation support | Typed predicates, enclosing tests/examples, and partial-source evidence exist. They expose supported source facts, not behavioral equivalence, complete dispatch, or intended business requirements. |
| Refresh | Full replacement and private delivered-pack metadata are implemented. Old refs stay stale; failed resolution does not confirm deletion. Delta delivery and semantic before/after obligation differences remain deferred. |
| Review handoff | Reports and applicability assessments exist. `CurrentVerification` retrieves the latest stored report; `assessVerificationApplicability` decides whether it applies. The proposed handoff must carry both result and applicability. |
| Model/host support | The recorded adoption campaigns use Luna/max. Shared instructions and real delivery/recovery checks across other clients/models remain product work, not demonstrated compatibility. |

The behavioral decisions are already fixed by the north star: selection occurs
before expensive expansion; required identity and uncertainty survive budgets;
incomplete source does not license stale semantics; and response omission is
distinct from confirmed source removal. Cache limits must cover bytes and
entries, and active observations must survive ordinary eviction. A refresh
returns complete current selected evidence; no delta reconstruction is required
for this cycle. Historical snapshot-bound artifact cursors remain stale even
when retained pack metadata is used for refresh.

Do not advertise inferred ownership, complete dispatch reachability, semantic
goal enforcement, or a measured speedup. Disk snapshots do not cover unsaved
editor buffers. Upstream gopls MCP already exposes workspace, package,
navigation, and diagnostic tools and has its own internal snapshot model.
Compare against upstream gopls with its actual workflow instructions.
Cross-operation lineage, impact, selection, and reviewable verification
applicability are candidate sources of value, not established differentiation.
Another harness can reproduce these mechanisms; measure engineering outcomes.

The minimal read-only change-consequence and verification-applicability slice
is now implemented. The additive `go_context` MCP tool and `agentic-go context`
command return `agentic.focus/v1`: exact Snapshot Ref, changed files and
declarations, conservative package impact, risks, uncertainty, and a separate
verification applicability decision. Context never runs verification.

New verification runs persist private applicability metadata without changing
`agentic.verify/v1`. Applicability compares local/ref and resolved commit
identity, exact semantic snapshot, package scope, build/provider context, and
the requested race, analyzer threshold, coverage, and closure policy. Legacy
reports without metadata and every mismatch remain visible but non-applicable.
Report outcome is returned independently, including applicable failures.

Declaration-focused selection is now implemented under the same focus
interface. `go_context` and `agentic-go context` accept one query, current
Symbol Ref, or source position. Unique selections return the declaration,
bounded observed excerpts, direct callers with call-site ranges, and enclosing
referenced test/example declarations; ambiguous queries return candidates
before relationship expansion. Selection reasons and uncertainty distinguish
absent, unavailable, unexamined, and budget-omitted evidence. The 8 KiB default
is bounded before optional expansion, and CLI/MCP summaries share one renderer.

The current working tree adds a private, process-local Go retrieval cache for
query selection. It parses observed `.go` files into declaration fragments and
combines deterministic lexical scoring with symbol, receiver, package, and
declaration-kind signals before resolving candidates through the current
gopls observation. It is advisory discovery only: `Core.Search`, the MCP/CLI
surface, schemas, exact snapshot validation, and semantic evidence contracts
are unchanged. Focused retrieval and focus tests are present; repository-wide
validation and held-out relevance evidence remain pending, so this slice is
implemented but not yet release-qualified.

The longer-term branch-aware indexing aspiration, GPT-6 Sol council findings,
and retrieval-specific evaluation plan are recorded in the
[codebase indexing research note](../research/codebase-indexing-retrieval.md).
The current cache is not a durable or branch-aware repository index. The next
product action for that proposal is to evaluate the current path after the
active reliability gates, before adding persistent indexing.

Full-replacement refresh is now implemented. Delivered focus evidence carries
an opaque pack ID backed by private content-addressed metadata containing the
original selection, logical declaration identity, and digests for the evidence
actually delivered. The store retains at most 32 packs and 2 MiB for 24 hours.
`previous_pack_id` reselects against a new observation and returns a complete
replacement with current locations and Symbol Refs. Expiry requires a fresh
request; changed selectors are rejected; ambiguous moves or renames require a
current candidate selection. Failed resolution and budget omission are marked
unavailable and never reported as confirmed deletion. Delta refresh remains
deferred.

The four planned Go relationship families are now implemented sequentially in
the focused evidence layer. Declaration, file, and package selection can return
source-attributed compile-time interface obligations, pointer/value method
sets, embedding, aliases and defined types, generic parameters/constraints and
origins, existing provider-supported implementations, referenced examples, and
bounded lifecycle-shaped calls resolved to the selected receiver type. Each
predicate carries the active Snapshot build context and a limitation statement.
Partial type checking and excluded build variants remain explicit uncertainty;
available syntax/type facts are retained without substituting older semantics.
The evidence does not claim ownership, complete runtime dispatch, guaranteed
cancellation, execution, coverage, or behavioral equivalence. Delta refresh
and general derived caches remain deferred.

Focus v1 stabilization is complete. `docs/schema/focus-v1.json` is the checked-in
machine-readable contract, backed by a representative canonical golden and
schema validation. Capability discovery now advertises the focus schema,
selectors, full-replacement refresh, and typed relationship families. The
post-v1 MCP surface test discovers and calls `go_context` while the frozen
`RegisterAll` golden remains 14 tools. A deterministic workflow test captures
context before an edit, refreshes after a shifted declaration, proves old
Symbol Refs stale, and proves earlier passing verification non-applicable;
CLI JSON/text and MCP structured/text parity are covered separately. The audit
also corrected UTF-8-to-UTF-16 conversion for query-selected declarations and
related-test follow-ups, and made unsupported document-symbol evidence fail
closed without a nil dereference. No delta refresh or new capability is pending
inside this implementation authorization.
Continue to preserve the
[north-star Stage 2 contract](../go-intelligence-north-star.md#5-coherent-observation-and-bounded-derived-state),
the [current freshness contract](../contracts.md#current-semantic-freshness-boundary),
and the [frozen v1 rules](../v0.9.0-release-scope.md#frozen-domain-contracts).
Apply the user's current instruction when resuming. A future explicit product
implementation request is sufficient to begin the next pending stage without
asking again for the same authorization. The private adoption harness links
the existing client-go and grpc-go tasks, validates sanitized records, hashes
transcripts, and produces deterministic condition summaries without changing
the frozen v0.8 corpus. The historical 27-run adoption follow-up is complete:
description alone produced 0/6 focus use, generic prompt guidance produced
6/6, the shipped skill produced 6/6, initial integrated safety was 5/6, and the
scope-wording rerun was 3/3 acceptance-pass, qualifying, and scope-safe. See
the [tracked adoption results](../../validation/v1.0.0/adoption-results.md)
for exact gates, metrics,
identities, and limitations. These historical results do not establish
comparative engineering benefit. Retain focus and full replacement; defer delta
refresh. Raw artifacts remain private and ignored.

## Current next action

The documentation revision is complete. The proposed R1-R4 workflow work and
comparative value remain pending. Continue from the actual remediation state:

1. Inspect the selector-remediation campaign record and relevant current diff.
   Complete only its outstanding engineering gates and six guidance cells,
   then the fresh canonical 18-cell matrix required by material guidance
   changes. Preserve exact campaign identities and failed-call counts. Model
   runs require available clients and their existing execution authorization.
2. After those reliability gates, run the retrieval-specific evaluation in the
   [research plan](../research/codebase-indexing-retrieval.md) against the
   existing process-local retrieval path. Separate snapshot, candidate ranking,
   and gopls costs. Do not implement a persistent index or make scale claims
   before the measurements.
3. Once that reliability milestone is resolved and implementation is requested,
   follow [R1 and R2](../go-intelligence-north-star.md#next-delivery-cycle):
   current declaration entry from the diff, then the complete API-change and
   review handoff. Use existing typed evidence, renderers, and schemas. Record
   any compatibility decision before changing behavior.
4. Exercise the workflow with supported clients/models, then run the separately
   scoped value screen when authorized. Evaluate actual engineering effort and
   decisions; do not use call adoption as its success criterion.

Source starting points are `internal/intelligence/focus.go` (`Core.Focus`,
`focusContext`, `assessVerificationApplicability`), `typed_relationships.go`
(`focusFileOrPackage`), `focus_action.go`, and `unified_verification.go`
(`CurrentVerification`). Recheck symbol locations before editing.
The [north star](../go-intelligence-north-star.md) owns the
acceptance cases and model/skill contract; this list is sequencing, not new
authorization for code changes, model spending, or publication.

## Current canonical campaign and pending remediation

The maintainer-supplied 2026-09-24 canonical summary records 18 Luna/max runs:
two pinned scenarios, three conditions, and three repetitions per cell. All
18 qualified and passed acceptance. These reported results are not rescored
by the documentation revision.

| Condition | Runs | Context evidence-use signal | Refresh | Median duration (ms) | Median tool calls |
| --- | --- | --- | --- | ---: | ---: |
| Baseline | 6 | 0/6 | 0/6 | 206,861 | 17 |
| MCP-only discoverability | 6 | 0/6 | 0/6 | 249,512 | 22 |
| Guidance | 6 | 6/6 | 6/6 | 308,899 | 26 |

Guidance recorded zero scope violations and zero accepted stale evidence.
There were five failed focus calls: one low-level `invalid_input` and four
low-level `provider` labels. The private transcript audit classified all five
as selector misuse: an altered Symbol Ref and invalid declaration coordinates.
No provider defect was established. The current remediation preserves those
raw outcome categories and adds bounded private cause/recovery classifications.
See its [record](../../validation/v1.0.0/adoption-remediation-2026-09-24.md)
for implementation and pending rerun status.

The pooled guidance/baseline duration difference is about 49%, but per-scenario
medians differ by about 17.5% for client-go and 4.8% for grpc-go. These are
descriptive, unpaired small-sample summaries; none identifies causal tool cost.
The evidence-use signal is temporal ordering, not a scored better decision.
The older campaigns below must not be pooled with this matrix.

## Historical integrated diagnostics

The post-guidance regression used six integrated Luna runs, with three
repetitions on each of two scenarios. All six qualified, passed acceptance,
stayed within scope, and required no operator intervention. All six used
`go_context`, refreshed after edits, and recorded evidence use. Five focus
calls failed: client-go had three `invalid_input` failures reporting invalid
symbol references; grpc-go run 1 had two `stale_snapshot` failures reporting
an observed semantic location absent from the snapshot manifest. grpc-go runs
2 and 3 had no failed focus calls. This shows continued workflow adoption
while exposing failure categories. It does not establish improved quality,
correctness, productivity, speed, or product value. External and multi-model
evaluation remain pending.

The failure audit classified the three client-go invalid-input calls as agent
misuse or ref reconstruction: each failed ref had an invalid version field or
inconsistent identity, while valid query-issued refs succeeded. The two gRPC
stale-snapshot calls were repeated without an intervening edit; workspace-symbol
search returned a location inside the workspace but outside the active snapshot
manifest. The provider now omits such unbound workspace locations as bounded
uncertainty while continuing to propagate stale errors for manifest entries that
changed or disappeared. Strict stale rejection and the frozen public contracts
remain unchanged.

Focused tests cover malformed refs, current observation membership, and omitted
workspace-symbol uncertainty. The private audit remains at
`/Users/ashwin/agentic-go-eval-audits/20260924-failure-verdicts.md`.

The six-run integrated Luna rerun completed with 6/6 qualification, 6/6
acceptance, zero scope violations, zero operator interventions, zero failed
focus calls, and complete refresh and evidence-use signals. Skill discovery was
6/6. Median duration was 411,537 ms, median tool calls were 31, and median
transcript evidence was 450,293 bytes. These are diagnostic regression values,
not evidence of improved speed, correctness, productivity, or product value.
External and multi-model evaluation remain pending.

## Historical integrated trace review

The six integrated traces have been reviewed. No transcript proves that
`go_context` improved the necessary code edit. In gRPC run 3, refreshed context
showed broader affected scope and the agent explicitly said this prompted
`./...` verification. That attempt was incomplete due to the output cap;
focused verification later passed after a stale-snapshot rejection. Client-go
runs made 3-4 context calls each, including ambiguous or unhelpful initial
selections. gRPC runs made 4-5 calls each; runs 1 and 3 repeated verification
after incomplete or stale results, and run 2 had a failing root test run and a
failing narrowed observability rerun.

The bounded guidance improvement is recorded in the
[`agentic-go-context` skill](../../.agents/skills/agentic-go-context/SKILL.md):
narrow an ambiguous result using returned current candidates instead of
repeating the broad query; finish each batch of edits and formatting before
refreshing; and refresh again after further edits or stale-snapshot rejection
while avoiding redundant refreshes when the snapshot is unchanged. This evidence does not
establish causal edit-quality or product value. The combined skill and MCP
workflow observations do not establish MCP-alone causality. Keep the studies
as regression evidence, not fresh proof of product superiority. External and
multi-model evaluation remain pending. Defer any new release label until a
reliability milestone passes.

## Standing continuation instruction

After every meaningful implementation milestone, architecture decision, or
blocker, update this handoff in the same change. Keep implemented behavior,
approved direction, and unverified proposals visibly distinct. Replace stale
status rather than appending a conversation transcript. Record relevant source
paths, decisions, evidence actually gathered, limitations, and the exact next
step. Do not add benchmark or evaluation claims unless a future request
explicitly includes them. Context gathering does not execute checks or prove
correctness.

## Copy-paste continuation prompt

Use this prompt only when the user separately authorizes further work:

```text
Read AGENTS.md, docs/go-intelligence-north-star.md, and this handoff. The first
customer is a Go engineer using agents; the first complete workflow is a
cross-package API/interface change through an inspectable handoff. Observation,
typed context, full-replacement refresh, and verification applicability exist.
Inspect current selector-remediation work and its campaign record before
choosing the next unfinished step. Do not repeat completed work or treat plans
as shipped capabilities. Continue only the user's requested scope; model runs
and publication need their respective authorization. Preserve frozen v1 and
agentic.focus/v1. Delta refresh, general caches, expanded refactoring, and
speculative test selection remain deferred. Preserve tags and public history.
```
