# gateeval

Command-line driver for the gate evaluation defined in
[`validation/gate/README.md`](../../gate/README.md). It mines commits, builds
the flawed variants, runs the arms, and scores the runs. The protocol is
normative; this file only lists the commands.

Every step is resumable: rerun the same command after an interruption and it
continues from the files already written.

## Environment

```sh
export PATH=$(go env GOROOT)/bin:$PATH      # Go 1.25.0 on PATH
export GOTOOLCHAIN=local GOFLAGS=-mod=mod   # the commands set these themselves; shown for manual checks
```

`golangci-lint` 2.5.0 must be on `PATH` for the B1, B2, and B2s arms. The module
proxy must be reachable so dependencies download.

## Runbook

Run from the repository root.

```sh
W=/var/tmp/gateeval              # clones/ and work/ live here
R=validation/gate/results        # JSONL results
mkdir -p "$R"

go build -o "$W/bin/gateeval" ./validation/cmd/gateeval
go build -o "$W/bin/agentic-go" ./cmd/agentic-go

# 1. Select commits (clones each corpus repository into $W/clones/<project>).
"$W/bin/gateeval" select \
  --corpus validation/gate/corpus.csv --work "$W" \
  --out "$R/selections.jsonl" --exclusions "$R/select-exclusions.jsonl"

# 2. Generate variants (one branch gateeval/<id> per variant in each clone).
"$W/bin/gateeval" generate \
  --work "$W" --selected "$R/selections.jsonl" \
  --out "$R/variants.jsonl" --exclusions "$R/generate-exclusions.jsonl"

# 3. Run the arms. --timing adds the second run of every arm on true patches.
"$W/bin/gateeval" run \
  --work "$W" --variants "$R/variants.jsonl" --out "$R/runs.jsonl" \
  --gate-bin "$W/bin/agentic-go" --workers 2 --timing

# 4. Score.
"$W/bin/gateeval" summarize \
  --variants "$R/variants.jsonl" --runs "$R/runs.jsonl" \
  --exclusions "$R/select-exclusions.jsonl,$R/generate-exclusions.jsonl" \
  --out "$R/summary"
```

## Commands and flags

| Command | Flags (defaults) |
| --- | --- |
| `select` | `--corpus` (`validation/gate/corpus.csv`), `--work`, `--out`, `--exclusions`, `--per-repo-true` (30), `--per-repo-flaw` (10), `--per-repo-destructive` (20), `--max-scan` (3000), `--test-timeout` (120s) |
| `generate` | `--work`, `--selected`, `--out`, `--exclusions`, `--max-mutant-attempts` (20), `--test-timeout` (120s) |
| `run` | `--work`, `--variants`, `--out`, `--arms` (`B0,B1,B2,B2s,B3,Gci,Ghook`), `--gate-bin`, `--b3` (`validation/gate/b3-grep.sh`), `--workers` (2), `--command-timeout` (10m), `--timing` |
| `summarize` | `--variants`, `--runs`, `--exclusions` (comma-separated files), `--out` (directory for `summary.json` and `summary.md`) |

Exit status: 0 on success, 1 on a runtime failure, 2 on a usage error.

## Files

- `selections.jsonl`: one line per mined commit and the strata it belongs to.
- `select-exclusions.jsonl`: commits that failed the build or test filters, plus
  one `static: N scanned, M kept` line per project and stratum.
- `variants.jsonl`: one line per variant; the tree is the branch named in the line.
- `generate-exclusions.jsonl`: variants that could not be built, with the reason.
- `runs.jsonl`: one line per arm execution.
