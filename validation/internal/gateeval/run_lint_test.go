package gateeval

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseLintIssues(t *testing.T) {
	text := "cmd/x.go:6:11: Error return value of `os.Remove` is not checked (errcheck)\n\tos.Remove(\"x\")\n\t         ^\n" +
		"./y_test.go:3:1: ST1000: at least one file in a package should have a package comment (staticcheck)\n" +
		"1 issues:\n* errcheck: 1\n"
	want := []lintIssue{
		{File: "cmd/x.go", Linter: "errcheck", Message: "Error return value of `os.Remove` is not checked"},
		{File: "y_test.go", Linter: "staticcheck", Message: "ST1000: at least one file in a package should have a package comment"},
	}
	if got := parseLintIssues([]byte(text)); !reflect.DeepEqual(got, want) {
		t.Errorf("text: parseLintIssues() = %+v, want %+v", got, want)
	}
	report := `{"Issues":[{"FromLinter":"errcheck","Text":"Error return value of ` + "`os.Remove`" + ` is not checked","Pos":{"Filename":"cmd/x.go","Line":6}},` +
		`{"FromLinter":"staticcheck","Text":"ST1000: at least one file in a package should have a package comment","Pos":{"Filename":"./y_test.go","Line":3}}],"Report":{}}`
	if got := parseLintIssues([]byte(report)); !reflect.DeepEqual(got, want) {
		t.Errorf("json: parseLintIssues() = %+v, want %+v", got, want)
	}
	if got := parseLintIssues([]byte("0 issues.\n")); len(got) != 0 {
		t.Errorf("clean output gave %+v", got)
	}
}

func TestNewLintIssuesComparesMultisetsWithoutLineNumbers(t *testing.T) {
	a := lintIssue{File: "a.go", Linter: "errcheck", Message: "unchecked"}
	b := lintIssue{File: "b.go", Linter: "errcheck", Message: "unchecked"}
	other := lintIssue{File: "a.go", Linter: "revive", Message: "unchecked"}
	tests := []struct {
		name      string
		current   []lintIssue
		reference []lintIssue
		want      []lintIssue
	}{
		{name: "identical", current: []lintIssue{a}, reference: []lintIssue{a}},
		{name: "fewer than reference", current: []lintIssue{a}, reference: []lintIssue{a, b}},
		{name: "new file", current: []lintIssue{a, b}, reference: []lintIssue{a}, want: []lintIssue{b}},
		{name: "same text from another linter", current: []lintIssue{a, other}, reference: []lintIssue{a}, want: []lintIssue{other}},
		{name: "duplicate beyond the reference count", current: []lintIssue{a, a}, reference: []lintIssue{a}, want: []lintIssue{a}},
		{name: "empty reference", current: []lintIssue{a}, want: []lintIssue{a}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newLintIssues(tt.current, tt.reference); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("newLintIssues() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// markerLint is a golangci-lint stand-in that reports one issue per source line
// containing LINTME, with the line number in the output, and logs where it ran.
func markerLint(t *testing.T, log string) {
	t.Helper()
	fakeLint(t, "echo \"$PWD\" >> '"+log+"'\n"+
		"out=$(grep -Hn LINTME ./*.go 2>/dev/null | sed -E 's|^\\./||; s|^([^:]+):([0-9]+):.*|\\1:\\2:1: marker found (fakelint)|')\n"+
		"if [ -n \"$out\" ]; then echo \"$out\"; exit 1; fi\nexit 0\n")
}

// addVariantOn commits files on a new branch off parent and registers a
// variant whose unmodified commit is the parent branch.
func (f *fixture) addVariantOn(t *testing.T, parent, id string, class Class, files map[string]string) {
	t.Helper()
	runGit(t, f.clone, "checkout", "-q", "-b", id, parent)
	for name, content := range files {
		writeFile(t, f.clone, name, content)
	}
	runGit(t, f.clone, "add", "-A")
	runGit(t, f.clone, "commit", "-q", "-m", id)
	runGit(t, f.clone, "checkout", "-q", "main")
	f.variants = append(f.variants, Variant{ID: id, Project: "proj", Commit: parent, Base: f.base, Class: class, Branch: id, Effective: true})
}

func lintFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t, false)
	// The true commit carries a lint issue in its own, human-written code.
	f.addVariant(t, "true", ClassTrue, map[string]string{"a.go": fixtureCode + "\n// LINTME\n"})
	f.variants[0].Commit = "true"
	log := filepath.Join(t.TempDir(), "lint-calls")
	markerLint(t, log)
	return f, log
}

func TestRunArmsB2StarIgnoresLintIssuesOfTheUnmodifiedCommit(t *testing.T) {
	f, log := lintFixture(t)
	shifted := "package proj\n\n// padding\n// padding\n// padding\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n\n// LINTME\n"
	f.addVariantOn(t, "true", "shifted", ClassStub, map[string]string{"a.go": shifted})
	f.addVariantOn(t, "true", "newfile", ClassStub, map[string]string{"b.go": "package proj\n\n// LINTME\n"})
	f.addVariantOn(t, "true", "twice", ClassStub, map[string]string{"a.go": fixtureCode + "\n// LINTME\n// LINTME\n"})
	grep := filepath.Join(t.TempDir(), "b3.sh")
	writeScript(t, grep, "exit 0\n")
	options := baseOptions(f, ArmB1, ArmB2, ArmB2Star, ArmB3)
	options.B3Script = grep
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)

	wants := []struct {
		variant string
		arm     Arm
		blocked bool
	}{
		// Raw lint arms keep blocking on the human-written issue.
		{"true", ArmB1, true},
		{"true", ArmB2, true},
		{"shifted", ArmB1, true},
		{"shifted", ArmB2, true},
		// B2* and B3 ignore it, even when the edit shifted its line.
		{"true", ArmB2Star, false},
		{"true", ArmB3, false},
		{"shifted", ArmB2Star, false},
		{"shifted", ArmB3, false},
		// A new issue, in a new file or beyond the reference count, still blocks.
		{"newfile", ArmB2Star, true},
		{"newfile", ArmB3, true},
		{"twice", ArmB2Star, true},
		{"twice", ArmB3, true},
	}
	for _, w := range wants {
		run := findRun(t, runs, w.variant, w.arm, 1)
		if run.Blocked != w.blocked || run.Unknown {
			t.Errorf("%s on %s = blocked %v unknown %v (reason %q, error %q), want blocked %v", w.arm, w.variant, run.Blocked, run.Unknown, run.Reason, run.Error, w.blocked)
		}
	}
	if reason := findRun(t, runs, "newfile", ArmB2Star, 1).Reason; !strings.Contains(reason, "1 new lint issue") || !strings.Contains(reason, "b.go") {
		t.Errorf("B2s reason on newfile = %q", reason)
	}
	if note := findRun(t, runs, "shifted", ArmB2Star, 1).Error; !strings.Contains(note, "also reported on the unmodified commit") {
		t.Errorf("B2s note on shifted = %q", note)
	}

	// The reference is linted once per commit and cached.
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(calls), "proj-ref"); got != 1 {
		t.Errorf("reference lint ran %d times for 4 variants of one commit, want 1:\n%s", got, calls)
	}
	cached, err := os.ReadFile(filepath.Join(f.workDir, "lintref", "proj-true.json"))
	if err != nil {
		t.Fatalf("reference cache: %v", err)
	}
	if !strings.Contains(string(cached), `"version":1`) || !strings.Contains(string(cached), `"linter":"fakelint"`) || strings.Contains(string(cached), `"line"`) {
		t.Errorf("reference cache = %s, want versioned issues without line numbers", cached)
	}

	// A second call reuses the cache (no run was missing, so nothing runs; delete
	// the runs to force the arms again and confirm the reference is not re-linted).
	if err = os.Remove(options.Out); err != nil {
		t.Fatal(err)
	}
	if err = RunArms(context.Background(), options); err != nil {
		t.Fatalf("second RunArms() error = %v", err)
	}
	calls, err = os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(calls), "proj-ref"); got != 1 {
		t.Errorf("reference lint ran %d times across two calls, want 1 (cache reused)", got)
	}
}

func TestRunArmsB2StarFallsBackToRawLintWhenReferenceFails(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "true", ClassTrue, map[string]string{"a.go": fixtureCode + "\n// LINTME\n"})
	f.variants[0].Commit = "true"
	// Lint works in variant worktrees and errors in the reference worktree.
	fakeLint(t, "case \"$PWD\" in *-ref) echo 'config error' >&2; exit 3;; esac\n"+
		"if grep -q LINTME a.go; then echo 'a.go:9:1: marker found (fakelint)'; exit 1; fi\nexit 0\n")
	options := baseOptions(f, ArmB2Star)
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	run := findRun(t, readRuns(t, options.Out), "true", ArmB2Star, 1)
	if !run.Blocked || run.Unknown || !strings.Contains(run.Error, "reference lint unavailable") {
		t.Errorf("B2s = %+v, want raw lint to block, with a note about the reference", run)
	}
}
