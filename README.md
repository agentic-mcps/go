<p align="center">
  <img src="assets/go-logo-blue.svg" alt="Go" width="145">
</p>

<h1 align="center">agentic-go</h1>

<p align="center">Language-native change verification for production Go code.</p>

<p align="center">
  <a href="docs/v0.2.0-release-scope.md">Release scope</a> |
  <a href="docs/contracts.md">Protocol contracts</a> |
  <a href="CONTRIBUTING.md">Contributing</a> |
  <a href="SECURITY.md">Security</a>
</p>

agentic-go produces a source-grounded report of what changed, what may be
affected, which checks ran, what evidence they produced, and what remains
uncertain. The report is the durable product boundary; the CLI, GitHub Action,
and MCP server are delivery adapters over the same verification engine. It does
not embed an LLM or an agent runtime.

> Status: pre-release. Install from this checkout. Release packages are not
> published yet.

## Verify a change

The primary workflow compares the final working tree with an explicit local
base. It runs whole-package checks for the conservative affected-package
closure; it never selects individual tests.

~~~sh
agentic-go verify --base origin/main --package ./... --format text
~~~

`--base` is required. Other verification flags are:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--workspace` | `.` | Workspace root |
| `--package` | `./...` | Package scope |
| `--format` | `text` | `text` or `json` |
| `--race` | off | Include race evidence |
| `--fail-on` | `error` | `error`, `warning`, `info`, or `none` |
| `--min-changed-coverage` | unset | Inclusive changed-statement threshold, `0..100` |
| `--max-packages` | `200` | Affected-package limit, `1..500` |

JSON uses the versioned `agentic.verify/v1alpha1` contract. Exit status is `0`
for `pass`, `1` for policy `findings`, and `2` for `incomplete` or an execution
error. A passing report means requested checks completed without policy-blocking
evidence; it does not mean the change is safe.

Abbreviated terminal report shape (values are illustrative):

~~~text
agentic-go verification

Status: pass
Base: origin/main (c34dc7ee0048)
Merge-base: c34dc7ee0048
Snapshot: sha256:8c16…
Change: 3 files, 2 declarations
Impact: 4 packages

Evidence:
- go.analysis.concurrency: passed — 0 introduced, 0 existing, 0 resolved, 0 unknown
- go.analysis.errors: passed — 0 introduced, 1 existing, 0 resolved, 0 unknown
- go.coverage: passed — 83.3% of changed statements covered
- go.test: passed — 42 passed, 0 failed, 1 skipped

requested verification completed without blocking findings. A passing report does not prove the change safe.
~~~

## GitHub Action

The root composite Action downloads the exact released binary, verifies its
checksum, runs the same CLI once, and writes a job summary, source annotations,
and a JSON report under `RUNNER_TEMP`. It is advisory by default; set
`enforce: true` to propagate verification exits `1` and `2`. It requests no
PR-write permission and posts no comments. It assumes checkout history and a
configured Go toolchain are already available.

After v0.2.0 is published, a pull-request job can use:

~~~yaml
permissions:
  contents: read

steps:
  - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
    with:
      fetch-depth: 0
  - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7
    with:
      go-version: 1.27.x
  - uses: ashwingopalsamy/agentic-go@v0.2.0
    with:
      enforce: false
~~~

When the Action is pinned by commit SHA instead of a semver tag, pass its
`version` input explicitly so the downloaded archive remains deterministic.

## Install

Go 1.25 or newer is required. Versions 1.25, 1.26, and 1.27 are explicitly
supported; newer stable toolchains may pass preflight but are not yet claimed.
From the repository root:

~~~sh
go install ./cmd/agentic-go ./cmd/agentic-go-vet
export PATH="$(go env GOPATH)/bin:$PATH"
agentic-go --version
~~~

Keep `$(go env GOPATH)/bin` on your `PATH` so an MCP client can find the
binaries. Published module commands and Homebrew are not available for this
pre-release checkout. The personal repository is
`github.com/ashwingopalsamy/agentic-go` through v0.2; the future organization
path is a planned, breaking module migration, not a current import path.

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

## MCP adapter

agentic-go also speaks stdio MCP for local coding agents. The v0.2 inventory is
eight tools, four resources, and four prompts. The seven v0.1 tools retain
their contracts; the additive `go_verify_change` tool returns the same report
as the CLI in authoritative `structuredContent`, with only a concise text
fallback to avoid duplicating the report in client context. It declares
execution annotations so supporting clients can gate approval. The
`verify-change` prompt invokes that operation once rather than
asking an agent to reconstruct a report from low-level tools.

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

### Change verification

| Tool | Use |
| --- | --- |
| `go_verify_change` | Return one source-grounded impact, evidence, findings, risk, and uncertainty report for a local change |

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
| `verify-change` | Call `go_verify_change` once and interpret its complete report |

## Boundaries

- Verification and execution tools can compile and run trusted target tests,
  benchmarks, or fuzz functions with the same operating-system privileges as
  the server process. The Action has the same trust boundary through the
  runner's process.
- Audit tools load and type-check source. They do not execute target test,
  benchmark, or fuzz functions.
- Workspace access is symlink-resolved and contained. Operations share
  cancellation, deadlines, concurrency limits, and bounded subprocess output.
  These are containment controls, not a sandbox.
- Set `AGENTIC_GO_TRACE=true` to write bounded JSONL traces under
  `os.UserCacheDir()/agentic-go/runs/<run-id>/trace.jsonl`. Traces hash
  arguments and retain summaries rather than source contents.
- MCP remains stdio only. HTTP, SARIF, `doctor`, automatic toolchain
  installation, gopls navigation, result caching, Homebrew distribution, and
  a Windows support claim are not shipped.

See the [protocol contracts](docs/contracts.md) and
[security guidance](SECURITY.md) for the full boundary.

## Evidence

The active analyzer rules have positive and near-miss fixtures. The v0.1.0
offline calibration report covers 10 pinned repositories and 467 reviewed
findings, with 0% observed false positives in that corpus. This is
corpus-specific evidence, not a universal guarantee. Read the
[validation evidence](validation/v0.1.0/summary.md) for the corpus, raw
sanitized reports, classifications, limitations, and reproduction command.

## Read next

- [v0.2.0 release scope](docs/v0.2.0-release-scope.md): the current executable specification
- [v0.1.0 release scope](docs/v0.1.0-release-scope.md): the compatibility baseline
- [Protocol contracts](docs/contracts.md): shared types, limits, and invariants
- [Validation evidence](validation/v0.1.0/summary.md): external calibration record
- [Contributing](CONTRIBUTING.md): review and change expectations
- [Security](SECURITY.md): safe usage and private vulnerability reports

## License

Apache-2.0. See [LICENSE](LICENSE).
