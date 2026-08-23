# agentic-go

`agentic-go` is a local-first Model Context Protocol (MCP) server for Go
coding agents. The focused v0.1.0 scope contains seven deterministic MCP
tools, four read-only resources, four prompts, and a standalone
`agentic-go-vet` analyzer CLI. It does not embed an LLM or become an agent
framework: it gives an external agent precise evidence so the agent can make
better engineering decisions.

v0.1.0 is being built locally and is not published yet. Do not infer release
availability or external validation from this source tree.

## Install

From a local checkout during pre-release development:

```sh
go install ./cmd/agentic-go ./cmd/agentic-go-vet
```

After the repository is published, the module install paths are:

```sh
go install github.com/ashwingopalsamy/agentic-go/cmd/agentic-go@latest
go install github.com/ashwingopalsamy/agentic-go/cmd/agentic-go-vet@latest
```

GitHub release archives and checksums will be documented after v0.1.0 is
published. There is no Homebrew tap in this release.

## MCP configuration

The server uses stdio only. A generic MCP client configuration is:

```json
{
  "mcpServers": {
    "agentic-go": {
      "command": "agentic-go",
      "args": ["--workspace", "/absolute/path/to/your/go/module"]
    }
  }
}
```

`--workspace` defaults to the current directory. The server validates that
the directory is a Go module or workspace before serving requests. Use
`agentic-go --version` to print the running version.

Available server flags are:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--workspace` | `.` | Validated Go workspace root |
| `--log-level` | `info` | Stderr lifecycle logging: `debug` or `info` |
| `--max-concurrent-loads` | `4` | Process-wide concurrent Go subprocess/load limit |
| `--max-tool-seconds` | `300` | Maximum duration of one tool operation, in seconds |
| `--version` | `false` | Print the version and exit |

Tool schemas and prompt arguments are discoverable through the MCP protocol;
the catalog below describes their intent rather than duplicating schemas.

## Shipped MCP surface

### Execution tools

These tools run trusted target-repository Go test commands and return bounded,
structured results:

- `go_test_structured` — parse a package's `go test -json` event stream.
- `go_race_report` — run tests with the race detector and structure reports.
- `go_coverage_gaps` — identify uncovered executable lines from a coverage run.
- `go_benchmark_diff` — compare benchmark results against a selected baseline.
- `go_flake_finder` — repeat tests and identify unstable test outcomes.

### Read-only audit tools

- `go_audit_concurrency` — report selected high-precision concurrency findings.
- `go_audit_errors` — report selected error-handling findings.

The analyzers are deliberately narrower than a general linter. A rule ships
only with a positive fixture, a near-miss fixture where applicable, documented
limitations, and evidence that its false-positive behavior is defensible.

### Resources

All four resources are computed fresh on read:

- `agentic-go://module` — reduced `go.mod` module, Go version, and dependency metadata.
- `agentic-go://packages` — reduced package inventory from `go list`.
- `agentic-go://analysis-rules` — the registered concurrency/error rule manifest.
- `agentic-go://trace-summary` — bounded summary of recent trace calls.

The deferred `agentic-go://config` and `agentic-go://cache-stats` resources are
not part of v0.1.0. v0.1.0 has no result cache.

### Prompts

- `audit-package` — run the two shipped audit domains for a package.
- `pre-commit-check` — run the broader pre-commit verification sequence.
- `bisect-flake` — investigate a flaky test and follow up with race evidence.
- `verify-change` — lightweight post-edit tests, race detection, and audits.

### `agentic-go-vet`

The standalone CLI runs the same concurrency and error analyzers through
`go/analysis`:

```sh
agentic-go-vet ./...
```

It is intended for local review and CI-compatible machine-readable workflows.
The underlying Go multichecker exposes its `-json` mode; consumers should
treat that as the standard `go/analysis` diagnostic stream, not as the MCP
tools' structured `AuditResult` schema. The upstream checker exits zero in
JSON mode even when the report contains analysis errors, so CI consumers must
parse and fail on `error` results. SARIF output is not shipped.

## Trust and resource boundaries

The five execution tools invoke the target repository's own Go test machinery.
That compiles and may execute tests, benchmarks, or fuzz functions with the
same operating-system privileges as the `agentic-go` process. This is the
normal local `go test` trust model, not a sandbox; do not point the server at
untrusted code expecting isolation.

The two audit tools load and type-check source and do not execute target test,
benchmark, or fuzz functions. All operations enforce workspace containment,
cancellation and deadlines, a shared concurrency limit, and bounded
subprocess output. These controls make behavior predictable; they are not a
claim of process or filesystem isolation.

## Trace and privacy

Set `AGENTIC_GO_TRACE=true` to enable bounded JSONL traces under:

```text
os.UserCacheDir()/agentic-go/runs/<run-id>/trace.jsonl
```

Trace records hash arguments instead of storing them, and retain summaries
rather than source contents or raw subprocess errors. Tracing is opt-in.

## Support and scope

- Go floor: 1.25.
- Explicitly supported/tested Go versions: 1.25, 1.26, and 1.27.
- Release targets: `darwin/arm64`, `darwin/amd64`, `linux/amd64`, and `linux/arm64`.
- v0.1.0 transport: stdio only.
- Not shipped: Windows support claim, HTTP/SSE, gopls/navigation, cache,
  doctor, SARIF, or Homebrew distribution.

The product boundary is intentionally different from navigation-oriented
gopls servers. Here, “audit suite” means dedicated concurrency and
error-handling rules, while “result cache” means caching MCP tool results:

| Project | Structured output | Audit suite | Result cache | Trace/observability |
| --- | --- | --- | --- | --- |
| agentic-go v0.1.0 | Yes — bounded domain schemas | Yes — the core product | No | Yes — opt-in local call traces and summary resource |
| [gopls MCP](https://go.dev/gopls/features/mcp) | Yes — MCP tool results | No | No dedicated MCP result cache | No agentic-go-style call summary |
| [mcp-gopls](https://github.com/hloiseau/mcp-gopls) | Yes — tests, coverage, and tooling results | No | No documented result cache | Yes — structured logs and progress events |

The [gopls CLI](https://go.dev/gopls/command-line) is also documented as
experimental. Agentic-go v0.1.0 makes no claim to replace either project: it
concentrates on structured test intelligence and narrow audits without a gopls
dependency.

## Validation status

Local positive and near-miss fixtures are part of the implementation gate.
Before tagging v0.1.0, the analyzers will also be evaluated against at least
ten diverse, user-cloned and locally pinned Go repositories. Every sampled
finding will be human-reviewed. A rule with more than 5% observed false
positives or a repeatable systemic false-positive pattern will be disabled or
fixed before release. Rules without meaningful external hits will not be
marketed as externally validated. No external validation metrics are claimed
yet.

## Development

Read [`docs/v0.1.0-release-scope.md`](docs/v0.1.0-release-scope.md) first;
[`docs/contracts.md`](docs/contracts.md) defines shared invariants and SDK
shapes. The intended local verification commands are:

```sh
go build ./...
go test -race ./...
go vet ./...
staticcheck ./...
golangci-lint run
```

The project is licensed under Apache-2.0. Contributions should preserve the
truthful trust boundary, fixture discipline, and documented release scope.
See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the review contract and
[`SECURITY.md`](SECURITY.md) for safe usage and private vulnerability reports.
