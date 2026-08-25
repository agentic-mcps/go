# v0.4.0 semantic dogfood

Date: 2026-08-25

This local dogfood exercises the v0.4 semantic workflow against the committed
agentic-go tree and three already-cloned Go repositories. It verifies bounded
workspace briefs, workspace-symbol search, stable Symbol Ref handoff, and one
snapshot lineage across all three operations.

## Command

The companion was built from the pinned local gopls v0.21.0 source, with its
version embedded exactly as the release companion does:

```sh
go build -trimpath \
  -ldflags='-s -w -X main.version=v0.21.0' \
  -o "$TMPDIR/agentic-go-gopls" \
  golang.org/x/tools/gopls

AGENTIC_GO_GOPLS="$TMPDIR/agentic-go-gopls" \
AGENTIC_GO_SEMANTIC_WORKSPACES="$AGENTIC_GO:$CORPUS/cobra:$CORPUS/gin:$CORPUS/grpc-go" \
GOTOOLCHAIN=local \
go test ./internal/intelligence \
  -run TestExternalSemanticWorkspaces \
  -count=1 \
  -v
```

The test selected one repository-specific exported symbol query, followed the
first returned Symbol Ref, and required the brief, search result, and symbol
context to report the same snapshot ID.

## Results

| Workspace | Commit | Query | Active packages | Search total | Brief bytes | Aggregate duration |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| agentic-go | `b6e32d5a6a22d3c9ebfe9aa01ad3e0ea48ffb753` | `New` | 21 | 6 | 8,147 | 3.361 s |
| Cobra | `adbc8813901bba65827259daa8e22ff94ec1f30e` | `Command` | 2 | 94 | 8,157 | 1.787 s |
| Gin | `dcaa4296d111981ffb31ac3eba90bb63e1eb5ab9` | `Engine` | 7 | 82 | 8,188 | 2.516 s |
| grpc-go | `4793ad0474669eafbd5346c8a3d098fdfa542498` | `ClientConn` | 259 | 39 | 8,081 | 9.254 s |

All four cases passed. Collections remained non-null, every brief stayed within
the declared 8 KiB response budget, search returned stable opaque references,
and symbol context consumed those references without caller-supplied positions.

## Observed usefulness

- A coding agent can establish a bounded package and module overview before
  asking for symbol details.
- Search results carry snapshot-bound references, so a later request cannot
  silently reinterpret a stale line and column.
- The same interface handled a 259-package workspace without returning raw
  dependency source or an unbounded symbol payload.
- Truncated detail remains addressable through private, snapshot-bound artifact
  cursors instead of increasing every MCP response.

## Limitations

- This is local deterministic contract evidence, not evidence of adoption,
  model accuracy, or token reduction.
- Queries were selected per repository and do not measure search relevance.
- Aggregate duration includes cold workspace discovery, snapshot capture, and
  semantic initialization. It is not the isolated warm-call p95 target.
- The grpc-go checkout had a pre-existing modified `go.sum`; the dogfood did
  not create, modify, stage, or restore it.
- Results cover the active host build configuration only. Alternate build tags,
  cgo environments, external consumers, and dynamic calls remain disclosed
  uncertainty.
- Unsaved editor overlays are not part of this v0.4 disk-snapshot workflow.

## Milestone release matrix

The final uncommitted documentation-only evidence diff was checked after the
implementation cleanup. All commands used `GOTOOLCHAIN=local` and the supported
local toolchain named below.

| Gate | Toolchain | Result |
| --- | --- | --- |
| Full tests | Go 1.25.13 | pass |
| Full tests | Go 1.26.7 | pass |
| Full tests | Go 1.27.0 | pass |
| Race tests | Go 1.27.0 | pass |
| Vet and build | Go 1.27.0 | pass |
| Staticcheck | v0.7.0, Go 1.26.7 | pass |
| golangci-lint | v2.12.2, Go 1.27.0 | 0 issues |
| Pinned gopls LSP and stdio MCP contracts | gopls v0.21.0, Go 1.27.0 | pass |
| Release bundles | Darwin/Linux, amd64/arm64 | pass |
| Exact-version installer | local shell contract | pass |
| GitHub Action adapters | local Node contract | 5 passed |
| Commit signatures and `git diff --check` | local history and worktree | pass |

No tag, push, transfer, release, or history rewrite was performed.
