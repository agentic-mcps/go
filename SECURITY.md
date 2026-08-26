# Security policy

## Trust boundary

Agentic-go is local developer tooling, not a sandbox. Five v0.1 tools invoke
`go test` and therefore compile and execute code already present in the target
repository:

- `go_test_structured`
- `go_race_report`
- `go_coverage_gaps`
- `go_benchmark_diff`
- `go_flake_finder`

The v0.2 `agentic-go verify` CLI, GitHub Action, and `go_verify_change` MCP
tool can also run trusted repository tests and analyzers as part of a report.
They use the caller/runner's privileges. Containment, cancellation, deadlines,
concurrency limits, and output caps reduce accidental scope and resource use;
they do not isolate hostile code.

That code runs with the same operating-system privileges as the agentic-go
process, just as it would when a developer runs `go test` directly. Use these
tools only with repositories you trust. The Go toolchain and the target tests
may access the network according to their normal configuration.

The two audit tools, `go_audit_concurrency` and `go_audit_errors`, type-check
and analyze source without running target tests, benchmarks, or fuzz targets.
Their package loading is closed-world: module downloads and toolchain downloads
are disabled.

The v0.4 semantic tools read and type-check workspace source through the pinned
gopls companion. They do not apply edits or run target tests. Workspace briefs,
search results, symbol context, and artifact chunks remain subject to the same
symlink-resolved containment and request deadline. Semantic results can still
reflect the local module cache, build configuration, cgo environment, and
generated source, so their uncertainty fields must not be treated as a safety
claim.

Agentic-go enforces a symlink-resolved workspace boundary, propagates
cancellation, applies deadlines and concurrency limits, and caps each
subprocess output stream. These controls limit accidental scope and resource
use; they do not isolate hostile code from the host.

The server uses stdio only. It exposes no HTTP listener. Optional traces
hash arguments and omit raw source, raw errors, and raw workspace paths, but a
trace file is still local operational data and should be handled accordingly.

Release archives include the exact gopls v0.21.0 companion. Managed gopls
sessions inherit the agentic-go process privileges and may read source inside
the configured workspace and maintain upstream caches under the user's normal
cache directories. Agentic-go sets `GOTELEMETRY=off` for managed sessions,
bounds JSON-RPC frames and stderr, and never accepts a gopls-initiated workspace
edit outside the guarded-refactor flow. These controls do not make gopls a
sandbox.

Context Pack overflow artifacts are stored under the user cache with private
directory and file permissions, content-addressed identities, and atomic
writes. They may contain source-derived signatures, diagnostics, and symbol
relationships. Normal operation never writes them into the analyzed worktree;
users should protect the local cache like other developer-tool state.

Change Contracts and Checkpoints use the same-machine, same-user cache and are
not a shared coordination service. Snapshot lineage is exact, and stale
checkpoint requests are rejected. `agentic-go contract export --output ...`
is the only documented Change Contract worktree export path. It requires a
contained, workspace-relative destination, creates a 0600 file, and never
overwrites an existing file. Normal contract operation does not write to the
worktree.

Guarded refactor plans and recovery journals are private source-bearing cache
state under `os.UserCacheDir()/agentic-go/refactors`. Preview does not modify
the worktree. Apply is limited to existing, contained, non-generated files and
requires the exact preview snapshot and exact SHA-256 file preimages. A
preimage mismatch blocks mutation. `agentic-go doctor --recover` restores only
from a valid journal whose targets still match their recorded preimages or
postimages; it refuses diverged files instead of overwriting user edits.
Refactoring does not stage, commit, or change Git history.

Unified verification reports are stored privately under
`os.UserCacheDir()/agentic-go/verifications`. They contain workspace-relative
locations, snapshot identities, diagnostics, findings, and source-derived
provenance, but no source contents, opaque Change Contract goal prose, or
absolute workspace path. Writes are atomic per report and latest pointer, with
0700 directories and 0600 files. Treat this cache as local development data.

## Supported releases

Security fixes target the latest published release. The pre-release branch is
development software and does not receive backports. Supported release
artifacts are limited to documented macOS and Linux targets; Windows has not
been validated.

## Report a vulnerability

Please use GitHub's private
[security advisory form](https://github.com/ashwingopalsamy/agentic-go/security/advisories/new).
Do not include private source code, credentials, or sensitive trace contents in
a public issue.
