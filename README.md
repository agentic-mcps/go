<p align="center">
  <img src="assets/go-logo-blue.svg" alt="Go" width="145">
</p>

<h1 align="center">agentic-go</h1>

<p align="center">Local MCP tooling for Go test intelligence and focused concurrency and error audits.</p>

<p align="center">
  <a href="docs/v0.1.0-release-scope.md">Release scope</a> |
  <a href="docs/contracts.md">Protocol contracts</a> |
  <a href="CONTRIBUTING.md">Contributing</a> |
  <a href="SECURITY.md">Security</a>
</p>

agentic-go is a local-first, stdio-only Model Context Protocol (MCP) server plus
the `agentic-go-vet` analyzer CLI. It gives an external coding agent
structured test evidence and focused concurrency and error-handling findings.
It does not embed an LLM or an agent runtime.

> Status: pre-release. Install from this checkout. Release packages are not
> published yet.

## Install

Go 1.25 or newer is required. From the repository root:

~~~sh
go install ./cmd/agentic-go ./cmd/agentic-go-vet
export PATH="$(go env GOPATH)/bin:$PATH"
agentic-go --version
~~~

Keep `$(go env GOPATH)/bin` on your `PATH` so an MCP client can find
the binaries. Published module commands, release archives, and Homebrew are
not available for this pre-release checkout. See the
[release scope](docs/v0.1.0-release-scope.md) for the current boundary.

## Connect an MCP client

agentic-go speaks stdio. A generic MCP client configuration looks like this:

~~~json
{
  "mcpServers": {
    "agentic-go": {
      "command": "agentic-go",
      "args": ["--workspace", "/absolute/path/to/your/go/module"]
    }
  }
}
~~~

`--workspace` defaults to the current directory. The directory must be
a Go module or workspace. The server checks the `go` executable inherited from
the MCP process `PATH`, requires Go 1.25 or newer, and uses
`GOTOOLCHAIN=local`. It does not download a replacement toolchain.

## Server flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `--workspace` | `.` | Validated Go workspace root |
| `--log-level` | `info` | Stderr lifecycle logging: `debug` or `info` |
| `--max-concurrent-loads` | `4` | Process-wide concurrent Go subprocess/load limit |
| `--max-tool-seconds` | `300` | Maximum duration of one tool operation |
| `--version` | `false` | Print the version and exit |

## Analyzer CLI

`agentic-go-vet` runs the same concurrency and error analyzers through
`go/analysis`:

~~~sh
agentic-go-vet ./...
agentic-go-vet -json ./... > findings.json
go vet -vettool="$(command -v agentic-go-vet)" ./...
~~~

Text mode follows the usual `go vet` diagnostic exit behavior. In
`-json` mode, the upstream multichecker exits zero even when findings are
present, so CI must parse the report and fail on analysis errors. The
[release scope](docs/v0.1.0-release-scope.md) documents the exact contract.

## MCP surface

### Execution tools

These tools run trusted target-repository Go test machinery and return bounded,
structured results.

| Tool | Use |
| --- | --- |
| `go_test_structured` | Parse a `go test -json` event stream |
| `go_race_report` | Run tests with the race detector and structure reports |
| `go_coverage_gaps` | Find uncovered executable lines from a coverage run |
| `go_benchmark_diff` | Compare benchmark results with a selected baseline |
| `go_flake_finder` | Repeat tests and identify unstable outcomes |

### Read-only audit tools

| Tool | Use |
| --- | --- |
| `go_audit_concurrency` | Report selected concurrency findings |
| `go_audit_errors` | Report selected error-handling findings |

The analyzers are narrower than a general linter. Each shipped rule has a
positive fixture, a meaningful near miss where applicable, documented
limitations, and integration coverage through the production audit path.

### Resources

Resources are computed fresh on read.

| URI | Returns |
| --- | --- |
| `agentic-go://module` | Module, Go version, and dependency metadata |
| `agentic-go://packages` | Package inventory from `go list` |
| `agentic-go://analysis-rules` | Registered concurrency and error rule manifest |
| `agentic-go://trace-summary` | Bounded summary of recent trace calls |

### Prompts

| Prompt | Use |
| --- | --- |
| `audit-package` | Run both shipped audit domains for a package |
| `pre-commit-check` | Run the broader pre-commit verification sequence |
| `bisect-flake` | Investigate a flaky test with race evidence |
| `verify-change` | Run lightweight post-edit tests, race detection, and audits |

## Boundaries

- Execution tools can compile and run target tests, benchmarks, or fuzz
  functions with the same operating-system privileges as the server process.
- Audit tools load and type-check source. They do not execute target test,
  benchmark, or fuzz functions.
- Workspace access is symlink-resolved and contained. Operations share
  cancellation, deadlines, concurrency limits, and bounded subprocess output.
  These are containment controls, not a sandbox.
- Set `AGENTIC_GO_TRACE=true` to write bounded JSONL traces under
  `os.UserCacheDir()/agentic-go/runs/<run-id>/trace.jsonl`. Traces hash
  arguments and retain summaries rather than source contents.
- v0.1.0 is stdio only. gopls navigation, result caching, SARIF output,
  Homebrew distribution, and a Windows support claim are not shipped.

See the [protocol contracts](docs/contracts.md) and
[security guidance](SECURITY.md) for the full boundary.

## Evidence

The active analyzer rules have positive and near-miss fixtures. The v0.1.0
offline calibration report covers 10 pinned repositories, 467 reviewed findings,
and a 0% observed false-positive rate. Read the
[validation evidence](validation/v0.1.0/summary.md) for the corpus, raw
sanitized reports, classifications, limitations, and reproduction command.

## Read next

- [Release scope](docs/v0.1.0-release-scope.md): what ships in v0.1.0
- [Protocol contracts](docs/contracts.md): shared types, limits, and invariants
- [Validation evidence](validation/v0.1.0/summary.md): external calibration record
- [Contributing](CONTRIBUTING.md): review and change expectations
- [Security](SECURITY.md): safe usage and private vulnerability reports

## License

Apache-2.0. See [LICENSE](LICENSE).
