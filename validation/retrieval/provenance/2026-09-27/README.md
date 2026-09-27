# Source worktree preservation record

Before reconciliation, both audited source worktrees were at base commit
`7b5111c6365a2a12806a561d86745d5bc50d7c9e`. The source worktrees remain
available at their original paths. This directory preserves each complete
tracked `git diff --binary` as a gzip archive, captured status, and SHA-256
list for its untracked files. Decompress `v1_2_reliability.diff.gz` or
`decision_value_eval.diff.gz` to recover the original diff; the table's
SHA-256 is for those uncompressed bytes.

| Source worktree | Branch | Tracked diff SHA-256 |
| --- | --- | --- |
| `/Users/ashwin/Desktop/projects/golang/agentic-mcps-go` | `codex/v1.2-reliability` | `387a368c2349345fe888e917fc00961583726cc120d9ef1d6948f9c537d5c6da` |
| `/Users/ashwin/Desktop/projects/golang/agentic-mcps-go-value-eval-2026-09-27` | `codex/decision-value-eval-2026-09-27` | `a34f2424ab1879399822d9c4eab05482c2c19c8a6e5072366ee932c36e3bca0a` |

The second worktree's 72-run decision-value harness remains parked in that
source worktree. It is pinned to a different snapshot and GPT-6 Sol/high, so it
does not contribute retrieval evidence and is not part of this integration.
The repeated `decision-value-eval-2026-09-27-refresh` worktree was also left
untouched; it is not one of the two audit snapshots above.

The saved SHA lists include untracked files from their respective source
worktrees. Validate an untracked file against the corresponding manifest before
reusing it. These records capture pre-reconciliation state and are not live
status reports.
