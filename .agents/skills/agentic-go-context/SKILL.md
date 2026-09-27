---
name: agentic-go-context
description: Use agentic-go context before unfamiliar or cross-package Go edits when impact or verification applicability matters; start with one selector and refresh after edits with base plus previous_pack_id only.
---

# Agentic Go Context

For unfamiliar or cross-package Go edits, call `go_context` before editing and
refresh after edits. The workflow is snapshot-bound evidence, not a correctness
claim.

1. Start with `base` and exactly one selector: `query`, `symbol_ref`, source
   `file`+`line`+`column`, or `focus_file`/`focus_package`. Selectors are
   mutually exclusive.
2. Symbol Refs are opaque byte strings. Copy them exactly. Never decode, edit,
   shorten, reconstruct, or re-encode one.
3. File coordinates must point to the declaration identifier itself, never
   whitespace, a line start, a comment, or a local variable.
4. If a result is ambiguous, copy one returned current candidate or issue a
   fresh query/file selector. Do not repeat the same broad query.
5. A selector failure produces no evidence. Do not retry the same selector;
   recover with fresh evidence. Stale rejection is expected and must not be
   bypassed.
6. Finish each batch of edits and formatting before refreshing. Refresh with
   `base` and `previous_pack_id` only; omit old selectors and Symbol Refs.
   Refresh again after further edits or stale-snapshot rejection; avoid
   redundant refreshes while the snapshot is unchanged.
7. Use the result for impact and verification applicability. Treat impact and
   reverse-dependency evidence as planning guidance, not authorization to edit
   affected packages; edit only the smallest task owner within supplied
   path/package scope. Skip trivial edits and preserve uncertainty when
   evidence is unavailable. Context does not execute checks or prove
   correctness.
