#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 7 ]; then
  echo 'usage: action-verify.sh BASE PACKAGE RACE FAIL_ON MIN_CHANGED_COVERAGE MAX_PACKAGES ENFORCE' >&2
  exit 2
fi

base="$1"
package_pattern="$2"
race="$3"
fail_on="$4"
minimum_coverage="$5"
max_packages="$6"
enforce="$7"
case "$race" in true|false) ;; *) echo 'agentic-go: race must be true or false' >&2; exit 2 ;; esac
case "$enforce" in true|false) ;; *) echo 'agentic-go: enforce must be true or false' >&2; exit 2 ;; esac

report_dir="$(mktemp -d "$RUNNER_TEMP/agentic-go-report.XXXXXX")"
report="$report_dir/report.json"
args=(verify --base "$base" --package "$package_pattern" --format json --fail-on "$fail_on" --max-packages "$max_packages")
[ "$race" = true ] && args+=(--race)
[ -n "$minimum_coverage" ] && args+=(--min-changed-coverage "$minimum_coverage")

set +e
agentic-go "${args[@]}" >"$report"
exit_code=$?
set -e
node "$GITHUB_ACTION_PATH/scripts/action-report.mjs" "$report" "$exit_code" "$enforce"
