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

The installer verifies `checksums.txt` before installing the binaries into `~/.local/bin`. Archives include the Apache-2.0 license, the upstream gopls BSD license, and dependency notices. Homebrew is not advertised because there is no maintained tap.

## Connect an MCP client

The server is stdio-only. Generate configuration without editing client files:

```sh
agentic-go mcp-config --client generic --workspace /absolute/path/to/your/go/module
agentic-go mcp-config --client codex --workspace /absolute/path/to/your/go/module
agentic-go mcp-config --client claude --workspace /absolute/path/to/your/go/module
```

The generic shape is:

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

`--workspace` defaults to the current directory and must resolve to a Go module or workspace. Codex, Claude, and other compatible clients can use the same local process. The client owns approval decisions for tools that execute trusted repository code.

## Why this exists

Finding a definition is easy. Keeping the same mental model after several edits is the expensive part. agentic-go gives an external coding agent compact, source-grounded context before an edit, a private change contract during the work, and executed evidence at the end.

Your agent can find the definition. Remembering why it opened the file is the harder bit.

## The workflow

```text
Brief → Begin Change → Search / Symbol Context
  → agent edits → Checkpoint Change
  → optional guarded Refactor → Verify Change
```

The primary CLI workflow is:

```sh
agentic-go verify --base origin/main --package ./... --format text
```

Verification compares the final worktree with an explicit local base, computes a conservative affected-package closure, runs whole-package checks, compares calibrated analyzer findings, and reports evidence, risk facts, and uncertainty. It never selects individual tests.

`--race` opts into race evidence. `--fail-on` accepts `error`, `warning`, `info`, or `none`. `--min-changed-coverage` sets an optional `0..100` threshold. `--max-packages` defaults to 200 and is bounded at 500. Use `--format json` for the frozen `agentic.verify/v1` report. Exit status is `0` for pass, `1` for policy findings, and `2` for incomplete or execution failure. Pass means the requested checks completed without policy-blocking evidence, not that the change is safe.

## GitHub Action

The composite Action downloads the exact release, verifies its checksum, and runs the same verification engine. It is advisory by default and requests no pull-request write permission.

```yaml
permissions:
  contents: read

steps:
  - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6
    with:
      fetch-depth: 0
  - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7
    with:
      go-version: 1.27.x
  - uses: agentic-mcps/go@v1.0.0
    with:
      enforce: false
```

Set `enforce: true` to propagate findings or incomplete verification as a failing job. The Action writes a job summary, source annotations, and a JSON report under `RUNNER_TEMP`; it posts no comments.

## Capabilities

### Understand the workspace

`go_workspace_brief`, `go_search`, and `go_symbol_context` return bounded Context Packs with package layout, APIs, diagnostics, definitions, references, implementations, related tests, and optional call hierarchy. Results carry an immutable snapshot and opaque Symbol Refs. Stale references are rejected, not silently guessed at.

### Maintain change continuity

`go_begin_change` and `go_checkpoint_change` persist a private Change Contract in the user cache. It records the goal, base, scope, focus, decisions, questions, structural drift, and exact snapshot lineage. Goal prose is retained for handoff and is never treated as a machine-enforceable instruction.

### Refactor with guardrails

`go_refactor` previews or applies deterministic rename, formatting, import organization, and allowed `source.fixAll` edits. Apply requires explicit approval, the exact preview snapshot, matching preimages, and existing contained non-generated files. It never stages, commits, creates, or deletes files.

### Verify with evidence

`go_verify_change` returns the same report as the CLI. The engine records changed declarations, reverse impact, tests, coverage, race evidence when requested, calibrated concurrency and error findings, contract compliance, risk areas, and explicit limitations.

The frozen v1 MCP surface is 14 tools, seven fixed resources, one artifact resource template, and six prompts. Four legacy resources and prompts remain available for compatibility.

## Evidence and uncertainty

The [v0.2 verification evidence](validation/v0.2.0/summary.md) records contract goldens, CLI and stdio dogfood, the release matrix, and reviewed historical changes from grpc-go, Prometheus client_golang, and Echo. These are implementation evidence, not adoption claims.

The active analyzer rules have positive and near-miss fixtures. The v0.1 calibration covered 10 pinned repositories and 467 reviewed findings, with 0% observed false positives in that corpus. That result is corpus-specific and is not a universal guarantee. See the [calibration record](validation/v0.1.0/summary.md).

No paid model pilot has run. The project therefore makes no universal claim about reducing model mistakes, tool calls, or token use.

## Compatibility and trust boundary

Explicit support covers Go 1.25, 1.26, and 1.27 on Darwin and Linux for amd64 and arm64. The release bundles gopls v0.21.0 as `agentic-go-gopls`, disables managed-session telemetry, and validates its exact version. Newer stable Go versions may pass preflight but are not claimed.

Verification and execution tools may compile and run trusted repository tests, benchmarks, or fuzz functions with the server process's operating-system privileges. Audit tools are read-only and do not execute target code. All operations enforce symlink-resolved workspace containment, cancellation, deadlines, concurrency limits, and bounded output. These are containment controls, not a sandbox.

Normal operation stores contracts, refactor journals, artifacts, and bounded traces under `os.UserCacheDir()/agentic-go`; it does not write source or Git metadata into the target worktree. MCP remains stdio-only. HTTP, automatic toolchain installation, SARIF, and a Windows support claim are not shipped.

## FAQ

**Does this replace gopls?** No. It uses a pinned gopls sidecar for semantic capabilities and adds snapshot lineage, change continuity, guarded mutation, and verification around it.

**Does it work with Codex or Claude?** Yes, through stdio MCP configuration. The client remains responsible for approvals and for the agent's edits.

**Does verification run tests?** Yes, when requested by the workflow. It runs trusted repository code with the caller's privileges and reports failures as evidence. It does not claim isolation.

**Can it change my files?** Only an explicitly approved, snapshot-bound deterministic refactor can mutate existing contained non-generated files. It never changes Git state.

**What does containment mean?** Paths are resolved inside the configured workspace, subprocesses are bounded, and cancellation propagates. Containment is not a security sandbox.

**Is this the same module as the personal repository?** No. `github.com/agentic-mcps/go` and `github.com/ashwingopalsamy/agentic-go` are separate Go module identities. There is no alias or automatic mirroring.

See the [module migration guide](docs/module-migration.md) before moving an existing installation or automation.

## Read next

- [Protocol contracts](docs/contracts.md)
- [Module migration](docs/module-migration.md)
- [v1 roadmap](docs/v1.0.0-roadmap.md)
- [v0.2 verification scope](docs/v0.2.0-release-scope.md)
- [v0.2 evidence](validation/v0.2.0/summary.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [License](LICENSE)

## Attribution

The project mark is a supplied adaptation controlled by Ashwin Gopalsamy, adapted from [Maria Letta's Free Gophers Pack](https://github.com/MariaLetta/free-gophers-pack), released under CC0. The original Go gopher is by Renée French and is licensed CC BY 4.0. This adaptation and derived graphics are licensed CC BY 4.0. It is the project mark, not the official Go logo.

See the [artwork terms and provenance](assets/brand/README.md).

## License

The software is available under the [Apache-2.0 license](LICENSE).
