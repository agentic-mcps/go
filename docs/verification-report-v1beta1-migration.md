# Verification report v1beta1 migration

`agentic.verify/v1beta1` is the current development report emitted by
`agentic-go verify` and production `go_verify_change` calls. The v0.2
`agentic.verify/v1alpha1` contract is frozen for historical evidence and is no
longer emitted as a parallel output mode.

Consumers must inspect `schema_version` before decoding. Supporting alpha and
beta inputs is a consumer choice; agentic-go itself emits beta only.

## What changed

Beta preserves the existing repository, change, impact, plan, evidence,
finding, risk, uncertainty, result, and exit-code semantics. It adds:

- `id`, a content-addressed `verify_<sha256>` identity computed after canonical
  normalization;
- `providers`, the effective capability manifests used for the report;
- `snapshot`, the exact semantic snapshot and contract/checkpoint transitions;
- `provenance`, bounded references to context operations and guarded refactor
  plans observed by the current process;
- `go.diagnostics` evidence for current compiler and gopls diagnostics; and
- `go.contract` evidence for optional machine-checkable Change Contract
  compliance.

The new top-level collections are always present and non-null. Their ordering
is deterministic. Report finalization still applies result precedence in this
order:

```text
incomplete > findings > pass
```

CLI exits remain `2`, `1`, and `0` respectively. A passing report still means
only that requested checks completed without policy-blocking evidence.

## Diagnostic and contract semantics

Current semantic diagnostics are recorded as evidence. Agentic-go does not
call them introduced findings without a comparable base diagnostic snapshot.
The report includes explicit uncertainty for that limitation.

Contract compliance evaluates only machine-checkable structure. A `forbid`
violation blocks independently of `--fail-on`; a `warn` observation remains
advisory. Human goal, decision, and unresolved-question prose is retained in
the private Change Contract and is not interpreted or copied into report
provenance.

The MCP request fields `contract_id` and `expected_snapshot_id` are optional.
Existing calls without them remain valid. When supplied, both identify the
exact active contract lineage; stale or inconsistent references fail loudly.

## Schema and goldens

- Current schema:
  [`schema/verification-report-v1beta1.json`](schema/verification-report-v1beta1.json)
- Frozen alpha schema:
  [`schema/archive/verification-report-v1alpha1.json`](schema/archive/verification-report-v1alpha1.json)
- Representative alpha report:
  [`../validation/v0.2.0/grpc-go-reverse-impact.json`](../validation/v0.2.0/grpc-go-reverse-impact.json)
- Strict beta goldens:
  [`../internal/verification/testdata/report-pass.json`](../internal/verification/testdata/report-pass.json),
  [`../internal/verification/testdata/report-findings.json`](../internal/verification/testdata/report-findings.json), and
  [`../internal/verification/testdata/report-incomplete.json`](../internal/verification/testdata/report-incomplete.json)

The alpha schema is retained as historical compatibility evidence. New beta
fields describe deterministic local evidence; they do not establish universal
correctness, model reliability, token savings, or adoption claims.
