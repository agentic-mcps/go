# Private Luna focus pilot results

## Executive decision

This private paired pilot does not establish that the focus workflow improves
agent decisions. The focus capability was delivered successfully in 10/10
focus runs, but `go_context` was used in 0/10, so outcome and efficiency
differences cannot be attributed to the treatment.

Retain the focus implementation because it introduced no observed safety
regression. Keep full-replacement refresh and defer delta refresh. The next
work is release hardening, focus discoverability and tool-description/guidance
improvements, and instrumentation that can independently establish treatment
use. Rerun a smaller paired pilot only after use is demonstrated.

This is feasibility evidence, not a claim of speedup, token savings,
reliability, adoption, or statistical significance.

## Question and method

The question was whether compact, source-grounded focus context helps an agent
make correct and safe engineering decisions. We ran two existing pinned
scenarios, two explicit conditions, and five repetitions per cell: 20 fresh
GPT-5.6 Luna runs at `max` reasoning. Baseline runs had the existing
navigation and verification surface. Focus runs had that same surface plus
the focus capability. Inputs, repository state, permissions, limits, and
instrumentation were paired and isolated.

The recorded metrics are evidence-artifact bytes / transcript tool calls /
focus-tool calls / wall-clock milliseconds. Evidence bytes are not tokens;
tool calls are not inference steps. Acceptance and qualification below are
recorded outcomes from the pilot artifacts and were not independently rerun
for this report.

## Safety and qualification gates

Acceptance was 20/20 and qualification was 20/20. There were zero scope violations
and zero operator interventions. The matrix contains 20 unique cells. Source
and binary identities match within all ten baseline/focus pairs.

The post-run workspace digest matches in all five client-go pairs and in only
one of five grpc-go pairs. The grpc-go workspace differs in repetitions 1–4,
despite matching source and binary identities. Those post-run differences are
expected when patches differ; initial-state identity should be verified
separately. They are not evidence of a source or binary mismatch.

## Per-run results

| Scenario | Condition | Rep | Bytes / calls / focus calls / ms | Delivery | Obligations |
|---|---|---:|---:|---|---:|
| client-go retry cancellation | baseline | 1 | 678549 / 25 / 0 / 129897 | n/a | 2/3 |
| client-go retry cancellation | baseline | 2 | 275660 / 31 / 0 / 201004 | n/a | 2/3 |
| client-go retry cancellation | baseline | 3 | 225299 / 22 / 0 / 168859 | n/a | 2/3 |
| client-go retry cancellation | baseline | 4 | 632187 / 11 / 0 / 125691 | n/a | 2/3 |
| client-go retry cancellation | baseline | 5 | 139002 / 29 / 0 / 165919 | n/a | 2/3 |
| client-go retry cancellation | focus | 1 | 87911 / 13 / 0 / 225849 | healthy | 2/3 |
| client-go retry cancellation | focus | 2 | 89694 / 12 / 0 / 138334 | healthy | 2/3 |
| client-go retry cancellation | focus | 3 | 660786 / 23 / 0 / 164604 | healthy | 2/3 |
| client-go retry cancellation | focus | 4 | 69989 / 7 / 0 / 111465 | healthy | 2/3 |
| client-go retry cancellation | focus | 5 | 141536 / 18 / 0 / 155180 | healthy | 2/3 |
| grpc-go RBAC normalization | baseline | 1 | 485655 / 29 / 0 / 191211 | n/a | 2/3 |
| grpc-go RBAC normalization | baseline | 2 | 385274 / 34 / 0 / 275312 | n/a | 2/3 |
| grpc-go RBAC normalization | baseline | 3 | 1365697 / 21 / 0 / 173935 | n/a | 2/3 |
| grpc-go RBAC normalization | baseline | 4 | 1250863 / 15 / 0 / 162873 | n/a | 2/3 |
| grpc-go RBAC normalization | baseline | 5 | 571492 / 9 / 0 / 127637 | n/a | 3/3 |
| grpc-go RBAC normalization | focus | 1 | 374442 / 22 / 0 / 189067 | healthy | 2/3 |
| grpc-go RBAC normalization | focus | 2 | 1359683 / 19 / 0 / 195799 | healthy | 2/3 |
| grpc-go RBAC normalization | focus | 3 | 281104 / 15 / 0 / 115239 | healthy | 2/3 |
| grpc-go RBAC normalization | focus | 4 | 344639 / 28 / 0 / 203458 | healthy | 2/3 |
| grpc-go RBAC normalization | focus | 5 | 394604 / 9 / 0 / 98637 | healthy | 2/3 |

## Comparisons

| Group | Runs | Obligations | Bytes median | Calls median | Duration median (ms) | Delivery | Use |
|---|---:|---:|---:|---:|---:|---|---:|
| client baseline | 5 | 10/15 | 275660 | 25 | 165919 | n/a | 0/5 |
| client focus | 5 | 10/15 | 89694 | 13 | 155180 | 5/5 | 0/5 |
| grpc baseline | 5 | 11/15 | 571492 | 21 | 173935 | n/a | 0/5 |
| grpc focus | 5 | 8/15 | 374442 | 19 | 189067 | 5/5 | 0/5 |
| baseline overall | 10 | 21/30 | 528573.5 | 23.5 | 167389 | n/a | 0/10 |
| focus overall | 10 | 18/30 | 312871.5 | 16.5 | 159892 | 10/10 | 0/10 |

The corrected baseline total is 21/30, not 19/30. The earlier aggregate
reported 19/30 and mixed scenario medians with overall values; the per-run
records sum to 21/30 and are the authority used here. Overall obligation
verdicts are 37 satisfied out of 60.

## Obligations and focus use

Client-go was 10/15 in both conditions. grpc-go was 11/15 baseline and 8/15
focus. Thus focus had 18/30 satisfied obligations overall versus 21/30 for
baseline. Because no focus run actually invoked `go_context`, this difference
does not measure focus efficacy.

Preflight capability delivery was healthy in every focus run, while recorded
focus-tool calls were zero in every focus run. Delivery proves availability,
not adoption or usefulness. The instrumentation therefore answers whether the
surface was exposed, but not whether an agent found it discoverable or chose
it for a decision.

## Limits and failure modes

- The sample has two scenarios and five repetitions per cell; no statistical
  significance or generalization is appropriate.
- Recorded wall-clock duration includes the complete run and is not model
  inference latency.
- Artifact size is a storage measurement, not a token measurement.
- Acceptance and qualification are recorded pilot fields, not a fresh replay
  or independent patch rescore here.
- Hashes establish recorded identity and integrity for compared fields, not
  semantic equivalence.
- Initial-state identity should be verified separately for the grpc-go pairs;
  post-run workspace differences can result from differing patches.
- Obligation coverage is a task-specific decision measure, not a repository
  correctness or production-safety verdict.

## Ordered next actions

1. Preserve the current focus design and full-replacement refresh behavior.
2. Complete release hardening and reconcile tracked documentation with the
   implemented additive post-v1 capability.
3. Improve focus discoverability, generic tool descriptions, and usage
   guidance without scenario-specific coaching.
4. Instrument independent treatment-use and selection evidence, and verify
   grpc-go initial-state identity separately.
5. Run a smaller paired pilot after focus use is independently demonstrated.
6. Reconsider delta refresh only if a later pilot exposes a measured
   full-replacement cost or correctness need.
