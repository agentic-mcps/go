# Phase 4 — Static Analysis Audit Suite (index)

> **Release status:** v0.1.0 implements only the concurrency and errors
> domains. The remaining five domains and `go_audit_all` are roadmap scope;
> see [v0.1.0-release-scope.md](v0.1.0-release-scope.md).

Read `contracts.md` first — it defines `Finding`, `AuditResult`,
`Severity`, `Location`, the canonical `go/analysis` pass skeleton, the
6-step rule→AST-pattern conversion recipe, and the isolated
fixture layout. Every file in this directory extends those types, does not
redefine them.

## Non-negotiable rigor requirement
**Every rule extracted from a domain's reference file gets full AST-pattern
detail — the exact `go/analysis` predicate, not a description of one.** This
was an explicit user decision after an adversarial cost/rigor tradeoff was
raised and rejected: partial detail (2 of 7 domains worked through, 5 left as
recipe-only) was proposed as the cheaper default and explicitly turned down.
A domain file that leaves any rule as "apply the 6-step recipe here" instead
of showing the applied result is incomplete, not acceptably scoped-down.

## The 7 domains

The original planning corpus is incorporated into each domain specification;
no private or external reference checkout is required to implement a rule.

| # | Domain | Rule source | Domain file | Tool name |
|---|---|---|---|---|
| 1 | Concurrency | Reproduced in domain spec | `phase-4a-concurrency.md` | `go_audit_concurrency` |
| 2 | Errors | Reproduced in domain spec | `phase-4a-errors.md` | `go_audit_errors` |
| 3 | Security | Reproduced security subset | `phase-4a-security.md` | `go_audit_security` |
| 4 | Observability | Reproduced observability subset | `phase-4a-observability.md` | `go_audit_observability` |
| 5 | Naming | Reproduced in domain spec | `phase-4a-naming.md` | `go_audit_naming` |
| 6 | Type design | Reproduced in domain spec | `phase-4a-type-design.md` | `go_audit_typedesign` |
| 7 | Performance | Reproduced in domain spec | `phase-4a-performance.md` | `go_audit_performance` |

**Cross-domain partition rule (applies to every domain pair, not just 3/4).**
Domains 3 and 4 read the same source file and must partition its rules by
topic rather than duplicate a rule in both files — but source-file overlap is
only the most visible case of a general problem: any two domains can converge
on the same AST pattern from different reference material. Two such pairs
are easy to miss with only a shared-source check: `performance-08`
and `concurrency-14` both matched "buffer pool `Put` without a preceding
reset," and `naming-23`/`typedesign-08` both matched the same embedded-mutex
shape. The rule is not "check domains 3 and 4 against each other," it is:
before a rule ships, check its concrete AST predicate against every other
domain file's predicates for the same `ast.Node` shape, not just the domain
sharing a reference source. If two domains' predicates would fire on the same
fixture, assign the rule to whichever domain's audience question it answers
more specifically (e.g. "is this a leak risk" → security; "can an operator
debug this from logs" → observability; a resource-lifecycle bug specific to
concurrency correctness → concurrency, not the more general performance
domain) and cross-reference in one line in the domain file that loses the
rule — don't duplicate the full AST pattern in both files, and don't leave
the cross-reference implicit.

## Required structure per domain file

Each `0N-<domain>.md` must contain, in this order:

1. **Rules table.** Every distinct rule found in the source reference file
   relevant to this domain, each with a short rule ID (`concurrency-01`,
   `concurrency-02`, ...) — this ID becomes `Finding.Rule`'s value verbatim
   (not a restated description — implementation copies this ID literally
   into the `go/analysis` pass code).
2. **Per-rule AST pattern** (the full 6-step recipe applied, not summarized):
   - The rule's exact source sentence (quoted).
   - Why it's checkable (what makes it a syntactic/semantic pattern and not
     a judgment call — if a rule genuinely cannot be reduced to an AST
     predicate with acceptable false-positive risk, say so explicitly and
     exclude it from the tool rather than inventing a fragile heuristic;
     document the exclusion in this file, don't silently drop it).
   - The concrete `go/analysis` predicate: which `ast.Node` type(s) to
     inspect, the exact structural condition (field checks, type checks via
     `go/types`, call-expression shape), and one short Go code fragment
     showing the check (not full pass boilerplate — that's in
     `contracts.md` already — just the predicate body).
   - The `Finding.Message` template (with placeholders for the specific
     values the pass fills in, e.g. `"goroutine created without a context
     parameter at %s"`).
   - `Finding.Severity` assignment with justification tied to real
     consequence (matches this project's own domain: a missed idempotency
     check on work-item routing is `SeverityError`, a naming-style
     nit is `SeverityInfo` — severities are not uniform across a domain,
     assign per-rule).
3. **Fixture file spec** — one isolated subpackage under
   `internal/tools/testdata/fixtures/audit-<domain>/`, per `contracts.md`'s
   8-subpackage rule. Must contain: one deliberate violation per rule (so
   every rule has at least one true-positive fixture case) AND one
   deliberate near-miss per rule that looks superficially similar but must
   NOT trigger the finding (so every rule has at least one guarded
   true-negative case — this is what proves the AST predicate is precise,
   not just present). List exact file names and the specific code each
   contains, not "a fixture with violations."
4. **Tool file spec** — `internal/tools/go_audit_<domain>.go`: input shape
   (`Audit<Domain>Input{Package string, MinSeverity, MaxFindings}`, matches
   every other audit tool per `contracts.md`'s conformance block, no
   domain-specific input needed), output is **always**
   `Audit<Domain>Output{Result finding.AuditResult}` — never a bare
   `finding.AuditResult` return. Every tool's `Output` struct wraps the
   result; a domain file returning the result type directly is a spec
   violation, not an accepted variant (this replaces the earlier wording,
   which is what let `phase-4a-performance.md` return `AuditResult` bare — that
   deviation cannot be fixed while this clause still licenses it). Wire into
   the canonical `checker.Analyze`-based pass-runner described in
   `contracts.md`. Not cached (matches every other audit/diagnostic tool
   in this spec — analysis results go stale the moment source changes).
5. **Verification** — one test asserting, per rule: the true-positive
   fixture produces exactly one `Finding` with that `Rule` ID at the correct
   `Location`, AND the near-miss fixture produces zero findings for that
   rule. A domain file whose verification section only checks the
   true-positive case is incomplete — the near-miss assertion is what
   separates a real check from a keyword grep, per this project's own
   `go-testing.md` WHY-not-WHAT rule.

## Tool 8: `go_audit_all` — cross-domain consolidation

The 7 domain tools above each run one `analysis.Analyzer` in isolation. A
caller auditing a whole package who wants "everything" currently has to call
all 7 and merge results itself — the `audit-package` prompt (see
`phase-3-gopls-navigation-resources-prompts.md`) did exactly that before this
tool existed, and merging client-side is both slower (7 separate
`packages.Load`s of the same package) and easy to get wrong (a caller
forgetting one domain silently under-audits).

`go_audit_all` is `internal/tools/go_audit_all.go`:

- Input: `AuditAllInput{Package string, MinSeverity, MaxFindings}` — same
  shape as every per-domain input, no new fields.
- Output: `AuditAllOutput{Result finding.AuditResult}` — same wrapped shape
  as every per-domain tool (item 4 above), not a map keyed by domain. A
  `Finding.Rule` value (`concurrency-01`, `security-07`, ...) already
  identifies which domain produced it; a second grouping structure would be
  a redundant view of the same data.
- Handler: one `packages.Load` (not 7), then a single
  `checker.Analyze(allDomainAnalyzers, pkgs, opts)` call passing the full
  7-element `[]*analysis.Analyzer` slice from `registry.go` — `checker`
  itself runs all 7 over the shared loaded packages and shared type
  information in one pass, so this is not "call 7 tools sequentially and
  concatenate," it is one load and one build feeding all 7 analyzers. Merge
  every `Action.Diagnostics` across all 7 into one `[]finding.Finding`, sort
  by `Location.File` then `Line`, then `MinSeverity`/`MaxFindings` filtering
  and truncation bookkeeping exactly as every per-domain tool already does.
- Not cached, same rule as every other audit tool in this suite.
- **Verification:** `TestAuditAll_UnionMatchesPerDomainTools` runs
  `go_audit_all` against a workspace containing all 7 domains' fixture trees
  (`audit-concurrency/`, `audit-errors/`, ..., `audit-typedesign/`) and asserts
  its `Findings` set (compared as `(Rule, Location)` pairs, order-independent)
  equals the union of the 7 domain tools' own findings over the same fixture
  set. This is both the compositional correctness check for this tool and,
  per finding 14's fix, the mechanical cross-domain-duplicate detector: a
  `Rule` pair sharing an identical `(Location, Message)` across two domains on
  the same fixture would surface here as a set-equality failure the individual
  per-domain tests can't see, since each only ever runs against its own
  fixture tree.

Registered in `registry.go` alongside the 7 domain analyzers; the count gate
in Phase 6 counts it as tool 8 of this suite (30 across the whole project —
see `contracts.md`'s tool inventory section).

## Dispatch model
Each domain file is written by one subagent, cold-started with only that
domain's row from the table above plus `contracts.md` — no access to this
planning session's conversation, no access to the other 6 domains' work in
progress. This mirrors the plan-arbiter decision memo's executor
recommendation and is deliberate: domain files must be self-contained enough
that a completely fresh reader (human or agent) can execute one without the
other 6 or this planning conversation.

**Cold-start isolation produces drift; it is not sufficient on its own.**
Seven independent dispatches, each free to interpret anything not pinned
down in `contracts.md`, is exactly how imports, `report()` signatures,
entry-point shapes, and fixture paths diverged across the 7 files before
this remediation pass. Isolated dispatch remains correct for *writing* a
domain file — it is what keeps each file self-contained — but it is not
sufficient for *finishing* the suite. A mandatory consolidation sweep runs
after all 7 domain files (and this index) are written: one pass, reading all
7 files together, checking each against `contracts.md`'s conformance
block and against each other for the categories that drifted last time (rule
ID format, import block presence and shape, `report()`/`Report()` call-site
signature, entry-point signature, fixture path form, `Tests: true` and
`NeedTypesSizes` presence, cross-domain rule duplication). This sweep is
itself now a permanent step in this suite's build process, not a one-time
fix — record it as such wherever this index is next revised.
