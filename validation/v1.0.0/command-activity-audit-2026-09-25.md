# Command Activity Audit

Date: 2026-09-25
Campaign: selector remediation guidance screen
Model: Luna
Scope: six guided traces and six paired baseline traces across client-go and grpc-go

Guided source campaigns: selector-remediation-20260924 and selector-remediation-20260924-b

Baseline source campaign: causal-20260924-c

The private source manifest retains the SHA-256 digests for all twelve run records.

## Decision

The guided workflow increased downstream shell activity, but this audit does not identify a safe product intervention.

Guided traces issued 172 completed shell commands compared with 111 in the paired baselines. The increase was concentrated in source inspection, not repeated selector failure or recovery:

| Arm | Commands | Discovery | Source inspection | Verification | Editing | Recovery | Unknown |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Guided | 172 | 37 | 134 | 1 | 0 | 0 | 0 |
| Baseline | 111 | 43 | 66 | 2 | 0 | 0 | 0 |

The traces contain no confirmed stale-evidence acceptance, unchanged rejected-selector retry, selector misuse, provider failure, or recovery loop. The command activity therefore does not justify changing provider behavior, relaxing freshness checks, suppressing refreshes, or adding more generic selector guidance.

## Trace-linked accounting

The table reports only bounded counts. Raw commands, transcripts, local paths, prompts, and opaque references remain in private operator-held artifacts.

| Task | Arm | Run | Commands | Before edit | After edit | File changes | MCP calls | Focus calls | Duration ms | Exact duplicate commands |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| client-go | Guided | r1 | 33 | 29 | 4 | 1 | 3 | 3 | 284919 | 1 |
| client-go | Guided | r2 | 23 | 19 | 4 | 1 | 3 | 3 | 282291 | 1 |
| client-go | Guided | r3 | 21 | 18 | 3 | 1 | 3 | 3 | 222703 | 1 |
| grpc-go | Guided | r1 | 37 | 25 | 12 | 1 | 4 | 4 | 367794 | 1 |
| grpc-go | Guided | r2 | 25 | 21 | 4 | 2 | 5 | 5 | 1026486 | 0 |
| grpc-go | Guided | r3 | 33 | 26 | 7 | 1 | 2 | 2 | 334137 | 1 |
| client-go | Baseline | r1 | 12 | 10 | 2 | 1 | 0 | 0 | 206861 | 0 |
| client-go | Baseline | r2 | 16 | 14 | 2 | 1 | 0 | 0 | 183559 | 0 |
| client-go | Baseline | r3 | 15 | 12 | 3 | 1 | 0 | 0 | 157568 | 0 |
| grpc-go | Baseline | r1 | 33 | 22 | 11 | 2 | 0 | 0 | 315501 | 0 |
| grpc-go | Baseline | r2 | 23 | 19 | 4 | 1 | 0 | 0 | 205915 | 0 |
| grpc-go | Baseline | r3 | 12 | 9 | 3 | 2 | 0 | 0 | 374857 | 0 |

## Activity interpretation

- Client-go runs narrowed from an ambiguous function query to the exact current candidate before refreshing. The additional inspection was relevant to candidate selection, not a repeated rejected selector.
- grpc-go runs narrowed broad symbol searches to the concrete RBAC normalizer before refreshing. The second refresh in guided r2 followed a further edit. Under strict snapshot lineage, that refresh is required and cannot be suppressed safely.
- Five exact duplicate command invocations occurred across the six guided traces. Every duplicate was discovery-class activity. No repeated source-inspection sequence or unchanged selector retry was established.
- Editing was represented by seven file-change events in guided traces and eight in baselines. No shell editing command was observed.
- The classifier was deterministic and bounded. Discovery includes repository state, history, file listing, module metadata, and environment lookup. Source inspection includes content search, file reads, and diff review. Verification includes explicit test, build, vet, and diff-check commands. No unclassified command remained.

## Recoverable overhead

There is no defensible time estimate because the traces do not attribute elapsed time to individual shell commands, and command duration is not total task duration.

The mechanical upper bound is five exact duplicate discovery invocations. A separate private focus audit identifies one additional refresh in grpc-go guided r2, but it followed an edit and is required by the current freshness contract. These six invocations are therefore a hypothetical counterfactual, not a safe optimization target. The defensible recoverable overhead is zero.

## Product and gate implications

- The guided runs still exceeded the focus-call target: client-go median 3 and grpc-go median 4 against a target of 2.
- The audit does not establish that command reduction would improve correctness, speed, productivity, token usage, adoption, or decision quality.
- No new Luna campaign or 18-run matrix is justified by this audit.
- CLI and GitHub Action remain the primary verification surfaces. MCP remains an agent-facing adapter whose value must come from evidence quality and decision relevance, not call-count reduction alone.

The next defensible evaluation would instrument agent action reasons and distinguish necessary source inspection from avoidable exploration before testing an intervention. That is outside this slice. Stop MCP efficiency remediation here unless a reproducible mechanism is identified.

## Privacy and provenance

Private raw JSON traces and the command classifier inputs remain outside the repository. This report stores aggregate counts, bounded categories, run labels, and conclusions only. No raw source, prompts, transcripts, filesystem paths, Symbol Refs, or provider errors are included.

Frozen MCP inventory and public schemas were not changed by this audit.
