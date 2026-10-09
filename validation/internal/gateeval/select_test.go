package gateeval

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestStaticMain(t *testing.T) {
	never := func(string) bool { return false }
	genAll := func(string) bool { return true }
	mod := func(p string) change { return change{Status: "M", Path: p, Added: 3, Deleted: 1} }
	tests := []struct { //nolint:govet // Table layout favors readability.
		name       string
		changes    []change
		generated  func(string) bool
		wantOK     bool
		wantDirect []string
	}{
		{
			name:    "code and test in one directory qualify",
			changes: []change{mod("a/x.go"), mod("a/x_test.go")}, generated: never,
			wantOK: true, wantDirect: []string{"a"},
		},
		{
			name:    "an added test file counts",
			changes: []change{mod("a/x.go"), {Status: "A", Path: "a/y_test.go", Added: 9}}, generated: never,
			wantOK: true, wantDirect: []string{"a"},
		},
		{
			name:    "code and test in different directories do not qualify",
			changes: []change{mod("a/x.go"), mod("b/x_test.go")}, generated: never,
			wantDirect: []string{"a"},
		},
		{
			name:    "only an added non-test file does not qualify",
			changes: []change{{Status: "A", Path: "a/x.go", Added: 3}, mod("a/x_test.go")}, generated: never,
		},
		{
			name:    "generated code does not qualify",
			changes: []change{mod("a/x.go"), mod("a/x_test.go")}, generated: genAll,
			wantDirect: []string{"a"},
		},
		{
			name:    "vendor and testdata do not qualify",
			changes: []change{mod("vendor/x/x.go"), mod("vendor/x/x_test.go"), mod("a/testdata/y.go"), mod("a/y_test.go")}, generated: never,
		},
		{
			name:    "go.mod change disqualifies",
			changes: []change{mod("a/x.go"), mod("a/x_test.go"), mod("go.mod")}, generated: never,
			wantDirect: []string{"a"},
		},
		{
			name:    "nested go.sum change disqualifies",
			changes: []change{mod("a/x.go"), mod("a/x_test.go"), mod("tools/go.sum")}, generated: never,
			wantDirect: []string{"a"},
		},
		{
			name:    "401 changed lines disqualify",
			changes: []change{{Status: "M", Path: "a/x.go", Added: 200, Deleted: 100}, {Status: "M", Path: "a/x_test.go", Added: 101}}, generated: never,
			wantDirect: []string{"a"},
		},
		{
			name:    "400 changed lines qualify",
			changes: []change{{Status: "M", Path: "a/x.go", Added: 200, Deleted: 100}, {Status: "M", Path: "a/x_test.go", Added: 100}}, generated: never,
			wantOK: true, wantDirect: []string{"a"},
		},
		{
			name:    "direct directories are sorted and unique",
			changes: []change{mod("z/a.go"), mod("b/a.go"), mod("b/b.go"), mod("b/a_test.go")}, generated: never,
			wantOK: true, wantDirect: []string{"b", "z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			direct, _, ok := staticMain(tt.changes, tt.generated)
			if ok != tt.wantOK {
				t.Errorf("qualifies = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(direct, tt.wantDirect) {
				t.Errorf("direct = %v, want %v", direct, tt.wantDirect)
			}
		})
	}
}

func TestDestructiveSignals(t *testing.T) {
	diff := func(removed, added []string) []fileDiff {
		return []fileDiff{{Path: "a/x_test.go", Removed: removed, Added: added}}
	}
	tests := []struct { //nolint:govet // Table layout favors readability.
		name string
		diff []fileDiff
		want bool
	}{
		{name: "removed test function", diff: diff([]string{"func TestOld(t *testing.T) {"}, nil), want: true},
		{name: "removed fuzz function", diff: diff([]string{"func FuzzOld(f *testing.F) {"}, nil), want: true},
		{name: "added Skip", diff: diff(nil, []string{"\tt.Skip(\"later\")"}), want: true},
		{name: "added SkipNow", diff: diff(nil, []string{"\tt.SkipNow()"}), want: true},
		{name: "added Skipf", diff: diff(nil, []string{"\tt.Skipf(\"%s\", x)"}), want: true},
		{name: "net loss of assertions", diff: diff([]string{"\tassert.Equal(t, 1, 2)", "\trequire.NoError(t, err)"}, []string{"\tassert.Equal(t, 1, 2)"}), want: true},
		{name: "assertions rewritten one for one", diff: diff([]string{"\tt.Errorf(\"a\")"}, []string{"\tt.Errorf(\"b\")"})},
		{name: "assertions added", diff: diff(nil, []string{"\tt.Fatal(\"x\")"})},
		{name: "renamed test is a removal by the naive rule", diff: diff([]string{"func TestA(t *testing.T) {"}, []string{"func TestB(t *testing.T) {"}), want: true},
		{name: "helper function removal is not a test removal", diff: diff([]string{"func helper(t *testing.T) {"}, nil)},
		{name: "added Skipped identifier", diff: diff(nil, []string{"\tskipped := true"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := weakensTests(tt.diff); got != tt.want {
				t.Errorf("weakensTests = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDestructiveCandidateAndTestData(t *testing.T) {
	never := func(string) bool { return false }
	mod := func(p string) change { return change{Status: "M", Path: p, Added: 1} }
	if !destructiveCandidate([]change{mod("a/x.go"), mod("a/testdata/in.txt")}, never) {
		t.Error("a non-test change beside testdata should be a candidate")
	}
	if destructiveCandidate([]change{mod("a/x_test.go"), mod("a/testdata/in.txt")}, never) {
		t.Error("a commit that changes no non-test .go file must not be a candidate")
	}
	if destructiveCandidate([]change{mod("a/x.go"), mod("go.mod")}, never) {
		t.Error("a go.mod change must not be a candidate")
	}
	if destructiveCandidate([]change{mod("a/x.go")}, func(string) bool { return true }) {
		t.Error("generated code alone must not be a candidate")
	}
	for _, p := range []string{"a/testdata/in.txt", "a/render.golden", "testdata/x.go"} {
		if !touchesTestData([]change{mod(p)}) {
			t.Errorf("touchesTestData(%s) = false", p)
		}
	}
	for _, p := range []string{"a/testdata_helpers.go", "a/golden.go", "a/x.golden.go"} {
		if touchesTestData([]change{mod(p)}) {
			t.Errorf("touchesTestData(%s) = true, a near miss", p)
		}
	}
}

func TestIsGeneratedSource(t *testing.T) {
	tests := []struct { //nolint:govet // Table layout favors readability.
		name string
		src  string
		want bool
	}{
		{name: "header", src: "// Code generated by stringer; DO NOT EDIT.\n\npackage x\n", want: true},
		{name: "header after license", src: "// Copyright\n\n// Code generated by x. DO NOT EDIT.\npackage x\n", want: true},
		{name: "header after package clause is ignored", src: "package x\n\n// Code generated by x. DO NOT EDIT.\n"},
		{name: "mention in prose", src: "// This is not Code generated by anything.\npackage x\n"},
		{name: "plain", src: "package x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGeneratedSource([]byte(tt.src)); got != tt.want {
				t.Errorf("isGeneratedSource = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseChanges(t *testing.T) {
	status := []byte("M\x00a/x.go\x00A\x00a/x_test.go\x00D\x00old.go\x00M\x00img.png\x00")
	numstat := []byte("3\t1\ta/x.go\x0010\t0\ta/x_test.go\x000\t5\told.go\x00-\t-\timg.png\x00")
	got := parseChanges(status, numstat)
	want := []change{
		{Status: "M", Path: "a/x.go", Added: 3, Deleted: 1},
		{Status: "A", Path: "a/x_test.go", Added: 10},
		{Status: "D", Path: "old.go", Deleted: 5},
		{Status: "M", Path: "img.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %+v, want %+v", got, want)
	}
	if totalLines(got) != 19 {
		t.Errorf("totalLines = %d, want 19", totalLines(got))
	}
	if len(parseChanges(nil, nil)) != 0 {
		t.Error("an empty diff must give no changes")
	}
}

func TestParseDiff(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -3 +3,2 @@ func f() {
-	old := 1
+	new := 2
+	more := 3
@@ -10,2 +11,0 @@ func g() {
-	gone()
-	gone2()
diff --git a/b_test.go b/b_test.go
new file mode 100644
--- /dev/null
+++ b/b_test.go
@@ -0,0 +1,2 @@
+package a
+-- looks like a header
diff --git a/c.go b/c.go
deleted file mode 100644
--- a/c.go
+++ /dev/null
@@ -1 +0,0 @@
-package a
`
	got := parseDiff([]byte(diff))
	if len(got) != 3 {
		t.Fatalf("files = %d, want 3", len(got))
	}
	if got[0].Path != "a.go" || !reflect.DeepEqual(got[0].Added, []string{"\tnew := 2", "\tmore := 3"}) ||
		!reflect.DeepEqual(got[0].Removed, []string{"\told := 1", "\tgone()", "\tgone2()"}) {
		t.Errorf("a.go = %+v", got[0])
	}
	if !got[0].Lines[3] || !got[0].Lines[4] || got[0].Lines[11] || len(got[0].Lines) != 2 {
		t.Errorf("a.go lines = %v, want {3,4}", got[0].Lines)
	}
	if got[1].Path != "b_test.go" || !got[1].Lines[1] || !got[1].Lines[2] || got[1].Added[1] != "-- looks like a header" {
		t.Errorf("b_test.go = %+v", got[1])
	}
	if got[2].Path != "c.go" || len(got[2].Lines) != 0 || len(got[2].Removed) != 1 {
		t.Errorf("c.go = %+v", got[2])
	}
}

func TestRankIsStable(t *testing.T) {
	a, b := rankOf("p", "abc"), rankOf("p", "abd")
	if a == b || a != rankOf("p", "abc") || len(a) != 64 {
		t.Fatalf("rank not a stable sha256: %s %s", a, b)
	}
	if rankOf("q", "abc") == a {
		t.Error("rank must depend on the project")
	}
}

func TestBuildSelectionsFlagsAndOrder(t *testing.T) {
	mk := func(sha string) commitInfo {
		return commitInfo{SHA: sha, Base: sha + "^", Rank: rankOf("p", sha), Direct: []string{"a", "."}, Lines: 4}
	}
	a, b, c := mk("aaa"), mk("bbb"), mk("ccc")
	got := buildSelections("p", []commitInfo{a, b, c}, []commitInfo{c}, 2)
	if len(got) != 3 {
		t.Fatalf("selections = %d, want one per commit", len(got))
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Rank < got[j].Rank }) {
		t.Error("selections must be in rank order")
	}
	byCommit := map[string]Selection{}
	for _, s := range got {
		byCommit[s.Commit] = s
	}
	if s := byCommit["aaa"]; !s.TruePatch || !s.FlawSeed || s.Destructive {
		t.Errorf("aaa = %+v", s)
	}
	if s := byCommit["bbb"]; !s.TruePatch || !s.FlawSeed || s.Destructive {
		t.Errorf("bbb = %+v", s)
	}
	if s := byCommit["ccc"]; !s.TruePatch || s.FlawSeed || !s.Destructive {
		t.Errorf("ccc = %+v, want true patch beyond the seed quota and destructive", s)
	}
	if !reflect.DeepEqual(byCommit["aaa"].DirectPackages, []string{"./a", "."}) {
		t.Errorf("direct = %v", byCommit["aaa"].DirectPackages)
	}
}

// selectFixture is a project whose history exercises each filter. Commits are
// in order; the pinned commit is the last.
type selectFixture struct {
	commits map[string]string
	dir     string
}

const selectCalc = `package proj

// Abs returns the absolute value of n.
func Abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
`

const selectTestBase = `package proj

import "testing"

func TestAbs(t *testing.T) {
	if Abs(-3) != 3 {
		t.Fatal("abs")
	}
}
`

func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", message)
	return runGit(t, dir, "rev-parse", "HEAD")
}

func newSelectFixture(t *testing.T) selectFixture {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	commits := map[string]string{}
	writeFile(t, dir, "go.mod", fixtureMod)
	writeFile(t, dir, "calc.go", selectCalc)
	writeFile(t, dir, "calc_test.go", selectTestBase)
	commits["root"] = commitAll(t, dir, "root")

	// good: code and test change, everything passes.
	clamp := "\n// Clamp limits v.\nfunc Clamp(v, lo, hi int) int {\n\tif v < lo {\n\t\treturn lo\n\t}\n\tif v > hi {\n\t\treturn hi\n\t}\n\treturn v\n}\n"
	writeFile(t, dir, "calc.go", selectCalc+clamp)
	writeFile(t, dir, "calc_test.go", selectTestBase+"\nfunc TestClamp(t *testing.T) {\n\tif Clamp(5, 0, 3) != 3 {\n\t\tt.Fatal(\"clamp\")\n\t}\n}\n")
	commits["good"] = commitAll(t, dir, "good")

	// gomod: go.mod changes with code and test.
	writeFile(t, dir, "go.mod", fixtureMod+"\n// touched\n")
	writeFile(t, dir, "calc.go", selectCalc+clamp+"\n// Zero is zero.\nfunc Zero() int { return 0 }\n")
	writeFile(t, dir, "calc_test.go", selectTestBase+"\nfunc TestClamp(t *testing.T) {\n\tif Clamp(5, 0, 3) != 3 {\n\t\tt.Fatal(\"clamp\")\n\t}\n}\n\nfunc TestZero(t *testing.T) {\n\tif Zero() != 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n")
	commits["gomod"] = commitAll(t, dir, "gomod")

	// breaks: code and test change, and a test fails at this commit.
	good := selectCalc + clamp + "\n// Zero is zero.\nfunc Zero() int { return 0 }\n"
	writeFile(t, dir, "calc.go", good+"\n// One is wrong.\nfunc One() int { return 2 }\n")
	writeFile(t, dir, "calc_test.go", selectTestBase+"\nfunc TestClamp(t *testing.T) {\n\tif Clamp(5, 0, 3) != 3 {\n\t\tt.Fatal(\"clamp\")\n\t}\n}\n\nfunc TestZero(t *testing.T) {\n\tif Zero() != 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\")\n\t}\n}\n")
	commits["breaks"] = commitAll(t, dir, "breaks")

	// fixes: repairs the code; its base is the broken commit.
	writeFile(t, dir, "calc.go", good+"\n// One is one.\nfunc One() int { return 1 }\n")
	writeFile(t, dir, "calc_test.go", selectTestBase+"\nfunc TestClamp(t *testing.T) {\n\tif Clamp(5, 0, 3) != 3 {\n\t\tt.Fatal(\"clamp\")\n\t}\n}\n\nfunc TestZero(t *testing.T) {\n\tif Zero() != 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\", One())\n\t}\n}\n")
	commits["fixes"] = commitAll(t, dir, "fixes")

	// weakens: removes a test function and changes code.
	writeFile(t, dir, "calc.go", good+"\n// One is one.\nfunc One() int {\n\treturn 1\n}\n")
	writeFile(t, dir, "calc_test.go", selectTestBase+"\nfunc TestZero(t *testing.T) {\n\tif Zero() != 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\", One())\n\t}\n}\n")
	commits["weakens"] = commitAll(t, dir, "weakens")

	// testsonly: no non-test code changes.
	writeFile(t, dir, "calc_test.go", selectTestBase+"\n// comment\nfunc TestZero(t *testing.T) {\n\tif Zero() != 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\", One())\n\t}\n}\n")
	commits["testsonly"] = commitAll(t, dir, "testsonly")

	// generated: only generated code changes beside a test.
	writeFile(t, dir, "gen.go", "// Code generated by hand; DO NOT EDIT.\n\npackage proj\n\n// Gen is generated.\nfunc Gen() int { return 1 }\n")
	writeFile(t, dir, "gen_test.go", "package proj\n\nimport \"testing\"\n\nfunc TestGen(t *testing.T) {\n\tif Gen() != 1 {\n\t\tt.Fatal(\"gen\")\n\t}\n}\n")
	commits["genroot"] = commitAll(t, dir, "genroot")
	writeFile(t, dir, "gen.go", "// Code generated by hand; DO NOT EDIT.\n\npackage proj\n\n// Gen is generated.\nfunc Gen() int { return 1 + 0 }\n")
	writeFile(t, dir, "gen_test.go", "package proj\n\nimport \"testing\"\n\nfunc TestGen(t *testing.T) {\n\tif Gen() != 1 {\n\t\tt.Fatal(\"gen!\")\n\t}\n}\n")
	commits["generated"] = commitAll(t, dir, "generated")

	// big: more than 400 changed lines.
	writeFile(t, dir, "calc.go", good+"\n// One is one.\nfunc One() int {\n\treturn 1\n}\n"+strings.Repeat("// filler line\n", 450))
	writeFile(t, dir, "calc_test.go", selectTestBase+"\n// comment\nfunc TestZero(t *testing.T) {\n\tif Zero() != 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\", One())\n\t}\n}\n\n// more\n")
	commits["big"] = commitAll(t, dir, "big")
	return selectFixture{dir: dir, commits: commits}
}

func TestSelectFiltersMinesAndExcludes(t *testing.T) {
	t.Parallel()
	f := newSelectFixture(t)
	work := t.TempDir()
	csv := filepath.Join(work, "corpus.csv")
	writeFile(t, work, "corpus.csv", "project,url,pinned\nproj,"+f.dir+","+f.commits["big"]+"\n")
	options := SelectOptions{
		CorpusCSV: csv, WorkDir: filepath.Join(work, "w"), Out: filepath.Join(work, "sel.jsonl"),
		ExclusionsOut: filepath.Join(work, "excl.jsonl"), PerRepoTrue: 10, PerRepoFlaw: 1, PerRepoDestructive: 5,
		MaxScan: 100, TestTimeout: time.Minute,
	}
	if err := Select(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	selections, err := ReadJSONL[Selection](options.Out)
	if err != nil {
		t.Fatal(err)
	}
	exclusions, err := ReadJSONL[Exclusion](options.ExclusionsOut)
	if err != nil {
		t.Fatal(err)
	}
	byCommit := map[string]Selection{}
	for _, s := range selections {
		byCommit[s.Commit] = s
	}
	if len(selections) != 2 {
		t.Fatalf("selections = %+v, want good and weakens only", selections)
	}
	good, weakens := byCommit[f.commits["good"]], byCommit[f.commits["weakens"]]
	if !good.TruePatch || good.Destructive || good.Project != "proj" || good.Base != f.commits["root"] ||
		!reflect.DeepEqual(good.DirectPackages, []string{"."}) || good.Rank != rankOf("proj", good.Commit) {
		t.Errorf("good = %+v", good)
	}
	if !weakens.TruePatch || !weakens.Destructive || weakens.Base != f.commits["fixes"] {
		t.Errorf("weakens = %+v, want a true patch that is also destructive", weakens)
	}
	if good.FlawSeed == weakens.FlawSeed {
		t.Errorf("exactly one of the two commits is the flaw seed (quota 1): %v %v", good.FlawSeed, weakens.FlawSeed)
	}
	if first := lowerRank(good, weakens); !first.FlawSeed {
		t.Errorf("the seed must be the lowest rank, got %+v", first)
	}
	reasons := map[string]string{}
	var static []string
	for _, e := range exclusions {
		if e.Commit == "" {
			static = append(static, e.Reason)
			continue
		}
		reasons[e.Commit] = e.Reason
	}
	sort.Strings(static)
	// 10 commits including the root; good, breaks, fixes, weakens pass the static filter.
	if want := []string{"complete: proj", "static destructive: 10 scanned, 1 kept", "static main: 10 scanned, 4 kept"}; !reflect.DeepEqual(static, want) {
		t.Errorf("static summaries = %v, want %v", static, want)
	}
	if got := reasons[f.commits["breaks"]]; got != "tests fail at commit" {
		t.Errorf("breaks reason = %q", got)
	}
	if got := reasons[f.commits["fixes"]]; got != "tests fail at base" {
		t.Errorf("fixes reason = %q", got)
	}
	for _, name := range []string{"gomod", "big", "testsonly", "generated", "good", "weakens"} {
		if r, ok := reasons[f.commits[name]]; ok {
			t.Errorf("%s must not be an expensive-filter exclusion, got %q", name, r)
		}
	}
}

func lowerRank(a, b Selection) Selection {
	if a.Rank < b.Rank {
		return a
	}
	return b
}

func TestSelectQuotaStopsExpensiveFilter(t *testing.T) {
	t.Parallel()
	f := newSelectFixture(t)
	work := t.TempDir()
	writeFile(t, work, "corpus.csv", "project,url,pinned\nproj,"+f.dir+","+f.commits["big"]+"\n")
	options := SelectOptions{
		CorpusCSV: filepath.Join(work, "corpus.csv"), WorkDir: filepath.Join(work, "w"), Out: filepath.Join(work, "sel.jsonl"),
		ExclusionsOut: filepath.Join(work, "excl.jsonl"), PerRepoTrue: 1, PerRepoFlaw: 1, PerRepoDestructive: 1, MaxScan: 100,
		TestTimeout: time.Minute,
	}
	if err := Select(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	selections, err := ReadJSONL[Selection](options.Out)
	if err != nil {
		t.Fatal(err)
	}
	trueCount := 0
	for _, s := range selections {
		if s.TruePatch {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Fatalf("true patches = %d, want exactly the quota of 1 (%+v)", trueCount, selections)
	}
}

func TestSelectRejectsMissingPaths(t *testing.T) {
	if err := Select(context.Background(), SelectOptions{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected an error without a corpus path")
	}
}
