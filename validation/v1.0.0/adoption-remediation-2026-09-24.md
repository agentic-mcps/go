# Selector remediation evidence

Campaign identity: `luna-guidance-remediation-20260924`

Status: implementation and focused verification complete. The post-change six-cell guidance screen completed, but its overhead gate failed. The canonical 18-cell matrix was not run.

## Pre-remediation baseline

The preceding Luna canonical campaign contained 18 runs across the two pinned scenarios, three arms, and three repetitions. The source hashes were:

- `7354d9c8debb4bcf2225bf429857078de310c176`
- `8c9ee70637600318f1cc4e3931da78f084e41123`

The evaluated binary hash was `bd7535d8c7172219d48ac45b873939af7d3cac004f018230e85db030c6302cc9`.

The guidance arm passed acceptance and qualification in 6/6 runs, had zero scope violations, used focus evidence in 6/6 runs, and completed refresh in 6/6 runs. It produced five failed focus calls. Client-go produced one low-level `invalid_input` failure. gRPC produced four low-level `provider` failures. Transcript audit classified all five as selector misuse: one mutated Symbol Ref and four invalid declaration coordinates. No provider defect was established.

Pre-remediation guidance medians were 215664 ms and 22 tool calls for client-go, and 330537 ms and 32 tool calls for gRPC. These are baseline evidence for the rerun, not a product-value claim.

## Remediation gates

- Context lineage: passed. The post-edit refresh used `base` plus the previous pack ID and returned `refresh.status: replaced`.
- Guidance and digest coverage: passed.
- Private failure classification and deterministic replay tests: passed.
- MCP inventory and schema goldens: passed.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, `git diff --check`, and task validation: passed.

## Post-remediation guidance screen

The six-cell screen used the same two pinned sources and three Luna repetitions per scenario. The client-go records are retained at `private://agentic-go-luna-selector-remediation-20260924/runs`. The gRPC records are retained at `private://agentic-go-luna-selector-remediation-20260924-b/runs`. The rebuilt server binary SHA-256 was `020a7b4ca2e9fdf940e2131d63ec4cc90544071ab859feb6147693f10a9c914e`. The evaluator binary SHA-256 was `3f1d6513c9ccc1d09be6ee8aef62788045ab4b6282df16fd63c0ce80e3e66022`.

All six runs passed acceptance and qualification, used evidence, completed refresh, and had zero scope violations, failed focus calls, selector misuse, repeated rejected selectors, and stale evidence acceptance. The reliability behavior therefore passed this screen.

The promotion gate did not pass:

- client-go median focus calls: 3, above the limit of 2;
- gRPC median focus calls: 4, above the limit of 2;
- client-go median duration: 282291 ms versus 183559 ms baseline, 1.54x;
- gRPC median duration: 367794 ms versus 315501 ms baseline, 1.17x;
- client-go median total tool calls: 27 versus 16 baseline, 1.69x;
- gRPC median total tool calls: 36 versus 24 baseline, 1.50x.

This is an overhead and workflow-efficiency failure, not evidence of a provider defect. The result blocks the canonical 18-cell rerun and any product-value or speed claim. The next fix should reduce redundant exploration and focus calls before another guidance campaign.

## Rejected efficiency probe

A follow-up wording probe required one initial context call and one post-edit refresh by default. It was run once on client-go with the rebuilt guidance and retained at `private://agentic-go-luna-selector-remediation-20250925-c/runs`. The run passed acceptance and qualification, but still made three focus calls and produced one `selector_misuse` failure. The probe was reverted and is not part of the shipped guidance. This supports treating call reduction as an agent-behavior problem requiring a better intervention design, not more repeated prose.

## Private efficiency diagnostic

Astra reviewed the paired trace audit and recommended no provider, guidance, threshold, or public-surface change. The evaluator now records a bounded `redundant_refreshes` diagnostic when a successful refresh repeats with unchanged snapshot, scope, and evidence requirements without an intervening edit or stale rejection. Focused tests cover valid narrowing, edits, stale rejection, and changed evidence requirements. The diagnostic does not suppress calls or relax freshness checks.

The six-cell efficiency gate remains failed. No new Luna campaign or canonical 18-cell matrix is justified by this slice.

## Bounded classification contract

The historical low-level categories remain unchanged. New private records contain only a bounded root cause, selector kind, normalized reason, recovery status, and repeated-selector status. They never contain raw Symbol Refs, source paths, prompts, transcripts, or provider error text.

The accepted root causes are `selector_misuse`, `provider_failure`, `stale_snapshot`, `timeout`, `transport`, and `unknown`.

## Limitations

This campaign does not establish improved correctness, speed, productivity, token usage, adoption, or product value. It does not justify provider fallback, relaxed stale checks, automatic Symbol Ref correction, a new MCP surface, or a release claim. External decision-value evaluation remains pending until real frontier and open-model clients are available.

Private raw artifacts are retained outside the repository at `private://agentic-go-luna-causal-20260924-c/canonical-only.json`.
