# v0.7.0 unified verification evidence

Date: 2026-08-28

This local milestone evidence covers the `agentic.verify/v1beta1` report,
semantic diagnostics, snapshot lineage, provider capabilities, provenance,
optional Change Contract compliance, private report persistence, and the
unified CLI and MCP path. It does not claim universal correctness, model
reliability, token savings, or user adoption.

## Agentic-go self-verification

A clean locally built development binary used the bundled gopls v0.21.0
companion and Go 1.27.0 to verify the complete v0.7 change against the final
v0.6 milestone:

```sh
agentic-go verify \
  --base 117d36f \
  --package ./... \
  --format json \
  --fail-on error
```

The report compared base commit
`117d36f4b35a0273b44f7499122a2745d59998c9` with clean HEAD
`338278dcc38747b9d31ab0385ed7ee51da8a1d45`. It produced report ID
`verify_498ab2f19ae06f89a02d7b6fc0d8c62bb5ada4e71ee49791327c806e25339e37`
and exact semantic snapshot
`sha256:4012ce0f51eb06efb159cbba34cb83dbbe8620646eaf0844f1dab604ac94a338`.

Observed evidence:

| Measure | Result |
| --- | --- |
| Changed files | 39 |
| Changed declarations | 204 |
| Affected package closure | 6 packages |
| Package tests | 226 passed, 0 failed, 6 skipped |
| Changed-statement coverage | 72.7% |
| Concurrency baseline | 0 introduced, 1 existing, 0 resolved, 0 unknown |
| Error baseline | 0 introduced, 0 existing, 0 resolved, 0 unknown |
| Current compiler and semantic diagnostics | 0 |
| Primary findings | 0 |
| Policy result | `pass`, exit 0 |
| Serialized JSON size | 24,584 bytes |

The report retained seven source-grounded risk lenses. It explicitly reported
`external_consumers` and `unmodelled_non_go_change` uncertainty. The passing
result means the requested checks completed without policy-blocking evidence;
it is not a safety verdict.

The first dogfood run identified one new analyzer note on `Core` concurrency
safety and one gopls range-over-int hint. The final implementation documents
the process-local concurrency contract, uses a cancellation-aware gate for
Change Contract mutation, and removes the mechanical diagnostic. The clean
rerun produced no introduced finding and no current diagnostic.

## Stdio MCP integration

The exact pinned companion was used for the gopls, intelligence, verification,
and subprocess MCP contract suites. The server exposed 14 tools, seven fixed
resources, one artifact resource template, and six prompts.

The fresh-process handoff test exercised this sequence:

1. Begin a private Change Contract against an exact snapshot.
2. Change source and checkpoint the expected lineage.
3. Reject a repeated stale checkpoint.
4. Start a fresh stdio server process.
5. Call `go_verify_change` with the contract ID and exact snapshot ID.
6. Read the same finalized report from
   `agentic-go://verification/latest`.
7. Confirm the persisted contract links to that report ID.

The fixture worktree contained only the intentional source edit afterward.
Private test contract and verification files were removed by exact identity.
The tests also retained semantic search, Symbol Ref, Context Pack artifact,
structured-test package status, and guarded-refactor coverage.

## Milestone release matrix

All Go commands used `GOTOOLCHAIN=local` and explicit local toolchains.

| Gate | Toolchain | Result |
| --- | --- | --- |
| Full tests | Go 1.25.13 | pass |
| Full tests | Go 1.26.7 | pass |
| Full tests | Go 1.27.0 | pass |
| Race tests | Go 1.27.0 | pass |
| Vet and build | Go 1.27.0 | pass |
| Staticcheck | v0.7.0, built and run with Go 1.26.7 | pass |
| golangci-lint | v2.12.2, run with Go 1.27.0 | 0 issues |
| Pinned gopls and stdio MCP contracts | gopls v0.21.0, Go 1.27.0 | pass |
| Verification schemas and strict goldens | alpha archive and beta current | pass |
| Release archives and checksums | Darwin/Linux, amd64/arm64 | pass |
| Exact-version installer | local shell contract | pass |
| GitHub Action adapters | local Node contracts | 5 passed |
| Commit identity and signatures | v0.7 implementation history | Ashwin Gopalsamy, valid signatures |
| `git diff --check` | final evidence worktree | pass |

The release-pack rerun produced four archives. Every checksum verified, and
every archive contained `agentic-go`, `agentic-go-vet`,
`agentic-go-gopls`, Apache-2.0 licensing, the upstream gopls BSD license, and
generated third-party notices.

## Boundaries and limitations

- Current diagnostics are evidence from the final snapshot. Without an
  equivalent base diagnostic snapshot, they are not classified as introduced
  findings.
- Change Contract mutation is serialized within one Core process and remains
  cancellation-aware. Separate processes must not concurrently mutate the
  same private contract; this milestone does not claim a distributed lock.
- The latest report pointer and the Change Contract link are individually
  atomic files, not one cross-store transaction.
- gopls evidence describes the active build configuration. Generated inputs,
  cgo variants, dynamic calls, and external consumers can remain uncertain.
- No new analyzer predicate shipped, so the external calibration corpus was
  not rerun.
- No paid model comparison or external repository evaluation was required for
  this milestone. No claim is made about reduced hallucinations or token use.
- No tag, push, transfer, release, or history rewrite was performed.
