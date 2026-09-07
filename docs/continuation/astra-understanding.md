# Astra analysis handoff

Status: authoritative continuation note, reviewed 2026-09-05 at HEAD
`284df97`. This records source inspection and product reasoning. It is not a
test report, benchmark, implementation approval, or proof of runtime behavior.

## Fast path

**Verdict: NARROW. Confidence: 88/100.** Build a small, dependable Go
evidence compiler for coding agents: bind observations to an explicit
repository state, select bounded source-grounded context, explain supported
change consequences, and connect the result to applicable verification.

The product boundary is not “more symbol tools.” Navigation is mostly a
normalization of gopls. The durable value is attributable evidence that can
change the agent's next action: what was observed, what relationship was
found, what may be affected, which checks apply, and what remains unknown or
stale.

Update after the 2026-09-05 Slice 1 implementation: Stage 2 observation
correctness is complete in the uncommitted worktree. The two risks below are
retained as the review record and are resolved by the current implementation:

- `Core.Symbol` now installs release immediately after successful acquisition,
  before position resolution.
- Source above the 4 MiB capture cap is accepted only when a contained reread's
  kind and digest match the observation manifest; the verified bytes are the
  bytes used by the caller.

Focused tests now cover both paths, including same-size and A→B→A rewrites,
Brief diagnostic forwarding, guidance mismatch, and lease release.

The next slice repairs observation correctness only. Its decision is whether
every local fact is tied to the observed state, or the service refuses to
answer. Do not add focus, refresh, caches, or broad relationships in that
slice. The worktree was already dirty in the following tracked paths:
`docs/README.md`, `docs/contracts.md`,
`docs/phase-3-gopls-navigation-resources-prompts.md`,
`internal/intelligence/{core,core_test,inventory,inventory_test,semantic,
semantic_gopls,snapshot,snapshot_test}.go`; continuation and north-star docs
were untracked at inspection. Inspect current status and only the relevant
diff because this dated state can drift.

After `AGENTS.md`, fresh agents should read this file and
`docs/continuation/go-intelligence.md` first, then the
north-star and only the frozen scope/schema or source files relevant to the
slice. Do not repeat the full historical review unless touching that area.

## Product judgment and comparison

**INFERENCE:** The repository has stronger mechanics around change impact,
verification evidence, snapshot lineage, guarded edits, and continuity than
around agent-oriented semantic selection. Evidence includes
`internal/changeimpact/impact.go:computeImpact`,
`internal/verification/{engine,execution,report}.go`, and
`internal/intelligence/unified_verification.go`.

**INFERENCE:** A reliable evidence compiler is the strongest realistic path.
“Reliable” means explicit snapshot identity, provenance, deterministic bounded
selection, honest omissions, and stale/refresh behavior. It does not mean
immutable filesystem isolation or complete semantic truth.

Direct frontier-model repository reading is flexible but leaves state identity,
selection discipline, omission meaning, and verification applicability to the
model. `go/parser` gives syntax; `go/types` semantic facts; `go/packages`
build-aware loading; gopls navigation/diagnostics/editor synchronization;
`go test`, `go vet`, and Staticcheck checks. Generic MCP wrappers expose
commands, files, and symbols. An agent can maintain its own history.

**PROPOSAL:** This layer earns a place only by composing those facts with one
observed state, deterministic context priorities, change-impact closure,
provenance, uncertainty, and a verification handoff saying whether a report
applies to the current edit. A raw gopls adapter does not reliably provide
that composition. An agent may reproduce it manually; the product makes the
invariants inspectable and repeatable.

**UNKNOWN:** Agent usage, acceptable latency/context cost, and improvement in
completed changes versus shell/gopls workflows. No model, adoption, token,
speed, or comparative-agent claim is justified by the reviewed evidence.

## Capability decisions

| Capability | Decision | Evidence and confidence |
| --- | --- | --- |
| Snapshot observation/provenance | **KEEP**, high | `snapshot.go:observe`, `remember`, `pin`, `manifestLease`; required for coherent evidence, after the two fixes. |
| Conservative change impact | **KEEP**, high | `changeimpact/impact.go:computeImpact` follows imports, test/x-test imports, embedding, and package closure; oversized analysis is incomplete. |
| Verification applicability/evidence | **KEEP**, high | `verification/engine.go:Collect`, `execution.go`, `report.go:Finalize`, `unified_verification.go` connect scope, checks, findings, and lineage. |
| Bounded useful context | **KEEP, narrow**, medium | North-star policy is plausible; current `boundSymbolContext` drops categories mechanically and Brief is not anchor-directed. |
| Semantic navigation | **NARROW**, medium-high | `semantic_gopls.go` mostly wraps/normalizes gopls. Keep as input; add relationships only when they change a decision. |
| Continuity/refactor support | **KEEP, defer breadth**, medium-high | `change_continuity.go` and `refactor.go` preserve lineage; goals are prose and cross-process coordination is limited. |
| Derived caches | **DEFER** | Stage 2 needs no general derived cache; introduce one only with sufficient identity and a demonstrated later-stage need. |
| Ownership inference, complete dispatch reachability, semantic goal enforcement | **DELETE from claims** | Reflection, external consumers, generated/build variants, and prose goals make universal claims unsound. |
| More raw MCP tools, speculative test selection, broad graph infrastructure | **DEFER/delete** | Existing tools cover much of the raw surface; tool count does not establish a changed engineering decision. |

## Confirmed Stage 2 risks

### Manifest lease retention

**FACT:** In `internal/intelligence/core.go:Core.Symbol`, observation is
declared, position observation and workspace resolution run, and the common
`if err != nil { return ... }` path occurs before `defer observation.release()`.
A valid but out-of-range position can reach this path after `c.observe` pins a
manifest.

**CAUSE:** `snapshotObservation.release` is the path that decrements
`Snapshotter.active` through `manifestLease.release`. A failed position request
can leave an entry active. Repeated failures can consume the 32-entry/8 MiB
retention budget and make admission fail closed. This is retention/availability
pressure, not evidence of a retained 4 MiB source buffer.

**LIMIT:** The cancellation test covers `Core.Search`, not this Symbol branch;
no runtime reproduction was performed. The code path itself is confirmed.

### Captured-source fallback

**FACT:** `snapshot.go:readState` retains Go source only within 4 MiB.
`core.go:sourcePositionObserved`, `inventory.go:exportedInventoryFromSources`,
and `semantic_gopls.go:goplsReader.sourceLocation` reread disk on a miss.
Diagnostics and uncertainty helpers also have independent reads.

**CAUSE:** A source can be captured as A, changed to B, and reread as B.
Persistent B is rejected by final validation; a return to A before validation
can evade that check while the answer contains evidence derived from B.
The cap counts retained source
bytes, not peak allocation or total process memory.

**LIMIT:** The fallback is confirmed statically; cap frequency and an A→B→A
race were not measured. Managed gopls synchronization does not make disk reads
an immutable overlay.

## Adversarial boundary

| Failure | Consequence | Mitigation/residual risk | Blocks boundary? |
| --- | --- | --- | --- |
| Stale snapshot/ref | Agent acts on obsolete evidence | Content identity, expected snapshot, final validation, explicit stale errors; changes can still occur between reads | No, if refusal is visible |
| Partial typing, build tags, cgo | Variant-specific or missing relationships | Preserve loader/build config; mark incomplete/unavailable | No scoped; blocks universal claims |
| Interfaces, reflection, external consumers | Static closure misses runtime users | Report direct static relationships and uncertainty | No |
| Generated code | Wrong edit target or omitted dependency | Identify generated/unavailable paths; preserve refactor limits | No |
| Budget omission/duplicate evidence | Agent overtrusts truncated/repeated facts | Deterministic priorities, dedupe, mandatory identity/uncertainty, examined/omitted/unavailable states | No |
| Cache invalidation/concurrency | Cross-request contamination/admission failure | Content/scope/provider keys, atomic retain-and-pin, release-once leases, verified fallback bytes | No for Stage 2 |
| Provider restart/unsaved editor buffer | Disk differs from editor state | Track provider/config changes and state source; editor overlay needs explicit capture | No if provenance says disk-only |
| Agent ignores tool | No product effect | Compact action-linked evidence; usage remains empirical | No, but can falsify value |
| Verification mismatch | Green result trusted for another edit/scope | Match snapshot/base/scope/check policy; expose absent/incomplete evidence | No if applicability is explicit |

## Three next slices

1. **Finish observation correctness.** Repair `Core.Symbol` release ordering;
   make source misses manifest-aware or fail/refuse; route Brief diagnostics
   and local helpers through the observation policy; preserve strict inventory
   errors, cancellation, provider ordering, bounds, and frozen v1. Focused
   checks: position-error lease release, cap misses after edits, Brief
   forwarding, A→B→A conversion, and active admission/replacement. Stop when
   each local read is captured, manifest-checked, or explicitly unavailable.
   Question: can the agent trust local evidence identity?

2. **Expose change consequences and verification applicability.** Reuse
   `computeImpact`, `CurrentVerification`, report types, and snapshot fields
   behind a minimal additive read-only surface. Check matching
   base/scope/snapshot, check policy, absent evidence, oversized impact, and
   text/structured parity. Do not run checks implicitly. Question: does output
   change whether the agent inspects, edits, reruns, or trusts a report?

   **Implemented 2026-09-05:** `agentic.focus/v1`, `go_context`, and
   `agentic-go context` expose this read-only boundary. New reports carry
   private applicability metadata; older reports fail closed as non-applicable.
   Applicability is independent of pass/findings/incomplete outcome.

3. **One declaration-focused context pack with explicit refresh.** Reuse
   `Core.Search`/`Core.Symbol`; preserve call ranges; add callers, ambiguity,
   excerpts, enclosing test declarations, current refs, and budget reasons.
   Use full replacement first. Test Unicode, receiver ambiguity, helper-vs-test,
   shifts/deletions, partial typing, and omission semantics. Question: does
   selected context beat repeated repository reading on a concrete next edit?

   **Selection implemented 2026-09-05:** query, current Symbol Ref, and source
   position anchors now produce bounded excerpts, direct callers with source
   ranges, and enclosing referenced tests/examples. Ambiguity returns candidates
   without expansion. Full-replacement refresh remains the next slice.

## Claims to forbid and open questions

Forbid complete dispatch reachability, inferred ownership, semantic goal
enforcement, universal safety, immutable filesystem snapshots, editor-overlay
coherence without capture, measured speed/token/model/adoption/market claims,
“all affected tests,” and treating historical evidence as current validation.

Open: focus/context wire shape; source-miss policy; how to route all diagnostics
through the observation; how to expose applicability of “latest” verification; cross-process
contract coordination; provider/editor overlay semantics; and whether agents
use the evidence often enough to justify its cost.

## Prompts for a fresh agent

Analysis only:

```text
Read AGENTS.md, docs/continuation/astra-understanding.md, and
docs/continuation/go-intelligence.md first.
Treat FACT, INFERENCE, PROPOSAL, and UNKNOWN exactly as marked. Reinspect only
the listed symbols and current diff. Decide whether the next slice changes an
agent's next engineering action. Do not edit or run validation; do not claim
runtime behavior without evidence.
```

Historical implementation authorization, already completed:

```text
Implement only slice 1, observation correctness, from the Astra handoff.
Repair Core.Symbol lease release and captured-source fallback semantics while
preserving frozen v1 interfaces, strict errors, cancellation, provider order,
freshness, and bounded retention. Add/run focused checks for this slice only.
Do not use this slice prompt for current work: it intentionally excluded
focus/refresh APIs, broad validation, commits, and publication. Use the current
north-star and continuation handoffs for any separately authorized work.
```

## Evidence map

Intent: `docs/go-intelligence-north-star.md`, `docs/contracts.md`,
`docs/v0.9.0-release-scope.md`, `docs/adr/0002-context-pack-boundary.md`.
Implementation: `internal/intelligence/{core,snapshot,inventory,semantic_gopls,
types,unified_verification,change_continuity,refactor}.go`,
`internal/changeimpact/impact.go`, and
`internal/verification/{engine,execution,report,baseline}.go`.
Historical evidence: `validation/v0.4.0/semantic-dogfood.md`,
`validation/v0.8.0/summary.md`, `validation/v1.0.0/release-evidence.md`.
These support historical bounded behavior and limitations only.

## Details to reuse instead of rediscovering

### Observation and semantic implementation

These are source findings at the recorded worktree, not promises about a later
checkout. Symbol names are the durable lookup anchors.

- `Core.Brief` calls `c.semantic.Read` for diagnostics, not
  `c.readObservation`. `goplsProvider.Read` supplies nil captured sources;
  `ReadObservation` supplies the map. Thus Brief location conversion may read
  disk even below the cap. `fileSemanticUncertainties` reads disk independently;
  `inventoryGuidance` walks and reads guidance independently. Slice 1 must
  account for these helpers, not merely the three map-miss fallbacks.
- `inventoryPackagesForObservation` caches a separate strict
  `go list -json -mod=readonly` call. Capture's `goPackageInputs` still runs its
  own `go list -e -json` per capture pass. Only Brief consumes the observed
  inventory helper. This is not reuse of capture-time discovery; merging those
  paths must preserve the distinct strict error behavior.
- `readState` hashes all selected content, but retains Go source only within
  4 MiB. `contentRecord` reads the full file before retention selection;
  `manifestSize` counts strings, not Go object overhead or RSS. Raising caps
  does not establish coherence or a process memory bound.
- `Snapshotter.observe` remembers then pins under separate lock acquisitions.
  Under pressure another request can evict between them; this is a possible
  fail-closed admission window, not proof of stale evidence. Active leaked
  leases, rather than garbage collection of source bytes, cause the main risk.
- gopls receives URI/change notifications, not an immutable overlay containing
  observation bytes. Captured bytes currently support local source conversion.
  Preserve serialization, notification order, restart rules, and advancing
  `p.last` only after successful callbacks. Neither double capture nor final
  validation proves filesystem transaction isolation or immutable provider state.
- `goplsReader.Implementations` now converts normalized UTF-8 byte locations
  through observation-backed source before issuing UTF-16 LSP follow-up
  lookups. Its cap and resolution failures still both increment `Omitted`,
  which does not prove why each item was unavailable.
- `goplsReader.Calls` retains incoming call-site ranges internally for focused
  evidence. `_test.go` references are resolved to enclosing test, benchmark,
  fuzz, or example declarations; they remain navigation evidence and never
  imply test execution or coverage.
  No obligation extractor using `go/types` was found in intelligence.
- Current budgets gather before trimming and can drop uncertainty. New focus
  policy must select before expansion and preserve essential uncertainty.
  Current text adapters return counts; useful text evidence remains proposed.

Focused tests already inspected: `core_test.go` contains
`TestCoreSearchUsesOneObservationForSemanticRead`,
`TestCoreSearchReleasesObservationAfterSemanticCancellation`, and
`TestSourcePositionRejectsSplitUTF8Encoding`. None establishes Symbol error-path
release. `snapshot_test.go` covers pin/eviction/admission/replacement;
`semantic_gopls_test.go` covers notification order, restart failure, and Unicode
forward conversion. Passing historical tests does not close the gaps above.

### Verification, continuity, and applicability

- `changeimpact.computeImpact` computes conservative reverse package closure
  from imports, test imports, and external-test imports in the selected current
  graph. It considers embed inputs and module/workspace metadata. Oversized
  closure stays whole and marks analysis incomplete; it does not execute an
  arbitrary prefix. This is not individual-test selection or external-consumer
  reachability. Inactive/unmapped Go files remain uncertainty; old/deleted
  dependency topology is not comprehensively reconstructed.
- `verification.Collect` plans required evidence and runs affected checks only
  for complete impact. `executeGoTest` uses `-json -count=1 -timeout=60s` with
  explicit target packages; coverage/race are scoped options.
  `compareAnalyzerFindings` distinguishes introduced/existing/unknown and does
  not promote ambiguous matches to introduced.
- `report.Finalize` evaluates full evidence before display truncation, with
  incomplete > findings > pass precedence. Stable ordering does not make test
  durations or outcomes deterministic across runs.
- `Core.Verify` captures with base/scope, collects, checks commit identities,
  attaches snapshot/provider/provenance, validates freshness, then saves.
  `CurrentVerification` retrieves the latest report for repository identity;
  it does not compare that report's snapshot to current source. Content-addressed
  report integrity is different from current applicability.
- Snapshot identity includes requested base, resolved base/merge base, scope,
  build inputs/configuration, and provider identity. A base-less Search ref is
  not automatically the same snapshot as base-aware Verify. Validate original
  refs in their context before issuing explicitly new-context refs.
- Applicability must also match requested checks/policy. Environment, toolchain,
  cgo/native dependencies, network, nondeterminism, and post-capture edits limit
  what a green report establishes. No report proves universal safety.
- `recordContextProvenance` stores operation/schema/snapshot, not actual
  delivered pack identity/content. Recent provenance is a process-local ring
  of 64 entries, not complete persisted agent history.
- `Core.Checkpoint` preserves explicit lineage; goal prose is not enforced.
  The state gate is process-local. Contract/report/latest-pointer saves are
  individually atomic, not a cross-store transaction or cross-process CAS.
  General artifact retention has no observed quota/expiry policy. These limits
  defer richer continuity; they need not block read-only change evidence.
- Guarded refactor preview/apply already uses exact snapshots/preimages and
  recovery journaling for existing contained non-generated files. Maintain
  this behavior; expanding refactoring is not the immediate differentiator.

### Concrete historical evidence and external baseline

- `validation/v0.2.0/summary.md` and
  `validation/v0.2.0/grpc-go-reverse-impact.json`: the historical ALTS change
  reached reverse importer `credentials/google`; 45 importer tests passed,
  while the directly changed package had zero executed cases and changed
  coverage was 0/1. This demonstrates a useful qualification of a green summary,
  not measured agent improvement. A broader 58-package closure exceeded output
  limits and became incomplete, exposing a real usability constraint.
- v0.4 semantic dogfood covers four workspaces, bounded 8 KiB briefs, and
  chosen search-to-symbol consistency. It does not prove relevance.
  v0.8 records deterministic historical task oracles/replay; v1 evidence is
  historical release qualification, not this dirty worktree's validation.
  The reviewed release records use the older personal module path; current
  `go.mod` uses `github.com/agentic-mcps/go`.
- [Official gopls MCP documentation](https://go.dev/gopls/features/mcp)
  distinguishes editor-attached unsaved-buffer state from detached disk state.
  [Navigation documentation](https://go.dev/gopls/features/navigation)
  already covers semantic navigation; call hierarchy is static and references
  depend on build configuration. Do not claim gopls lacks snapshots.
- The reviewed [upstream MCP source at c418235](https://go.googlesource.com/tools/+/c418235894f89f8a19e9990196efd285da936a9e/gopls/internal/mcp/mcp.go)
  enables workspace/package API/diagnostics/rename/symbol references/search/file
  context/vulnerability tools. Its broad `go_context` is disabled because import
  expansion can produce excessive context. This is version-specific evidence,
  not an assertion about every released version. Raw MCP navigation is therefore
  a weak differentiator; disciplined change evidence is the proposed boundary.

### Smallest durable boundary and falsification

**PROPOSAL:** Accept explicit base/scope and declaration/query/ref/position/path
anchors, budget, expected snapshot, and optionally a previous selection.
Resolve ambiguity before expensive expansion. Emit source excerpts with
digest/location/provider/build provenance; prioritize anchors and changes,
signatures, obligations, direct callers, enclosing tests, then broader context
using stable ties and deduplication. Bound work before expansion. Distinguish
examined-absent, unexamined, unavailable, and gathered-but-omitted evidence.
Fail or require narrower selection when identity and essential uncertainty
cannot fit. Go relationships should use compiler/provider facts, including
method sets and embedding where supported, rather than guessed ownership.

Refresh should initially return a full replacement with current refs. Keep
logical identity, content revision, and locator separate; ambiguous moves need
selection, expiry needs fresh capture, and omission never implies deletion.
Defer delta reconstruction and general derived caches. Verification runs only
when explicitly requested; read-only applicability can guide whether to rerun.

Full-replacement refresh is implemented through opaque focus pack IDs. Private
content-addressed metadata retains the original selection, selected logical
identity, and the manifest of evidence actually delivered under 32-entry,
2-MiB, and 24-hour retention bounds. Refresh observes the repository again,
reselects the declaration, and issues only current locations and Symbol Refs.
Expired metadata requires a fresh request; changed selection is rejected;
ambiguous resolution requests a current candidate. Resolution failure and
budget omission remain unavailable evidence and never confirm deletion. Delta
refresh remains deferred.

Typed relationship expansion is implemented for declaration, active-file, and
active-package selection. Observation-backed active source supplies interface
obligations, pointer/value method sets, embedding, alias/defined-type and
generic facts, provider-supported implementations, referenced examples, and
bounded resolved lifecycle-shaped calls. Every emitted predicate includes a
workspace source location, active build context, evidence basis, and limits.
Partial typing and inactive build variants remain explicit uncertainty. No
relationship is presented as ownership, complete runtime dispatch, guaranteed
cancellation, executed evidence, coverage, or behavioral equivalence. Delta
refresh and general caches remain deferred.

Focus v1 is stabilized and locally qualified as an additive post-v1 capability.
The checked-in JSON Schema and
representative golden cover the public result, including refresh, applicability,
typed evidence, provenance, and omission states. Capability discovery exposes
the schema, selectors, replacement refresh, and relationship families without
changing the frozen 14-tool `RegisterAll` surface. Deterministic coverage now
walks before-edit context, post-edit replacement refresh, stale old Symbol Refs,
stale earlier verification, CLI JSON/text, and MCP structured/text delivery.
The public-semantics audit corrected Unicode follow-up positions and an
unsupported-capability nil path. Delta refresh and broader caches remain
deferred and were not needed for this qualification. The earlier private paired
GPT-5.6 Luna pilot remains historical evidence: 20/20 runs passed acceptance
and qualification, with zero scope violations and interventions. Its corrected
obligation coverage was baseline 21/30 versus focus 18/30. Focus delivery was
healthy in 10/10 runs, but `go_context` usage was 0/10, so the pilot did not
establish causal value or support performance, token, reliability, or adoption
claims. The later 27-run adoption follow-up found 0/6 use with description-only
discoverability, 6/6 with generic guidance, and 6/6 with the shipped skill; it
established observed workflow use and safety, not causal engineering
improvement. Retain focus, keep full replacement, and do not build delta yet.
Raw run artifacts remain private and ignored; the tracked reports contain only
sanitized evidence.

**INFERENCE:** External state still needs observation as models improve, but
this engineering is replicable, not a unique moat. Verdict confidence is a
strategy judgment, not a statistical estimate. Falsifiers: agents repeatedly
redo the same investigation despite using results; freshness costs overwhelm
the saved work; or upstream/client composition supplies the same dependable
evidence with less integration burden. A benchmark campaign is not the next
milestone; each slice must change a concrete next engineering action.
