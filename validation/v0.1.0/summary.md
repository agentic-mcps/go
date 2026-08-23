# v0.1.0 analyzer validation

This directory records the local, offline calibration run for the v0.1.0
concurrency and error analyzers. The corpus consists of ten user-cloned Go
repositories pinned by full commit SHA in [`corpus.csv`](corpus.csv). The scan
used the analyzer and validation harness at repository HEAD
`0e60cd3439a88d93e5bcb24cdd20a0aa1d280db7`, and Go 1.26.7 with
`GOTOOLCHAIN=local`, `GOPROXY=off`, and `GOSUMDB=off`.
Go 1.26.7 was selected because every pinned module declares Go 1.26 or older;
the pinned `client_golang` checkout has test fixture symbols that do not build
under Go 1.27.

## Outcome

The final complete scan produced 467 unique findings. Every row in
[`results.csv`](results.csv) was reviewed against its pinned source and
classified as a true positive. The observed aggregate false-positive rate is
0%, and every active rule with an external hit is individually at 0%.

| Metric | Value |
| --- | ---: |
| Repositories | 10 |
| Go LOC scanned | 1,804,320 |
| Reviewed findings | 467 |
| Findings per 1,000 Go LOC | 0.259 |
| False positives | 0 |
| Observed false-positive rate | 0% |

LOC counts include Go test files and exclude `vendor` directories. They are a
size denominator, not a claim that every line is executable or independently
analyzed.

## Results by rule

| Rule | Findings | False positives | Status |
| --- | ---: | ---: | --- |
| `concurrency-04` | 93 | 0 | externally validated |
| `concurrency-08` | 9 | 0 | externally validated |
| `concurrency-10` | 118 | 0 | externally validated |
| `concurrency-18` | 8 | 0 | externally validated |
| `errors-04` | 232 | 0 | externally validated |
| `errors-19` | 7 | 0 | externally validated |
| `concurrency-05` | 0 | — | fixture-tested; no external hit |
| `concurrency-09` | 0 | — | fixture-tested; no external hit |
| `concurrency-12` | 0 | — | fixture-tested; no external hit |
| `concurrency-19` | 0 | — | fixture-tested; no external hit |
| `errors-09` | 0 | — | fixture-tested; no external hit |

Rules without a meaningful external hit are intentionally not described as
externally validated.

## Results by project

| Project | Go LOC | Findings | Findings / 1k LOC |
| --- | ---: | ---: | ---: |
| cobra | 16,765 | 2 | 0.119 |
| gin | 24,099 | 1 | 0.041 |
| echo | 41,432 | 2 | 0.048 |
| testify | 29,694 | 3 | 0.101 |
| chi | 12,082 | 0 | 0.000 |
| grpc-go | 338,320 | 37 | 0.109 |
| client_golang | 44,401 | 1 | 0.023 |
| etcd | 218,937 | 0 | 0.000 |
| vault | 734,445 | 383 | 0.522 |
| client-go | 344,145 | 38 | 0.110 |

## Calibration decisions

The first complete scan produced 1,057 findings. Findings were reviewed by
rule across all projects, including test and near-miss shapes. Rules were
disabled when their observed false-positive rate exceeded 5% or when review
found a repeatable systemic false-positive mechanism. Retained rules were
narrowed and the entire corpus was rerun after each consequential change.

Notable calibrated-off predicates include:

- receiver method names as proof of goroutine lifecycle ownership
  (`concurrency-02`);
- multiple atomics as proof of one compound invariant (`concurrency-15`);
- multiple lock calls as proof of simultaneous lock ownership
  (`concurrency-17`);
- every unscoped loop defer as harmful accumulation (`concurrency-20`);
- non-final error return positions as correctness failures (`errors-01`);
- multiple `%w` verbs as invalid wrapping (`errors-13`);
- every error-valued field as a transparent wrapper contract (`errors-14`);
- every string-based retry check as replaceable by a typed error
  (`errors-17`).

Other rules had already been disabled during the same external-calibration
stage for similarly repeatable patterns; their exact reasons are exposed as
disabled-rule metadata by `agentic-go://analysis-rules`.

Retained predicates were narrowed to exclude test-only noise, intentional lock
wrappers, field-owned ticker lifecycles, consumed one-shot timers, terminal
command packages, and fatal-logger implementations where the corpus showed
those shapes were not defects.

## Reproduction and evidence

Run from the repository root with the ten pinned checkouts present:

```sh
validation/run-fp-check.sh \
  "$VALIDATION_ROOT" \
  /path/to/go1.26.7/bin/go
```

The runner verifies every checkout SHA, builds `agentic-go-vet`, runs with the
network disabled, rejects analyzer-error JSON, de-duplicates package/test
variants, caps output, and sanitizes local checkout and temporary binary paths.
Local checkout paths are not stored in captured raw reports.

`findings/` contains one path-sanitized raw report per project. `results.csv`
contains all 467 reviewed findings. Its SHA-256 digest is:

```text
618f1960725d52655e37f1ce5d8291a7d7df5630238c84a8dc0714a98bb9435c
```
