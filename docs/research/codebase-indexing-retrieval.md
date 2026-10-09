# Branch-aware codebase indexing and retrieval

Status: user-directed product aspiration and research proposal, recorded
2026-09-27. This document captures the requested direction and the current
findings. It does not change the frozen v1 contracts or claim that the proposed
product behavior exists.

The current release and reliability sequence remains in the
[continuation handoff](../continuation/go-intelligence.md),
[product plan](../plan.md), and
[north star](../go-intelligence-north-star.md). This proposal is a separate
research track. Its first product action is to evaluate the retrieval work
already in the current working tree before adding a durable index.

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

At the time of writing, the branch is `codex/v1.2-reliability` at `7b5111c`,
with existing uncommitted changes. The working tree already contains a private,
process-local retrieval cache in
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
The current retrieval does not index
separate branches, all repository file types, or a complete cross-package
behavioral graph. The handoff records focused retrieval work as present and
held-out relevance and repository-wide qualification as pending; see the
[current implementation status](../continuation/go-intelligence.md#important-limits).

Exact source freshness remains a core property. Current snapshot capture reads
the scoped inputs twice. It retains at most 32 manifests and 8 MiB of manifest
metadata, plus 4 MiB of captured source per observation. A future index can
avoid reparsing unchanged committed content, but it cannot assume that an
uncommitted worktree stayed unchanged because a file timestamp or watcher was
quiet. The existing snapshot and stale-reference rules remain the authority
for current semantic evidence.

The current focus interface uses `base` to describe change context. It is not
a branch selector. The proposed branch-aware behavior needs an explicit branch
identity and an exact source view for that branch. Do not overload `base` with
that meaning.

## GPT-6 Sol council findings

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

Keep the existing `go_context` behavior intact. If branch selection requires a
public selector or operation, make that a separately versioned additive
contract. The existing v1 registry and frozen schemas stay unchanged. Exact
branch semantics also require a source view that matches the indexed branch;
gopls reading a different checked-out branch cannot validate a hit. Choosing
between a private branch snapshot, a managed worktree, or a narrower lexical
only result for inactive branches remains an architecture decision.

The index should remain local by default. Store only the derived material
needed for search where possible, use private cache permissions, enforce disk
and memory quotas, and never put source text, prompts, paths, or branch content
into telemetry. Hosted embeddings or remote indexing would change the privacy
and product boundary and need a separate decision.

## Design roads

### Road 1: qualify the current retrieval path

Finish and evaluate the process-local Go declaration cache within the existing
focus flow. Improve candidate coverage, ranking, and reported incompleteness
only where measured retrieval errors justify the change. This keeps the public
surface stable and establishes whether the current approach finds useful code.

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

## Retrieval evaluation: next product action

The next product action for this proposal is to evaluate the current retrieval
path before implementing a persistent index or claiming broad speed, token, or
accuracy gains. The active reliability work and its required gates remain the
immediate engineering prerequisite. No retrieval campaign has run for this
proposal.

### Evaluation question

Does the current retrieval path help an agent find and use the correct
source-grounded evidence faster, with fewer consequential omissions or less
engineer review effort, than a well-guided native Go workflow? If it does, is
the time spent in retrieval large enough to justify persistent indexing?

### Study plan

1. Freeze three representative Go repositories at pinned commits: small,
   medium, and large. Treat at least 10,000 Go files or 250 MiB of Go source as
   a proposed large-repository stratum, not a claim of existing support. Include
   a repository with multiple packages/modules where available.
2. Prepare held-out questions for symbol discovery, a multi-package behavior
   path, a cross-package API change, and branch-specific behavior. Record
   independently reviewed relevant declarations, call sites, tests, docs, and
   answer limitations before seeing ranked results.
3. Compare the existing retrieval path with native `rg`, Go tools, and upstream
   gopls using equivalent instructions and task budgets. Keep ordinary tool
   availability separate from extra workflow guidance so integration effects
   are visible.
4. Measure candidate Recall@5/10, reciprocal rank, source-span precision,
   omitted relevant tests/relationships, and explicit coverage. Measure cold
   index construction, warm query p50/p95, one-file and branch update time,
   snapshot capture, gopls resolution, peak memory, disk size, response bytes,
   and actual model token/cost data when available. Report snapshot, retrieval,
   and semantic-provider time separately.
5. On a smaller controlled set of coding tasks, measure answer correctness,
   accepted changes, consequential omissions, engineer review/rework effort,
   total time, and real token usage when exposed. Tool calls and output bytes
   are diagnostic data, not proof of value or token savings.
6. Include branch checks: results identify the requested branch and commit;
   `main` and a feature branch do not share changed-file evidence; branch moves,
   stale indexes, interrupted updates, and dirty worktree overlays either
   resolve exactly or return an explicit unavailable/incomplete state.

### Decision gates

- First establish whether the existing retrieval improves held-out candidate
  relevance and engineering outcomes over guided native tools. If it does not,
  make at most one bounded ranking or interaction redesign before stopping this
  expansion.
- Prototype a persistent index only when the current approach misses a
  predeclared interactive latency or memory budget on representative large
  repositories, profiles attribute most of that cost to candidate retrieval,
  and retrieval quality is useful enough to preserve.
- Before evaluation, select a single warm retrieval p95 target from the
  council's proposed two-to-five second range and choose the memory budget.
  Require a persistent prototype to materially reduce measured retrieval cost
  without lowering held-out relevance, increasing stale/branch leakage, or
  hiding incomplete coverage. A two-times warm retrieval improvement is a
  proposed minimum for the extra storage complexity.
- If snapshot capture dominates, address that cost or narrow the observed
  scope before investing in disk indexing. If retrieval is useful but lexical
  misses dominate, test one local semantic-ranking option on held-out queries
  before expanding the index format.
- Use the repository's existing finite model/client evaluation design for a
  later engineering-outcome screen. Name the model and host combinations
  actually tested; a result from one model is not a claim for all agents.

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
- 2026-09-27: The evaluation plan is the next product action after the current
  reliability prerequisite. No evaluation was run and no new product behavior
  was implemented in this documentation change.
