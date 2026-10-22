package gateeval

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	genCalcV1 = `package proj

// Abs returns the absolute value of n.
func Abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Double doubles n.
func Double(n int) int { return n + n }
`
	genCalcV2 = `package proj

import "strconv"

// Abs returns the absolute value of n.
func Abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Double doubles n.
func Double(n int) int {
	return n * 2
}

// Clamp limits v to [lo, hi].
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Parse parses a decimal integer.
func Parse(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n, nil
}
`
	genTestV1 = `package proj

import "testing"

func TestAbs(t *testing.T) {
	if Abs(-3) != 3 {
		t.Fatal("abs")
	}
}
`
	genTestV2 = genTestV1 + `
func TestClamp(t *testing.T) {
	if got := Clamp(5, 0, 10); got != 5 {
		t.Errorf("Clamp(5) = %d", got)
	}
	if got := Clamp(-1, 0, 10); got != 0 {
		t.Errorf("Clamp(-1) = %d", got)
	}
	if got := Clamp(11, 0, 10); got != 10 {
		t.Errorf("Clamp(11) = %d", got)
	}
}

func TestParse(t *testing.T) {
	n, err := Parse("12")
	if err != nil || n != 12 {
		t.Fatalf("Parse = %d, %v", n, err)
	}
}
`
	genUtilV1     = "package proj\n\n// Untested returns a constant.\nfunc Untested() int { return 4 }\n"
	genUtilV2     = "package proj\n\n// Untested returns a constant.\nfunc Untested() int { return 5 }\n"
	genUtilTestV1 = "package proj\n\nimport \"testing\"\n\nfunc TestUtilPresent(t *testing.T) { _ = Untested }\n"
	genUtilTestV2 = genUtilTestV1 + "\nfunc TestUtilOther(t *testing.T) {\n\tif Abs(-1) != 1 {\n\t\tt.Fatal(\"abs\")\n\t}\n}\n"
	genConsumer   = "package consumer\n\nimport \"example.com/proj\"\n\n// Quad quadruples n.\nfunc Quad(n int) int { return proj.Double(proj.Double(n)) }\n"
	genConsumerT  = "package consumer\n\nimport \"testing\"\n\nfunc TestQuad(t *testing.T) {\n\tif Quad(3) != 12 {\n\t\tt.Fatal(\"quad\")\n\t}\n}\n"
)

// genFixture is a source repository with three commits: c0 is the base, c1
// adds tested functions and an untested-by-the-package Double (covered only by
// a consumer package), and c2 changes a constant nothing checks.
type genFixture struct {
	commits map[string]string
	dir     string
}

func newGenFixture(t *testing.T) genFixture {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "go.mod", fixtureMod)
	writeFile(t, dir, "calc.go", genCalcV1)
	writeFile(t, dir, "calc_test.go", genTestV1)
	writeFile(t, dir, "util.go", genUtilV1)
	writeFile(t, dir, "util_test.go", genUtilTestV1)
	writeFile(t, dir, "consumer/consumer.go", genConsumer)
	writeFile(t, dir, "consumer/consumer_test.go", genConsumerT)
	writeFile(t, dir, "other/other.go", "package other\n\n// Always is one.\nfunc Always() int { return 1 }\n")
	writeFile(t, dir, "other/other_test.go", "package other\n\nimport \"testing\"\n\nfunc TestPreexistingFailure(t *testing.T) { t.Fatal(\"fails at every commit\") }\n")
	commits := map[string]string{"c0": commitAll(t, dir, "c0")}
	writeFile(t, dir, "calc.go", genCalcV2)
	writeFile(t, dir, "calc_test.go", genTestV2)
	commits["c1"] = commitAll(t, dir, "c1")
	writeFile(t, dir, "util.go", genUtilV2)
	writeFile(t, dir, "util_test.go", genUtilTestV2)
	commits["c2"] = commitAll(t, dir, "c2")
	return genFixture{dir: dir, commits: commits}
}

// worktree clones the fixture and checks out rev detached.
func (f genFixture) worktree(t *testing.T, rev string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	runGit(t, filepath.Dir(dir), "clone", "-q", f.dir, dir)
	runGit(t, dir, "checkout", "-q", "--detach", rev)
	return dir
}

func (f genFixture) selection(name string, truePatch, flaw, destructive bool) Selection {
	return Selection{
		Project: "proj", Commit: f.commits[name], Base: f.commits[map[string]string{"c1": "c0", "c2": "c1"}[name]],
		Rank: rankOf("proj", f.commits[name]), DirectPackages: []string{"."},
		TruePatch: truePatch, FlawSeed: flaw, Destructive: destructive, ChangedLines: 10,
	}
}

func writeSelections(t *testing.T, path string, selections ...Selection) {
	t.Helper()
	for _, s := range selections {
		if err := AppendJSONL(path, s); err != nil {
			t.Fatal(err)
		}
	}
}

func variantsByID(t *testing.T, path string) map[string]Variant {
	t.Helper()
	variants, err := ReadJSONL[Variant](path)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Variant{}
	for _, v := range variants {
		if _, dup := byID[v.ID]; dup {
			t.Fatalf("variant %s recorded twice", v.ID)
		}
		byID[v.ID] = v
	}
	return byID
}

func TestGenerateBuildsEveryVariantAndResumes(t *testing.T) {
	// CI=true must not leak into the code under test: D1 skips only when CI is unset.
	t.Setenv("CI", "true")
	f := newGenFixture(t)
	work := t.TempDir()
	cloneDir := filepath.Join(work, "w", "clones", "proj")
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o750); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "clone", "-q", f.dir, cloneDir)
	selected := filepath.Join(work, "selected.jsonl")
	writeSelections(t, selected, f.selection("c1", true, true, true), f.selection("c2", false, true, false))
	options := GenerateOptions{
		WorkDir: filepath.Join(work, "w"), Selected: selected, Out: filepath.Join(work, "variants.jsonl"),
		ExclusionsOut: filepath.Join(work, "excl.jsonl"), TestTimeout: time.Minute,
	}
	if err := Generate(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	byID := variantsByID(t, options.Out)
	c1, c2 := f.commits["c1"], f.commits["c2"]
	id := func(commit string, class Class) string { return "proj-" + commit[:7] + "-" + string(class) }

	var ids []string
	for key := range byID {
		ids = append(ids, key)
	}
	sort.Strings(ids)
	wantClasses := []Class{
		ClassTrue, ClassDestructive, ClassStub, ClassDroppedErr, ClassMutant, ClassConsumerBreak,
		ClassDeleteTests, ClassSkipTests, ClassLogAssertions, ClassRevertTests,
		ClassEnvGuardedSkip, ClassEarlyReturn, ClassBuildTag, ClassLowercaseName, ClassHelperSkip,
		ClassHeldout6, ClassHeldout7, ClassHeldout8, ClassHeldout9, ClassHeldout10,
	}
	for _, class := range wantClasses {
		v, ok := byID[id(c1, class)]
		if !ok {
			t.Errorf("missing variant %s; have %v", id(c1, class), ids)
			continue
		}
		if v.Base != f.commits["c0"] || v.Commit != c1 || v.Project != "proj" || v.Class != class || v.Branch != "gateeval/"+v.ID {
			t.Errorf("%s = %+v", v.ID, v)
		}
		if v.Oracle == nil {
			t.Errorf("%s oracle must be non-nil", v.ID)
		}
	}
	if v, ok := byID[id(c2, ClassStub)]; !ok || !v.Effective {
		t.Errorf("c2 must still get an S1 stub: %+v", v)
	}
	for key := range byID {
		if strings.HasPrefix(key, "proj-"+c2[:7]) && key != id(c2, ClassStub) {
			t.Errorf("unexpected c2 variant %s", key)
		}
	}

	checkMutant(t, byID[id(c1, ClassMutant)])
	consumer := byID[id(c1, ClassConsumerBreak)]
	wantConsumer := []TestRef{{Package: "example.com/proj/consumer", File: "consumer/consumer_test.go", Name: "TestQuad"}}
	if !reflect.DeepEqual(consumer.Oracle, wantConsumer) || !consumer.Effective || !strings.HasPrefix(consumer.Operator, "op3@calc.go:") {
		t.Errorf("S2 = %+v, want a zero-return consumer break found by TestQuad", consumer)
	}
	for _, class := range append(append([]Class{}, wantClasses[6:10]...), wantClasses[10:]...) {
		v := byID[id(c1, class)]
		if !v.Effective {
			t.Errorf("%s should be effective on this fixture", v.ID)
		}
		if !reflect.DeepEqual(v.Oracle, byID[id(c1, ClassMutant)].Oracle) {
			t.Errorf("%s oracle = %+v, want the mutant's", v.ID, v.Oracle)
		}
	}
	for _, class := range []Class{ClassHeldout6, ClassHeldout7, ClassHeldout8, ClassHeldout9, ClassHeldout10} {
		if byID[id(c1, class)].Notes == "" {
			t.Errorf("%s must carry the technique description", class)
		}
	}
	checkBranches(t, cloneDir, byID, f)

	exclusions, err := ReadJSONL[Exclusion](options.ExclusionsOut)
	if err != nil {
		t.Fatal(err)
	}
	got := map[Class]string{}
	for _, e := range exclusions {
		if e.Commit != c2 {
			t.Errorf("unexpected exclusion %+v", e)
		}
		got[e.Class] = e.Reason
	}
	if reason := got[ClassMutant]; reason != "no killing mutant (op1=0, op2=0, op3=1, op4=0, op5=1)" {
		t.Errorf("M reason = %q, want the per-operator candidate counts", reason)
	}
	got[ClassMutant] = "no killing mutant"
	want := map[Class]string{ClassMutant: "no killing mutant", ClassConsumerBreak: "no consumer break", ClassDroppedErr: "no compiling candidate"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("exclusions = %v, want %v", got, want)
	}

	assertResumes(t, options, cloneDir, c1)
}

func checkMutant(t *testing.T, m Variant) {
	t.Helper()
	want := []TestRef{{Package: "example.com/proj", File: "calc_test.go", Name: "TestClamp"}}
	if !reflect.DeepEqual(m.Oracle, want) || !m.Effective || !strings.HasPrefix(m.Operator, "op1@calc.go:") {
		t.Errorf("M = %+v, want an op1 mutant killed by TestClamp", m)
	}
}

// checkBranches verifies the branch topology and commit messages in the clone.
func checkBranches(t *testing.T, clone string, byID map[string]Variant, f genFixture) {
	t.Helper()
	c1 := f.commits["c1"]
	rev := func(ref string) string { return runGit(t, clone, "rev-parse", ref) }
	if got := rev(byID["proj-"+c1[:7]+"-T"].Branch); got != c1 {
		t.Errorf("T branch = %s, want the commit itself", got)
	}
	if got := rev(byID["proj-"+c1[:7]+"-DT"].Branch); got != c1 {
		t.Errorf("DT branch = %s, want the commit itself", got)
	}
	mutantTip := rev(byID["proj-"+c1[:7]+"-M"].Branch)
	for class, parent := range map[Class]string{
		ClassMutant: c1, ClassStub: c1, ClassDroppedErr: c1, ClassConsumerBreak: c1,
		ClassDeleteTests: mutantTip, ClassSkipTests: mutantTip, ClassLogAssertions: mutantTip, ClassRevertTests: mutantTip,
		ClassEnvGuardedSkip: mutantTip, ClassEarlyReturn: mutantTip, ClassBuildTag: mutantTip, ClassLowercaseName: mutantTip,
		ClassHelperSkip: mutantTip, ClassHeldout6: mutantTip, ClassHeldout7: mutantTip, ClassHeldout8: mutantTip,
		ClassHeldout9: mutantTip, ClassHeldout10: mutantTip,
	} {
		branch := byID["proj-"+c1[:7]+"-"+string(class)].Branch
		if got := rev(branch + "^"); got != parent {
			t.Errorf("%s parent = %s, want %s", class, got, parent)
		}
		if msg := runGit(t, clone, "log", "-1", "--format=%s", branch); msg != "gateeval "+string(class) {
			t.Errorf("%s message = %q", class, msg)
		}
		if author := runGit(t, clone, "log", "-1", "--format=%an <%ae>", branch); author != "gateeval <gateeval@example.invalid>" {
			t.Errorf("%s author = %q", class, author)
		}
	}
	files := func(class Class) string {
		return runGit(t, clone, "diff", "--name-only", rev(byID["proj-"+c1[:7]+"-"+string(class)].Branch)+"^", byID["proj-"+c1[:7]+"-"+string(class)].Branch)
	}
	if got := files(ClassMutant); got != "calc.go" {
		t.Errorf("M touches %q, want calc.go only", got)
	}
	if got := files(ClassRevertTests); got != "calc_test.go" {
		t.Errorf("C4 touches %q, want calc_test.go", got)
	}
	if got := files(ClassBuildTag); got != "calc_test.go" {
		t.Errorf("D3 touches %q, want calc_test.go", got)
	}
	if got := files(ClassHeldout8); !strings.Contains(got, "calc_test.go") {
		t.Errorf("D8 touches %q", got)
	}
}

// assertResumes reruns Generate unchanged, then after dropping variant lines.
func assertResumes(t *testing.T, options GenerateOptions, clone, commit string) {
	t.Helper()
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	before, beforeExcl := read(options.Out), read(options.ExclusionsOut)
	shaOf := func(key string) string { return runGit(t, clone, "rev-parse", "gateeval/"+key) }
	wasSHA := map[string]string{}
	for _, class := range []string{"C1", "D6", "S1"} {
		wasSHA[class] = shaOf("proj-" + commit[:7] + "-" + class)
	}
	if err := Generate(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if read(options.Out) != before || read(options.ExclusionsOut) != beforeExcl {
		t.Fatal("a rerun must not add variants or exclusions")
	}
	dropped := map[string]bool{"proj-" + commit[:7] + "-C1": true, "proj-" + commit[:7] + "-D6": true, "proj-" + commit[:7] + "-S1": true}
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(before), "\n") {
		skip := false
		for key := range dropped {
			if strings.Contains(line, `"id":"`+key+`"`) {
				skip = true
			}
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	if len(kept) != strings.Count(before, "\n")-len(dropped) {
		t.Fatalf("expected to drop the dropped lines of %d, kept %d", strings.Count(before, "\n"), len(kept))
	}
	if err := os.WriteFile(options.Out, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Generate(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	after := variantsByID(t, options.Out)
	if len(after) != strings.Count(before, "\n") {
		t.Fatalf("variants after resume = %d, want %d", len(after), strings.Count(before, "\n"))
	}
	for key := range dropped {
		if _, ok := after[key]; !ok && strings.HasPrefix(key, "proj-"+commit[:7]) {
			t.Errorf("%s was not regenerated", key)
		}
	}
	if read(options.ExclusionsOut) != beforeExcl {
		t.Error("resuming must not duplicate exclusions")
	}
	for class, was := range wasSHA {
		if now := shaOf("proj-" + commit[:7] + "-" + class); now != was {
			t.Errorf("%s regenerated as %s, want the identical commit %s", class, now, was)
		}
	}
}

func TestGenerateRejectsBadInput(t *testing.T) {
	if err := Generate(context.Background(), GenerateOptions{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("expected an error without paths")
	}
	work := t.TempDir()
	selected := filepath.Join(work, "s.jsonl")
	writeSelections(t, selected, Selection{Project: "ghost", Commit: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), TruePatch: true})
	err := Generate(context.Background(), GenerateOptions{
		WorkDir: work, Selected: selected, Out: filepath.Join(work, "o.jsonl"), ExclusionsOut: filepath.Join(work, "e.jsonl"),
	})
	if err == nil || !strings.Contains(err.Error(), "clone of ghost") {
		t.Fatalf("error = %v, want a missing clone", err)
	}
}
