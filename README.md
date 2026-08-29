<p align="center">
  <img src="assets/go-logo-blue.svg" alt="Go" width="145">
</p>

<h1 align="center">agentic-go</h1>

<p align="center">Source-grounded Go change intelligence for coding agents.</p>

<p align="center">
  <a href="docs/v0.2.0-release-scope.md">Release scope</a> |
  <a href="docs/contracts.md">Protocol contracts</a> |
  <a href="CONTRIBUTING.md">Contributing</a> |
  <a href="SECURITY.md">Security</a>
</p>

agentic-go currently produces a source-grounded report of what changed, what may be
affected, which checks ran, what evidence they produced, and what remains
uncertain. The report is the durable product boundary; the CLI, GitHub Action,
and MCP server are delivery adapters over the same verification engine. It does
not embed an LLM or an agent runtime.

> Status: pre-release. Install from this checkout. Release packages are not
> published yet.

## Keep an agent oriented

The current v0.9 development workflow starts with compact semantic context,
retains structural continuity while an agent edits, can apply a reviewed
deterministic refactor, and ends with executed change evidence:

~~~text
Workspace Brief -> Begin Change -> Search / Symbol Context
  -> agent edits -> Checkpoint Change
  -> optional Refactor Preview / Apply -> Verify Change
~~~

`go_workspace_brief` returns package layout, exported APIs, diagnostics,
repository guidance hashes, optional change impact, risks, and explicit
uncertainty in a bounded `agentic.context/v1` Context Pack. `go_search`
returns stable Symbol Refs. `go_symbol_context` uses those refs for hover,
definitions, reference totals, implementations, related tests, diagnostics,
and optional call hierarchy without trusting stale line numbers.

Every response identifies the exact workspace snapshot it observed. When full
detail exceeds the compact byte budget, the response provides an opaque cursor
for the `agentic-go://artifact/{id}` resource template. Agentic-go rejects stale
refs instead of silently resolving them against changed source.

`go_begin_change` stores an opaque human goal, local base, scope, optional
focus and allowed paths, structural policies, and the initial Snapshot Ref in a
private Change Contract. `go_checkpoint_change` requires the exact latest
snapshot ID and reports structural drift, affected packages, focused
diagnostics, policy violations, and uncertainty. Goal and decision prose is
retained for handoff, never interpreted as an enforceable instruction.

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

JSON uses the frozen `agentic.verify/v1` contract. The
[v1 schema migration guide](docs/v1-schema-migration.md) describes pre-freeze
compatibility and identity changes. Exit status is `0`
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
- go.analysis.concurrency: passed; 0 introduced, 0 existing, 0 resolved, 0 unknown
- go.analysis.errors: passed; 0 introduced, 1 existing, 0 resolved, 0 unknown
- go.coverage: passed; 83.3% of changed statements covered
- go.test: passed; 42 passed, 0 failed, 1 skipped

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

After v0.3.0 is published, the canonical exact-version installer is:

~~~sh
curl --fail --location --silent --show-error \
  https://raw.githubusercontent.com/ashwingopalsamy/agentic-go/v0.3.0/scripts/install.sh \
  | bash -s -- 0.3.0
~~~

It verifies `checksums.txt` before installing `agentic-go`, `agentic-go-vet`,
and the exact `agentic-go-gopls` companion into `~/.local/bin`. Release
archives also contain the Apache license, upstream gopls BSD license, and
gopls dependency notices.

For development from this checkout, the advanced module path remains:

~~~sh
go install ./cmd/agentic-go ./cmd/agentic-go-vet
go install golang.org/x/tools/gopls@v0.21.0
cp "$(go env GOPATH)/bin/gopls" "$(go env GOPATH)/bin/agentic-go-gopls"
export PATH="$(go env GOPATH)/bin:$PATH"
agentic-go doctor
~~~

Keep `$(go env GOPATH)/bin` on your `PATH` so an MCP client can find the
binaries. Homebrew is not advertised because no maintained tap exists. The
personal repository and module path remain `github.com/ashwingopalsamy/agentic-go`
through public v0.x releases. The future organization path is a planned,
breaking module migration, not a current import path.

## Connect an MCP client

agentic-go speaks stdio. Generate configuration without editing client files:

~~~sh
agentic-go mcp-config --client generic --workspace /absolute/path/to/your/go/module
agentic-go mcp-config --client codex --workspace /absolute/path/to/your/go/module
agentic-go mcp-config --client claude --workspace /absolute/path/to/your/go/module
~~~

A generic MCP client configuration looks like this:

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

Run `agentic-go doctor --format text` or `agentic-go doctor --format json` to
inspect the effective workspace, inherited Go toolchain, exact gopls companion,
and guarded-refactor recovery state. If an apply was interrupted, ordinary
refactor mutation stops. `agentic-go doctor --recover` restores only files
that still match the journaled preimage or postimage and refuses to overwrite
diverged user edits.

## Server flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `--workspace` | `.` | Validated Go workspace root |
| `--log-level` | `info` | Stderr lifecycle logging: `debug` or `info` |
| `--max-concurrent-loads` | `4` | Process-wide concurrent Go subprocess/load limit |
| `--max-tool-seconds` | `300` | Maximum duration of one tool operation |
| `--version` | `false` | Print the version and exit |

## MCP adapter

agentic-go also speaks stdio MCP for local coding agents. The current v0.9
development inventory is 14 tools, seven fixed resources, one artifact resource
template, and six prompts. Earlier tool contracts remain compatible. Change
Contracts are private same-machine user-cache state, preserve exact snapshot
lineage, and reject stale checkpoints. Goal and decision prose is context only
and is never semantically enforced.
The three semantic tools are read-only and return canonical structured content.
The two continuity tools persist private state but do not edit source.
`go_refactor` previews or applies one deterministic, snapshot-bound plan.
`go_verify_change` returns the same report as the CLI with only a concise text
fallback, and supporting clients can approval-gate its trusted-code execution.
The report records the exact semantic snapshot, provider capabilities,
compiler and gopls diagnostic evidence, bounded context and refactor
provenance, and optional Change Contract compliance. Current-only diagnostics
remain explicit evidence and uncertainty; they are not mislabeled as newly
introduced defects without a comparable baseline.

Use `agentic-go contract export --output <workspace-relative-path>` for an
explicit 0600, contained, non-overwriting workspace copy. Normal operation
does not write into the worktree. Exported API policy compares declaration
shape and reports uncertainty when source cannot be classified. These are
implementation properties, not model accuracy or adoption claims.

## Guarded refactoring

`go_refactor` supports `rename`, `format`, `organize_imports`, and `fix_all`.
A preview asks the pinned gopls companion for edits, normalizes UTF-16 ranges,
rejects overlapping or outside-workspace changes, and returns a
content-addressed plan, diff, affected files, and exact SHA-256 preimages.
Preview does not modify the worktree.

Apply requires the plan ID and the exact preview snapshot. The MCP tool is
marked destructive and non-idempotent so clients can require explicit
approval. Agentic-go reloads and validates the private plan, rechecks every
preimage, writes a durable recovery journal, and changes only existing,
contained, non-generated files. It never creates or deletes files and never
stages, commits, or rewrites Git history. Plans and journals remain private
under `os.UserCacheDir()/agentic-go/refactors`.

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

### Semantic context

| Tool | Use |
| --- | --- |
| `go_workspace_brief` | Return a compact snapshot-bound workspace and optional change overview |
| `go_search` | Search workspace symbols and return stable Symbol Refs |
| `go_symbol_context` | Resolve a Symbol Ref or source position into bounded semantic context |

### Change verification

| Tool | Use |
| --- | --- |
| `go_verify_change` | Return one source-grounded impact, evidence, findings, risk, and uncertainty report for a local change |

### Guarded refactoring

| Tool | Use |
| --- | --- |
| `go_refactor` | Preview a deterministic rename, format, import organization, or fix-all plan, then apply that exact plan after approval |

### Change continuity

| Tool | Use |
| --- | --- |
| `go_begin_change` | Persist a private snapshot-bound goal, scope, focus, and structural policy contract |
| `go_checkpoint_change` | Record exact snapshot lineage, structural drift, decisions, questions, and focused diagnostics |

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
| `agentic-go://capabilities` | Effective pinned-gopls capability manifest and Context Pack limits |
| `agentic-go://change-contract/current` | Latest active private Change Contract for the workspace repository |

`agentic-go://artifact/{id}` is a resource template. Replace `{id}` with the
opaque cursor returned by a truncated Context Pack or symbol context. Each read
returns a bounded UTF-8-safe chunk and, when needed, another cursor.

### Prompts

| Prompt | Use |
| --- | --- |
| `audit-package` | Run both shipped audit domains for a package |
| `pre-commit-check` | Run the broader pre-commit verification sequence |
| `bisect-flake` | Investigate a flaky test with race evidence |
| `verify-change` | Call `go_verify_change` once and interpret its complete report |
| `understand-change` | Begin one private Change Contract before editing |
| `resume-change` | Read and checkpoint the current Change Contract before continuing |

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
- The v0.3 distribution bundles gopls v0.21.0 as `agentic-go-gopls`, disables
  its telemetry in managed sessions, and validates its exact version. The v0.4
  development line exposes compact Context Packs, not raw LSP or the
  experimental upstream gopls MCP interface.
- MCP remains stdio only. HTTP, SARIF, automatic toolchain installation,
  Homebrew distribution, and a Windows support claim are not shipped.

See the [protocol contracts](docs/contracts.md) and
[security guidance](SECURITY.md) for the full boundary.

## Evidence

The [v0.2.0 change-verification evidence](validation/v0.2.0/summary.md) records
contract goldens, CLI and stdio MCP dogfood, the complete release matrix, and
three reviewed historical changes from grpc-go, Prometheus client_golang, and
Echo. The samples demonstrate reverse impact, changed coverage, analyzer
baselining, and a bounded-output limitation; they are implementation evidence,
not an adoption claim.

The active analyzer rules have positive and near-miss fixtures. The v0.1.0
offline calibration report covers 10 pinned repositories and 467 reviewed
findings, with 0% observed false positives in that corpus. This is
corpus-specific evidence, not a universal guarantee. Read the
[validation evidence](validation/v0.1.0/summary.md) for the corpus, raw
sanitized reports, classifications, limitations, and reproduction command.

## Read next

- [v0.2.0 release scope](docs/v0.2.0-release-scope.md): the verification compatibility baseline
- [v1 roadmap](docs/v1.0.0-roadmap.md): current additive v0.3+ development authority and implementation status
- [v0.1.0 release scope](docs/v0.1.0-release-scope.md): the compatibility baseline
- [Protocol contracts](docs/contracts.md): shared types, limits, and invariants
- [v0.2.0 evidence](validation/v0.2.0/summary.md): change-verification reports and release gates
- [v0.1.0 evidence](validation/v0.1.0/summary.md): external analyzer calibration record
- [Contributing](CONTRIBUTING.md): review and change expectations
- [Security](SECURITY.md): safe usage and private vulnerability reports

## License

Apache-2.0. See [LICENSE](LICENSE).
