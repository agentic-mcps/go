# v0.6.0 guarded refactor evidence

Date: 2026-08-26

This local milestone evidence covers deterministic gopls-backed refactor
preview, content-addressed plans, exact-snapshot apply, private recovery
journals, and the stdio MCP operation. It does not claim that arbitrary edits
are safe, that client approval UI was exercised, or that model accuracy or
token use improved.

## Stdio MCP dogfood

The current release bundle was built with the pinned gopls v0.21.0 companion.
The Darwin arm64 companion from that bundle was then used by the subprocess MCP
contract tests:

```sh
PATH="$GO127_DIR:$PATH" \
GOTOOLCHAIN=local \
GOTELEMETRY=off \
AGENTIC_GO_GOPLS="$BUNDLE_DIR/agentic-go-gopls" \
go test ./internal/gopls ./internal/intelligence ./internal/mcptest \
  -count=1 -v
```

Result: pass at implementation commit `4177369`.

The run discovered 14 tools, six fixed resources, one resource template, and
six prompts. It retained the existing structured-test, semantic search,
Symbol Ref, Context Pack artifact, and fresh-process Change Contract checks.

`TestSubprocessGuardedRefactor` created a disposable Git repository and used
three MCP calls for the mutation flow:

1. `go_search` returned a snapshot-bound reference for `OldName`.
2. `go_refactor` previewed a rename to `NewName`, returned one affected file
   and a content-addressed plan, and left the source unchanged.
3. An apply request containing preview-only arguments failed without mutation.
   A subsequent request containing only `apply`, `plan_id`, and the exact
   snapshot applied the stored plan.

The renamed fixture still passed `go test ./...`. Git status contained only
` M value.go`; the refactor did not stage files or alter Git history. The test
removed only its exact private plan file and empty repository-specific plan
directory.

The pinned companion contract also exercised rename, formatting, organize
imports, and `source.fixAll` previews. Mutation requests remained non-retried,
while UTF-16 positions, overlapping edits, unsupported resource operations,
sidecar restart behavior, cancellation, containment, and exact capability
negotiation retained focused coverage.

## Recovery and mutation boundaries

Focused tests verified:

- previews are deterministic and do not write source;
- plan identity includes the immutable snapshot and exact preimages;
- stale snapshots, changed preimages, generated files, outside paths, and
  unsupported file creation, deletion, or rename are rejected;
- an exclusive recovery journal is durable before the first source write;
- two processes cannot begin the same recovery state concurrently;
- an injected partial multi-file write is rolled back to recorded preimages;
- recovery refuses a file that matches neither its recorded preimage nor its
  postimage; and
- `doctor --recover` reports clean, required, recovered, and diverged states
  without exposing private cache paths or source contents.

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
| golangci-lint | v2.12.2, built and run with Go 1.27.0 | 0 issues |
| Pinned gopls and stdio MCP contracts | gopls v0.21.0, Go 1.27.0 | pass |
| Release archives and checksums | Darwin/Linux, amd64/arm64 | pass |
| Exact-version installer | local shell contract | pass |
| GitHub Action adapters | local Node contract | 5 passed |
| Commit identity and signatures | 10 v0.6 implementation commits | Ashwin Gopalsamy, valid signatures |
| `git diff --check` | final evidence worktree | pass |

Staticcheck v0.7.0 was built with Go 1.26.7 because its export-data reader is
not compatible with Go 1.27 output. golangci-lint initially reported field
layout, shadowing, and exported-comment issues in the new v0.6 code. The
focused cleanup removed those issues without changing the established Context
Pack, snapshot, or semantic response field order.

The release-pack command produced four archives and `checksums.txt` with local
module resolution only. Every checksum verified. Every archive contained
`agentic-go`, `agentic-go-vet`, `agentic-go-gopls`, Apache-2.0 licensing, the
upstream gopls BSD license, and generated third-party notices. The installer
accepted the valid fixture archive and rejected an invalid checksum.

## Boundaries and limitations

- Apply is limited to rename, formatting, organize imports, and
  `source.fixAll` edits to existing, contained, non-generated files. General
  coding edits remain the external agent's responsibility.
- The subprocess dogfood exercised an explicit apply request and the MCP
  destructive annotation contract. It did not automate a particular client's
  human approval dialog.
- Recovery behavior is fault-injected and process-safe in focused tests. This
  run did not terminate the host operating system during a real disk write.
- gopls results describe the active build configuration. Generated inputs,
  cgo variants, dynamic calls, and external consumers can remain uncertain.
- No paid model comparison or external repository evaluation was required for
  this milestone. No claim is made about reduced hallucinations or token use.
- No tag, push, transfer, release, or history rewrite was performed.
