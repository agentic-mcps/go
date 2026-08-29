# v0.9.0 release-candidate evidence

Date: 2026-08-29

This local evidence covers the v1 domain-contract freeze, MCP interface
compatibility, private-state upgrades, interrupted refactor recovery, pinned
sidecar compatibility, deterministic release archives, and the standard local
release matrix. It adds no model-reliability, token-saving, adoption, or
universal-correctness claim.

The milestone started from v0.8 evidence commit `0557708` and was implemented
in three signed commits before this report:

- `2226e6c` defines the executable v0.9 scope;
- `8647bb2` freezes the v1 domain contracts and upgrade behavior; and
- `5e169a7` freezes the normalized MCP interface.

## Frozen contracts

New responses use these schema identities:

| Domain | Current | Archived compatibility |
| --- | --- | --- |
| Context Pack | `agentic.context/v1` | `agentic.context/v1alpha1` retained byte-for-byte |
| Change Contract | `agentic.change/v1` | alpha loads as v1 in memory and is rewritten only by an explicit save |
| Verification report | `agentic.verify/v1` | beta remains readable by its exact content identity and is never rewritten |

Strict schema tests validate current identities, non-null collections, exact
representative Go JSON goldens, and SHA-256 stability for every archived
schema. Verification report goldens cover `pass`, `findings`, and `incomplete`
result precedence. A v1 report has a new content identity because
`schema_version` participates in its digest.

The normalized MCP golden freezes exactly 14 tools, seven fixed resources, one
artifact resource template, and six prompts. It covers tool names, input and
output schema digests, execution annotations, resource URIs and MIME types,
template identity, and prompt arguments. Descriptions and registration order
remain intentionally outside the compatibility contract.

## Upgrade, stale-state, and interruption evidence

Existing focused suites and the new migration tests cover:

- exact gopls v0.21.0 companion acceptance, unpinned-version rejection, and a
  missing-companion diagnostic;
- one retry for an idempotent read after a sidecar crash, no mutation replay,
  explicit restart, request cancellation, and bounded malformed or oversized
  protocol input;
- exact Snapshot Ref and Symbol Ref identity, stale checkpoint and refactor
  rejection, and cancellation propagation through intelligence operations;
- alpha Change Contract non-mutating load and v1 promotion on explicit save;
- exact beta verification-report reads without file or latest-pointer rewrite;
- recovery of only journaled postimages, refusal of a second active journal,
  clean state after recovery, partial-apply rollback, and divergence refusal
  without source mutation; and
- CLI argument compatibility, `pass` exit 0, policy `findings` exit 1,
  operational or `incomplete` exit 2, doctor recovery status, generated MCP
  configuration, and contained Change Contract export.

No duplicate test was added where an existing focused suite already proved the
required behavior.

## Deterministic release archives

The release-pack command built all four supported targets twice from identical
inputs with Go 1.27.0. Each corresponding archive and `checksums.txt` compared
byte-for-byte. Every archive contained `agentic-go`, `agentic-go-vet`,
`agentic-go-gopls`, the Apache-2.0 license, the upstream gopls BSD license, and
generated gopls dependency notices.

First-build SHA-256 digests:

| Artifact | SHA-256 |
| --- | --- |
| `agentic-go_0.9.0-rc.1_darwin_amd64.tar.gz` | `db18b735dc0c7945bca76ebd71156a85088ce57151d484578233629a124f2891` |
| `agentic-go_0.9.0-rc.1_darwin_arm64.tar.gz` | `b294b6124686d37d9b73f38bc2c4566e54e171f33ead8f4303df75583c5d567a` |
| `agentic-go_0.9.0-rc.1_linux_amd64.tar.gz` | `373ade3fd68215b193b856847195a48d1903248f2e3fe2b46fb6b48a7af7c34b` |
| `agentic-go_0.9.0-rc.1_linux_arm64.tar.gz` | `43f9bd767433567a4f8d074e6b9551c37a16a96189e4f35064f1b2e6478d04bf` |
| `checksums.txt` | `e86e559c5bba6456a928acdc0bc685a4b2aade2641f4340a9fcae476a429e393` |

The exact-version installer accepted a matching archive and rejected malformed,
duplicate, and mismatched checksum entries. All five GitHub Action adapter
suites passed locally, including base resolution, release selection, checksum
verification, summary and annotation rendering, and advisory versus enforced
exit behavior.

## Release matrix

All Go commands used explicit local toolchains with `GOTOOLCHAIN=local`.

| Gate | Toolchain | Result |
| --- | --- | --- |
| Full tests | Go 1.25.13 | pass, 30 s |
| Full tests | Go 1.26.7 | pass, 34 s |
| Full tests | Go 1.27.0 | pass, 34 s |
| Race tests | Go 1.27.0 | pass, 41.76 s |
| Vet and build | Go 1.27.0 | pass |
| Staticcheck | v0.7.0 with Go 1.26.7 | pass |
| golangci-lint | v2.12.2 with Go 1.27.0 | 0 issues |
| Pinned gopls and stdio MCP contracts | gopls v0.21.0 with Go 1.27.0 | pass |
| v1 schemas and normalized MCP golden | Go 1.27.0 | pass |
| Release bundles and checksums | Darwin/Linux, amd64/arm64 | pass, two identical builds |
| Installer and Action adapters | local shell and Node contracts | pass |
| Commit identity and signatures | local v0.9 implementation history | Ashwin Gopalsamy, valid signatures |
| `git diff --check` | final evidence worktree | pass |

## Bundled stdio dogfood

The Darwin arm64 `0.9.0-rc.1` candidate archive was extracted into an ephemeral
directory and used to verify agentic-go itself against v0.8 evidence commit
`0557708`:

```sh
agentic-go verify \
  --workspace . \
  --base 0557708 \
  --package ./... \
  --format json \
  --fail-on error
```

The report used `agentic.verify/v1`, exited 0, and produced report ID
`verify_cd36608dbf28e1e0a8e23da62fa555e4f7649b8ec094d1c7827a436340a0dd8e`
for snapshot
`sha256:f308282f8027f8cb03a499ee3213d11676853fdb5163e146ab30429e553c8c46`.

| Measure | Result |
| --- | --- |
| Changed files | 36 |
| Changed declarations | 51 |
| Affected package closure | 6 packages |
| Package tests | 233 passed, 0 failed, 6 skipped |
| Changed-statement coverage | 81.8% |
| Current compiler and semantic diagnostics | 0 |
| Concurrency baseline | 0 introduced, 1 existing, 0 resolved, 0 unknown |
| Error baseline | 0 introduced, 0 existing, 0 resolved, 0 unknown |
| Primary findings | 0 |
| Risk lenses | 5 |
| Explicit uncertainties | 2 |
| Policy result | `pass`, exit 0 |

The pinned subprocess MCP suites independently enumerated the 14/7/1/6
surface, read snapshot-bound artifacts, exercised structured tests, semantic
search and symbol context, resumed a Change Contract across fresh processes,
rejected a stale checkpoint, linked verification evidence, and previewed and
applied one guarded rename. The fixture worktrees retained only their intended
source edits. The agentic-go repository status was byte-identical before and
after self-verification.

## Deferred proposal classification

### Shipped locally

- v0.3 pinned-sidecar installation and distribution foundation;
- v0.4 snapshot-bound semantic Context Packs, search, and symbol context;
- v0.5 private Change Contracts, checkpoints, export, and fresh-process resume;
- v0.6 guarded refactor preview, apply, journaling, and fail-closed recovery;
- v0.7 unified diagnostics, contract compliance, and verification provenance;
- v0.8 reproducible eight-task qualification and 24-call server replay; and
- v0.9 frozen v1 domain and MCP contracts.

### Retained research

- the bounded paid Codex and Claude pilot;
- additional navigation and creative workflows;
- tier-2 tools; and
- security, observability, naming, type-design, and performance analyzers that
  first pass fixtures, near misses, production-path coverage, stated limits,
  and focused external calibration.

Retained research is not a release promise.

### Rejected for v1

- selective individual-test execution, public test maps, and SSA/VTA
  reachability claims;
- SARIF, HTTP/SSE, hosted analysis, and autonomous fixes;
- a shared language-plugin runtime or embedded LLM;
- automatic dependency changes; and
- Windows support without runtime evidence.

The repository transfer to `github.com/agentic-mcps/go` and explicit Go module
identity migration are scheduled for the v1 release candidate after separate
approval. They are not part of v0.9.

## Boundaries

- Passing verification means requested checks completed without blocking
  evidence. It does not prove a change safe or correct.
- The release archives are local candidates, not published artifacts.
- The paid model pilot has not run, so no reduced-mistake or token-saving claim
  is made.
- No new analyzer predicate shipped, so no external analyzer recalibration was
  required.
- No tag, push, transfer, release, paid pilot, or history rewrite was performed.
