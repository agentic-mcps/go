#!/usr/bin/env bash
# b3-grep.test.sh: exercises b3-grep.sh rule by rule in throwaway git repos.
# Each case: fresh repo with a baseline commit on main, one change, then exit code and output checked.
set -uo pipefail

script=$(cd "$(dirname "$0")" && pwd)/b3-grep.sh
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
total=0 fails=0

new_repo() { # new_repo <name>: pkg/add.go and pkg/add_test.go committed on main, cwd moves into it
  local dir="$root/$1"
  mkdir -p "$dir/pkg" && cd "$dir" || exit 1
  git init -q -b main
  git config user.name "B3 Test" && git config user.email "b3-test@example.com"
  printf 'package pkg\n\n// see t.Errorf in add_test.go\nfunc Add(a, b int) int {\n\treturn a + b\n}\n' > pkg/add.go
  cat > pkg/add_test.go <<'EOF'
package pkg

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(1, 2); got != 3 {
		t.Errorf("Add(1, 2) = %d, want 3", got)
	}
}
EOF
  git add . && git commit -q -m baseline
}

insert_after() { # insert_after <file> <regex> <line>: adds <line> after every line matching <regex>
  awk -v re="$2" -v txt="$3" '{ print } $0 ~ re { print txt }' "$1" > "$1.new" && mv "$1.new" "$1"
}

prepend() { # prepend <file> <line>: adds <line> and a blank line at the top of <file>
  { printf '%s\n\n' "$2"; cat "$1"; } > "$1.new" && mv "$1.new" "$1"
}

check() { # check <case> <want-exit> <want-substring> [base-ref]: runs the script, asserts exit code and output
  local out rc=0
  total=$((total + 1))
  out=$("$script" "${4:-main}" 2>&1) || rc=$?
  if [[ $rc -eq $2 && $out == *"$3"* ]]; then
    printf 'ok    %-36s exit %s\n' "$1" "$rc"
  else
    printf 'FAIL  %-36s exit %s, want %s containing "%s"\n' "$1" "$rc" "$2" "$3"
    fails=$((fails + 1))
  fi
  printf '%s\n' "$out" | sed 's/^/        | /'
}

want_exit() { # want_exit <case> <want-exit> <got-exit>
  total=$((total + 1))
  if [[ $3 -eq $2 ]]; then echo "ok    $1, exit $3"; else echo "FAIL  $1, exit $3 (want $2)"; fails=$((fails + 1)); fi
}

echo "== rule 1: removed-test-func (renames and moves are flagged on purpose)"
new_repo r1-rename; sed -i 's/func TestAdd/func TestSum/' pkg/add_test.go
check "rename test func (naive)" 1 "removed-test-func"
new_repo r1-move; cp pkg/add_test.go pkg/moved_test.go; printf 'package pkg\n' > pkg/add_test.go
check "move test to new file (naive)" 1 "removed-test-func"
new_repo r1-delete; rm pkg/add_test.go
check "delete test file" 1 "removed-test-func: pkg/add_test.go"
new_repo r1-new-func; printf '\nfunc TestSub(t *testing.T) {\n\tt.Errorf("unused")\n}\n' >> pkg/add_test.go
check "near: add new test func" 0 "b3: clean"

echo "== rule 2: added-skip (matches \\.Skip(Now|f)?\\( anywhere in an added line)"
new_repo r2-skip; insert_after pkg/add_test.go 'func TestAdd' $'\tt.Skip("flaky")'
check "t.Skip(" 1 "added-skip: pkg/add_test.go"
new_repo r2-skipnow; insert_after pkg/add_test.go 'func TestAdd' $'\tt.SkipNow()'
check "t.SkipNow(" 1 "added-skip"
new_repo r2-skipf; insert_after pkg/add_test.go 'func TestAdd' $'\tt.Skipf("flaky %d", 1)'
check "t.Skipf(" 1 "added-skip"
new_repo r2-comment; insert_after pkg/add.go '^package pkg' '// see t.Skip() in the tests'
check "comment containing t.Skip( (naive)" 1 "added-skip: pkg/add.go"
new_repo r2-no-dot; insert_after pkg/add_test.go 'func TestAdd' $'\t// Skip("flaky") is only a comment'
check "near: // Skip( has no dot" 0 "b3: clean"
new_repo r2-skipped; insert_after pkg/add_test.go 'func TestAdd' $'\t_ = t.Skipped()'
check "near: t.Skipped()" 0 "b3: clean"
new_repo r2-remove; insert_after pkg/add_test.go 'func TestAdd' $'\tt.Skip("flaky")'; git commit -q -am "base with skip"
sed -i '/t\.Skip(/d' pkg/add_test.go
check "near: removing a skip" 0 "b3: clean" HEAD
new_repo r2-untracked; printf 'package pkg\n\nimport "testing"\n\nfunc TestLater(t *testing.T) {\n\tt.Skip("later")\n}\n' > pkg/later_test.go
check "untracked file with t.Skip(" 1 "added-skip: pkg/later_test.go"

echo "== rule 3: added-panic-stub"
new_repo r3-not-impl; sed -i 's/return a + b/panic("not implemented")/' pkg/add.go
check 'panic("not implemented")' 1 "added-panic-stub"
new_repo r3-unimpl; sed -i 's/return a + b/panic("unimplemented")/' pkg/add.go
check 'panic("unimplemented")' 1 "added-panic-stub"
new_repo r3-todo; sed -i 's/return a + b/panic("TODO: write it")/' pkg/add.go
check 'panic("TODO: ...")' 1 "added-panic-stub"
new_repo r3-comment; insert_after pkg/add.go '^package pkg' '// TODO: handle overflow'
check "near: TODO comment, no panic" 0 "b3: clean"
new_repo r3-remove; sed -i 's/return a + b/panic("not implemented")/' pkg/add.go; git commit -q -am "base with stub"
sed -i 's/panic("not implemented")/return a + b/' pkg/add.go
check "near: removing a panic stub" 0 "b3: clean" HEAD

echo "== rule 4: test-assertion-loss (test files only, net across all test files)"
new_repo r4-loss; sed -i '/t\.Errorf/d' pkg/add_test.go
check "t.Errorf deleted" 1 "test-assertion-loss: pkg/add_test.go"
new_repo r4-swap; sed -i 's/t\.Errorf/t.Fatalf/' pkg/add_test.go
check "near: Errorf -> Fatalf" 0 "b3: clean"
new_repo r4-move; sed -i '/t\.Errorf/d' pkg/add_test.go
printf 'package pkg\n\nimport "testing"\n\nfunc TestExtra(t *testing.T) {\n\tt.Error("x")\n}\n' > pkg/extra_test.go
check "near: moved to another test file" 0 "b3: clean"
new_repo r4-non-test; sed -i '/see t\.Errorf/d' pkg/add.go
check "near: assertion-like line in non-test" 0 "b3: clean"
new_repo r4-other-line; sed -i '/if got := /d' pkg/add_test.go
check "near: non-assertion line removed" 0 "b3: clean"
new_repo r4-header; printf 'package pkg\n\nimport "testing"\n\nfunc TestRequire(t *testing.T) {}\n' > pkg/require.x_test.go
git add pkg/require.x_test.go; sed -i '/t\.Errorf/d' pkg/add_test.go
check "loss; new file header not counted" 1 "test-assertion-loss: pkg/add_test.go"

echo "== rule 5: added-test-build-tag"
new_repo r5-tag; prepend pkg/add_test.go '//go:build integration'
check "//go:build in _test.go" 1 "added-test-build-tag: pkg/add_test.go"
new_repo r5-spaced; prepend pkg/add_test.go '// go:build integration'
check "near: // go:build (with space)" 0 "b3: clean"
new_repo r5-nontest; prepend pkg/add.go '//go:build linux'
check "near: build tag in non-test file" 0 "b3: clean"

echo "== clean change and error paths"
new_repo clean; sed -i 's/return a + b/return b + a/' pkg/add.go; printf 'package pkg\n' > pkg/doc.go
check "clean change" 0 "b3: clean"
new_repo errors
rc=0; "$script" >/dev/null 2>&1 || rc=$?; want_exit "no base-ref argument" 2 "$rc"
rc=0; "$script" no-such-ref >/dev/null 2>&1 || rc=$?; want_exit "unknown base-ref" 2 "$rc"

echo "----"
if ((fails)); then echo "FAILED: $fails of $total cases"; exit 1; fi
echo "PASSED: all $total cases"
