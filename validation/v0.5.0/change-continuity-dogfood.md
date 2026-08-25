# v0.5.0 change continuity evidence

Date: 2026-08-25

This local milestone evidence covers private Change Contracts, exact Snapshot
Ref lineage, structural checkpoints, explicit export, and fresh-process stdio
MCP handoff. It does not claim adoption, model accuracy, token reduction, or
universal Go API compatibility.

## Fresh-process stdio MCP dogfood

`TestSubprocessChangeHandoff` built the current `agentic-go` binary beside the
pinned `agentic-go-gopls` v0.21.0 companion and created a disposable Git Go
repository. It exercised three independent server processes:

1. Process A called `go_begin_change` against `HEAD`, verified non-null
   collections, and exited.
2. The fixture changed an exported function signature.
3. Process B read `agentic-go://change-contract/current`, observed the exact
   contract and initial snapshot created by process A, then called
   `go_checkpoint_change` with that snapshot.
4. The checkpoint reported `exported_api_change`. Reusing the old expected
   snapshot produced a protocol error.
5. Process C read the same contract and observed the persisted decision and
   one immutable checkpoint transition.

The final fixture worktree contained only the intentional `value.go` edit.
The test removed only its exact contract file and empty repository-specific
cache directory. It did not remove or inspect any other contract.

The companion protocol run also discovered 13 tools, six fixed resources, one
resource template, and six prompts. The established semantic, resource,
structured-test, and artifact-cursor checks remained green.

Command:

```sh
PATH="$GO127_DIR:$PATH" \
AGENTIC_GO_GOPLS="$PINNED_GOPLS" \
GOTELEMETRY=off \
GOTOOLCHAIN=local \
go test ./internal/gopls ./internal/intelligence ./internal/mcptest \
  -count=1 -v
```

Result: pass. The external four-workspace semantic test was skipped because
`AGENTIC_GO_SEMANTIC_WORKSPACES` was intentionally not supplied in this run;
its unchanged v0.4 evidence remains recorded in
[`../v0.4.0/semantic-dogfood.md`](../v0.4.0/semantic-dogfood.md).

## Structural precision checks

The exported API policy compares declaration shape instead of declaration
ranges. Focused positive and near-miss cases cover:

- function and method body-only edits, which do not report API drift;
- a same-type inferred exported-variable initializer change, which does not
  report API drift;
- function signature, method receiver, exported type, and exported field
  changes;
- an inferred exported-variable type change and exported constant value
  change; and
- exported additions and deletions.

Unavailable or unmodelled exported declaration shape becomes
`exported_api_unknown` uncertainty. It is not silently treated as unchanged and
does not masquerade as a finding.

The explicit export tests require a new workspace-relative path in an existing
contained directory, reject absolute paths, traversal, symlink escape, Git
metadata, missing parents, and existing destinations, and verify a 0600 JSON
copy with a SHA-256 digest. Cancellation stops before a copy is written.

## Milestone release matrix

All supported-toolchain commands used `GOTOOLCHAIN=local`.

| Gate | Toolchain | Result |
| --- | --- | --- |
| Full tests | Go 1.25.13 | pass |
| Full tests | Go 1.26.7 | pass |
| Full tests | Go 1.27.0 | pass |
| Race tests | Go 1.27.0 | pass |
| Vet and build | Go 1.27.0 | pass |
| Staticcheck | v0.7.0, Go 1.26.7 | pass after pinning Go 1.26 first on `PATH` |
| golangci-lint | v2.12.2, Go 1.27.0 | 0 issues after fixing 18 reported issues |
| Pinned gopls LSP and stdio MCP contracts | gopls v0.21.0, Go 1.27.0 | pass |
| Release archives and checksums | Darwin/Linux, amd64/arm64 | pass |
| Exact-version installer | local shell contract | pass |
| GitHub Action adapters | local Node contract | 5 passed |
| Commit identity and signatures | 12 v0.5 implementation commits | Ashwin Gopalsamy, valid signatures |
| `git diff --check` | final evidence worktree | pass |

The first Staticcheck attempt inherited Go 1.24.2 from the shell `PATH` and
correctly refused the Go 1.25 module. The supported-toolchain rerun passed. The
first golangci-lint run reported 18 formatter, shadowing, field-layout,
documentation, and test-helper issues in the new surface. Those issues were
fixed in `chore: satisfy change continuity linters`; the rerun reported zero
issues.

The release-pack command produced four archives. Every checksum verified, and
every archive contained `agentic-go`, `agentic-go-vet`,
`agentic-go-gopls`, Apache-2.0 licensing, the upstream gopls BSD license, and
generated third-party notices. The temporary release output was moved to the
user Trash after inspection.

## Boundaries and limitations

- Private resume is same-machine and same-user. It is not a shared
  coordination service. Export is one-way in v0.5; no import workflow ships.
- `change-contract/current` selects the most recently updated active contract
  for the repository. No public close operation ships in this milestone.
- Checkpoint structural evidence represents the complete configured
  base-to-current change. Snapshot lineage still records every observed
  transition between checkpoints.
- Checkpoint does not execute tests, race detection, coverage, or analyzers.
  Final executed evidence remains the responsibility of `go_verify_change`.
- Exported API evidence is conservative structural guidance. Complex inferred
  variable types, build variants, generated inputs, cgo, and external consumers
  can remain uncertain.
- No tag, push, transfer, release, or history rewrite was performed.
