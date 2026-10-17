# Agentic Go vs native Go tools: Luna Max Q&A screen

**Decision:** the current Agentic Go surface has not shown useful value for
natural codebase questions against native Go tools. Do not add a persistent
index on this evidence.

The main comparison ran 32 read-only answers: eight questions, two repetitions
per arm, across one small, one medium, and one large Go repository. All agent
and reviewer calls used `gpt-6-luna` with `max` reasoning. No Sol, GPT-5.6, or
other model was used.

## Main comparison

The native arm used `rg`, `sed`, Go tools, and gopls. The matched Agentic arm
had the Agentic Go MCP server available with the same question prompt and source
commit. Run order was randomized. The source tree was checked before and after
each run; all 32 stayed clean at their pinned tree.

| Measure | Native Go tools | Agentic Go available |
|---|---:|---:|
| Luna answer runs | 16 | 16 |
| Mean blind score, /10 | 8.88 | 8.81 |
| Mean correctness, /4 | 3.69 | 3.75 |
| Mean completeness, /4 | 4.00 | 3.88 |
| Mean citation support, /2 | 1.19 | 1.19 |
| Median answer time | 125.2 s | 128.1 s |
| Median reported input tokens | 325,798 | 345,212 |
| Agentic MCP calls | 0 | 0 |

By question, Agentic availability won one, tied six, and lost one. Its average
score was 0.06 points lower. The native answers had one reviewer-marked major
error and the Agentic answers had none, but no Agentic tool was called, so that
difference cannot be credited to retrieval. Median reported input usage was
about 6% higher and median answer time about 2.3% higher in the Agentic arm.
These small deltas are descriptive, not a measured retrieval cost or a
statistically reliable product effect.

The Luna reviewer received pinned source excerpts and randomized answer labels.
It scored correctness, completeness, and citation support. The reviewer was
from the same GPT-6 Luna family as the answer model, so this is directional
review rather than independent validation. Citation support averaged 1.19/2
in both arms; the reviewed excerpts did not cover every claim made in each
answer. Token counts are Codex's reported input totals; cached input is a
subset of that total. Duplicate identical usage events were counted once.

Codex logged no MCP startup error in 12 of 16 sessions, covering all medium
and large runs. The server failed preflight in the four small runs:
both small repositories lacked `go.mod` and an active `go.work`. In the 12
sessions where the server started, the model still made no Agentic Go call.
The question prompts asked for code explanations, while the current `go_context`
description recommends calling it before editing. The study therefore measures
the effect of making the current MCP surface available during ordinary Q&A; it
does not measure the quality of a successful retrieval call.

## Prompted-use diagnostics

These diagnostics are separate from the matched 32-run result.

An eight-run prompt-guided probe requested one `go_context` call before each
answer. The three medium-repository calls completed but returned 9–15 ambiguous
candidates. The three large Kubernetes calls failed with `stdout exceeded
8388608 bytes` while building change context. The two small-repository servers
failed the same Go workspace preflight described above.

A second, three-task medium probe allowed candidate selection and follow-up
calls. Each selected-symbol call completed but the default 8 KiB focus budget
omitted the focused evidence pack. The agent then read source files with native
tools. In a separate medium retry, Luna changed the supplied Symbol Ref while
copying it, and the server correctly rejected the malformed reference.

One additional medium task used an explicit file, line, and column selector
with a 64 KiB budget. It returned the selected declaration, 20 source-line
anchors, 30 direct call sites (24 returned, truncated), and nine related test
declarations. Typed evidence was incomplete, and the response contained line
anchors rather than function bodies. The agent then issued three native shell
reads/searches to verify the implementation and tests. Against one native
guided run on that question, the full run took 144.0 s versus 152.5 s and used
304,805 versus 351,892 input tokens. This is one manually guided pair, below
the 15% time-gain target, and was not blind-scored. It shows that context
anchors can direct source inspection on one supported medium module; it does
not establish a general speed, accuracy, or token advantage.

## What this study establishes

- Adding the current MCP server to ordinary codebase-question sessions did
  not cause Luna to use it or improve the reviewed answer scores.
- A deliberate `go_context` workflow can surface symbol, caller, test, and
  source-location anchors in a supported module. It can require multiple
  selections, a larger byte budget, and native source reads.
- The tested setup rejected Go source directories without a module and could
  not complete `go_context` on the large Kubernetes source tree because a
  subprocess exceeded its 8 MiB output limit.
- This study did not test accepted patches, branch movement, default-main
  selection, dirty overlays, persistent generations, warm-index latency, or
  repository-independent coverage. It makes no claim of arbitrary-scale
  support.

The eight questions had already been used in the model-free retrieval screen,
so they are not an independent question holdout. The main screen used fresh
Luna sessions, not fresh questions. The study also did not compare against a
prebuilt persisted index.

## Next decision

Do not add persistent indexing yet. The current surface did not demonstrate
retrieval value for natural Q&A, and the study did not isolate repeated parsing
or ranking as the dominant cost.

If the product continues toward the requested “ask a codebase question” use
case, scope a query path that returns enough source-grounded excerpts for the
selected branch, handles bounded output on large repositories, and gives an
agent a reliable way to resolve candidates. Evaluate that path on fresh held-
out questions against matched native workflows. Consider persistence only if
retrieval quality is useful and profiling shows repeat parsing or ranking is
the bottleneck.

The separate accepted-change screen remains gated. This Q&A screen does not
qualify patch quality or the branch-aware persistent-index aspiration.

## Reproducibility record

The machine-readable run and review manifest is
[`agentic-go-native-context-outcome-luna-max.json`](../../validation/retrieval/results/2026-09-28/agentic-go-native-context-outcome-luna-max.json).
It records the pinned commits and trees, task and review prompt hashes, per-run
answer hashes, timing, token usage, MCP call summaries, blind ratings, and the
separate prompted-use diagnostics.

The run used Codex CLI `0.155.0-alpha.16.4`, SHA-256
`93169e745735930598e867ad837abf3fdc50774a3ad7e7aa89c0d0c51b0189a5`; the
Agentic Go binary SHA-256 was
`6efe4c0b67f95ecf528299dc8be8c769ac388e6f303f564366f0f4b3cc9495c3`. The
gopls companion reported `v0.21.0`.
