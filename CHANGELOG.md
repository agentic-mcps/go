# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Change verification through `agentic-go verify`, the root GitHub Action, and
  the additive `go_verify_change` MCP tool, all sharing the
  `agentic.verify/v1alpha1` report contract.
- Stdio MCP server with eight tools (the seven v0.1 tools remain compatible):
  `go_test_structured`, `go_race_report`, `go_coverage_gaps`,
  `go_benchmark_diff`, `go_flake_finder`, `go_audit_concurrency`, and
  `go_audit_errors`, plus `go_verify_change`.
- Four fresh-on-read resources: `agentic-go://module`,
  `agentic-go://packages`, `agentic-go://analysis-rules`, and
  `agentic-go://trace-summary`.
- Four workflow prompts: `audit-package`, `pre-commit-check`, `bisect-flake`,
  and `verify-change`.
- `agentic-go-vet`, a `go vet`-compatible CLI for the concurrency and error
  analyzers with standard text and JSON output.
- Workspace containment, bounded execution, event-driven progress, and
  optional privacy-preserving local traces.

[Unreleased]: https://github.com/ashwingopalsamy/agentic-go/commits/main
