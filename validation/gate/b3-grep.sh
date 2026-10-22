#!/usr/bin/env bash
# b3-grep.sh: frozen B3 grep baseline, spec in validation/gate/README.md ("B3 grep gate").
# Naive text rules over `git diff -U0`; do not make it smarter. Usage: b3-grep.sh <base-ref>
set -euo pipefail

[[ $# -eq 1 ]] || { echo "usage: b3-grep.sh <base-ref>" >&2; exit 2; }
base=$1
patch=$(git diff -U0 --no-color "$base") || { echo "b3: git diff $base failed" >&2; exit 2; }
untracked=$(git ls-files --others --exclude-standard) || { echo "b3: git ls-files failed" >&2; exit 2; }

# Emits "<file><TAB><+ or - line>" for each changed line inside a hunk; ---/+++ headers sit outside hunks.
parse() {
  local file= line in_hunk=0
  while IFS= read -r line; do
    case $line in
      "diff --git "*) file=${line##* b/}; in_hunk=0 ;;
      "@@ "*) in_hunk=1 ;;
      [+-]*) if ((in_hunk)); then printf '%s\t%s\n' "$file" "$line"; fi ;;
    esac
  done
}

# Untracked files: every line counts as added.
added_untracked() {
  local f t
  while IFS= read -r f; do
    if [[ -n $f ]]; then
      while IFS= read -r t || [[ -n $t ]]; do printf '%s\t+%s\n' "$f" "$t"; done < "$f"
    fi
  done <<< "$untracked"
}

rows=$({ parse <<< "$patch"; added_untracked; })
re_func='^-func (Test|Fuzz)'
re_skip='\.Skip(Now|f)?\('
re_panic='panic\("(not implemented|unimplemented|TODO)'
re_assert='t\.(Error|Fatal)|assert\.|require\.'
hits=0 removed=0 added=0
hit() { printf 'b3: %s: %s: %s\n' "$1" "$2" "$3"; hits=$((hits + 1)); }

while IFS=$'\t' read -r file line; do
  if [[ $line == -* && $line =~ $re_func ]]; then hit removed-test-func "$file" "$line"; fi
  if [[ $line == +* && $line =~ $re_skip ]]; then hit added-skip "$file" "$line"; fi
  if [[ $line == +* && $line =~ $re_panic ]]; then hit added-panic-stub "$file" "$line"; fi
  if [[ $file == *_test.go ]]; then
    if [[ $line == '+//go:build'* ]]; then hit added-test-build-tag "$file" "$line"; fi
    if [[ $line =~ $re_assert && $line == -* ]]; then removed=$((removed + 1)); fi
    if [[ $line =~ $re_assert && $line == +* ]]; then added=$((added + 1)); fi
  fi
done <<< "$rows"

if ((removed > added)); then
  while IFS=$'\t' read -r file line; do
    if [[ $file == *_test.go && $line == -* && $line =~ $re_assert ]]; then hit test-assertion-loss "$file" "$line"; fi
  done <<< "$rows"
fi

if ((hits)); then exit 1; fi
echo "b3: clean"
