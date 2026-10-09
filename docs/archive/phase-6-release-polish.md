# Phase 6 — Release polish

Read [`contracts.md`](contracts.md) first. The canonical v0.1.0 release
authority is [`v0.1.0-release-scope.md`](v0.1.0-release-scope.md); this file
defines release-polish implementation details only.

## Deliverables
1. `.golangci.yml`
2. `.goreleaser.yml` — GitHub archives and checksums for macOS/Linux,
   amd64/arm64; no Homebrew tap.
3. `cmd/agentic-go/main.go` — `--version` and optional `--workspace` (default
   cwd), with stdio as the only transport.
4. `internal/mcptest/e2e_test.go` — new, real end-to-end MCP protocol test.
5. `internal/tools/registry_test.go` — new, replaces the release checklist's
   `grep`-based tool-count check with a Go test.
6. `README.md`
7. `SECURITY.md`
8. `CONTRIBUTING.md`
9. `CHANGELOG.md`
10. Tag and publish `v0.1.0` only after the release checklist and final FP
    gate are complete.

## `.golangci.yml`
Must enable, at minimum, the linters that enforce this repository's stated
error-handling, correctness, formatting, naming, and resource-lifecycle
standards:
```yaml
version: "2"

run:
  timeout: 5m
  modules-download-mode: readonly

linters:
  enable:
    - bodyclose
    - errcheck
    - govet
    - revive
    - staticcheck
    - unused
  settings:
    govet:
      enable:
        - shadow
        - fieldalignment
    revive:
      enable-default-rules: true
      rules:
        - name: package-comments
          disabled: true

formatters:
  enable:
    - gofumpt
```
This is the golangci-lint v2 schema: `gofumpt` is a formatter and
`fieldalignment` is a `govet` analyzer. The list is a deliberate floor, not
an invitation to enable every available linter. `bodyclose` remains useful
as a general lifecycle guard even though v0.1.0 makes no direct HTTP request.

## `.goreleaser.yml`
Cross-compile matrix must match `contracts.md`'s CI contract exactly (no
silent drift between "what CI tests" and "what we ship" — a target that
builds in CI but isn't released, or vice versa, is a real shipped-but-
untested-or-tested-but-unshipped bug):
```yaml
builds:
  - id: agentic-go
    main: ./cmd/agentic-go
    binary: agentic-go
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    # No `ignore:` stanza. contracts.md's CI contract step 6 explicitly
    # cross-compiles darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 — all
    # 4 combinations of the goos/goarch lists above. An `ignore: linux/arm64`
    # here would ship 3 of the 4 targets CI itself verifies build-clean,
    # which is a shipped-but-untested-elsewhere / tested-but-unshipped bug
    # by this same file's own stated no-silent-drift rule. Do not add one
    # back without first narrowing contracts.md's matrix — that file is the
    # single source of truth for the release matrix, this file copies it.
  - id: agentic-go-vet
    main: ./cmd/agentic-go-vet
    binary: agentic-go-vet
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: "checksums.txt"
release:
  github:
    owner: ashwingopalsamy
    name: agentic-go
```
Install through GitHub release archives/checksums or
`go install github.com/ashwingopalsamy/agentic-go/cmd/agentic-go@latest`.
Homebrew is deliberately not a v0.1.0 channel.

CI wiring: one per-push workflow runs build, race tests, vet, staticcheck,
lint, the macOS/Linux amd64/arm64 cross-compile matrix, and MCP protocol
checks. A tag-triggered release workflow packages only after those checks
are green; it does not invent a second CI count or a Homebrew workflow.

## `cmd/agentic-go/main.go` release behavior

**No subcommand router.** The release scope explicitly defers the
four `audit`/`test`/`race`/`coverage` subcommands to v0.2.0 —
they'd re-expose the MCP tool surface to a second, CLI-shaped caller and
double the interface to keep in sync. Doctor and SARIF are also deferred
until their workflows are established.

| Flag | Default | Meaning |
|---|---|---|
| `--version` | `false` | Print the module version and exit. |
| `--workspace` | current directory | Optional workspace root for analysis; defaults to cwd. |

The server is stdio-only in v0.1.0. Do not add gopls, navigation, cache,
doctor, SARIF, HTTP, or unsupported Windows behavior to this release.

## `README.md`
Required sections, in order (this is the primary artifact a human or a
future coding agent reads before touching this repo — token-efficiency
applies here too, no marketing filler):
1. One-paragraph what-this-is (MCP server, exactly 7 tools, Go-only
   analysis, zero-LLM-in-the-loop for deterministic checks).
2. Install with `go install github.com/ashwingopalsamy/agentic-go/cmd/agentic-go@latest`
   — this is the actual onboarding path for this project's primary audience
   (a Claude Code user adding an MCP server), not a generic Go-tooling
   install line. A GitHub release archive/checksum is the other supported
   channel. Homebrew is not a v0.1.0 channel.
3. Wire into other MCP clients using stdio only; use `--version` for
   diagnostics and `--workspace` defaults to cwd.
4. **Comparison table** — summarize the documented product boundary and
   independently verified competitor behavior (gopls's own MCP server and a
   maintained standalone gopls MCP server) across structured output, custom
   audit suite, MCP result caching, and trace/observability. Qualify external
   cells narrowly; absence from public documentation is not proof that a
   capability cannot exist.
5. Full tool catalog — one line per tool, grouped by tier, name + one-clause
   purpose (not full input/output schemas, those are in the MCP server's own
   `tools/list` response and don't need duplicating in prose).
6. Development: `go test -race ./...`, `golangci-lint run`, link to
   `CONTRIBUTING.md`.

## `SECURITY.md`
**Lead with the code-execution boundary, not the network-exfiltration
non-issue.** The previous version of this file led with "never transmits
source code off-machine" — true, but not the actual risk a security-
conscious adopter needs to evaluate before running this server against a
private repo. The real boundary: **this server compiles and executes
target-repo code as part of its normal operation, on whatever machine it
runs on, with whatever privileges that process has.** Name it precisely,
don't soften it into "operates on source code":

- **Five v0.1.0 tools build and run target-repo test binaries** —
  `go_test_structured`, `go_race_report`, `go_coverage_gaps`,
  `go_benchmark_diff`, and `go_flake_finder`. Each shells out to `go test`
  against the target package, which compiles and executes every `Test*`,
  `Benchmark*`, or `Fuzz*` function that package already has — this server
  does not add new execution capability, it invokes the same `go test` a
  developer would run by hand, but an MCP client can now trigger it. A
  malicious or compromised target repo's test code runs with this process's
  privileges the same way it would under a human running `go test` directly.
  This is not a vulnerability in this server — it's the same trust model
  every CI runner and every local `go test` invocation already has — but an
  adopter pointing this server at an untrusted repo should know it's
  identical to running that repo's own test suite, not a sandboxed read.
- **The two v0.1.0 audit tools compile (type-check) but do not run** target-repo
  code — `packages.Load` + `go/analysis`/`go/types` only, no `Test*`/
  `Benchmark*`/`Fuzz*` function is ever invoked.
- v0.1.0 has no module-risk/network-proxy tool; analysis is local and stdio
  only.
- Supported versions table (latest tagged release only — this is a v0.1.0
  tool, not a maintained-LTS product yet, say so rather than implying a
  support matrix that doesn't exist).
- Vulnerability reporting: private disclosure channel (GitHub Security
  Advisories "Report a vulnerability" — do not publish a public email
  address in a committed file if avoidable, GitHub's private advisory flow
  exists for exactly this).
- There is no HTTP transport in v0.1.0.

## `CONTRIBUTING.md`
- Point to `contracts.md` and the phase specification files under `docs/` as the
  authoritative design reference for anyone adding a new tool — a new tool
  PR should extend the same canonical types (`Finding`, `AuditResult`,
  `Location`), not invent parallel ones.
- New-tool checklist: register via `mcp.AddTool`, add to the flat
  `Register<Name>` call list in `main.go`, declare whether the result may be
  cached when caching is introduced, add a fixture
  under an isolated `testdata/fixtures/<domain>/` subpackage (never the
  shared mega-fixture pattern this project explicitly rejected), and add a
  test that states the behavior or safety property it protects.
- PR must pass the full CI gate (the six checks in `contracts.md`)
  before merge — no `--no-verify`, no skipped steps.

## `CHANGELOG.md`
`v0.1.0` — single entry, Keep-a-Changelog format, listing every tool by name
grouped by tier (matches the README's tool catalog grouping, single source
of truth for "what shipped in this version" duplicated in exactly one other
place, the README, both generated from the same phase-file tool list so they
can't drift independently if kept in sync at release time — call this out
as a release-checklist item, not just a file to write once).

## `internal/mcptest/e2e_test.go`
Every other test in this project exercises one tool's handler function
directly — real, but it never proves the MCP protocol plumbing (schema
generation, JSON-RPC framing, the stdio transport loop) actually works
end-to-end. This file is the one test that does: it builds the real
`cmd/agentic-go` binary (`go build -o <tmp> ./cmd/agentic-go` in `TestMain`),
launches it as a subprocess over stdio exactly as a
real MCP client would, performs the `initialize` handshake, calls
`tools/list` and asserts the count equals the registry (see below), then
calls one real tool (`go_test_structured` against
`internal/tools/testdata/fixtures/testing/` — the same self-hosting fixture
Phase 1 already uses) and asserts a well-formed `TestStructuredOutput` comes
back over the wire. Keep its fixture small and deterministic; it runs under
the normal `go test -race ./...` CI step and has no external language-server
dependency.

## `internal/tools/registry_test.go`
Replaces the release checklist's `grep -c 'mcp.AddTool'` check with a real
test: `TestRegistrySize` calls the same flat `Register<Name>` list `main.go`
calls (refactored into a shared `tools.RegisterAll(server)` both `main.go`
and this test call, so there is exactly one list, not a duplicate one for
testing) against a throwaway `mcp.NewServer`, then asserts
the v0.1.0 count of seven tools. A `grep` over source text breaks silently
the moment a tool is registered via a helper or a loop instead of a literal
`mcp.AddTool(...)` call; a test against the live registry cannot drift from
what a client actually sees.

## CI contract

The release gate has six checks: build, race tests, vet, staticcheck,
golangci-lint, and the macOS/Linux amd64/arm64 cross-compile matrix. MCP
protocol checks are local and do not require gopls.

## Release checklist (mechanical, run in order)
1. `go test -race ./...` green.
2. `golangci-lint run` clean.
3. Cross-compile matrix (`contracts.md`'s CI contract) green for every
   target.
4. MCP protocol checks green without an external language-server dependency.
5. `CHANGELOG.md` and README tool catalog cross-checked against the actual
   registered tool list — run `go test -run TestRegistrySize
   ./internal/tools/...` (replaces the earlier `grep`-based check; the
   registry test is the source of truth for the count, currently 7, not a
   number restated by hand in this checklist).
6. `git tag v0.1.0 -m "..."`. This is a production-adjacent action because
   the tag triggers a public release; obtain fresh user confirmation
   immediately before running it.
7. `git push origin v0.1.0` triggers the release workflow — same
   confirmation requirement as step 6, this is the point of no easy return
   (a public release asset).

## Verification (this phase's own gate)
Not a code-test gate (there's no new Go logic in this phase, it's config +
docs) — the gate is the release checklist above, executed in order, with
step 6/7's explicit confirmation requirement treated as load-bearing, not
optional ceremony.
