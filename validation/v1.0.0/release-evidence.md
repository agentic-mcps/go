# v1.0.0 release evidence

Date: 2026-08-30 (Asia/Kolkata)

This record qualifies the personal-path v1 candidate at source commit
`709e7d42b301f40041d80e75e9be3d74daf5c13c`. The module identity is
`github.com/ashwingopalsamy/agentic-go`. The evidence files added afterward do
not change production code, schemas, analyzer predicates, or protocol behavior.

## Release identity

- `agentic-go --version` reports `1.0.0` in the candidate archive.
- Managed gopls initialization receives the effective agentic-go version.
- Release archives contain the exact `agentic-go-gopls` v0.21.0 companion.
- The frozen inventory remains 14 tools, seven fixed resources, one artifact
  resource template, and six prompts.
- The emitted schemas remain `agentic.context/v1`, `agentic.change/v1`, and
  `agentic.verify/v1`.

## Verification matrix

All Go commands used explicit local toolchains with `GOTOOLCHAIN=local`.

| Gate | Toolchain | Result |
| --- | --- | --- |
| Full tests | Go 1.25.13 | pass, about 36 s |
| Full tests | Go 1.26.7 | pass, about 39 s |
| Full tests | Go 1.27.0 | pass, about 36 s |
| Race tests | Go 1.27.0 | pass, about 39 s |
| Vet and build | Go 1.27.0 | pass |
| Staticcheck | v0.7.0 with Go 1.26.7 | pass |
| golangci-lint | v2.12.2 with Go 1.27.0 | pass, 0 issues |
| Pinned gopls, intelligence, MCP, tool, and CLI contracts | gopls v0.21.0 with Go 1.27.0 | pass |
| Installer contract | local shell suite | pass |
| GitHub Action adapters | Node 24-compatible test runner | pass, five suites |
| Release bundles | Darwin/Linux, amd64/arm64 | pass, two byte-identical builds |

## Hosted pre-tag qualification

GitHub Actions Verify run
[`33288916240`](https://github.com/ashwingopalsamy/agentic-go/actions/runs/33288916240)
passed at commit `2229f8db6e7205a257985dc2987b9b7f0304bc98` on 2026-08-30.
All 17 jobs passed: the Go 1.25, 1.26, and 1.27 Linux/macOS matrix;
race, vet, and build; Staticcheck; golangci-lint; pinned-gopls contracts;
Action adapters; installer contracts; and all four release targets.

Two hosted-only defects were corrected before this successful run. GitHub
output-channel variables are now isolated from stdout-based adapter assertions,
and golangci-lint runs with Go 1.26 because its released binary cannot
type-check the Go 1.27 standard library. Go 1.27 remains covered by the full
build, race, vet, gopls, and cross-build gates.

## Deterministic candidate archives

Two independent four-target builds produced byte-identical files. The first
build had these SHA-256 digests:

| File | SHA-256 |
| --- | --- |
| `agentic-go_1.0.0_darwin_amd64.tar.gz` | `862f33278f96b72516c1b6e5e233d8161cd8ac5617d9273d998bf54c396ceac1` |
| `agentic-go_1.0.0_darwin_arm64.tar.gz` | `a5847a4f7e7d407483c2d03c99a17465df9bc8f602c87b95824edc19c8c4d1ae` |
| `agentic-go_1.0.0_linux_amd64.tar.gz` | `e82162f2487c56e9eb62fb88bf0df395407b402bf752f4811e1136509b10f776` |
| `agentic-go_1.0.0_linux_arm64.tar.gz` | `7434bd99eaf937daa5ba3410e1f529b1fe125086a50a83bcee4ac73fd019024f` |
| `checksums.txt` | `b19b65941a5bedfc431cb2eaa260a04a60dbd3d6c1ee4aa0290ab14274310993` |

The Darwin arm64 archive contained both agentic-go binaries, the gopls
companion, the Apache license, the upstream gopls BSD license, dependency
notices, and third-party notices. `agentic-go doctor` accepted Go 1.27.0 and
the bundled sidecar from the extracted archive.

Publication CI rebuilds assets from the signed tag. Those published checksums
must be verified independently and are not assumed to equal these pre-tag
candidate hashes.

## Bundled self-verification

The extracted Darwin arm64 candidate verified this repository against base
`326a17e` with `--package ./... --format json --fail-on error`.

| Measure | Result |
| --- | --- |
| Report schema | `agentic.verify/v1` |
| Report ID | `verify_0cf9b0dc7f4cab1ee964a8a8fd20a532d8756cc84d728fb008bc7ce00338aa2f` |
| Snapshot | `sha256:5e3037779544636ea1d0338cce1dc798bac1be4ad1f22d9622f68f95e1f45db7` |
| Changed files and declarations | 17 files, 18 declarations |
| Affected package closure | 6 packages |
| Tests | 198 passed, 0 failed, 7 skipped |
| Changed-statement coverage | 69.2% |
| Diagnostics | 0 |
| Concurrency baseline | 0 introduced, 3 existing, 0 resolved, 0 unknown |
| Error baseline | 0 introduced, 0 existing, 0 resolved, 0 unknown |
| Findings | 0 |
| Risk lenses and uncertainties | 4 risks, 2 uncertainties |
| Policy result | `pass`, exit 0 |

The repository worktree status was unchanged by verification.

## Client and protocol smoke

- The complete normalized MCP suite passed against the bundled pinned-gopls
  path and exercised the frozen protocol surface.
- Claude Code 2.1.212 loaded an isolated temporary configuration and reported
  the extracted stdio server connected.
- Codex CLI 0.149.1 loaded an isolated temporary configuration and reported
  the extracted server enabled. Its configuration command does not perform a
  health probe, so connectivity evidence comes from the normalized stdio suite
  and the earlier Codex protocol dogfood rather than a paid model invocation.
- Neither client smoke changed persistent client configuration.

## Public-history replacement preflight

Fresh remote inspection matched the approved public refs:

- `main`: `b3558117496ba0241314899aa5205b9297bcec80`
- `v0.1.0` tag object: `718882d24ef6befdfdfa94e073f70afd0218ecd2`
- `v0.1.0` peeled commit: `38d575daadb86632f9ea8d7827934b0f5a966d9c`

A complete external bundle named
`agentic-go-public-before-v1-2026-08-30.bundle` contains the former public
`main` and `v0.1.0` histories. Its SHA-256 is
`5cde244401fa1718dc6a63a680c17a2ca5b080ca4f66d1693c036d236bb587df`.

The former public `main` tree equals curated commit `22e41bb` at tree
`285d381f8f5f94662ea28a71eaadf67c2f26fe1c`. The peeled public `v0.1.0`
tree equals curated commit `b62b73c` at tree
`7a37104e2e3a48b4f02edfd3e2c00dc731d96b0d`. The complete 58-commit mapping
is recorded in [`public-history-map.md`](public-history-map.md).

The source lineage through `2229f8d` contains 147 commits; every one is signed
and authored by Ashwin Gopalsamy. Curated `main` is published at that commit.
The existing `v0.1.0` tag remains unchanged; no `v1.0.0` tag or release exists,
and no repository rule or organization repository was changed.

## Claim boundary

The v0.8 corpus and deterministic replay qualify shipped contracts. The paid
Codex and Claude pilot did not run. This release therefore makes no universal
claim that agentic-go reduces model mistakes, tool calls, or token use. The
v0.1 analyzer false-positive result remains corpus-specific evidence, not a
universal guarantee.
