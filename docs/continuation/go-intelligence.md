# Go intelligence continuation handoff

## Purpose

This document preserves the current product understanding for a future agent
working in another account or session. It is independent of conversation
history, account identity, private memory, and previous tool output.

The user’s end goal is a flagship Go MCP and language-server/code-navigator
experience usable through any coding agent. It should materially remove the
repeated investigation normally required to understand and change unfamiliar
Go code: the agent should receive compact, source-grounded context for the
next change, understand implementation obligations and existing examples,
retain orientation after edits, and see what changed. “1000x” and quality
rankings express ambition, not measured or promised results.

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

## Current status

As of 2026-09-22, this branch is based on the signed `v1.1.0` release. **Stage
2, coherent observation**, and the additive focus slice are shipped in that
release.
The observation-correctness follow-up closed the source-confirmed gaps recorded
by the Astra review.
The change preserves the existing exact content-based Snapshot Ref and v1
interfaces while threading a private request-scoped observation through the
intelligence paths. The implemented additive slice includes `go_context`,
`agentic-go context --format text|json`, and `agentic.focus/v1`; these remain
post-v1 capabilities and are not frozen v1 interfaces. The broader roadmap
stages remain partially implemented or pending and must not be inferred as
complete from this slice.

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
Those historical checks qualify the v1.1.0 release work; they do not qualify
later changes. The current v1.2 candidate separately passed the full
repository gates (`go test ./...`, `go test -race ./...`, `go vet ./...`,
`go build ./...`, and `git diff --check`). No benchmark, evaluation,
publication, tag, or push was performed.

Stage 2 does not introduce a public interface or a general derived cache.
Derived parsing/semantic caching, useful-context selection, richer
relationships, refresh, and verification lineage remain later-stage work.

## Current post-v1.1.0 follow-up

The current `codex/v1.2-reliability` follow-up narrows focus semantic expansion
by the selected declaration kind. Function and method selections do not request
type definitions, and non-callable declarations do not request call hierarchy.
Skipped facets are reported as unexamined rather than as examined-and-absent
evidence; incomplete provider evidence is not reported as absent. This is a
bounded reliability fix for observed provider failures; the public MCP
inventory and `agentic.focus/v1` schema remain unchanged. Focused package
validation and full repository gates have passed. The prerequisite snapshot
input fix is committed locally as `5ff6902`; the focus follow-up remains the
current gated slice.

## Findings recorded from the documentation and targeted source inspection

Use symbol names to relocate sections if line numbers change. These are
recorded static inspection findings from the earlier review, not a runtime
audit or source inspection repeated during the documentation revision.

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

The initial review recommended a comparative pilot. The user then explicitly
redirected the strategy toward material product behavior, not eval/benchmark
milestones. The resulting plan follows that correction: context selection,
coherent reads, Go-specific relationships, and explicit refresh. The user chose
additive interface evolution and focused correctness checks when asked.

These are repository observations and design opportunities, not claims that
the current runtime is fast, relevant, or superior to other tools. Those
properties have not been verified in this documentation task.

## Important limits and exact next action

The approved plan is architectural direction, not an already-frozen wire
specification. Implementation must resolve and record these local facts in the
stage that needs them:

| Stage | Facts to inspect and decisions to record |
| --- | --- |
| 2. Coherent observation | Implemented: request-scoped ownership, atomic retain-and-pin, release-once leases, captured-or-manifest-verified reads, guidance identity, strict admission, and existing provider ordering. The provider still observes the live disk workspace through gopls; this is attributable evidence with final validation, not transactional filesystem isolation. |
| 3. Useful context | Exact input/output fields, existing budget ceiling, combined MCP rendering costs, required-envelope failure behavior, and interoperable current Symbol Refs. |
| 4. Implementation support | Provider capabilities and typed predicates for method sets, embedding, aliases, generic instantiations, enclosing tests, lifecycle sites, and partial-source results. |
| 5. Refresh | Private delivered-manifest storage and retention, logical identity versus content revision and locator, current-reference issuance, and evidence that can confirm deletion. |
| 6. Integration | Explicit additive inventory expectations and verification references tied to the observed snapshot. |

The behavioral decisions are already fixed by the north star: selection occurs
before expensive expansion; required identity and uncertainty survive budgets;
incomplete source does not license stale semantics; and response omission is
distinct from confirmed source removal. Cache limits must cover bytes and
entries, and active observations must survive ordinary eviction. Applying a
delta to the previous delivered pack must reconstruct the current selected
pack, including locators, omissions, and uncertainty. Historical snapshot-bound
artifact cursors remain stale even when retained pack metadata is used for
refresh.

Do not advertise inferred ownership, complete dispatch reachability, semantic
goal enforcement, or a measured speedup. Disk snapshots do not cover unsaved
editor buffers. Upstream gopls MCP already exposes workspace, package,
navigation, and diagnostic tools and has its own internal snapshot model.
Differentiate through cross-operation evidence lineage, impact, selection, and
verification applicability rather than duplicate navigation wrappers. The
reviewed upstream source disables its broad `go_context` tool because of
context-size/redundancy concerns; this supports bounded explicit context, not
a claim of unique navigation.

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

Full-replacement refresh is now implemented. Delivered focus evidence carries
an opaque pack ID backed by private content-addressed metadata containing the
original selection, logical declaration identity, and digests for the evidence
actually delivered. The store retains at most 32 packs and 2 MiB for 24 hours.
`previous_pack_id` reselects against a new observation and returns a complete
replacement with current locations and Symbol Refs. Expiry requires a fresh
request; changed selectors are rejected; ambiguous moves or renames require a
current candidate selection. Failed resolution and budget omission are marked
unavailable and never reported as confirmed deletion. Delta refresh remains
deferred. The next separately authorized extension is delta refresh.

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
the frozen v0.8 corpus. The 27-run adoption follow-up is complete: description
alone produced 0/6 focus use, generic prompt guidance produced 6/6, the shipped
skill produced 6/6, initial integrated safety was 5/6, and the scope-wording
rerun was 3/3 acceptance-pass, qualifying, and scope-safe. Provider failures
remain the next reliability issue. See the [tracked adoption
results](../../validation/v1.0.0/adoption-results.md) for exact gates, metrics,
identities, and limitations. Retain focus and full replacement; defer delta
refresh. The next slice is release hardening, instruction-surface
discoverability, and provider-failure investigation. Raw artifacts remain
private and ignored.
This handoff predates the separately authorized publication workflow. Public
publication must preserve existing tags and history, and does not create a new
release or claim that the paid comparison ran.

## Standing continuation instruction

After every meaningful implementation milestone, architecture decision, or
blocker, update this handoff in the same change. Keep implemented behavior,
approved direction, and unverified proposals visibly distinct. Replace stale
status rather than appending a conversation transcript. Record relevant source
paths, decisions, evidence actually gathered, limitations, and the exact next
step. Do not add benchmark or evaluation claims unless a future request
explicitly includes them.

## Copy-paste continuation prompt

Use this prompt only when the user separately authorizes further work:

```text
Read docs/continuation/astra-understanding.md and this handoff first. Observation,
verification applicability, declaration selection, and full-replacement refresh
and focus-v1 stabilization are complete. Inspect the current diff and choose a
new explicitly authorized objective. Delta refresh, general derived caches,
expanded refactoring, speculative test selection, and release creation remain
outside the completed scope. Publication is separately authorized only when a
maintainer explicitly requests it; preserve existing tags and public history.
```
