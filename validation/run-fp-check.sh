#!/usr/bin/env bash
set -euo pipefail

corpus_root=${1:?usage: run-fp-check.sh CORPUS_ROOT [GO_BINARY]}
go_binary=${2:-go}
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
manifest=${MANIFEST:-"$repo_root/validation/v0.1.0/corpus.csv"}
output=${OUTPUT_DIR:-"$repo_root/validation/v0.1.0"}

cd "$repo_root"
args=(run -mod=readonly ./validation/cmd/fpcheck -corpus-root "$corpus_root" -manifest "$manifest" -output "$output" -go "$go_binary")
if [[ -n "${AGENTIC_GO_VET:-}" ]]; then args+=(-vettool "$AGENTIC_GO_VET"); fi
exec "$go_binary" "${args[@]}"
