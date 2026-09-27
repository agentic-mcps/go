# Go engineering workflows across coding agents and models

Status: maintainer-directed product plan, revised 2026-09-24. This revision
updates direction and delivery criteria; it does not implement capabilities or
establish comparative product value. `v1.2.1` (tag `67f54b7`) remains the
released baseline. The [continuation handoff](continuation/go-intelligence.md)
owns the current branch, implementation status, evidence, and exact next action.
The [v1 interfaces](v0.9.0-release-scope.md),
[shared contracts](contracts.md), and
[verification behavior](v0.2.0-release-scope.md) govern shipped behavior.

## Customer and intended outcome

The first customer is a Go engineer using a coding agent on a real repository.
Agentic-Go should help that agent understand, implement, debug, and verify a
change, then leave an inspectable handoff. The engineer should spend less time
reconstructing the investigation, correcting avoidable mistakes, and checking
whether the reported evidence still applies.

The technical foundation is a local, deterministic evidence compiler: Go
semantics, change impact, bounded context, guarded operations, and verification
lineage. The product earns its place through better engineering decisions or
less effort to obtain an acceptable change. More calls, larger reports, and
successful instruction following are insufficient evidence of that value.

The ambition is to reduce dependence on what a model remembers or guesses.
Small open models and stronger proprietary models should receive the same
defined operations, source facts, and failure semantics. Each should be able
to use the product through a supported host without learning repository
internals or reconstructing opaque identifiers. Equal task success across
models is a hypothesis to evaluate, not a property of the tool protocol.

## Model independence and limits

Separate three promises:

| Layer | Required property | Evidence needed |
| --- | --- | --- |
| Engine | Equivalent explicit inputs and repository state receive consistent contracts, grounded facts, limits, and rejection behavior. | Deterministic contract and failure-path evidence. Test results, provider availability, and timing need not be deterministic. |
| Agent integration | Supported hosts deliver usable text/structured evidence, instructions, exact references, and recovery paths to the model. | Real client checks, including a compact-context model and a stronger model; protocol delivery alone is insufficient. |
| Engineering outcome | An engineer obtains an acceptable, inspectable change with fewer consequential omissions or less total work. | Comparative tasks and observed use in real repositories. |

The engine must not change truth, freshness, or check semantics based on a
model name or confidence score. Host-specific configuration and instruction
placement may differ. Publish support only for combinations actually exercised;
do not turn a Luna result into an open-model, Sol, Astra, or all-agent claim.

A model can ignore evidence, misunderstand a requirement, or author a bad
algorithm. Selected checks cannot prove an arbitrary business requirement,
and advisory tools cannot stop an agent from claiming completion. A future
completion gate would need an explicit host/CI enforcement point and a
machine-checkable repository policy; even that would prove policy compliance,
not universal safety.

Another sufficiently capable harness can compose the same Go tools and build
similar mechanisms. Irreplicability and guaranteed superiority over every
model/harness are not engineering requirements. Differentiation must be earned
through reliable composition, useful Go-specific evidence, low interaction
cost, and less engineer effort. Upstream gopls already offers an
[experimental MCP server and workflow instructions](https://go.dev/gopls/features/mcp);
it is a baseline to compare with and a semantic provider to build upon.

## End goal and observable workflows

The complete workflow is:

```text
understand -> implement -> inspect diagnostics/failures -> refresh
           -> verify -> inspect applicable evidence -> revise or hand off
```

The agent and engineer retain the implementation decision. Agentic-Go provides
the facts and bounded operations that decision requires. Context never runs
verification implicitly, and ordinary navigation requires no Change Contract.

| Workflow | Required experience |
| --- | --- |
| Enter unfamiliar code | Resolve queries, files, packages, or current declarations into bounded context; expose ambiguity and reasons for each inclusion. |
| Implement a feature | Return relevant method sets, implementation relationships, typed API usages, tests/examples, and scoped repository guidance. Distinguish observed patterns from requirements. |
| Change an API | Connect changed declarations to supported interface relationships, direct call sites, tests, and conservative package impact. |
| Debug and revise | Make current diagnostics, executed failures, and useful source locations inspectable; expose unsupported evidence and an actionable recovery path. |
| Continue after edits | Refresh the selection, issue current references, and reassess verification applicability. Missing evidence is not proof of source deletion. |
| Verify | Explain selected scope and checks, execute them explicitly, and distinguish pass, failure, omission, and incomplete execution. |
| Review or resume | Present current change scope, report applicability, executed checks, findings, and limitations without requiring the engineer to replay the agent conversation. |

First make one complete job dependable: change an existing API or interface
across packages, migrate relevant consumers, debug the change, verify it, and
hand it back. Adding cancellation support is a representative case. Type facts
and existing tests can guide that change; they do not prove cancellation
propagates correctly or that every external consumer was found.

Use the existing implementation before inventing capabilities. Declaration,
file/package, typed-relationship, full-replacement refresh, guarded-refactor,
and verification foundations already exist. The delivery cycle below names
the remaining integration work and proposals.

## Skills and interaction requirements

Tools and skills form one workflow contract. Maintain concise shared guidance
with only the installation/placement differences needed by supported hosts.
Do not encode a particular model's conversational quirks in the engine.

- Prefer a query, file, or package when exact coordinates are unnecessary.
  Return current candidates and useful follow-up arguments from actual results.
- Copy Symbol Refs exactly. Never invent, decode-and-rebuild, or modify them.
  The current position selector requires the declaration identifier; a line
  start, comment, or local variable is not an equivalent selection.
- Recover from ambiguity using a current candidate or a fresh selection.
  Explain selector errors without disguising them as usable evidence.
  Repeated misuse is an interface problem to investigate as well as a correct
  rejection; it is not automatically a provider defect.
- Finish a batch of edits and formatting, then refresh with `base` and
  `previous_pack_id` only. Refresh again after further edits or stale
  rejection. Avoid repeating unchanged-state context calls.
- Treat impact as planning evidence. It does not authorize editing every
  affected package or exceeding the task's supplied scope.
- Inspect failures and missing evidence before continuing. Skills must not
  equate a passing requested check with task completion or bypass strict
  rejection to keep a workflow moving.
- Make useful evidence and limits available in text as well as structured
  responses. Keep output small enough for limited-context clients without
  concealing uncertainty or requiring private implementation knowledge.
- Permit skipping context compilation for trivial, familiar changes or when
  current evidence already answers the question. Appropriate non-use is valid.

Broader fresh-position resolution is a possible ergonomic improvement, not a
license to snap an invalid selector to a guessed symbol. Resolve its semantics
and compatibility explicitly before implementation. Existing guarded edits
retain exact preimages and approval boundaries; the external agent authors
feature code with its own editor.

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

Maintain the read-only `go_context` MCP tool and equivalent
`agentic-go context --format text|json` CLI command over the same intelligence
implementation. Their post-v1 contract is `agentic.focus/v1`. The frozen v1
registry remains 14 tools, seven resources, one resource template, and six
prompts; `go_context` is the existing additive fifteenth tool. This plan adds
no MCP operation or schema. MCP and LSP types stay outside the intelligence
domain.

### Selection

| Entry point | Intended result |
| --- | --- |
| Symbol query | Bounded candidates with package, receiver, declaration kind, and location. |
| Current Symbol Refs | Focused context interoperating with existing search results. |
| Source positions | Context for the current declaration identifier. More general enclosing-declaration resolution remains a separate design decision. |
| Workspace-relative paths | File or package orientation and implementation examples. |
| Changes against a local base | Current change/impact evidence. Automatically offering focused declaration candidates from that diff is proposed in the next delivery cycle. |
| No explicit selector | The current base-selected change view; use existing brief/file/package entry points for orientation. |

Reuse existing package scope, contained paths, and one-based UTF-8 byte
locations. Accept one mutually exclusive selector group. File and package
selection may gather several anchors within one observation. Ambiguous queries
return candidates for selection; a candidate is not silently chosen.

Accept a response-byte budget and an optional previous pack ID. A previous pack
supplies its original selection; refresh does not accept changed selectors.
Choose a fresh request for a new selection. Ordinary navigation requires no
Change Contract. Free-form goals remain explanatory context, not executable
semantics.

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

Preserve the request-scoped observation consumed by discovery, source
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

Snapshot identity is not execution isolation. Current verification runs Go
commands against the live workspace and validates state before and after the
operation. Require a stable worktree during checks; endpoint equality cannot
exclude transient A-to-B-to-A edits during execution. Stronger support for
concurrent writers needs a separately demonstrated execution/coordination
design. External services and unrecorded runtime inputs are not made immutable
by a source snapshot.

## 6. Explicit refresh of delivered evidence

Store the original selection and a manifest of the evidence actually delivered
with each private pack. Reuse the private artifact infrastructure and its
containment and permissions. A refresh reads retained comparison metadata;
it does not make old snapshot-bound artifact cursors valid against new source.

An explicit previous pack ID refreshes that selection against a new observation
and returns a complete replacement. Changed selectors require a fresh request;
preserve existing build/scope validation and rejection semantics. Separate
three concepts:

| Concept | Meaning |
| --- | --- |
| Logical identity | Which declaration or relationship an item describes. |
| Content revision | Whether the evidence for that item changed. |
| Current locator | Where the item exists in the new snapshot. |

Return self-contained current evidence with updated locations and references,
explicit omissions, and unavailable relationships. Retained metadata describes
the evidence actually delivered, not an undelivered internal overflow artifact.
Leaving a response must never be mistaken for deletion from source. Inability
to resolve or examine an item is not confirmation of deletion.

Old Symbol Refs remain stale. Refresh explicitly resolves current identities
and issues current references. Ambiguous renames or moves require selection;
expired packs require a fresh request. Reconnection or switching agents must
not silently omit needed context. Full replacement is the current policy.
Per-evidence semantic differences and delta delivery are deferred; neither is
an acceptance requirement for the next workflow.

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

A review handoff must present outcome and applicability independently. Include
the observed change, relevant build/scope/check policy, checks actually run,
unexecuted or incomplete checks, findings, and limitations. A stored latest
report may be historical; it becomes evidence for the current change only
through the existing applicability comparison. Use the current reports and
renderers before proposing another artifact or wire format. Free-form intent
and unresolved business questions remain agent/engineer judgments.

## Evidence boundary

The [historical adoption report](../validation/v1.0.0/adoption-results.md),
later integrated diagnostics, and the 2026-09-24 canonical Luna matrix are
different campaigns. Preserve their identities and do not pool them. Current
campaign status belongs in the [handoff](continuation/go-intelligence.md).
None establishes broad model compatibility or comparative engineering value.

The current matrix establishes guided use and successful task acceptance on
two pinned scenarios. Its failed selectors were rejected correctly; audited
selector misuse does not establish a provider defect. Evidence-use counters
record event ordering, not improved decisions. Pooled duration medians across
heterogeneous tasks do not isolate the cost of the tooling. Unmeasured value
is unknown, not zero; equal final correctness may still conceal differences
in investigation, review, or rework.

## Next delivery cycle

Keep this cycle focused on the cross-package API/interface change. These are
ordered implementation proposals after the current reliability work, not
completed features or permission to run model campaigns.

| Order | Deliverable and status | Completion criterion |
| --- | --- | --- |
| R0 | Selector guidance, private cause classification, and recovery replay. Remediation is in progress. | Malformed refs, invalid coordinates, genuine provider errors, stale results, and recovery remain distinguishable. Finish the recorded reliability gates and reruns; do not relabel failed calls as successes. |
| R1 | Offer current declaration candidates from the observed diff. Proposed. | A base-selected change request can lead to relevant focused evidence without invented coordinates. Preserve explicit selector behavior; bound and explain candidates, retain ambiguity, and mark deleted/unresolvable declarations unavailable. Resolve compatibility and representation in existing fields before editing code. |
| R2 | Complete the edit/debug/verify/review handoff using current evidence and renderers. Proposed integration work. | The engineer can inspect the current change, check scope, execution result, applicability, findings, and omissions together. A subsequent edit makes old evidence visibly non-applicable. Agent judgment is distinguishable from machine facts. |
| R3 | Exercise the shared skill/workflow in real clients and different model families. Planned. | Text/structured delivery, exact reference use, refresh, failure recovery, and handoff work in the named combinations, including a usable lower-capacity open model and a stronger model. Record unsupported cases instead of claiming universal compatibility. |
| R4 | Decide whether the complete workflow earns repeated use. Planned. | Compare against equipped alternatives on fresh tasks and observe engineer effort. Continue only for a recurring benefit; simplify or stop expanding capabilities that repeatedly add work without useful outcomes. |

R1 and R2 should reuse the typed relationships, tests/examples, impact engine,
refresh, and applicability checks already present. A feature list is not a
reason to rebuild them. Any design that requires a new public field or changes
a frozen behavior must return to an explicit compatibility decision; this
cycle does not pre-authorize that expansion.

### Acceptance cases for the complete workflow

- Begin before any diff exists using an interface, query, file, or package;
  after editing, use current changed declarations to continue investigation.
- For an API migration, expose at least one source-supported implementation or
  caller obligation and relevant existing tests/examples. Validate those facts
  independently; the presence of output alone is not usefulness.
- Preserve pointer/value method sets, embedding, generic/type facts, internal
  and external test imports, and scoped repository guidance where supported.
  State unavailable relationships and excluded build configurations.
- Recover from temporary compile errors using current source and diagnostics;
  never substitute earlier type facts as current. Do not guess business logic.
- Reject altered refs and invalid coordinates, then recover through an exact
  current candidate or fresh file/query selection. Record recovery effort.
- After an edit, formatting pass, or relevant build/module change, refresh and
  reassess the report. A historical pass is not current evidence; an applicable
  failure remains a failure. Missing race checks or incomplete output remain
  visible under the requested policy.
- Keep provenance, currentness, essential uncertainty, and next actions usable
  under a small response budget. A host receiving only text must not receive
  a misleadingly stronger conclusion than a structured-content consumer.
- Permit a routine local edit to bypass unnecessary context work. Allow a
  different supported agent to resume from current evidence without reusing
  stale refs; private continuity remains bound to its documented environment.

Retain existing stale-state, cancellation, containment, interruption, and
schema evidence. New focused checks should cover the actual behavioral change,
not duplicate the existing corpus. A capability described here is complete
only after its implementation and applicable evidence are recorded.

### Finite product evaluation

Keep historical corpus/scorer contracts unchanged. Start a separately named
screening campaign only when its clients, permissions, and budget are available.
Preselect four tasks across at least three repositories: two ordinary Go
changes, a cross-package migration, and a controlled stale/incomplete-evidence
case. Keep challenge results separate from ordinary-work results. Define
acceptance and review obligations before observing treatment outcomes.

Use two model families in one host for the first screen, including a usable
smaller open model and a stronger model when available. Compare three conditions:

1. Native editing, Go tools, and upstream gopls with its workflow guidance.
2. The same baseline with Agentic-Go available through its normal descriptions.
3. The same baseline with Agentic-Go and concise operational skill guidance.

All conditions receive equivalent task requirements, outcome-oriented workflow
instructions, permissions, and budgets. Availability measures the delivered
integration; the guided condition additionally measures instruction effects.
This is 24 screening runs, not a statistically conclusive efficacy study.
Randomize order and block comparisons by task, model, and initial repository
state. A missing model/client is a coverage gap, not permission to substitute
Luna results and label them model-independent.

Blind-review candidate changes and standardized handoffs using independent
behavioral and review criteria. Hidden oracles must not enter instructions or
tool guidance. Substring matches and tool-call counters cannot establish
obligation satisfaction. Inspect traces separately to explain a result.

Measure accepted changes, consequential omissions, engineer review/rework
effort, setup friction, total time, tool execution time, retries, and real
token/cost data when available. Bytes are not tokens, calls are not inference
steps, and total elapsed time is not tool overhead. Report per-task/model
results and dispersion; do not present pooled median ratios as causal effects.
Account for recurring operational cost as well as time saved in review.

Use the screen to choose one concrete improvement, then confirm that hypothesis
on fresh held-out tasks and a second host before widening claims. Observe real
Go engineers on subsequent ordinary changes without requiring tool use. Their
choice to keep the workflow and concrete saved work are useful evidence;
arbitrary retention fractions or invented product scores are not proof.

Continue when independently checked benefits recur across different changes
and repositories without an unacceptable correctness or interaction regression.
Set acceptable cost and the primary outcome before a campaign, based on the
engineer's task. If a capability repeatedly adds effort without improving
decisions or reviewability, make one bounded redesign around the observed cause;
stop expanding it if that fails. A correct baseline alone is never a kill
criterion. Benchmark growth is not a substitute for product improvement.

## Boundaries and release decisions

- Preserve Go-only, local operation, pinned gopls, stdio MCP/CLI, contained
  access, disk snapshots, and strict stale rejection. Keep the frozen v1
  inventory, existing additive focus interface, and schema semantics intact.
- Retain the dependency stack and private artifact infrastructure. Delta
  refresh, general caches/graphs, speculative individual-test selection,
  broader analyzer domains, and expanded refactoring await demonstrated need.
- Unsaved editor overlays, distributed agent coordination, hosted services,
  additional languages, an embedded model, and autonomous repair are outside
  this cycle. Guarded supported edits remain explicitly requested operations.
- Source facts and selected checks do not establish behavioral equivalence,
  complete runtime reachability, guaranteed cancellation, or universal safety.
- Reliability maintenance can ship after its applicable engineering and
  release gates. Comparative usefulness, model generalization, and default
  workflow placement require their own evidence; a maintenance release must
  not imply those claims. No giant benchmark campaign is a blanket release
  prerequisite.
- This revision changes documentation only. Continue implementation when the
  user requests it; that request is sufficient authorization for its bounded
  scope. Paid runs, external outreach, commits, pushes, tags, and publication
  require their respective authorization. Preserve existing public history.
