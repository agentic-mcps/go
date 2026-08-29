# v0.8.0 reproducible evaluation evidence

Date: 2026-08-29

This local evidence covers eight pinned historical Go changes, deterministic
fixture preparation, behavioral qualification, structural scope enforcement,
and source-grounded MCP replay. It does not claim model reliability, token
savings, patch equivalence, or user adoption.

## Qualification

Every task was evaluated in three isolated states with Go 1.27.0 and network
access disabled during scoring:

1. The fixture base plus the upstream regression-test oracle failed.
2. The historical production change plus the same oracle passed.
3. The passing historical change plus one forbidden probe path failed only
   structural scope enforcement.

The qualification gate rejected three earlier task candidates whose tests did
not actually fail on their parent revisions. They were replaced before this
evidence was recorded.

| Task | Repository | Qualification | Duration |
| --- | --- | --- | ---: |
| `client-go-retry-context-cancellation` | client-go | pass | 5.372 s |
| `client-golang-gather-format` | client_golang | pass | 5.007 s |
| `cobra-unique-args` | Cobra | pass | 4.426 s |
| `echo-csrf-token-lookup` | Echo | pass | 5.644 s |
| `gin-optional-writer-interfaces` | Gin | pass | 7.325 s |
| `grpc-go-rbac-header-normalization` | grpc-go | pass | 10.879 s |
| `testify-setup-skip-stats` | Testify | pass | 6.100 s |
| `vault-redirect-query-preservation` | Vault | pass | 61.453 s |

All fixture bundles were prepared twice with identical SHA-256 digests. The
digests and exact upstream parent and target commits are recorded in
[`evidence.json`](evidence.json). Bundles and third-party source remain outside
this repository.

## Deterministic MCP replay

One local development server built from the v0.7 production implementation
used the pinned gopls v0.21.0 companion. Each transcript made three stdio MCP
calls and retained its complete response as a private content-addressed
artifact.

| Task | Calls | Result bytes | Duration |
| --- | ---: | ---: | ---: |
| `client-go-retry-context-cancellation` | 3 | 9,458 | 4.769 s |
| `client-golang-gather-format` | 3 | 24,716 | 19.806 s |
| `cobra-unique-args` | 3 | 32,953 | 7.326 s |
| `echo-csrf-token-lookup` | 3 | 33,521 | 9.224 s |
| `gin-optional-writer-interfaces` | 3 | 26,072 | 11.603 s |
| `grpc-go-rbac-header-normalization` | 3 | 17,890 | 11.478 s |
| `testify-setup-skip-stats` | 3 | 59,979 | 13.648 s |
| `vault-redirect-query-preservation` | 3 | 19,453 | 115.085 s |

All 24 calls completed successfully. Checked response artifacts contained no
absolute task-workspace path. Result bytes measure serialized MCP result size,
not model tokens.

## Milestone release matrix

All Go commands used explicit local toolchains with `GOTOOLCHAIN=local`.

| Gate | Toolchain | Result |
| --- | --- | --- |
| Full tests | Go 1.25.13 | pass |
| Full tests | Go 1.26.7 | pass |
| Full tests | Go 1.27.0 | pass |
| Race tests | Go 1.27.0 | pass |
| Vet and build | Go 1.27.0 | pass |
| Staticcheck | v0.7.0 with Go 1.26.7 | pass |
| golangci-lint | v2.12.2 with Go 1.27.0 | 0 issues |
| Pinned gopls contract | gopls v0.21.0 with Go 1.27.0 | pass |
| Release bundles and checksums | Darwin/Linux, amd64/arm64 | pass |
| Installer and Action adapters | local shell and Node contracts | pass |
| `git diff --check` | final evidence worktree | pass |

The first parallel Go 1.26 run hit the gopls discovery test's 10-second limit
under three simultaneous full-suite runs. The isolated package and complete
sequential Go 1.26 rerun passed. This is recorded as test-environment
contention, not silently counted as a clean first run.

## Boundaries

- Historical regression tests demonstrate one pinned behavior. They do not
  prove that a different passing implementation is equivalent to upstream.
- Timings are warm-cache observations from one macOS arm64 machine, not an
  SLA or cross-mode performance comparison.
- Replays measure deterministic server behavior, not whether a coding model
  would produce a correct change.
- The bounded paid pilot has not run. No comparison with native client tools
  or upstream gopls MCP is recorded, so no reliability or token-saving claim
  is made.
- No tag, push, transfer, release, or history rewrite was performed.
