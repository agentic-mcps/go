# Adoption and discoverability results

## Executive decision

The focus capability is technically usable when the instruction surface points
the agent to it. MCP description-only discoverability produced no treatment use
in 0/6 runs. Generic prompt guidance produced use in 6/6 runs, and the shipped
`agentic-go-context` skill produced use in all 6 integrated diagnostic runs.
The initial integrated set was safe and qualifying in 5/6 runs; the one failure
was a scope violation. The scope-wording rerun was safe and qualifying in 3/3
runs.

Retain the focus implementation and full-replacement refresh. Do not implement
delta refresh from this evidence. Investigate provider failures as the next
reliability issue, then improve discoverability and instruction-surface
placement and repeat a smaller adoption check after independent instrumentation
shows treatment use.

This report is descriptive feasibility evidence. It does not establish causal
engineering improvement, statistical significance, speedup, token savings,
generalized reliability, general adoption, or performance.

## Evaluation design

The evidence contains 27 fresh GPT-5.6 Luna runs at `max` reasoning:

1. The canonical matrix has 18 runs: two existing pinned scenarios, three
   conditions, and three repetitions per condition. Baseline had the existing
   navigation and verification surface. Discoverability added focus through the
   MCP surface. Guidance added the same generic usage guidance to the prompt.
2. The integrated diagnostic has six runs: three per scenario with the shipped
   `agentic-go-context` skill and the integrated MCP instruction surface. It is a
   diagnostic set, not an additional arm of the canonical matrix.
3. The scope-wording rerun has three runs for the grpc scenario after the
   instruction wording was narrowed. It checks safety and workflow use; it does
   not replace either earlier set.

Each run used an isolated workspace, the same task input and initial repository
state for its scenario, fixed permissions and limits, and a fresh agent context.
The explicit `arm` field is authoritative; it is never inferred from a
filename. Source, binary, prompt, instruction-surface, skill, workspace,
transcript, patch, and acceptance-evidence identities were recorded and hashed.

The runner recomputed adoption metrics from raw transcript events with
`validation/internal/pilot.FocusMetrics`; stored derived fields were not
trusted. A successful focus call is a completed `go_context` MCP event with a
usable non-failed result. A failed call is a completed focus event classified as
failed or unusable. First-call positions are one-based transcript event
positions. Refresh requires `previous_pack_id`. Focus evidence use is an
objective transcript signal: a focus result was followed by an edit or a
refresh completed after an edit. Medians use the repository's upper-middle
convention for even-sized sets.

Evidence bytes, tool calls, and wall-clock milliseconds are the available
resource-cost proxies. Evidence bytes are not tokens, tool calls are not
inference steps, and duration includes the complete run. No monetary cost was
recorded, so this report makes no monetary-cost claim.

## Stage 1: canonical 18-run, three-arm causal matrix

The matrix was designed to separate baseline behavior, MCP description-only
discoverability, and generic prompt guidance. It measures observed use and
workflow signals; because downstream decisions and the treatment were not
uniformly exercised, it does not support a causal usefulness claim.

### Gates, adoption, and resource proxies

| Condition | Runs | Acceptance | Qualifying | Scope-safe | Intervention-free | Healthy delivery | Runs with successful focus use | Refresh complete | Focus evidence use | Focus calls successful / failed | Tool calls median | Evidence bytes median | Duration ms median |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| baseline | 6 | 6/6 | 6/6 | 6/6 | 6/6 | n/a | 0/6 | 0/6 | 0/6 | 0 / 0 | 28 | 277,717 | 336,919 |
| discoverability | 6 | 5/6 | 5/6 | 5/6 | 6/6 | 6/6 | 0/6 | 0/6 | 0/6 | 0 / 0 | 27 | 255,323 | 376,629 |
| guidance | 6 | 6/6 | 6/6 | 6/6 | 6/6 | 6/6 | 6/6 | 6/6 | 6/6 | 18 / 41 | 30 | 412,802 | 382,059 |
| **matrix total** | **18** | **17/18** | **17/18** | **17/18** | **18/18** | **12/12 focus** | **6/18** | **6/18** | **6/18** | **18 / 41** | **28** | **255,323** | **372,456** |

Description alone produced 0/6 successful focus use, 0/6 refresh completion,
and 0/6 focus evidence use despite healthy delivery in 6/6. Generic prompt
guidance produced 6/6 on all three use signals. The grpc discoverability
repetition 1 was the single matrix run that failed acceptance and qualification;
it touched two unexpected files. The other 17 matrix runs were scope-safe.

### Matrix failure classifications

| Condition | Successful focus calls | Failed focus calls | Error categories |
|---|---:|---:|---|
| baseline | 0 | 0 | none |
| discoverability | 0 | 0 | none |
| guidance | 18 | 41 | invalid_input=18, provider=18, stale_snapshot=3, unknown=2 |

Provider failures remain material in the guidance transcripts. The other
categories are recorded classifications, not evidence that focus caused an
agent failure.

### Matrix descriptive metrics by scenario

| Scenario / condition | Runs | Qualifying | Successful / failed focus calls | Refresh | Evidence use | Tool calls median | Evidence bytes median | Duration ms median |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| client retry/context / baseline | 3 | 3/3 | 0 / 0 | 0/3 | 0/3 | 26 | 179,454 | 279,745 |
| client retry/context / discoverability | 3 | 3/3 | 0 / 0 | 0/3 | 0/3 | 27 | 151,641 | 315,618 |
| client retry/context / guidance | 3 | 3/3 | 8 / 18 | 3/3 | 3/3 | 28 | 103,374 | 358,120 |
| grpc RBAC header / baseline | 3 | 3/3 | 0 / 0 | 0/3 | 0/3 | 28 | 277,717 | 336,919 |
| grpc RBAC header / discoverability | 3 | 2/3 | 0 / 0 | 0/3 | 0/3 | 26 | 255,323 | 376,629 |
| grpc RBAC header / guidance | 3 | 3/3 | 10 / 23 | 3/3 | 3/3 | 30 | 422,715 | 411,285 |

## Stage 2: six integrated-skill diagnostics

The shipped skill produced 6/6 skill discovery, 6/6 successful focus use, 6/6
refresh completion, and 6/6 focus evidence use. Initial integrated safety was
5/6: the first grpc run failed acceptance and qualification because it touched
`internal/xds/rbac/matchers.go` and
`internal/xds/rbac/rbac_engine_test.go` outside the permitted scope. The other
five runs were acceptance-pass, qualifying, and scope-safe.

| Scenario / condition | Runs | Acceptance | Qualifying | Scope-safe | Intervention-free | Healthy delivery | Skill discovered | Successful / failed focus calls | Refresh | Evidence use | Tool calls median | Evidence bytes median | Duration ms median |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| client retry/context / integrated | 3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 14 / 5 | 3/3 | 3/3 | 36 | 721,024 | 328,428 |
| grpc RBAC header / integrated | 3 | 2/3 | 2/3 | 2/3 | 3/3 | 3/3 | 3/3 | 11 / 6 | 3/3 | 3/3 | 42 | 675,550 | 724,498 |
| **integrated total** | **6** | **5/6** | **5/6** | **5/6** | **6/6** | **6/6** | **6/6** | **25 / 11** | **6/6** | **6/6** | **42** | **721,024** | **480,201** |

Integrated failures were provider=9, stale_snapshot=1, and unknown=1. The
shipped skill demonstrates that the workflow can be found and used with that
instruction surface; it does not establish that the skill caused better
engineering decisions.

## Stage 3: three scope-wording reruns

The narrowed wording was exercised in all three reruns. Every run discovered
the skill, completed focus use and refresh, produced focus evidence use, passed
acceptance, qualified, stayed scope-safe, and required no intervention.

| Scenario / condition | Runs | Acceptance | Qualifying | Scope-safe | Intervention-free | Healthy delivery | Skill discovered | Successful / failed focus calls | Refresh | Evidence use | Tool calls median | Evidence bytes median | Duration ms median |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| grpc RBAC header / integrated | 3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 15 / 7 | 3/3 | 3/3 | 49 | 593,359 | 644,822 |

All seven failed focus calls in this stage were classified as provider
failures. Provider reliability remains the next technical issue before using
the workflow as a dependable default.

## Identity evidence

The values below are truncated SHA-256 prefixes for readability. Full digests,
including transcript, patch, acceptance-evidence, workspace, and raw-record
identities, remain in the private evidence. The identity checks were stable for
model/reasoning, source within each scenario, initial workspace within each
scenario, prompt within each scenario and condition, and each set's binary,
MCP description, and skill.

| Set | Scenario | Source | Initial workspace | Binary | MCP description | Skill | Prompt by condition |
|---|---|---|---|---|---|---|---|
| canonical matrix | client retry/context | 3fcdd4c72588 | a51633035a93 | 4666d2f540db | 2ee46804cde2 | n/a | baseline/discoverability: de52fa4cc341; guidance: 468b820d6843 |
| canonical matrix | grpc RBAC header | 4793ad047466 | 3b36a25aaafc | 4666d2f540db | 2ee46804cde2 | n/a | baseline/discoverability: d87cb44e2e7f; guidance: 1ad6c70869f2 |
| integrated final | client retry/context | 3fcdd4c72588 | a51633035a93 | fd9e489f9c25 | 1f6b58011132 | 4cb0874ae6ae | integrated: de52fa4cc341 |
| integrated final | grpc RBAC header | 4793ad047466 | 3b36a25aaafc | fd9e489f9c25 | 1f6b58011132 | 4cb0874ae6ae | integrated: d87cb44e2e7f |
| scope rerun | grpc RBAC header | 4793ad047466 | 3b36a25aaafc | 8c3e2414baf2 | 96e726a08d31 | 228edff90b7e | integrated: d87cb44e2e7f |

The binary and instruction-surface identities intentionally differ between the
canonical, integrated, and scope sets. Source and initial-workspace identities
remain stable within each scenario, allowing the stage-specific observations
to be read as separate diagnostics rather than as one unchanged binary
experiment. Post-run workspace identities differ when agents produce different
patches; they are not initial-state identity checks.

## Per-run evidence

Each row reports exact recorded gates and adoption signals. `Focus` is
successful / failed focus calls. `First` is the one-based position of the first
successful focus completion. `Edit` is the post-result edit signal. `Evidence`
is the objective focus-evidence signal. Scope is either zero unexpected paths
or the recorded paths for the failing runs.

| Stage | Scenario | Condition | Rep | Accept | Qual | Scope | Focus | Errors | First | Refresh | Edit | Evidence | Skill | Tools | Bytes | Duration ms |
|---|---|---|---:|:---:|:---:|---|---|---|---:|:---:|:---:|:---:|:---:|---:|---:|---:|
| matrix | client retry/context | baseline | 1 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 26 | 1,354,712 | 255,489 |
| matrix | client retry/context | baseline | 2 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 28 | 121,292 | 279,745 |
| matrix | client retry/context | baseline | 3 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 23 | 179,454 | 384,002 |
| matrix | client retry/context | discoverability | 1 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 51 | 362,068 | 683,671 |
| matrix | client retry/context | discoverability | 2 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 27 | 151,641 | 258,971 |
| matrix | client retry/context | discoverability | 3 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 23 | 106,990 | 315,618 |
| matrix | client retry/context | guidance | 1 | pass | yes | 0 | 4 / 8 | invalid_input=4, provider=2, stale_snapshot=2 | 7 | yes | yes | yes | n/a | 28 | 103,374 | 372,456 |
| matrix | client retry/context | guidance | 2 | pass | yes | 0 | 2 / 5 | invalid_input=3, provider=2 | 7 | yes | yes | yes | n/a | 22 | 82,526 | 327,013 |
| matrix | client retry/context | guidance | 3 | pass | yes | 0 | 2 / 5 | invalid_input=3, provider=2 | 5 | yes | yes | yes | n/a | 34 | 156,905 | 358,120 |
| matrix | grpc RBAC header | baseline | 1 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 28 | 277,717 | 336,919 |
| matrix | grpc RBAC header | baseline | 2 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 20 | 534,527 | 302,295 |
| matrix | grpc RBAC header | baseline | 3 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 35 | 252,064 | 397,580 |
| matrix | grpc RBAC header | discoverability | 1 | fail | no | internal/xds/rbac/matchers.go; internal/xds/rbac/rbac_engine_test.go | 0 / 0 | none | 0 | no | no | no | n/a | 34 | 310,782 | 396,223 |
| matrix | grpc RBAC header | discoverability | 2 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 18 | 255,323 | 376,629 |
| matrix | grpc RBAC header | discoverability | 3 | pass | yes | 0 | 0 / 0 | none | 0 | no | no | no | n/a | 26 | 189,228 | 280,481 |
| matrix | grpc RBAC header | guidance | 1 | pass | yes | 0 | 4 / 6 | invalid_input=3, provider=2, stale_snapshot=1 | 18 | yes | yes | yes | n/a | 28 | 422,715 | 411,285 |
| matrix | grpc RBAC header | guidance | 2 | pass | yes | 0 | 4 / 14 | invalid_input=3, provider=9, unknown=2 | 5 | yes | yes | yes | n/a | 50 | 412,802 | 583,601 |
| matrix | grpc RBAC header | guidance | 3 | pass | yes | 0 | 2 / 3 | invalid_input=2, provider=1 | 15 | yes | yes | yes | n/a | 30 | 574,720 | 382,059 |
| integrated | client retry/context | integrated | 1 | pass | yes | 0 | 5 / 2 | provider=2 | 7 | yes | yes | yes | yes | 29 | 121,070 | 310,758 |
| integrated | client retry/context | integrated | 2 | pass | yes | 0 | 5 / 2 | provider=2 | 7 | yes | yes | yes | yes | 51 | 1,408,497 | 480,201 |
| integrated | client retry/context | integrated | 3 | pass | yes | 0 | 4 / 1 | provider=1 | 7 | yes | yes | yes | yes | 36 | 721,024 | 328,428 |
| integrated | grpc RBAC header | integrated | 1 | fail | no | internal/xds/rbac/matchers.go; internal/xds/rbac/rbac_engine_test.go | 3 / 0 | none | 7 | yes | yes | yes | yes | 42 | 675,550 | 422,409 |
| integrated | grpc RBAC header | integrated | 2 | pass | yes | 0 | 4 / 3 | provider=2, stale_snapshot=1 | 14 | yes | yes | yes | yes | 60 | 1,815,864 | 731,581 |
| integrated | grpc RBAC header | integrated | 3 | pass | yes | 0 | 4 / 3 | provider=2, unknown=1 | 9 | yes | yes | yes | yes | 40 | 553,351 | 724,498 |
| scope | grpc RBAC header | integrated | 1 | pass | yes | 0 | 6 / 2 | provider=2 | 12 | yes | yes | yes | yes | 49 | 593,359 | 621,556 |
| scope | grpc RBAC header | integrated | 2 | pass | yes | 0 | 4 / 2 | provider=2 | 7 | yes | yes | yes | yes | 62 | 765,096 | 644,822 |
| scope | grpc RBAC header | integrated | 3 | pass | yes | 0 | 5 / 3 | provider=3 | 8 | yes | yes | yes | yes | 46 | 582,268 | 668,666 |

All 27 runs were intervention-free. The only recorded scope paths are the two
paths in the initial grpc integrated failure and the same two paths in the
canonical grpc discoverability failure.

## Limitations and claim boundaries

- The canonical set has two scenarios and three repetitions per cell. The
  integrated and scope sets are diagnostics, not a balanced causal experiment.
- Baseline and description-only runs did not invoke focus. Their task outcomes
  cannot evaluate focus efficacy. The guidance and integrated runs establish
  observed workflow use, not improved decisions.
- Provider failures occurred in guidance, integrated, and scope transcripts.
  Successful use must not be treated as reliable until this failure category
  is understood and bounded.
- Acceptance and qualification are recorded evidence fields from the pilot;
  this report does not replay or independently rescore every patch.
- Hashes establish recorded identity and integrity for compared fields, not
  semantic equivalence. Different post-run workspace hashes can result from
  different patches.
- Adoption records do not contain scored decision-obligation verdicts. This
  report makes no obligation-coverage claim.
- Raw transcripts, patch text, absolute local paths, and private run artifacts
  are not copied here. Only sanitized metadata and readable hash prefixes are
  included.

## Ordered next actions

1. Retain the current focus contracts, snapshot lineage, uncertainty handling,
   and full-replacement refresh.
2. Improve MCP description and default instruction placement so the intended
   pre-edit and post-edit workflow is discoverable without scenario-specific
   coaching.
3. Investigate and reduce provider failures while preserving strict transcript
   instrumentation and explicit failure categories.
4. Rerun a smaller paired adoption check only after independent instrumentation
   demonstrates natural focus use; then compare downstream correctness and
   descriptive resource measures.
5. Defer delta refresh until treatment-exercised evidence demonstrates a real
   cost or latency bottleneck.

Generated from private raw records on 2026-09-06. No raw evidence, code,
schema, release, commit, tag, or publication state is included in this report.
