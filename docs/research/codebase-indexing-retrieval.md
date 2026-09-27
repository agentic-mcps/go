# Branch-aware codebase indexing and retrieval

Status: user-directed product aspiration and research proposal, recorded
2026-09-27. This document captures the requested direction and current findings.
It does not change the frozen v1 contracts or claim that full repository
indexing and retrieval exist.

The pre-integration source worktree diffs, statuses, and untracked-file hashes
are preserved in the
[`2026-09-27 source worktree record`](../../validation/retrieval/provenance/2026-09-27/README.md).
The separate 72-run Sol/high decision-value harness remains parked in its
source worktree and is excluded from retrieval evidence.

The current release and reliability sequence remains in the
[continuation handoff](../continuation/go-intelligence.md),
[product plan](../plan.md), and
[north star](../go-intelligence-north-star.md). This proposal is a separate
research track. A model-free retrieval screen has run and exposed weak
relevance, zero document retrieval, and an incomplete large-corpus capture.
The exact branch source-view preview is implemented. There is no persistent
index or measured agent-product advantage yet.

## Product aspiration

The product owner wants a developer to install Agentic Go, point it at a local
directory or Go repository, and have it build reusable repository knowledge.
The first setup should index the repository's default branch, using `main` when
that branch exists. A developer should also be able to select a development or
feature branch and keep its index separate.

After setup, an engineer or coding agent should be able to ask where a behavior
is implemented, how a product flow works, which packages enforce an invariant,
or what needs to change for a difficult requirement. The tool should return
current code, tests, documentation, and Go relationships that help answer the
question. Repeated questions against an unchanged branch should reuse the
index instead of reparsing and rediscovering the repository from the beginning.

The intended gains are faster orientation, less repeated search, lower context
and token cost, fewer missed relationships, and less effort to make and review a
change. The ambition is to make Agentic Go a game changer for people and AI
agents navigating large Go codebases. That is a product goal; it is not a
measured claim.

The tool currently provides deterministic, source-grounded context to an
external coding agent. It does not embed an LLM or independently guarantee a
correct natural-language explanation of business logic. The index can give an
agent better evidence quickly. The agent still has to interpret that evidence,
and the engineer still judges requirements and behavior.

## Current findings

The benchmarked source was commit `7b5111c6365a2a12806a561d86745d5bc50d7c9e`
with an explicit dirty-diff fingerprint in each report. The integration topic
branch preserves two dirty source worktrees' changes. The working tree contains
a private, process-local retrieval cache in
[`internal/intelligence/retrieval/index.go`](../../internal/intelligence/retrieval/index.go).
It parses observed Go files into declaration fragments and ranks them using
deterministic lexical scoring with symbol, receiver, package, and declaration
kind signals. `go_context` resolves candidate source positions through the
active gopls observation. The public CLI/MCP surface and schemas are unchanged.

This is useful candidate discovery, not the proposed persistent repository
index. The cache targets an estimated 64 MiB cap for retained parsed fragment
metadata in process memory and is lost on restart. That estimate does not bound
actual allocation or files and fragments materialized while searching. Each
query still gathers every observed Go file and scores all declaration fragments.
The current retrieval remains process-local and indexes Go declarations only;
it does not index repository text or build a complete cross-package behavioral
graph. The corrected medium screen and repository-scale coverage limits are
recorded below and in the [continuation handoff](../continuation/go-intelligence.md#current-next-action).

Exact source freshness remains a core property. Current snapshot capture reads
the scoped inputs twice. It retains at most 32 manifests and 8 MiB of manifest
metadata, plus 4 MiB of captured source per observation. A future index can
avoid reparsing unchanged committed content, but it cannot assume that an
uncommitted worktree stayed unchanged because a file timestamp or watcher was
quiet. The existing snapshot and stale-reference rules remain the authority
for current semantic evidence.

The current focus interface uses `base` to describe change context. It is not
a branch selector. The preview command now creates a detached Git worktree at
the selected exact commit. With no branch supplied, it selects local `main`
when present, otherwise the configured `refs/remotes/origin/HEAD`; it does not
silently use the currently checked-out branch. The JSON result names the ref,
commit, tree, view path, overlay digest, and checkout limitations. Configure the
agent against that exact returned view path so snapshot validation can reject
a moved branch ref. This is explicit source-view setup, not persistent
indexing or automatic retargeting of an already-running MCP server.

An optional dirty overlay is accepted only when the source checkout's `HEAD`
equals the selected branch commit. It captures staged, unstaged, and regular
untracked files up to 8 MiB, including additions and deletions; it rejects
cross-branch overlays, merge conflicts, unsupported untracked paths, and
concurrent overlay drift. The detached view does not change the source checkout
branch or files. Git submodules are reported as incomplete. The preview does
not capture external `go.work` or local `replace` inputs, so a complete Git
tree checkout is not a claim that every Go build input is present. Use the view
root itself as `--workspace`; nested workspace roots do not validate the
branch-view marker.

## Historical GPT-6 Sol council findings

Three GPT-6 Sol council members examined the existing implementation from
index architecture, retrieval experience, and product/release perspectives.
They initially differed on whether persistent indexing should start now. Their
cross-review converged on measuring the current path before adding persistence.

**Index architecture.** A local, content-addressed disk index with per-file
segments and bounded top-K retrieval is the likely scale path. Publish complete
generations atomically, and resolve candidates through the exact current
semantic observation.

**Retrieval experience.** Keep the agent flow progressive: question, ranked
candidates with reasons and coverage, selected source, then bounded related
evidence. Use existing Go relationships for semantic expansion. Consider
embeddings only if held-out natural-language queries expose lexical misses.

**Product and release.** The current cache has not demonstrated held-out
relevance or large-repository benefit. Compare it with guided `rg`, Go tools,
and upstream gopls before adding storage, invalidation, corruption recovery, or
a new public interface.

The strongest argument for a disk index is repeated work: the process-local
cache disappears between sessions, and warm queries still walk all observed
Go files and declarations. The strongest argument to wait is that snapshot
capture may dominate the request, so a disk index could make the wrong stage
faster while adding a second freshness and recovery system.

Council members suggested possible screening values such as a two-to-five
second warm retrieval p95, 512 MiB peak memory, a large-repository stratum of
10,000 Go files or 250 MiB of Go source, and at least a two-times retrieval
improvement from a persistent prototype. These are planning hypotheses, not
measured targets or supported limits. Freeze actual thresholds before running
the evaluation.

## GPT-6 Luna Max council verdicts (2026-09-27)

These are the current verdicts and remain separate from the historical
GPT-6 Sol findings above:

- Preserve both dirty source worktrees while preparing or running research.
- Do not use the historical GPT-6 Sol/high 72-run harness or combine results
  from different models.
- First run a model-free held-out retrieval and repository-scale benchmark.
  This measures retrieval quality and costs without an agent model.
- Only after exact branch selection and branch-matched source views exist, run
  a separate matched native-versus-Agentic-Go engineering screen with GPT-6
  Luna Max only, using the same model settings in both conditions.

These are sequencing and study-design decisions, not evidence that retrieval
quality, repository-scale performance, branch support, or comparative value
has been demonstrated.

## Product behavior to design

### Setup and indexing

The target first-run flow should:

1. Resolve and show the repository root, selected branch, commit/tree identity,
   Go build configuration, index format, and file coverage.
2. Build the first local index for `main` when present, otherwise use and name
   the repository's configured default branch. Do not change the developer's
   checked-out branch as a side effect.
3. Let the developer select another branch. Keep its index generation separate
   from `main`, and identify results by the resolved commit/tree rather than
   relying on a mutable branch name alone.
4. Track staged, unstaged, and untracked worktree changes as an exact overlay
   over the selected branch. A lost or overflowed filesystem watcher requires
   reconciliation before the index can be treated as current.
5. Show partial indexing, unsupported files, generated/vendor policy, and
   resource limits. Never describe skipped files as searched.

The first full index necessarily reads the selected source once. The target for
later questions is to reuse the unchanged branch index, update changed files,
and read exact source spans needed for the response. For committed trees, Git
tree and blob identities are promising stable inputs. Dirty worktrees still
need exact change detection; watcher state, timestamps, and file sizes alone
cannot prove content freshness.

### What to index

Start with a visible support matrix instead of claiming to index arbitrary
repository content:

- Go source: declaration identity, signatures, comments, package names, source
  terms, tests, examples, build constraints, and supported typed relationships.
- Repository text: README and design documents, local instructions, module and
  workspace files, configuration, and other supported text that explains
  behavior or build assumptions.
- Other languages and binary formats: report them as unsupported or add them
  only through a separate, evaluated parser. Text matches must not be presented
  as Go type or call-graph evidence.

Generated files, vendored dependencies, very large files, embedded assets,
build tags, cgo, multiple Go modules, and `go.work` files need explicit
inclusion and coverage rules. Reflection, dynamic dispatch, runtime data,
external services, and business requirements remain outside what static source
retrieval can prove.

### Retrieval and delivery

The index should return a short list of candidates with current branch/commit,
source locations, match reasons, and coverage. A follow-up operation should
expand a chosen candidate into a bounded context pack with signatures, relevant
source, tests, callers, implementations, documentation, build facts, and
uncertainty where available. Ambiguity should remain visible. The response
should provide a clear way to narrow or continue retrieval without placing the
entire repository into the model's context.

Keep candidate discovery separate from evidence. Lexical or vector ranking can
find a likely location; exact snapshot validation and the active semantic
provider establish which source and relationships may be returned as current.
Every result should name its branch, commit/tree, build configuration, indexed
coverage, and omissions.

Keep the existing `go_context` behavior intact. If an MCP branch selector is
needed, make it a separately versioned additive contract. The existing v1
registry and frozen schemas stay unchanged. The current CLI preview selects a
ref and creates a visible detached worktree; it does not add branch selection
to the MCP protocol or make an existing server switch workspaces. A future
branch retrieval operation must bind candidates and semantic reads to the
selected view rather than the live checkout.

### Branch source-view review (GPT-6 Luna Max, 2026-09-27)

Two Luna Max reviewers agreed that the live-workspace `go_context` and its
`SnapshotRef` must not silently become branch selectors. Existing snapshots,
source reads, gopls sessions, and mutating tools are rooted in the configured
workspace. Branch retrieval therefore needs a separate, versioned read-only
result identity that binds the requested branch alias to its resolved commit,
tree, build inputs, and overlay digest. Resolve branch names again for a new
request; if the ref moves during a request, discard the result and require a
new view. Branch-view references must not be accepted by live-workspace
refresh or edit operations.

Dirty changes have a base. Include staged, unstaged, and untracked changes only
when their captured parent commit/tree is the selected branch view. Reject an
explicit overlay request against a different branch; do not use three-way or
fuzzy patch application to imply that feature-branch edits also describe
`main`. When no overlay is requested for an inactive branch, return its exact
committed source and label the overlay as absent. Recheck source contents and
the branch ref before returning results. Never fall back to live-workspace
bytes if the selected view or its gopls session fails.

An inactive read-only branch view can answer questions about that commit, but
it does not prove the coding agent is editing the same tree. The response must
identify whether its source view matches the agent's configured workspace and
make its source root available for a deliberately selected edit workspace.
The agent outcome study must direct edits to that exact view or reject the run;
patches written to a different checkout do not qualify branch-aware value.

The preview chooses a **visible detached Git worktree**. This preserves Git's
normal checkout and filter behavior and leaves the source checkout on its
current branch, while making the managed worktree visible and opt-in. It adds
metadata under the source repository's Git worktree area and must be removed
with normal `git worktree remove` lifecycle handling when no longer needed.
Uninitialized submodules are reported as partial. External `go.work` and local
`replace` inputs, and agent edits made outside the returned view, remain outside
the current source-view contract. The CLI tests cover default `main`, explicit
feature selection, source-checkout isolation, staged/unstaged/untracked
overlays, additions/deletions, wrong-base rejection, branch movement, missing
or corrupt markers, and failed-worktree cleanup. This is a branch source-view
foundation, not a retrieval or accepted-patch qualification.

The index should remain local by default. Store only the derived material
needed for search where possible, use private cache permissions, enforce disk
and memory quotas, and never put source text, prompts, paths, or branch content
into telemetry. Hosted embeddings or remote indexing would change the privacy
and product boundary and need a separate decision.

## Design roads

### Road 1: qualify the current retrieval path

The process-local Go declaration cache has a model-free relevance screen, but
the corrected medium result is weak and the large result is partial. Improve
coverage and the native comparison before deciding whether a single ranking
change is justified. Keep public contracts stable until a separately versioned
interface is supported by evidence.

### Road 2: persistent local branch index

If evaluation shows that repeated parsing or ranking is a material cost, build
a local content-addressed index with per-file records and inverted postings.
Reuse identical files across commits, update changed blobs, keep a bounded
memory cache, and publish a branch generation atomically. A crash or corrupt
generation must leave a known usable generation or produce an explicit rebuild
state. A quota, garbage collection, index-version migration, lock strategy, and
cancellation behavior are part of the design, not later polish.

Separate retrieval cost from snapshot and gopls cost. Persistent storage helps
only if retrieval work is the bottleneck. Preserve strict freshness and do not
serve a result from an older branch or worktree overlay as current.

### Road 3: add semantic retrieval selectively

Use Go syntax and gopls-supported relationships after an anchor is selected.
Add broader graph expansion only when held-out tasks show a missed relationship
that changes the answer. Test local embeddings only if lexical retrieval
repeatedly misses natural-language questions; vector scores remain discovery
signals and must be checked against source. Do not embed a hosted model or
expand to other languages in the current Go-only product cycle.

## Retrieval evaluation: model-free screen results

The initial model-free screen of the existing process-local Go declaration
retrieval path is complete. It used pinned Git snapshots, reviewed gold spans,
and three repetitions on the medium corpus; the small and large corpora were
single-run screens. This evaluates candidate discovery, not natural-language
answers, accepted patches, or persistent indexing. Each report records the
source, manifest, retrieval-code, and dirty-diff hashes.

| Corpus | Pinned source and coverage | Retrieval result | Interpretation |
| --- | --- | --- | --- |
| Small Go 1.27 Interactive Tour | `61ca6068c067756caf8d98b0a933163b39894416`; 17 Go files / 9,444 Go bytes; two Go files parse-incomplete. Git archive is complete, but retrieval metrics are unusable. | No usable retrieval or native metrics; gopls reports unknown completeness. | Too little valid Go evidence for a relevance conclusion. Most reviewed evidence is documentation, and retrieval indexes no text files. |
| Medium Agentic Go | `7b5111c6365a2a12806a561d86745d5bc50d7c9e`; all 420 supported source files captured (270 Go files / 1,343,953 bytes; 150 text files / 1,638,309 bytes); no parse-incomplete Go files. | Four questions, three repetitions: Recall@5 0.20, Precision@5 0.20, MRR@5 0.2375; Recall@10 0.35, Precision@10 0.20, MRR@10 0.26875. | Weak candidate relevance. Text exists in the corpus but `text_indexed_by_retrieval` is zero. This is not an agent-outcome or product-value result. |
| Kubernetes | `dfd7b93a1783878be367e1fc4a780318330cb3bf`; 17,856 Go files / 188,240,197 Go bytes. It meets the proposed large stratum by file count, not by the 250 MiB source threshold. Two supported symlinks were not indexed and two blobs (5,588 bytes) were transformed by `git archive`; package-inventory output reached its 16 MiB cap. | Relevance metrics are unusable. One run observed warm retrieval p50 10.39 s / p95 10.52 s, cold p50 10.48 s, and peak sampled Go heap 702,613,528 bytes. Parsing was about 9.43 s warm. | Partial-run operational signal only. Warm p95 exceeds the 5 s query target. Sampled Go heap was about 703 MB; process RSS was not measured, so this does not establish whether the 512 MiB process-memory target was met. Do not claim arbitrary-large support. |

The corrected medium report is
[`medium-agentic-go-anchor-v2.json`](../../validation/retrieval/results/2026-09-27/medium-agentic-go-anchor-v2.json)
(SHA-256 `f4c8f8d8a9546b24d1251829b96acbf8bcbed18dd5bd4d8d0cf2133a167a893e`).
The original `medium-agentic-go.json` artifact is retained, but its relevance
score is superseded: some gold declaration ranges began inside a declaration
body while candidates are declaration-name anchors. The corrected spans
include the relevant symbol-name anchor lines. The corrected manifest SHA-256
is `ef352f80ddf9a31c5db7e68465e0c208290313ebd315245ca3ec85d6d1ecba4b`.

The capped large rerun is
[`large-kubernetes-capped-v2.json`](../../validation/retrieval/results/2026-09-27/large-kubernetes-capped-v2.json)
(SHA-256 `f95231a4bcf9f3942a89c815f73c4be727a627fc0221858ecbaf6d0d50c29efa`).
The earlier large artifact recorded package-inventory output above the stated
limit. The bounded writer and regression tests now cap it at 16 MiB and mark
the inventory `partial_output_limit`; the corrected rerun records 736 package
objects before truncation. It still does not repair the separate Git archive
coverage gaps, so its relevance scores remain unusable.

The fixed `rg` arm scored zero on all four corrected medium questions because
the harness tokenizes natural-language queries and ranks matching lines by
token overlap. That is not a competent native `rg` + Go tools + gopls workflow
and cannot support a superiority claim. The gopls `workspace/symbol` arm is
directional only: exhaustive-result completeness is unknown. The package
inventory is separate and has no relevance score. Improve and freeze the
native workflow before making comparative claims. No GPT-6 Luna Max task runs
have been performed; this screen is model-free and is not mixed with
historical GPT-6 Sol/high findings.

The separate six-cell selector screen completed with safety passed and the
efficiency promotion gate failed. That is selector-guidance reliability
evidence, not retrieval evidence; it neither blocks nor qualifies retrieval.
Do not run the stale 18-cell rerun. Keep its outcome separate from retrieval.

### Private text-candidate ablation

A fresh four-question manifest was reviewed before scoring. It is pinned to
the same Agentic Go commit and tree as the medium screen and covers coordinate
handling, audit-rule evidence, artifact pagination, and execution trust
boundaries. The manifest hash is
`3983e5ffdbd8155ba8aedbbc70eb0b251b35629c81005f4fb9b71e2feef9e2fb`. A
GPT-6 Luna Max evaluation-methodology review removed query hints and tightened
gold anchors before the run. The scoring itself is deterministic and model-free.

The ablation captured all 150 supported text files (1,638,309 bytes) from the
complete 420-file archive, and its capped mixed candidate pools remained
complete at 6,125–9,231 candidates per query. Query-matched candidate-pool
Recall was 1.00 for all four questions (15/15 gold spans). The current Go-only
retrieval path on these fresh questions had macro Recall@10 0.05, Precision@10
0.025, and MRR@10 0.25. Adding text line fragments to the evaluation-only
candidate set raised macro Recall@10 to 0.1625, Precision@10 to 0.075, and
MRR@10 to 0.28125; it retrieved 3/15 gold spans in the top 10. This is a
small-sample coverage and ranking diagnosis: candidate generation can surface
the reviewed evidence, while the existing scorer does not rank enough of it
near the top. The result remains weak and does not demonstrate useful agent
outcomes or justify a public retrieval change or persistent index.

The ablation emitted at most 50,000 eligible text-line fragments per query;
all four candidate-pool audits were below the 10,000 mixed-candidate audit cap.
The retrieval report records capture counts, coverage, per-question metrics,
timings, and source hashes at
[`medium-agentic-go-text-ablation-v1.json`](../../validation/retrieval/results/2026-09-27/medium-agentic-go-text-ablation-v1.json)
(SHA-256
`b276711e4cfdde2c997a67c227fcb52a25ec6596b2deb253f1896abd0e9628ab`). Text
capture and ranking were evaluation-only; live `SearchProfiled` remains
Go-declaration-only. Its roughly 86–94 ms per-question ablation timings include
the mixed-candidate work and are not a warm-query comparison.

### Evaluation question

Does the current retrieval path surface independently judged relevant,
source-grounded candidates at useful quality and cost across repository
scales, compared with native `rg`, Go tools, and upstream gopls? How much of
the measured request time belongs to candidate retrieval rather than snapshot
capture or semantic resolution?

### Study plan

1. Freeze three representative Go repositories at pinned commits: small,
   medium, and large. Treat at least 10,000 Go files or 250 MiB of Go source as
   a proposed large-repository stratum, not a claim of existing support. Include
   a repository with multiple packages/modules where available.
2. Prepare held-out questions for symbol discovery, a multi-package behavior
   path, and a cross-package API change. Record independently reviewed relevant
   declarations, call sites, tests, docs, and evidence limitations before
   seeing ranked results. Use a branch-matched source view and mark incomplete
   archives and unsupported evidence unusable rather than pooling them.
3. Compare the existing retrieval path with native `rg`, Go tools, and upstream
   gopls using equivalent instructions and task budgets. Keep ordinary tool
   availability separate from extra workflow guidance so integration effects
   are visible.
4. Measure candidate Recall@5/10, reciprocal rank, source-span precision,
   omitted relevant tests/relationships, and explicit coverage. Measure cold
   and warm query p50/p95, snapshot capture, candidate retrieval, gopls
   resolution, peak memory, disk size, and response bytes. Report snapshot,
   retrieval, and semantic-provider time separately. This is a model-free
   benchmark; it makes no claims about agent decisions, accepted changes, or
   token savings.

### Later branch-supported engineering screen

After the product can select a branch and resolve evidence against that exact
branch's source view, run a separate matched native-versus-Agentic-Go screen
using GPT-6 Luna Max only. Keep the model version, settings, tasks, instructions,
and budgets matched across conditions. Measure answer correctness, accepted
changes, consequential omissions, engineer review/rework effort, and total
time. Do not use the historical GPT-6 Sol/high 72-run harness or mix models.
This later screen tests engineering outcomes; it is not part of the model-free
retrieval and repository-scale benchmark.

### Decision gates

- The current medium relevance screen is weak and the large screen is partial.
  Before the one allowed retrieval redesign, repair coverage and freeze a
  competent native baseline plus fresh held-out questions. Make one isolated
  change against a diagnosed failure, then evaluate it on different fresh
  cases. If relevance does not improve or coverage cannot be established, stop
  this retrieval-value path and do not add persistence. Engineering outcomes
  belong to the later matched model screen.
- Prototype a persistent index only when the current approach misses a
  predeclared interactive latency or memory budget on representative large
  repositories, profiles attribute most of that cost to candidate retrieval,
  and retrieval quality is useful enough to preserve. Require a persistent
  prototype to improve warm-query time by at least 2x without lowering
  held-out relevance or freshness. The current screening targets are 5 s warm
  query p95 and 512 MiB peak process memory; they are not support promises.
- If snapshot capture dominates, address that cost or narrow the observed
  scope before investing in disk indexing. If retrieval is useful but lexical
  misses dominate, test one local semantic-ranking option on held-out queries
  before expanding the index format.
- The branch source-view preview exists, but the engineering-outcome screen
  stays gated on useful retrieval and an exact task workspace. Then compare
  native Go tools and Agentic Go on eight held-out tasks across three
  repositories, randomized paired order and two fresh repetitions per arm
  (32 GPT-6 Luna Max runs). Freeze source, binary, task, prompt, and evaluator
  hashes first. Require zero accepted stale/wrong-branch evidence, no accepted
  patch-quality loss, and a practical gain such as 15% lower median time to an
  accepted patch; track review effort and actual token use. Report the
  same-model blind-reviewer limitation. A screen is directional, not a
  statistically powered or general claim.

The measurements qualify only the repositories, questions, builds, branches,
models, and hosts included. They cannot prove complete understanding of every
codebase or guarantee that an agent will follow retrieved evidence.

## Decision record

- 2026-09-27: The product owner set branch-aware, reusable codebase indexing
  and fast, accurate retrieval as a long-term product aspiration.
- 2026-09-27: The GPT-6 Sol council recommended finishing and evaluating the
  existing process-local retrieval path before persistent storage. Persistent
  local indexing remains the conditional scale path; embeddings and broader
  language support remain deferred research.
- 2026-09-27: The six-cell selector screen passed safety but failed its
  efficiency promotion gate. This result is separate from retrieval evaluation;
  do not run or direct an 18-cell rerun absent a new bounded intervention.
- 2026-09-27: GPT-6 Luna Max verdicts set the model-free held-out retrieval and
  repository-scale benchmark as the first research action, followed only after
  branch support exists by a separate matched GPT-6 Luna Max native-versus-
  Agentic-Go screen. Preserve both dirty source worktrees; do not use the Sol/
  high 72-run harness or mix models. At the time of that decision, retrieval
  evaluation and branch-aware product behavior were still pending.
- 2026-09-27: Luna Max source-view review separated branch retrieval from live
  `go_context`, required exact-base overlays and agent/edit alignment, and left
  the source materialization mechanism open pending a fidelity fixture.
- 2026-09-27: The model-free screen completed. The corrected medium corpus
  scored 0.35 Recall@10 and 0.20 Precision@10 with zero document retrieval.
  The small screen had no usable retrieval metrics. Kubernetes coverage was
  partial; the capped package inventory records its 16 MiB cutoff. No
  persistent index or retrieval superiority claim is justified.
- 2026-09-27: A fresh, reviewed four-question text-candidate ablation recovered
  all 15/15 gold spans in complete candidate pools, but the top-10 mixed
  ranking reached only macro Recall 0.1625, Precision 0.075, and MRR 0.28125.
  This evaluation-only candidate augmentation diagnoses both a text-coverage
  gap and a remaining ranking failure. It does not qualify live text retrieval
  or persistence; the one bounded retrieval redesign allowance is consumed.
- 2026-09-27: Luna Max architecture, evaluation, and product reviewers agreed
  that persistence and the 32-run engineering screen remain gated. A private
  text-candidate ablation measured full-pool coverage but weak ranking, so no
  persistence or 32-run agent-value study is justified. The source-view CLI is
  a preview foundation with exact branch identity, not a branch-aware
  persistent retrieval product. Repair the native workflow before any future
  comparative claim; keep the reliability screen and historical Sol studies
  separate.
