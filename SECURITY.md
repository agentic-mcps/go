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

That code runs with the same operating-system privileges as the agentic-go
process, just as it would when a developer runs `go test` directly. Use these
tools only with repositories you trust. The Go toolchain and the target tests
may access the network according to their normal configuration.

The two audit tools, `go_audit_concurrency` and `go_audit_errors`, type-check
and analyze source without running target tests, benchmarks, or fuzz targets.
Their package loading is closed-world: module downloads and toolchain downloads
are disabled.

Agentic-go enforces a symlink-resolved workspace boundary, propagates
cancellation, applies deadlines and concurrency limits, and caps each
subprocess output stream. These controls limit accidental scope and resource
use; they do not isolate hostile code from the host.

The v0.1 server uses stdio only. It exposes no HTTP listener. Optional traces
hash arguments and omit raw source, raw errors, and raw workspace paths, but a
trace file is still local operational data and should be handled accordingly.

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
