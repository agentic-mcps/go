# Contributing to agentic-go

Thank you for improving agentic-go. The project favors precise, explainable
analysis over a large rule or tool count.

## Before changing code

Read [`AGENTS.md`](AGENTS.md) and the canonical
[`v0.1.0 release scope`](docs/v0.1.0-release-scope.md). Shared protocol and
safety invariants live in [`docs/contracts.md`](docs/contracts.md); domain
specifications under `docs/` explain individual analyzer rules and their
limitations.

Discuss additions that change the MCP inventory, trust boundary, output schema,
or supported platforms before implementation. Deferred roadmap items are not
implicitly part of v0.1.0.

## Development

Use Go 1.25 or newer. The supported test range is Go 1.25, 1.26, and 1.27.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
staticcheck ./...
golangci-lint run
```

Run focused package checks while iterating, then the complete gate before
opening a pull request. CI also cross-builds both binaries for macOS and Linux
on amd64 and arm64.

## Analyzer rules

A new or changed rule must include:

- one isolated positive fixture with the expected rule ID, severity, file, and
  source location;
- a compliant or near-miss fixture that exercises the most likely false
  positive;
- an explicit limitation in the domain specification;
- coverage through the upstream analyzer harness and the production
  `audit.Run` path; and
- evaluation against the release validation corpus before it is described as
  externally validated.

Disable a rule when reviewed evidence shows more than 5% false positives or a
repeatable systemic false-positive pattern. Rules with no meaningful external
hits may remain fixture-tested but are not marketed as validated.

## Protocol changes

Reuse `finding.Finding`, `finding.AuditResult`, and `finding.Location` for code
findings. Register shipped protocol surfaces through
`internal/tools.RegisterAll` and extend the in-memory protocol inventory test.
Keep the MCP interface focused on local coding agents; CI-oriented output
belongs in compatible command-line surfaces when it would distort MCP.

## Pull requests

Keep changes coherent and reviewable. Use a short Conventional Commit subject
such as `feat: add analyzer rule` or `fix: preserve error identity`. By
submitting a contribution, you agree that it is licensed under Apache-2.0 as
described in [`LICENSE`](LICENSE).
