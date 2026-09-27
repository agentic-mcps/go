# Retrieval benchmark

`retrievalbench` screens the existing declaration retrieval kernel on immutable
Go repository snapshots. It runs no model and makes no claim about full
`go_context` output, semantic resolution, or arbitrary-size repository support.

## Run

```sh
go run ./validation/cmd/retrievalbench \
  --manifest validation/retrieval/heldout-v1/medium-agentic-go.json \
  --repo /path/to/agentic-go-clone \
  --source-repo . \
  --gopls /path/to/gopls \
  --out /tmp/agentic-go-retrieval-medium.json
```

For the private text-candidate ablation, use a fresh reviewed manifest and add
`--text-candidate-ablation`:

```sh
go run ./validation/cmd/retrievalbench \
  --manifest validation/retrieval/heldout-v2/medium-agentic-go-text.json \
  --repo /path/to/agentic-go-clone \
  --source-repo . \
  --text-candidate-ablation \
  --out /tmp/agentic-go-retrieval-text-v2.json
```

The ablation is evaluation-only. It captures at most 100,000 supported text
files, 1 MiB per file, and 64 MiB total; any omitted content makes text
candidate metrics partial. It adds line-anchored text fragments to Go
declaration anchors and uses the existing BM25 scorer over the mixed pool. The
ablation emits at most 50,000 eligible text-line fragments per query. If that
candidate cap is reached, candidate-pool recall is partial. The
live intelligence path continues to call Go-only `SearchProfiled`; this flag
does not change MCP schemas or runtime behavior. The report records the text
capture counts and limits, mixed top-10 metrics, and a separate query-matched
candidate-pool recall audit with a 10,000-candidate cap. Pool recall is exact
only when the result, archive, and bounded text capture are complete. A capped
pool's recall is a lower bound and is labeled partial.

The `--repo` clone is used only to validate repository identity and read the
pinned objects. The harness confirms the full commit and tree IDs, then exports
that commit with `git archive`; dirty checkout content is never indexed. For a
local repository with no `origin`, set `repository_id` to
`local/<exact-repository-basename>`. The manifest SHA-256, source HEAD, source
dirty-diff SHA-256, and evaluated retrieval source SHA-256 are recorded in the
report. Keep report files outside the source checkout so the output does not
change the dirty-diff fingerprint.

The manifest is one repository per file. Its strict JSON form is specified in
[`manifest.schema.json`](manifest.schema.json). Each gold span is a reviewed,
one-based inclusive line range in the pinned tree. Allowed evidence types are
`declaration`, `caller`, `test`, `documentation`, and `configuration`. Optional
`gopls_query` gives `workspace/symbol` a concept anchor; when absent, the
question text is passed verbatim.

## Compared workflows

Agentic Go calls the current `retrieval.Cache.SearchProfiled` with the complete
archived Go source set. It records independent cache instances for cold samples
and repeats warm samples against the last cold cache for each question and
limit (`5`, `10`). A bounded LRU can evict entries between calls; cache hits and
files reparsed are reported, so “warm” means the same cache instance, not a
claim that every file remained cached. Its per-query profile separates file
parsing, candidate aggregation, ranking, and total search time.

The native baseline runs one fixed `rg` search over these supported paths:
`.go`, `.md`, `.rst`, `.txt`, `.yaml`, `.yml`, `.json`, `.toml`, `.proto`,
`.mod`, `.sum`, `.work`, `Makefile`, `GNUmakefile`, `Dockerfile`,
`Containerfile`, `.gitignore`, `.editorconfig`, and `.gitattributes`. The source
extensions are matched case-insensitively. Query terms use the same Unicode,
underscore, and lower-to-upper camel-case tokenization as the current retrieval
package. Ripgrep receives the unique tokens in sorted order as fixed-string,
case-insensitive alternatives. Matching lines rank by distinct query-token
count descending, total token occurrences descending, repository-relative path
ascending, then line ascending. The report includes the exact command template,
globs, tokenizer description, and per-question tokens.

If `--gopls` is supplied, it must report the repository-pinned gopls `v0.21.0`.
The optional arm runs `workspace/symbol`; initialization and per-question query
time are separate. The provider's result cap and exhaustive-coverage behavior
are not established by this harness, so its results are always marked
`unknown_completeness`, even when each request succeeds. The `rg` arm is a
fixed token-overlap workflow, not a complete native-agent baseline: the timed
package inventory has no query-level relevance score, and no `go doc` or
interactive native-agent workflow is measured. Do not use these component
scores alone to claim advantage over native tools. Without `--gopls`,
gopls is explicitly `unavailable`; the retrieval and ripgrep arms still run.
The harness also runs a bounded `go list -e -json ./...` package inventory in
the exported snapshot, with `GOWORK=off` and `GOTOOLCHAIN=local`. It counts
package objects and package load errors without retaining their path-bearing
diagnostics. Its status, count, latency, output-cap state, and timeout are
reported separately; it has no query-level gold scoring. A completed status
covers this command's root `./...` pattern, not nested modules or every possible
build-tag configuration.

## Scoring and limits

Candidates are source anchors. Agentic Go returns a Go declaration name
coordinate, gopls returns a workspace-symbol coordinate, and ripgrep returns a
matching source line. A candidate is relevant when its path and line fall
inside a reviewed gold span. Gold spans for declarations must include the
declaration-name line because the candidate is anchored there; a span that
starts inside a declaration body would unfairly mark that declaration missed.
Recall counts gold spans found at least once. Precision uses a fixed
denominator of 5 or 10, so missing result slots count as non-relevant. MRR is
reciprocal rank of the first relevant anchor, capped at the same cutoff. The
report includes per-query metrics, aggregate macro and micro values, and missed
spans grouped by evidence type at both cutoffs.

File and byte coverage distinguishes all regular committed files, supported
source files expected from the Git tree, extracted Go files and supported
text, rejected binary or non-UTF-8 source, symlinks, and ignored archive
entries. Extracted blobs are checked against their pinned Git object IDs.
Files omitted by `export-ignore` and content changed by `export-subst` are
reported and excluded; such source coverage makes relevance metrics partial.
The existing retrieval kernel indexes
Go declarations only; `text_indexed_by_retrieval` is therefore zero. Heap
statistics are sampled Go runtime heap values; they are not process RSS and do
not include the separate gopls process.

When text ablation is enabled, its candidate index is reported independently
from `text_indexed_by_retrieval`, which continues to describe the live Go-only
path. Candidate-pool recall measures whether each reviewed gold span appears
anywhere among the query-positive candidates before the top-k cutoff; top-5
and top-10 ranking remain separate metrics. This ablation tests text candidate
coverage on the pinned corpus. It is not a native-agent comparison or a
product-value result.

The exporter has explicit limits for total eligible-source bytes and bytes per
file. Ripgrep also has output-byte, matching-line, and per-command time limits.
If one of those bounds truncates an arm, the report marks that arm partial and
does not count its incomplete query in complete-query aggregates. These are
screening limits, not support promises. No database, result cache, persistence,
or branch-selection behavior is introduced by this harness.
