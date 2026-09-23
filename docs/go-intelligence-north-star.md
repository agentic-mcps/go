# Go intelligence for navigating, generating, and changing code

Status: approved product direction, revised 2026-09-23. `v1.2.1` (tag
`67f54b7`) is the latest released baseline. The current
`codex/v1.2-reliability` branch contains unreleased post-v1.2.1 work. The
product is a deterministic, snapshot-bound evidence compiler for Go coding
agents. Its wedge is a dependable edit, refresh, verify, inspect loop that
helps an agent know what to reconsider before treating a change as complete.

The `go_context`, `agentic-go context`, and `agentic.focus/v1` capabilities are
post-v1 additions. Slice 1A adds next-action guidance to existing MCP text
using existing structured verification data. Slice 1B refines the private
evidence projection with cause-specific guidance. Both preserve the frozen
public MCP inventory and `agentic.focus/v1` schema. See the
[continuation handoff](continuation/go-intelligence.md) for current milestone
status and next action.

Start with the [continuation handoff](continuation/go-intelligence.md) for
implementation status, source pointers, and the exact next step. Existing
[v1 interfaces](v0.9.0-release-scope.md), [shared contracts](contracts.md), and
[verification behavior](v0.2.0-release-scope.md) remain authoritative for shipped
behavior.

## End goal and observable workflows

The core workflow is:

```text
context -> edit -> refresh -> verify -> inspect evidence -> reconsider or continue
```

A coding agent should be able to answer four questions from compact,
source-grounded context: what matters for this change, what an implementation
must satisfy, which existing implementations and tests provide examples, and
what needs reconsideration after an edit.

| Workflow | Required behavior |
| --- | --- |
| Enter unfamiliar code | Resolve a query, reference, or position into declarations and relevant relationships; expose ambiguity before expansion. |
| Implement new code | Select a file, package directory, or interface and obtain obligations, typed API usage, and existing tests or examples with explicit selection reasons. |
| Change an API | Relate changed declarations to method sets, direct call sites, related tests, and conservative package impact. |
| Continue after an edit | Refresh the previous selection with current locations and evidence differences, without mandatory Change Contract setup. |
| Verify a change | Request executed verification explicitly and identify the snapshot to which that evidence belongs. |

The historical private 20-run Luna feasibility pilot and 27-run adoption
follow-up record workflow and instruction-use observations. A new private
20-run discoverability pilot qualified all runs and kept them scope-safe, but
its 10 focus runs made no `go_context` calls. A 12-run integrated adoption
follow-up also qualified all runs and kept them scope-safe; all six integrated
runs discovered the shipped skill, used `go_context`, refreshed after editing,
and used the evidence. The integrated arm combines the skill and MCP surface.
These results do not establish MCP-alone causality, improved correctness,
productivity, token efficiency, speed, statistical significance, or broad
model generalization. External and multi-model evaluation remain pending. A
review of all six integrated traces found no transcript proving that context
improved the necessary code edit. In gRPC run 3, refreshed context prompted
broader `./...` verification, which was incomplete due to the output cap;
focused verification later passed after a stale-snapshot rejection. Client-go
runs made 3-4 context calls each and gRPC runs made 4-5, including ambiguous
or unhelpful initial selections and repeated verification after incomplete or
stale results. The bounded guidance improvement is to narrow ambiguity with
returned current candidates, finish each batch of edits and formatting before
refreshing, and refresh again after further edits or stale-snapshot rejection
while avoiding redundant refreshes when the snapshot is unchanged. This trace review does
not establish causal edit-quality or product value. The historical adoption
report is in
[validation/v1.0.0/adoption-results.md](../validation/v1.0.0/adoption-results.md).

A post-guidance regression comprised six integrated Luna runs, with three
repetitions on each of two scenarios. All six qualified, passed acceptance,
had zero scope violations and zero operator interventions, and used
`go_context`, refreshed after edits, and recorded evidence use. Five focus
calls failed: three client-go calls reported `invalid_input` for invalid
symbol references, and two calls in grpc-go run 1 reported `stale_snapshot`
because an observed semantic location was absent from the snapshot manifest;
grpc-go runs 2 and 3 had no failed focus calls. Workflow adoption remained
possible while the failures exposed categories to investigate. The result does
not establish that guidance improved quality, correctness, productivity,
speed, or product value. Before changing provider behavior, classify the
invalid-symbol-reference and stale-snapshot failures as expected strict
rejection, agent misuse, or a reproducible provider defect. External and
multi-model evaluation remain pending.

Keep Go-only, local, deterministic operation; pinned gopls; source provenance;
explicit uncertainty; and existing containment and guarded-refactor guarantees.
The intelligence implementation owns context selection. gopls supplies semantic
facts, and MCP and CLI deliver the result. A passing verification report remains
executed evidence, never a safety verdict.

## 1. Documentation and implementation continuity

This file owns product intent, architectural decisions, interface direction,
stages, acceptance criteria, and exclusions. The
[handoff](continuation/go-intelligence.md) owns current status, recorded source
findings, checks actually performed, unresolved implementation facts, and the
exact next action. Both are linked from the documentation index.

At each meaningful milestone, architecture decision, or blocker, update the
handoff in the same change. Replace stale status rather than append a
transcript. Separate implemented behavior, approved architectural direction,
and unverified implementation proposals. Approval of this direction does not
make its wire contract frozen or its features available.

Shared contracts distinguish current content-based freshness and the managed
gopls lifecycle from historical TTL and navigation proposals. Superseded
guidance does not authorize stale results or expand the frozen v1 surface.
Historical release evidence remains unchanged.

## 2. One useful context interface

Add the read-only `go_context` MCP tool and the equivalent
`agentic-go context --format text|json` CLI command over the same intelligence
implementation. Introduce the separate `agentic.focus/v1` response contract.
Existing v1 inputs, schemas, and behavior remain compatible; register the new
tool as an explicit additive interface and update inventory expectations
deliberately. MCP and LSP types stay outside the intelligence domain.

### Selection

| Entry point | Intended result |
| --- | --- |
| Symbol query | Bounded candidates with package, receiver, declaration kind, and location. |
| Current Symbol Refs | Focused context interoperating with existing search results. |
| Source positions | Enclosing declarations and relevant relationships. |
| Workspace-relative paths | File or package orientation and implementation examples. |
| Changes against a local base | Changed declarations, structural consequences, callers, and related tests. |
| No selection | Compact workspace orientation. |

Reuse existing package scope, contained paths, and one-based UTF-8 byte
locations. Multiple explicit anchors share one observation. Ambiguous queries
return candidates for selection; a candidate is not silently chosen.

Accept a response-byte budget and an optional previous pack ID. A previous pack
supplies its original selection when no new selection is given. A changed
selection produces a full pack. Ordinary navigation requires no Change
Contract. Free-form goals remain explanatory context, not executable semantics.

### Evidence and rendering

Every full response includes:

- Snapshot and pack identity, resolved anchors, and current locations.
- Bounded source excerpts, selected relationships, and their selection reasons.
- Relevant tests, examples, and repository guidance with source attribution.
- Missing capabilities, omitted detail, and known limits.
- A concrete way to obtain additional detail through a narrower selection,
  current reference, or available artifact cursor.

Use an 8 KiB default budget and the existing maximum context ceiling. Budget
rendered content, including the MCP text contribution. Useful text and
structured content derive from the same selected evidence; text does not
duplicate the complete JSON document. Adapter rendering must not independently
infer relationships or reinterpret conclusions.

Identity, provenance, anchor representation, and essential uncertainty survive
budgeting. Bound large declaration excerpts and identify omitted spans. If the
required envelope cannot fit, require a narrower selection or larger budget.
Additional detail must not depend on an artifact containing evidence that was
never gathered.

Compatibility means standard MCP and CLI consumption, not a guarantee that
every agent will automatically select the tool.

## 3. Context for the next engineering decision

Use this deterministic selection pipeline:

1. Resolve and disambiguate anchors.
2. Collect declarations, signatures, documentation, and package facts.
3. Expand directly relevant typed relationships.
4. Add representative tests and existing implementations.
5. Deduplicate excerpts and fit the rendered response budget.

Prioritize explicit anchors and changed declarations, their signatures and
relevant source, implementation obligations, direct callers, related tests,
and then broader context. Break ties by stable source identity. Distribute
detail across anchors and relationship categories before one large symbol or
category consumes the budget. Explain every non-anchor inclusion.

For code generation, select examples that implement the same interface, call
the same typed API, or test the selected declaration. Identify the exact
relationship. Prefer relevant same-package examples before broader workspace
examples. Their presence demonstrates an existing approach, not its correctness
or a repository-wide requirement.

Distinguish documented repository guidance from observed coding patterns.
Preserve the source path and applicable scope of guidance. Selection ranking
is a deterministic policy; it does not claim to understand free-form intent.

Bound work before expensive expansion. Reuse declarations and requested facets
instead of gathering every facet for every symbol before trimming. A small
response must not require unrestricted relationship discovery. Existing source,
package, output, concurrency, and deadline limits still apply.

Distinguish relationships that were examined and absent, not examined,
unavailable, or gathered but omitted. Report complete totals only when the
relevant discovery completed. Partial exploration cannot establish zero
relationships or manufacture a full total.

## 4. Go relationships that affect implementation

Use existing semantic providers and extraction facilities to assemble bounded
typed relationships within the intelligence module.

| Relationship | Evidence to return |
| --- | --- |
| Interfaces and implementations | Required methods, matching declarations, pointer/value method-set distinctions, and relevant embedding. |
| Aliases and defined types | Resolved identity and the distinction affecting the selected declaration. |
| Generics | Declaration origin, relevant constraints, and resolved instantiation arguments. |
| Calls | Direct caller/callee identity and the corresponding call-site excerpt. |
| Tests and examples | Enclosing test, benchmark, fuzz function, or example containing a direct reference. |
| Lifecycle operations | Typed context parameters, cancellation calls, error wrapping, resource acquisition, and cleanup sites. |

Each relationship carries source support and the applicable build configuration.
Keep static calls distinct from possible interface dispatch. Reflection,
unresolved dynamic behavior, unavailable types, generated inputs, cgo, inactive
build variants, and external consumers remain explicit limits.

Lifecycle evidence is bounded to what syntax and resolved types establish.
Show relevant parameter, call, return, or `defer` sites. Do not infer
cross-function ownership, guaranteed cancellation, complete runtime reachability,
or behavioral equivalence. These observations are context, not new analyzer
findings. Related tests remain navigation evidence and do not replace
conservative package verification.

When source is temporarily incomplete or does not type-check, the new interface
still exposes available source, declarations, and diagnostics, marking typed
relationships it cannot establish. Cached semantics from an earlier state must
not be labeled current. This partial-source behavior does not turn stale
snapshots, containment failures, cancellation, deadlines, corrupt state, or
protocol failures into successful partial results, and does not change the
existing v1 tool error contracts.

## 5. Coherent observation and bounded derived state

Introduce one request-scoped observation consumed by discovery, source
extraction, context assembly, and semantic synchronization. Carry captured
source, package/build inputs, and snapshot identity through internal helpers
instead of recursively recapturing state. Reuse package discovery within the
observation. Preserve capture, final content validation, and gopls ordering.

Cache only derived work with sufficient identity:

- Parsing: content digest, source identity, and relevant parsing inputs.
- Semantic results: exact snapshot, scope, build inputs, and provider identity.
- Selection and excerpts: reuse within the observation.

Timestamps, file sizes, Git status, watcher silence, and TTL are not content
validation. Exact snapshot identity remains required even when a derived cache
hit avoids repeated parsing or semantic assembly.

Bound caches by retained memory and entry count. Pin active manifests for the
request lifetime; release them on success, error, and cancellation. Apply
admission limits when active work exhausts capacity instead of evicting active
state or allowing unbounded growth. Keep caches private and introduce each
cache only in the stage that uses it.

Maintain the long-lived gopls session, existing restart rules, and ordering
between file notifications and semantic reads. Sidecar restarts invalidate the
relevant derived state. Concurrent requests must not combine evidence from
different observations. Preserve shared concurrency limits and cancellation;
do not introduce unsafe provider concurrency to increase throughput.

## 6. Explicit refresh of delivered evidence

Store the original selection and a manifest of the evidence actually delivered
with each private pack. Reuse the private artifact infrastructure and its
containment and permissions. A refresh reads retained comparison metadata;
it does not make old snapshot-bound artifact cursors valid against new source.

An explicit previous pack ID refreshes that selection against a new observation.
A changed selection or incompatible build/scope context produces a full pack.
Separate three concepts:

| Concept | Meaning |
| --- | --- |
| Logical identity | Which declaration or relationship an item describes. |
| Content revision | Whether the evidence for that item changed. |
| Current locator | Where the item exists in the new snapshot. |

Report added evidence, replacement content for changed evidence, confirmed
source removals, updated locations and current references, previously delivered
evidence omitted by selection or budget, and relationships that became
unavailable. Unchanged content may refer to previously delivered evidence while
its locator and snapshot-bound references are updated.

Compare against the previous delivered response, not its complete internal
overflow artifact. Leaving a response must never be mistaken for deletion from
source. Confirm a removal from current observation evidence; inability to
resolve or examine an item is not confirmation of deletion.

Old Symbol Refs remain stale. Refresh explicitly resolves current identities
and issues current references. Ambiguous renames or moves require selection;
expired packs require a fresh request. Normal calls remain self-contained, and
delta delivery requires an explicitly supplied prior pack. Reconnection or
switching agents must not silently omit needed context.

The reconstruction invariant is: applying a delta to the previous delivered
pack reconstructs the current selected pack, including updated locators,
omission state, and uncertainty. It does not reconstruct unexamined evidence.

## 7. Change consequences and verification

For base-selected context, explain body, signature, receiver,
exported-declaration, import, and build-input changes. Reuse existing structural
classification and connect changed declarations to directly supported interface
obligations, call sites, related tests, and conservative package impact.

Context gathering remains read-only. The external agent authors feature code
with its normal editing tools. Existing guarded refactoring remains available
for supported deterministic transformations; this plan does not expand its
mutation boundary.

Verification remains an explicit operation. Associate referenced verification
evidence with its observed snapshot so later edits cannot make an older passing
report appear current. Preserve verification result semantics, conservative
package selection, and the distinction between context and executed evidence.

## Historical adoption evidence and next decision

The 27-run Luna/max adoption follow-up contains an 18-run canonical three-arm
matrix, six integrated-skill diagnostics, and three scope-wording reruns.
Description-only discoverability produced 0/6 focus use; generic prompt
guidance produced 6/6; and the shipped skill produced 6/6. Initial integrated
safety was 5/6, while the scope-wording rerun was 3/3 acceptance-pass,
qualifying, and scope-safe. These are historical observations, not proof that
the workflow improves task outcomes.

These records establish observed instruction-surface use and safety only. They
do not support causal speed, token, reliability, adoption, performance, or
generalization claims. Keep focus and full-replacement refresh; defer delta
refresh. Trace review is complete and its bounded guidance improvement is
recorded in the project skill. Keep the studies as regression evidence, not
fresh proof of product superiority. External and multi-model evaluation remain
pending. Defer any new release label until a reliability milestone passes.
Raw artifacts remain private and ignored.

## Delivery order and completion criteria

| Stage | Deliverable | Completion criterion |
| --- | --- | --- |
| 1. Documentation | Revised north star, handoff, authority clarifications, and index links | Another engineer can recover the direction and current status without conversation history. |
| 2. Coherent observation | Shared discovery, captured source, safe cache identities, and active-manifest lifetime | Relevant helpers reuse one observation while preserving validation and cancellation. |
| 3. Useful context | MCP/CLI entry points, query/ref/position selection, excerpts, direct relationships, and useful text | One request provides grounded context to begin a focused edit; ambiguity and budgets are explicit. |
| 4. Implementation support | Path/base selection, richer Go relationships, existing examples, and incomplete-source behavior | The agent can inspect implementation obligations and examples together, including explicit unavailable facets. |
| 5. Refresh | Delivered-evidence manifests, current locators, replacements, removals, and omission distinctions | Refresh communicates what must be reconsidered after an edit and satisfies the reconstruction invariant. |
| 6. Integration | Verification lineage, compatible discovery, documentation, and client guidance | The complete workflow works without mandatory continuity setup and preserves existing v1 behavior. |

Each stage is one coherent implementation slice. Do not build all later stages
while introducing shared observation. Record consequential implementation
decisions and the evidence actually gathered in the handoff.

## Focused acceptance cases for future implementation

- Ambiguous names, identical methods on different receivers, embedding,
  aliases, and generic instantiations.
- Direct test references, external test packages, and unresolved interface
  dispatch; each extraction predicate includes a meaningful near miss.
- Temporarily invalid source, unavailable semantic facets, and the distinction
  between missing evidence and examined empty results.
- Tight budgets, large declarations, multiple anchors, Unicode byte locations,
  and preserved provenance and omission markers.
- Same-size rewrites, edits during observation, deletion, module/build changes,
  cancellation, sidecar restart, concurrent requests, and cache pressure.
- Refresh across shifted locations, changed content, deletion, ambiguous
  renames, expiry, and evidence omitted by budget.
- Reconstruction of a current selected pack from its previous delivered pack
  and delta, including current references and unavailable relationships.
- Equivalent evidence across CLI text, CLI JSON, and MCP, accounting for
  rendered budgets and deliberate compatibility checks for existing v1 tools.

These cases specify future focused correctness work. This documentation change
introduces no new tests or benchmarks. Complete the agreed repository gates and
scope checks before committing.

## Boundaries and implementation decisions

- The local north-star document is the planning target. No separate Notion
  document was found in the repository documentation reviewed for this plan.
- Preserve Go-only, local, deterministic operation, pinned gopls, stdio MCP,
  contained access, disk snapshots, and explicit stale rejection.
- Retain the existing dependency stack and private artifact infrastructure.
  Unsaved editor overlays, hosted services, and general graph infrastructure
  remain outside this plan.
- Additional languages, an embedded LLM, an agent framework, autonomous
  editing, broad analyzer expansion, and speculative test selection are not
  introduced by this work.
- No broad comparative evaluation, paid model run, or benchmark campaign is a
  release gate. The private Luna focus and adoption follow-up are recorded
  evidence only and do not establish causal engineering improvement. Local
  Conventional Commits are authorized after the agreed gates and scope checks
  pass. Pushes, tags, and releases require separate authorization and must
  preserve existing public history.
- The documentation stage and the implemented observation, context, focus, and
  verification slices are recorded above. Continue only from the current
  handoff next action with an explicit bounded implementation slice; later
  roadmap rows are not complete merely because they are listed here.

The behavioral decisions above govern future implementation. Exact new wire
fields, cache capacities, serialized budget accounting, and typed extraction
predicates must be recorded alongside their implementation contracts before
those stages ship. Resolve them against the existing constraints and record
their rationale in the handoff. They are not frozen or verified by this
documentation change.
