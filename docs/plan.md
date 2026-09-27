# agentic-go product plan

Revised 2026-09-24. This is the routing summary for the
[Go engineering north star](go-intelligence-north-star.md).
The [continuation handoff](continuation/go-intelligence.md) records what is
implemented, the current evidence, and the next unfinished step.

## Customer and product thesis

Build for Go engineers using coding agents on real repositories. Help the
agent understand, implement, debug, refresh, and verify a change, then leave
an inspectable handoff. Reduce the engineer's investigation, avoidable rework,
and effort to determine whether the evidence still applies.

The technical foundation is a local, deterministic evidence compiler for Go.
It combines semantic facts, change impact, bounded context, guarded operations,
and executed verification. Skills explain when and how to use those operations.
The external agent authors feature code; the engineer retains judgment about
requirements and completion.

The product should reduce dependence on model memory and guesswork. It cannot
guarantee that every model reasons equally well or that another capable harness
cannot reproduce its mechanisms. Reliable integration and recurring saved work
are sufficient sources of value, if demonstrated.

## Existing foundation

- `internal/intelligence` owns observations, focused context, typed
  relationships, refresh, verification applicability, continuity, and guarded
  refactoring.
- `internal/changeimpact` discovers changed declarations and conservative
  affected packages from Go, Git, module, workspace, and embedded-file inputs.
- `internal/verification` owns check policy, execution planning, and portable
  evidence reports.
- The CLI, advisory GitHub Action, and MCP adapters expose the same domain
  behavior. MCP/LSP transport types stay outside intelligence domain contracts.
- Current focus supports query/ref/position/file/package selection,
  source-supported Go relationships, full-replacement refresh, and report
  applicability. Current verification distinguishes requested checks passing
  from findings or incomplete execution.

These mechanisms exist. Their usefulness across the complete workflow and
different model/client combinations remains to be established. The durable
verification boundary is `agentic.verify/v1`; focused context uses the separate
`agentic.focus/v1` contract. Neither contract proves business correctness.

## First complete workflow

Start with a cross-package API or interface change: find relevant declarations,
implementations and consumers, inspect existing examples/tests, edit and debug,
refresh, verify affected packages, and hand the evidence back to the engineer.

An interface migration or cancellation-support change should expose Go-specific
facts such as pointer/value method sets, embedding, typed usages, test imports,
and build assumptions where supported. Their limits remain visible. A compiler
already catches many signature errors; measure whether this workflow reduces
discovery/repair cycles, consequential omissions, or review effort.

The next cycle is ordered in the [north star](go-intelligence-north-star.md#next-delivery-cycle):

1. Finish the existing selector-remediation reliability work and its recorded
   checks/reruns. Preserve cause classification and strict rejection.
2. Design current declaration candidates derived from the observed diff so an
   agent can enter focused context without guessing coordinates. Reuse existing
   fields only after resolving compatibility and budget behavior.
3. Complete the edit/debug/verify/review handoff using existing reports,
   applicability assessments, and renderers. A historical pass must remain
   distinguishable from evidence applicable to the current change.
4. Exercise shared instructions, text/structured delivery, exact refs, and
   recovery in named real clients with different model families.
5. Use a finite comparison and real engineer use to decide which capability
   deserves further investment.

Items 2-5 are planned product work. Updating this plan does not implement them,
authorize paid runs, or qualify a release.

## Model and skill contract

Keep engine semantics independent of model identity. Maintain concise shared
workflow instructions, with thin host-specific installation and placement.
Support claims must name the combinations actually exercised, including a
usable smaller open model and a stronger model before making broader claims.

Selectors and opaque references must be usable without guessing. Recover from
errors with current candidates or fresh queries/files; do not correct refs
silently or accept stale evidence. Refresh after an edit batch with `base`
and `previous_pack_id` only. Repeated mistakes count against interface quality,
even when rejection is correct.

Context helps plan scope and checks; it neither authorizes broader edits nor
executes verification. Skills must expose uncertainty and passing-check limits.
Permit trivial/familiar edits to skip unnecessary context work. A client must
deliver useful evidence to the model, not merely complete an MCP handshake.

## Evidence and product decisions

Compare against native Go tools and upstream gopls with useful workflow
guidance. Keep the existing canonical adoption matrix and older diagnostic
campaigns separate. They record bounded use and task acceptance, not broad
compatibility or comparative engineering value.

Measure independently accepted changes, consequential omissions, engineer
review/rework effort, setup and interaction cost, and per-task/model time.
Tool use and event ordering are diagnostics. Equal final correctness can still
leave meaningful differences in effort; a passing baseline is not a kill rule.

The north star specifies a small initial screen, followed only by a focused
redesign and confirmation on fresh tasks/another host when warranted. Keep
hidden acceptance oracles out of treatment instructions. Record unsupported
clients and missing cost data honestly. Continue capabilities with recurring
benefit; simplify or stop expanding those that repeatedly add work without
improving decisions or reviewability.

The separate [codebase indexing research note](research/codebase-indexing-retrieval.md)
records the user-directed aspiration for reusable, branch-aware repository
retrieval. Its next product action is to evaluate the current local retrieval
path before proposing a persistent index. This research does not change the
current release scope or product contracts.

Reliability maintenance and comparative product claims have separate gates.
A maintenance release can satisfy its engineering contracts without claiming
that it makes models better. Evidence for a narrow task/model must remain a
narrow claim.

## Authority and compatibility

- [North star](go-intelligence-north-star.md): current product requirements,
  model/skill contract, delivery cycle, and value evaluation.
- [Continuation handoff](continuation/go-intelligence.md): current state,
  evidence identities, and sequencing.
- [v0.9 freeze](v0.9.0-release-scope.md) and [contracts](contracts.md):
  frozen interfaces and shared invariants.
- [v0.2 scope](v0.2.0-release-scope.md) and
  [v0.1 scope](v0.1.0-release-scope.md): compatibility baselines.
- [v1 roadmap](v1.0.0-roadmap.md): completed stages and historical evidence.
- [v0.8 evaluation scope](v0.8.0-evaluation-scope.md): historical corpus,
  scorer, replay, and paid-pilot boundaries.
- [Verification ADR](adr/0001-verification-report-boundary.md) and
  [Context Pack ADR](adr/0002-context-pack-boundary.md): architectural rationale.

The frozen v1 registry remains 14 tools, seven fixed resources, one template,
and six prompts. The existing additive `go_context` brings the server to 15
tools. This plan preserves those surfaces and current schemas.

Require a stable worktree during verification. Source snapshots and endpoint
validation are not transactional execution isolation. Preserve contained access,
cancellation, bounds, guarded edit preimages, and explicit uncertainty.

The module is `github.com/agentic-mcps/go`; the former personal module is a
separate identity with an explicit [migration](module-migration.md).
The current cycle is Go-only. Delta refresh, broad graphs/caches, additional
analyzer domains, speculative test selection, autonomous repair, distributed
agent orchestration, hosted analysis, and other languages remain outside it.
