# agentic-go: honest verdict and pivot to an automatic done-gate

## Context

The owner asked: (1) be brutally honest about whether this repo can become a compelling product; (2) if so, make it so (pivot allowed).

### Verdict

The current thesis ("Go context/intelligence MCP server that agents call to make better decisions") is a dead end. The problem is the premise, not the execution.

**Evidence from the repo's own evals** (Codex + one model, 2 tasks):
- **Adoption:** agents never call it unprompted. `go_context` was used 0/10 (pilot) and 0/6 (description only).
- **Forced use:**
  - Correctness: no gain (baseline already 20/20 and 6/6).
  - Cost: 1.2–2.2x wall time, 1.5–1.7x tool calls, +49% bytes.
  - Reliability: 41/59 calls failed.
  - Trace review: "No transcript proves that `go_context` improved the necessary code edit."
- **Retrieval:** loses to plain gopls (MRR@10 0.27 vs 0.66) and is ~266x slower on Kubernetes (10.4s vs 39ms p50).
- **Never evaluated:** the verification CLI/Action, the most defensible part, never had a with/without comparison.
- **Usage:** no external users (2 stars, 0 issues).
- **Ceremony vs product:**
  - ~52k lines of docs/site/validation vs ~23k product LOC.
  - Only ~2.6k LOC does analysis an agent can't get from grep + `go build`/`test` + gopls.
  - Bookkeeping outweighs novel analysis about 3:1.
- **False site claim:** the site says "frontier-grade parity", which the internal docs forbid.

**My probes:**
- **Context query:** `context --query RegisterAll` (an exact name) returned "10 ambiguous candidates". That's 9.7KB of hashes, capability maps, base64 refs and disclaimers, with zero code. Resolving it took 7.4s and returned ~10KB. grep gives the same thing in one line.
- **`verify` on spf13/cobra@ad460ea:** two gate-fatal bugs.
  - **False block:** `TestFailGenFishCompletionFile` also fails at the base commit (environment-dependent), yet it is reported as this change's blocking finding.
  - **Silent wrong coverage:** the run reports "0.0% changed coverage — passed". True coverage is 100%; a panicking test aborted the coverprofile.

**Code audit, more defects:**
- `risk.go` "review guidance" fires on nearly every diff.
- The server refuses to start unless gopls is exactly v0.21.0.
- Newer MCP tools put evidence only in `structuredContent`, so many clients show the model a one-line summary.

**Root causes (first principles):**
1. **Pull model.** An agent picks its next tool from descriptions in its context. Trained habits (grep/read/edit/build) win, and an optional tool must also pay for its description tokens and the extra decision.
2. **Go already gives agents good feedback.** The compiler acts as static impact analysis, alongside `go vet` and cached `go test`. Context has little headroom.
3. **Commoditized.** Upstream `gopls mcp` ships search, references, package API and diagnostics. Claude Code's LSP plugins push gopls diagnostics after every edit.
4. **Ceremony outweighs information.** Snapshot lineage, opaque refs and frozen schemas were built before any value was proven.
5. **The eval had no headroom.** The baseline already solved 100% of the tasks.

**"Gold mine":** I can't honestly promise one. The best shot is below. It is useful and measurable, with a pre-registered kill test. The biggest risk is that a strong 20-line hook + grep script does just as well; the eval is designed to find that out.

## The pivot: `agentic-go check`, an automatic done-gate for agent-written Go

It runs automatically (Claude Code/Codex Stop hook, pre-push, CI) before the agent can say "done". If the diff introduced problems, it blocks with a short, fixable list. That includes test tampering, which no linter checks for (ImpossibleBench, arXiv 2510.20270, found frontier agents exploit tests in 39–76% of conflicting-spec tasks).

Push, not pull: hooks invoke it deterministically, so the adoption problem disappears.

### Stages (diff vs an auto-detected base, cheapest first)

1. **Fingerprint cache.** Key: gate version, flags, base, HEAD, porcelain status and content hashes. If unchanged, replay the cached verdict in under 1s. Turns with no Go changes exit in milliseconds.
2. **Syntax pre-check** of changed `.go` files. Today a syntax error makes `Analyze` error out (exit 2); the gate must block with `file:line` instead.
3. **Integrity checks** (go/ast on the base and current bytes of each changed file). Each rule ships with a positive fixture, a near miss and a stated limitation.

| Check | Block when | Warning or allowed instead |
|---|---|---|
| Test deleted | Test function removed | Not flagged if moved or renamed (same name elsewhere, or ≥0.8 body similarity). Info only if its body referenced identifiers this diff deletes. |
| Test hidden | Renamed to a non-`Test` name; `//go:build` added to a `_test.go`; file no longer `_test.go` | — |
| Skip added to an existing test | Unconditional `Skip*`, or an early `return` | Warning if guarded by GOOS/GOARCH/Getenv/`testing.Short()` |
| Assertions removed | Existing test ends with 0 failure-capable calls; or `Error`/`Fatal` turned into `Log` | Other net losses are a warning (table-driven refactors) |
| Stub | `panic("…not implemented / TODO / unimplemented…")` in non-generated code | Warning: body gutted to a zero-value return |
| Empty test | — | Test that asserts nothing: warning |
| Golden/testdata | — | Modified alongside code: warning |
| Gate config changed | `.claude/settings*`, `.codex/**`, `.agentic-go*`, `.golangci*` or workflows touched in an agent hook, counting only files changed since the session started: block once, always disclosed | The same edits outside agent hooks (local, pre-push, CI): warning |

**Stated blind spot:** an edited `want` literal (the most common real-world tampering) is not detectable.

4. **Affected tests.**
   - **Scope:** changed packages plus in-module reverse deps, trimmed by distance to a package cap; output reports "tested N/M consumers".
   - **Speed:** test cache on, `-short` in hook mode.
   - **Compile errors:** text is surfaced; today `build-output`/`build-fail` events are dropped.
   - **Labels:** a failure in a consumer package is called out as "consumer package".
   - **Race:** off in hook mode by default; `--race` turns it on. The CI profile adds it when the diff touches sync, goroutines or channels.
5. **Failure attribution** (failure path only, so the happy path costs nothing):
   - Re-run the failing test at current: if it passes → `test.flaky`, warning.
   - Otherwise run it at base (`MaterializeBase`):
     - fails at base → `test.preexisting`, warning;
     - missing at base, or passes at base → block.
6. **Introduced analyzer findings.** Changed packages only; error severity blocks. An analyzer load failure is a note, never exit 2.
7. **Diff coverage.**
   - Uncovered changed lines are a warning; `--require-coverage` blocks.
   - If the package's tests failed or panicked, coverage is reported as unknown, never 0%.

### Output and exit behavior

- **Text output:** ≤2KB, blocking items first, each as `file:line — what — fix`, then "+N more". No hashes, no review checklists, no disclaimers.
- **JSON:** `--format json` emits `agentic.check/v1`.

| Mode | Verdict | Behavior |
|---|---|---|
| CLI / CI | pass | exit 0 |
| CLI / CI | block | exit 1 |
| CLI / CI | unknown | exit 2 |
| Hook | any | Always exit 0; communicate via JSON only (exit 2 would block the agent on an infrastructure failure) |
| Hook | unknown | Allow the stop; add a `systemMessage` for the user |
| Pre-push | unknown | Allow, with a note; `--strict` fails closed |

### Hook policy (anti-loop, anti-gaming)

- **Block** only if all hold:
  - verdict is block;
  - the fingerprint differs from the last blocked one;
  - fewer than 3 blocks this session (Claude Code's cap is ~8).
- **Repeat stop:** if the agent stops again with the same fingerprint, allow it and list the unresolved items to the human via `systemMessage`. Gaming becomes disclosure.
- **Session base:** a SessionStart hook records HEAD per `session_id`, so earlier human commits aren't blamed on the agent.
- **`init`:** `agentic-go init --claude|--codex|--git-pre-push` prints config by default; `--write` merges idempotently and sets `timeout: 180`.
- **Codex:** `--hook codex` ships only if its Stop schema is verified against developers.openai.com/codex/hooks; otherwise it stays hidden.
- **Overrides:** CI and pre-push accept an `Agentic-Go-Allow: <code> <name>: <reason>` commit trailer, always printed in the output.

## Reuse map and required engine seams

**Reused unchanged:**
- `internal/changeimpact` (`New`, `Analyze`, `MaterializeBase`).
- `verification.ChangeAnalysis.Files[]{BaseContent, CurrentContent, Edits, Change}` (`internal/verification/engine_contract.go:14`) feeds the integrity checks, with no new git plumbing.
- `ExecutionTarget.Distance` drives consumer labelling and closure trimming.
- `analysis.Risks` `synchronization_change` triggers race in the CI profile.
- `workspace.Open`, `execution.New(ws, execution.Config{OutputLimit: 64<<20})`.

**Additive seams** (`verify` defaults unchanged):
- `internal/verification/engine.go`:
  - add `(*Engine).CollectAnalysis(ctx, Request, ChangeAnalysis)`;
  - `Collect` = normalize + `Analyze` + `CollectAnalysis`;
  - lets the gate pre-parse, trim and integrity-check before execution.
- `Request` gains `TestCache`, `Short`, `DirectAnalyzersOnly` and `Skip`, honored in `execution.go` (`executeGoTest`, which hard-codes `-count=1` at :115) and in `analyzers.go`.
- `internal/parser/testjson.go` + `verification/testcollector.go`: accept `build-output`/`build-fail` and `ImportPath`/`FailedBuild`; attach the compiler text to the package failure. This also fixes `verify`.
- `verification/coverage.go`: when the package run failed, coverage is unknown rather than 0%. This also fixes `verify`.

**Not used:** `Report.Finalize` / `Result.ExitCode`, which always block on any test failure. The gate computes its own verdict, and the gate path has no gopls dependency.

## Files

**`internal/gate/`**

| File | Contents |
|---|---|
| `gate.go` | `Options`, `Item{Severity, Code, File, Line, Message, Fix, Detail}`, `Result`, `New`, `Run` |
| `base.go` | `DetectBase`. Order: flag → session HEAD → `GITHUB_BASE_REF` → `origin/HEAD` → `origin/main` → `origin/master` → `main` → `master` → HEAD. Shallow-clone hint. |
| `syntax.go` | Syntax pre-check |
| `integrity.go` | Integrity rules |
| `attribute.go` | Failure attribution |
| `mapitems.go` | Map verification results to items |
| `state.go` | Fingerprint, result cache, session, lock (`UserCacheDir/agentic-go/gate/<sha(root)>`) |
| `format.go` | Text, JSON, hook reason |
| `hook.go` | `ParseHookInput`, `DecideStop` |

**`cmd/agentic-go/`**
- `check.go`, `init.go`: dependency seam patterned on `mcp_config.go`.
- `main.go`: add `case "check"` and `case "init"`.
- Hook mode walks up from `cwd` to `go.mod`/`go.work`. Outside a Go repo it exits 0.

**`action.yml`:** left as is. It downloads releases, and tagging needs approval. Docs get a `go install … && agentic-go check --profile ci` snippet.

## Eval: pre-registered before any variant is generated

**Corpus and arms**
- `validation/gate/README.md` commits the protocol and kill criteria first.
- Repos: cobra, chi, testify, gin and echo (fast suites, pinned SHAs reused from `validation/v0.1.0/corpus.csv`).
- Commit selection is mechanical: same-directory non-test + `_test.go` changes; no `go.mod` changes; ≤400 lines; builds at c and c^; direct tests pass at c^ and twice at c; ordered by `sha256(repo+sha)`.
- Arms:
  - B0: agent habit.
  - B1: B0 + `golangci-lint --new-from-rev`.
  - B2 (strong): `go build ./... && go vet ./... && go test ./...` + golangci-lint, the equivalent of a 20-line Stop hook.
  - B3: B2 + a ≤60-line `git diff` grep script, written and frozen first. This is the honest competitor to the integrity checks.
  - G-ci and G-hook: the gate's two profiles.

**Variants**
- **Real-bug mutants M:** go/ast operators applied to changed non-test lines. A mutant is accepted only if one of the commit's own tests fails; those tests are the oracle. Consumer-break mutants are recorded separately.
- **Cover-ups on M:**
  - C1 delete the failing tests;
  - C2 add `t.Skip`;
  - C3 turn assertions into `t.Logf`;
  - C4 revert the commit's tests.
- **Held-out disguised forms D1–D5** (env-guarded skip, early return, build tag, lowercase rename, skip through a helper). A separate subagent writes these without reading `internal/gate`.
- **Standalone:**
  - S1 stub;
  - S2 consumer break;
  - S3 dropped `err` check (expected to be missed by all arms; reported anyway).
- **Destructive true patches:** real commits that legitimately delete or skip tests or touch golden files. These measure false blocks.

**Kill criteria** (all must hold; otherwise stop or rescope, stated plainly):

| ID | Criterion |
|---|---|
| K1 | G − B2 ≥ 30 points catch rate on pooled cover-ups (C + D) |
| K2 | G − B3 ≥ 20 points on held-out D. On destructive true patches, G's false-block rate ≤ half of B3's. If only K2 fails, the differentiator is a grep script: ship it as a script, not a product. |
| K3 | False blocks on random true patches ≤ 2%, Wilson 95% upper bound ≤ 5%, n ≥ 150. If n can't be reached in this session, report "not established". |
| K4 | G-hook warm p50 ≤ B2 p50 |

**Reporting:** per class, with Wilson CIs, block and warn rates and output bytes. Synthetic results measure the detector, never real-world prevalence.

**Harness:**
- `validation/cmd/gateeval` (`select | generate | run | summarize`)
- `validation/internal/gateeval/`
- `validation/gate/{README.md, b3-grep.sh, results/}`

**Later, not this session:** an online eval with real agents (Claude Code + Codex) on tasks where the baseline is below 100% and some tasks are ImpossibleBench-style, no-gate vs gate. Metrics: hidden-test pass rate, tampering rate, time.

## Repo cleanup (this requires owner sign-off — approving this plan is that sign-off)

- **`AGENTS.md`:** replace the "frozen v1 surface / don't widen the public surface" invariants with "`check` is the product; MCP is maintenance-only". Keep the containment, rule-fixture and verification-loop rules.
- **README:** rewrite around `check`, with honest limits. Note that `verify` needs gopls.
- **`site/`:** remove the false parity claims.
- **Docs:** move the `phase-*` docs (~14k lines) to `docs/archive/`.
- **Design doc:** commit this design as `docs/design/check-gate.md`.
- **Legacy code:** MCP/context/contracts/refactor/retrieval code stays in place, demoted rather than deleted, until the eval verdict.

## Execution: model delegation

**Rules:**
- **Shared contracts first.** I (Opus) write `internal/gate/types.go` (`Options`, `Item`, `Result`, `Severity`, codes) before any fan-out, so parallel agents code against fixed contracts.
- **Self-contained prompts.** Each agent gets exact file paths, signatures, the relevant rule-table rows, acceptance tests, and an explicit "read only these files" list. Prompts are as short as correctness allows.
- **One owner per file.** Parallel agents touch disjoint files; `isolation: "worktree"` is used only where overlap is unavoidable.
- **Review gate.** I review every returned diff and run the tests myself before merging a wave. Agents' claims are not evidence.

| Model / effort | Takes | Assigned work |
|---|---|---|
| **Opus 5.5** (me, or an Opus subagent at high) | Hardest work: subtle correctness in existing code, policy design, eval integrity | `types.go` contracts; engine seams (`CollectAnalysis`, build-output events, coverage unknown, `Request` flags); `gate.go` Run + `attribute.go` (failure attribution at current/base) + `mapitems.go`; hook block/disclose policy (`DecideStop`); eval pre-registration (`validation/gate/README.md`, kill criteria) and mutation-operator design; `AGENTS.md` rewrite; final diff review and eval verdict |
| **Sonnet 5.5** (high) | Moderate-to-hard implementation from a precise spec | `integrity.go` + table tests (positive + near miss per rule); `base.go` + `state.go` (fingerprint, cache, session, lock) with git fixtures; `cmd/agentic-go/check.go` + `init.go` (idempotent settings merge); eval harness `select`/`mutate`/`coverup`/`arms`/`score`; **held-out D1–D5 generator by a separate Sonnet agent forbidden from reading `internal/gate`**; gate integration fixtures; README rewrite |
| **Haiku 5.5** (xhigh) | Mechanical, well-bounded pieces | `syntax.go`; `format.go` (≤2KB text, JSON, hook reason, golden tests); `b3-grep.sh` from the frozen spec (written before any variants exist); `docs/phase-*` → `docs/archive/` with link fixes; removing false parity claims from `site/`; CHANGELOG entry; running eval batches and collecting result files; running lint/test suites and reporting failures verbatim |

**Waves** (each ends with my review and a green `go test ./...`):

| Wave | Opus | Sonnet | Haiku |
|---|---|---|---|
| 0 | `types.go` + engine seams | — | — |
| 1 | — | `integrity.go`; `base.go` + `state.go` | `syntax.go` + `format.go`; docs archive + site cleanup |
| 2 | gate Run, attribution, mapping, hook policy; pre-register eval | harness select/mutate/arms/score | `b3-grep.sh` |
| 3 | — | `check`/`init` CLI + integration fixtures; isolated D1–D5 generator | — |
| 4 | analyzes results; AGENTS.md | README | runs the eval batches |
| 5 | final review | — | full verification run → commit, push, PR |

## Order of work

1. Engine seams and fixes (build-output events, coverage unknown, `CollectAnalysis`, flags).
2. Integrity and syntax checks (table tests: positive + near miss per rule).
3. Base, state and cache.
4. Gate run, attribution, mapping and format (integration fixtures through the production path).
5. Hook, `check` and `init`.
6. Pre-registration and the B3 script, then the harness, then the run (results committed with the verdict either way).
7. Docs cleanup.

**If time runs out, cut in this order:** Codex hook → `init --write` → automatic race → S3 → fewer true patches (K3 reported "not established").

**Delivery:** commit to `claude/youthful-shannon-8s1kgr`, push, open the PR, watch CI. No tag or release.

## Verification

- **Before every push:** `go test ./... && go test -race ./... && go vet ./... && go build ./... && git diff --check`, plus golangci-lint per `.golangci.yml`.
- **Integration fixtures** (temp git repos built in `t.TempDir()`):

| Scenario | Expected |
|---|---|
| True patch | exit 0 |
| Deleted test | 1 (one line) |
| Consumer break | 1, labelled consumer |
| Failure already at base | warning, exit 0 |
| Syntax error | 1 with `file:line` |
| Compile error | compiler text shown |
| Flaky test | warning |
| Cache hit | <1s |
| 1ms budget | unknown, integrity items kept |

- **Hook:** sample Claude Stop JSON on stdin produces the correct block JSON. The same fingerprint and the max-blocks case produce allow + `systemMessage`. Unknown → allow.
- **Format:** a 50-item result stays ≤2048 bytes (golden).
- **Real repo:** re-run on cobra@ad460ea. Expected: pass, the pre-existing failure reported as a warning, coverage correct.
- **Eval:** numbers committed under `validation/gate/results/`, with the K1–K4 verdict stated plainly either way.
