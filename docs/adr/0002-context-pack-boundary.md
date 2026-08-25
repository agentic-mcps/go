# Make Context Packs the semantic boundary

Agentic-go exposes source-grounded semantic intelligence through compact,
versioned Context Packs produced by `internal/intelligence`. MCP is a delivery
adapter and gopls is a pinned infrastructure provider. Neither protocol defines
the durable product interface.

Every semantic operation observes an immutable Snapshot Ref. Symbol Refs retain
normalized Go identity and the exact snapshot, base, and package scope used to
create them. A stale ref is rejected rather than silently resolved against new
source. Public locations are workspace-relative with one-based UTF-8 byte
columns; UTF-16 conversion stays inside the LSP adapter.

Compact responses retain totals, truncation, provenance, and uncertainty.
Complete overflow detail is stored privately as content-addressed artifacts and
read through opaque snapshot-bound cursors. This keeps common agent calls small
without hiding that more information exists.

This boundary lets later language implementations reuse proven concepts without
sharing Go runtime code or flattening language-native semantics into an MCP or
LSP-shaped plugin system.
