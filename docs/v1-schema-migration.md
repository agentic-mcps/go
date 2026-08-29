# v1 schema migration

Agentic-go v0.9 freezes the current domain contracts as
`agentic.context/v1`, `agentic.change/v1`, and `agentic.verify/v1`. New CLI and
MCP results emit only these versions. Consumers must inspect
`schema_version` before decoding.

## Context Packs

`agentic.context/v1` retains the v1alpha1 field shape, location convention,
collection requirements, truncation semantics, snapshot binding, and
uncertainty semantics. The only contract change is the schema identity.

Context responses and artifact cursors are snapshot-bound rather than durable
cross-version storage. Existing alpha artifact bytes are not rewritten or
reinterpreted.

## Change Contracts

`agentic.change/v1` retains the v1alpha1 field shape and structural-policy
semantics. Private alpha contracts are validated and promoted to v1 in memory
when loaded. A read does not change the cache file. The next explicit
checkpoint or other save writes v1 atomically.

Malformed alpha state and unknown schema identities fail as corrupt state.
There is no flag that emits a new alpha contract.

## Verification reports

`agentic.verify/v1` retains the v1beta1 field shape, content-addressed identity,
policy, evidence, diagnostic, contract, provenance, and exit semantics. The
new schema identity changes the content-addressed report ID even when all other
fields are equal.

Private beta reports remain readable by their exact existing identity and are
never rewritten. Newly executed verification produces v1. The historical v0.2
alpha schema remains checked-in evidence and is not accepted as private
verification-store state.

Consumers migrating directly from v0.2 alpha should first read the historical
[`v1beta1 migration guide`](verification-report-v1beta1-migration.md), then
apply the identity-only beta-to-v1 change described here.

## Schema locations

Current contracts:

- [`context-pack-v1.json`](schema/context-pack-v1.json)
- [`change-contract-v1.json`](schema/change-contract-v1.json)
- [`verification-report-v1.json`](schema/verification-report-v1.json)

Immutable pre-freeze schemas are under [`schema/archive/`](schema/archive/).
