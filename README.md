<p align="center">
  <img src="assets/brand/project-mark.svg" alt="agentic-go project mark" width="180">
</p>

<h1 align="center">agentic-go</h1>

<p align="center">A local Go MCP server and CLI for source-grounded code intelligence, semantic navigation, change continuity, guarded refactoring, and executed verification.</p>

<p align="center">
  <a href="#install"><img src="assets/brand/pills/install.svg" alt="Install"></a>
  <a href="#connect-an-mcp-client"><img src="assets/brand/pills/mcp-setup.svg" alt="MCP setup"></a>
  <a href="docs/contracts.md"><img src="assets/brand/pills/docs.svg" alt="Read docs"></a>
  <a href="https://github.com/agentic-mcps/go/releases/tag/v1.0.0"><img src="assets/brand/pills/release.svg" alt="v1.0.0 release"></a>
</p>

<p align="center"><a href="#why-this-exists">Why</a> · <a href="#workflow">Workflow</a> · <a href="#capabilities">Capabilities</a> · <a href="#evidence">Evidence</a> · <a href="#trust-boundary">Trust</a> · <a href="#faq">FAQ</a></p>

agentic-go keeps a coding agent oriented around the codebase it is changing. It is deterministic local tooling. It does not embed an LLM, author arbitrary feature code, or become an agent framework.

## Install

Install the exact signed `v1.0.0` release and its bundled `agentic-go-gopls` companion:

```sh
curl --fail --location --silent --show-error \
  https://raw.githubusercontent.com/agentic-mcps/go/v1.0.0/scripts/install.sh \
  | bash -s -- 1.0.0

export PATH="$HOME/.local/bin:$PATH"
agentic-go doctor
```

The installer verifies `checksums.txt` and installs into `~/.local/bin`. Archives include the Apache-2.0 license, the upstream gopls BSD license, and dependency notices. There is no Homebrew tap to pretend otherwise.

## Connect an MCP client

The server is stdio-only. Generate configuration without editing client files:

```sh
agentic-go mcp-config --client generic --workspace /absolute/path/to/your/go/module
agentic-go mcp-config --client codex --workspace /absolute/path/to/your/go/module
agentic-go mcp-config --client claude --workspace /absolute/path/to/your/go/module
```

The generic shape:

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

`--workspace` defaults to the current directory and must resolve to a Go module or workspace. The client controls approvals for tools that execute repository code.

## Why this exists

Finding a definition is easy. Keeping the same mental model after several edits is the expensive part. agentic-go gives an external coding agent compact context before an edit, a private contract during the work, and executed evidence at the end.

Your agent can find the definition. Remembering why it opened the file is the harder bit.

## Workflow

```text
Brief → Begin Change → Search / Symbol Context
  → agent edits → Checkpoint Change
  → optional guarded Refactor → Verify Change
```

Direct report:

```sh
agentic-go verify --base origin/main --package ./... --format text
```

Verification compares the final worktree with an explicit local base, computes affected packages, runs whole-package checks, compares calibrated findings, and reports evidence, risks, and uncertainty. It never selects individual tests. Use `--format json` for `agentic.verify/v1`; exit `0` means pass, `1` findings, and `2` incomplete or execution failure. Pass does not mean safe.

## Capabilities

| Focus | What it gives an agent |
| --- | --- |
| Understand | Bounded Context Packs with APIs, diagnostics, definitions, references, implementations, related tests, and optional call hierarchy. Immutable snapshots and opaque Symbol Refs reject stale guesses. |
| Continue | Private Change Contracts record goal, base, scope, focus, decisions, questions, drift, and snapshot lineage for handoff. |
| Refactor | Preview or apply deterministic rename, formatting, import organization, and allowed `source.fixAll` edits with approval, exact preimages, and contained files. |
| Verify | The CLI and `go_verify_change` share one report covering impact, tests, coverage, race evidence, calibrated findings, contract compliance, and uncertainty. |

v1 exposes 14 tools, seven resources, one artifact template, and six prompts; legacy resources and prompts remain compatible.

## GitHub Action

The composite Action downloads the exact release, verifies its checksum, and runs the same engine. It is advisory by default and requests no pull-request write permission.

```yaml
permissions:
  contents: read
steps:
  - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
    with: { fetch-depth: 0 }
  - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7
    with: { go-version: 1.27.x }
  - uses: agentic-mcps/go@v1.0.0
    with: { enforce: false }
```

Set `enforce: true` to fail on findings or incomplete verification. The Action writes a job summary, source annotations, and a JSON report under `RUNNER_TEMP`; it posts no comments.

## Evidence

<details>
<summary>What has been measured</summary>

The [v0.2 evidence](validation/v0.2.0/summary.md) records contract goldens, CLI dogfood, the release matrix, and reviewed historical changes from grpc-go, Prometheus client_golang, and Echo. Active rules have positive and near-miss fixtures. The [v0.1 calibration](validation/v0.1.0/summary.md) covered 10 pinned repositories and 467 reviewed findings, with 0% observed false positives in that corpus. That is corpus-specific evidence, not a universal guarantee.

No paid model pilot has run, so there is no universal claim about reducing model mistakes, tool calls, or token use.

</details>

## Trust boundary

Explicit support covers Go 1.25, 1.26, and 1.27 on Darwin and Linux for amd64 and arm64. The release bundles gopls v0.21.0 as `agentic-go-gopls`; newer stable Go versions may pass preflight but are unclaimed.

Execution tools may compile and run trusted repository tests, benchmarks, or fuzz functions with the server process's privileges. Audit tools are read-only. Operations enforce symlink-resolved workspace containment, cancellation, deadlines, concurrency limits, and bounded output. Containment is not a sandbox.

Contracts, journals, artifacts, and traces live under `os.UserCacheDir()/agentic-go`; normal operation does not write source or Git metadata into the target worktree. MCP remains stdio-only. HTTP, automatic toolchain installation, SARIF, and a Windows support claim are not shipped.

## FAQ

<details>
<summary>Does this replace gopls?</summary>

No. A pinned gopls sidecar provides semantic capabilities; agentic-go adds snapshots, continuity, guarded mutation, and verification around it.
</details>

<details>
<summary>Does it work with Codex or Claude?</summary>

Yes, through stdio MCP configuration. The client remains responsible for approvals and the agent's edits.
</details>

<details>
<summary>Can it change my files?</summary>

Only an explicitly approved, snapshot-bound deterministic refactor can mutate existing contained non-generated files. Git state never changes.
</details>

<details>
<summary>Is this the same module as the personal repository?</summary>

No. `github.com/agentic-mcps/go` and `github.com/ashwingopalsamy/agentic-go` are separate Go module identities with no alias or automatic mirroring. See the [migration guide](docs/module-migration.md).
</details>

## Read next

[Contracts](docs/contracts.md) · [Module migration](docs/module-migration.md) · [v1 roadmap](docs/v1.0.0-roadmap.md) · [v0.2 scope](docs/v0.2.0-release-scope.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [License](LICENSE)

## Attribution and license

The project mark is a supplied adaptation controlled by Ashwin Gopalsamy, adapted from [Maria Letta's Free Gophers Pack](https://github.com/MariaLetta/free-gophers-pack), released under CC0. The original Go gopher is by Renée French under CC BY 4.0. This adaptation and derived graphics are licensed CC BY 4.0. It is the project mark, not the official Go logo.

Software is available under the [Apache-2.0 license](LICENSE). See the [artwork terms](assets/brand/README.md).
