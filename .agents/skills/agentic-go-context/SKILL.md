---
name: agentic-go-context
description: Use agentic-go context before unfamiliar or cross-package Go edits when impact or verification applicability matters; start with one selector and refresh after edits with base plus previous_pack_id only.
---

# Agentic Go Context

For unfamiliar or cross-package Go edits, call `go_context` before editing and
refresh after edits.

1. Start with `base` and one selector: `query`, `symbol_ref`, source
   `file`+`line`+`column`, or `focus_file`/`focus_package`. Selectors are
   mutually exclusive.
2. If a result is ambiguous, narrow it using the returned current candidates
   instead of repeating the same broad query.
3. Finish each batch of edits and formatting before refreshing. Refresh with
   `base` and `previous_pack_id` only; omit old selectors and Symbol Refs.
   Refresh again after further edits or stale-snapshot rejection; avoid
   redundant refreshes while the snapshot is unchanged.
4. Stale rejection is expected: stop and select current evidence; never bypass
   it.
5. Use the result for impact and verification applicability. Treat impact and
   reverse-dependency evidence as planning guidance, not authorization to edit
   affected packages; edit only the smallest task owner within supplied
   path/package scope. Skip trivial edits and preserve uncertainty when
   evidence is unavailable. Context does not execute checks or prove
   correctness.
