package gateeval

import (
	"context"
	"go/format"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// allLines marks lines 1..n as changed.
func allLines(n int) map[int]bool {
	lines := map[int]bool{}
	for i := 1; i <= n; i++ {
		lines[i] = true
	}
	return lines
}

// mutate applies a candidate to src and formats the result.
func mutate(t *testing.T, src string, c candidate) string {
	t.Helper()
	out, err := applyTextEdits([]byte(src), c.Edits)
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("mutant does not parse: %v\n%s", err, out)
	}
	return string(formatted)
}

func TestApplyTextEdits(t *testing.T) {
	src := []byte("abcdef")
	got, err := applyTextEdits(src, []textEdit{{Start: 4, End: 5, Text: "X"}, {Start: 1, End: 3}, {Start: 0, End: 0, Text: ">"}})
	if err != nil || string(got) != ">adXf" {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = applyTextEdits(src, []textEdit{{Start: 1, End: 5}, {Start: 2, End: 3, Text: "inner"}})
	if err != nil || string(got) != "af" {
		t.Fatalf("contained edit must be dropped: %q, %v", got, err)
	}
	if _, err := applyTextEdits(src, []textEdit{{Start: 1, End: 4}, {Start: 3, End: 5}}); err == nil {
		t.Error("partial overlap must fail")
	}
	if _, err := applyTextEdits(src, []textEdit{{Start: 2, End: 99}}); err == nil {
		t.Error("an edit beyond the source must fail")
	}
}

func TestSyntaxCandidatesOperators(t *testing.T) {
	const src = `package p

func f(a, b int, m map[string]int) int {
	if a > b {
		g()
	}
	x := 7
	x = h(x)
	switch {
	case a == 1:
		g()
	}
	return a + 0x10
}
`
	cands, err := syntaxCandidates("p.go", []byte(src), allLines(16))
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Offset < cands[j].Offset })
	byOp := map[int][]candidate{}
	for _, c := range cands {
		byOp[c.Op] = append(byOp[c.Op], c)
	}
	check := func(op, n int, contains ...string) {
		t.Helper()
		if len(byOp[op]) != n {
			t.Fatalf("op%d candidates = %d, want %d", op, len(byOp[op]), n)
		}
		for i, want := range contains {
			if text := mutate(t, src, byOp[op][i]); !strings.Contains(text, want) {
				t.Errorf("op%d[%d] result lacks %q:\n%s", op, i, want, text)
			}
		}
	}
	check(opNegateIf, 1, "if !(a > b) {")
	check(opFlipRel, 2, "if a <= b {", "case a != 1:")
	check(opDeleteStmt, 3, "if a > b {\n\t}", "x := 7\n\tswitch", "case a == 1:\n\t}")
	check(opBumpInt, 3, "x := 8", "case a == 2:", "return a + 17")
}

func TestSyntaxCandidatesNearMisses(t *testing.T) {
	const src = `package p

func f(a int) (int, error) {
	if v := g(); v > 0 {
		return a, nil
	}
	y := g()
	_ = y
	var z = 1.5
	_ = z
	for i := 0; i < a; i++ {
		defer g()
	}
	return a, nil
}
`
	cands, err := syntaxCandidates("p.go", []byte(src), allLines(15))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		text := mutate(t, src, c)
		switch c.Op {
		case opDeleteStmt:
			if strings.Contains(src[c.Edits[0].Start:c.Edits[0].End], ":=") || strings.HasPrefix(src[c.Edits[0].Start:c.Edits[0].End], "defer") {
				t.Errorf("deleted a statement that is not a call or plain assignment: %s", text)
			}
		case opBumpInt:
			if strings.Contains(text, "1.6") {
				t.Errorf("float literal must not be bumped:\n%s", text)
			}
		}
	}
	// `v := g()` in the if-init, `y := g()`, `i++`, `defer` and the float literal give no deletion or bump.
	ops := map[int]int{}
	for _, c := range cands {
		ops[c.Op]++
	}
	if ops[opDeleteStmt] != 2 || ops[opBumpInt] != 2 {
		t.Errorf("deletions = %d, bumps = %d; want only `_ = y`, `_ = z` and the 0s of v > 0 and i := 0", ops[opDeleteStmt], ops[opBumpInt])
	}
}

func TestSyntaxCandidatesRespectChangedLines(t *testing.T) {
	const src = "package p\n\nfunc f(a int) int {\n\tif a > 1 {\n\t\treturn 1\n\t}\n\tif a < 0 {\n\t\treturn 2\n\t}\n\treturn 0\n}\n"
	cands, err := syntaxCandidates("p.go", []byte(src), map[int]bool{7: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.Line != 7 {
			t.Errorf("candidate on unchanged line: %s", c.label())
		}
	}
	if len(cands) != 3 {
		t.Errorf("candidates = %d, want negate, flip and bump on line 7 only", len(cands))
	}
	if cands, err = syntaxCandidates("p.go", []byte(src), nil); err != nil || len(cands) != 0 {
		t.Errorf("no changed lines must give no candidates: %d, %v", len(cands), err)
	}
	if _, err = syntaxCandidates("p.go", []byte("package"), allLines(1)); err == nil {
		t.Error("a file that does not parse must be an error")
	}
}

func TestIncrementInt(t *testing.T) {
	tests := map[string]string{"0": "1", "41": "42", "0x10": "17", "1_000": "1001", "0b11": "4", "0o7": "8", "010": "9"}
	for in, want := range tests {
		if got, ok := incrementInt(in); !ok || got != want {
			t.Errorf("incrementInt(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := incrementInt("zz"); ok {
		t.Error("garbage must not parse")
	}
}

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", fixtureMod)
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	return dir
}

func TestReturnCandidatesZeroValues(t *testing.T) {
	const src = `package p

import "time"

type S struct{ A int }

type N string

type G[T any] struct{ v T }

func (g G[T]) Get() T { return g.v }

func f() (int, string, bool, *S, []int, map[string]int, error, S, [2]int, time.Duration, N, func(), any) {
	return 1, "x", true, &S{}, []int{1}, nil, errBad, S{A: 1}, [2]int{1}, time.Second, N("n"), nil, 5
}

var errBad error

func g() int {
	h := func() string { return "inner" }
	_ = h
	return 0
}

func k(p *S) (S, error) {
	return *p, nil
}

func tuple() (int, error) { return f2() }

func f2() (int, error) { return 0, nil }
`
	dir := writeModule(t, map[string]string{"p.go": src})
	typed := loadTyped(context.Background(), dir, []string{"."})
	tf, ok := typed["p.go"]
	if !ok {
		t.Fatalf("p.go was not loaded: %v", typed)
	}
	cands := returnCandidates("p.go", []byte(src), allLines(30), tf)
	var got []string
	for _, c := range cands {
		got = append(got, string([]byte(src)[c.Edits[0].Start:c.Edits[0].End])+" => "+c.Edits[0].Text)
	}
	want := []string{
		`1 => 0`, `"x" => ""`, `true => false`, `&S{} => nil`, `[]int{1} => nil`,
		// map nil already; skipped
		`errBad => nil`, `S{A: 1} => S{}`, `[2]int{1} => [2]int{}`, `time.Second => 0`, `N("n") => ""`, `5 => nil`,
		// no candidate for nil results, the literal 0 in g(), or the tuple return in tuple()
		`"inner" => ""`, `*p => S{}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates:\n got %q\nwant %q", got, want)
	}
	for _, c := range cands {
		if c.Op != opZeroReturn {
			t.Errorf("op = %d", c.Op)
		}
	}
}

func TestReturnCandidatesSkipUnloadablePackage(t *testing.T) {
	dir := writeModule(t, map[string]string{"p.go": "package p\n\nfunc f() int { return undefined }\n"})
	if typed := loadTyped(context.Background(), dir, []string{"."}); len(typed) != 0 {
		t.Errorf("a package with type errors must contribute nothing, got %d files", len(typed))
	}
	if typed := loadTyped(context.Background(), dir, nil); len(typed) != 0 {
		t.Error("no directories must load nothing")
	}
}

func TestBuildCandidatesOrder(t *testing.T) {
	const a = "package p\n\nfunc A(n int) int {\n\tif n > 1 {\n\t\treturn n + 1\n\t}\n\treturn 7\n}\n"
	const b = "package p\n\nfunc B(n int) int {\n\tif n < 0 {\n\t\treturn n\n\t}\n\treturn 3\n}\n"
	dir := writeModule(t, map[string]string{"a.go": a, "b.go": b})
	sources := []sourceFile{
		{Path: "b.go", Src: []byte(b), Lines: allLines(8)},
		{Path: "a.go", Src: []byte(a), Lines: allLines(8)},
	}
	cands, err := buildCandidates(context.Background(), dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, c := range cands {
		labels = append(labels, c.label())
	}
	want := []string{
		"op1@a.go:4", "op1@b.go:4",
		"op2@a.go:4", "op2@b.go:4",
		"op3@a.go:5", "op3@a.go:7", "op3@b.go:5", "op3@b.go:7",
		"op5@a.go:4", "op5@a.go:5", "op5@a.go:7", "op5@b.go:4", "op5@b.go:7",
	}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("order:\n got %v\nwant %v", labels, want)
	}
}

func TestScanTestStream(t *testing.T) {
	stream := `{"Action":"run","Package":"x/p","Test":"TestA"}
{"Action":"fail","Package":"x/p","Test":"TestA/sub"}
{"Action":"fail","Package":"x/p","Test":"TestA"}
{"Action":"fail","Package":"x/q","Test":"TestB"}
{"Action":"pass","Package":"x/q","Test":"TestC"}
{"Action":"fail","Package":"x/q"}
not json
`
	got := scanTestStream([]byte(stream))
	want := []testID{{"x/p", "TestA"}, {"x/q", "TestB"}}
	if !reflect.DeepEqual(got.Failed, want) || got.BuildErr || got.TimedOut {
		t.Errorf("scan = %+v", got)
	}
	if got := scanTestStream([]byte(`{"Action":"build-fail","ImportPath":"x/p [x/p.test]"}`)); !got.BuildErr || len(got.Failed) != 0 {
		t.Errorf("build failure = %+v", got)
	}
	if got := scanTestStream([]byte(`{"Action":"output","Package":"x/p","Output":"panic: test timed out after 1s\n"}`)); !got.TimedOut {
		t.Errorf("timeout = %+v", got)
	}
}

func TestOracleOfResolvesFiles(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a_test.go":       "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"x_test.go":       "package p_test\n\nimport \"testing\"\n\nfunc TestExternal(t *testing.T) {}\n\ntype T struct{}\n\nfunc (T) TestMethod() {}\n",
		"sub/sub_test.go": "package sub\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) {}\n",
	})
	oracle := oracleOf(dir, "example.com/proj", []testID{
		{"example.com/proj", "TestA"},
		{"example.com/proj", "TestExternal"},
		{"example.com/proj/sub", "TestSub"},
		{"example.com/proj", "TestMethod"},
		{"example.com/proj", "TestMissing"},
		{"other.org/x", "TestSub"},
	})
	want := []TestRef{
		{Package: "example.com/proj", File: "a_test.go", Name: "TestA"},
		{Package: "example.com/proj", File: "x_test.go", Name: "TestExternal"},
		{Package: "example.com/proj/sub", File: "sub/sub_test.go", Name: "TestSub"},
	}
	if !reflect.DeepEqual(oracle, want) {
		t.Errorf("oracle = %+v, want %+v", oracle, want)
	}
}

func TestMutantSearchFindsKillingMutantAndOracle(t *testing.T) {
	f := newGenFixture(t)
	tree := f.worktree(t, f.commits["c1"])
	modPath, err := modulePath(context.Background(), tree)
	if err != nil || modPath != "example.com/proj" {
		t.Fatalf("module path = %q, %v", modPath, err)
	}
	search, err := newMutantSearch(context.Background(), tree, f.commits["c0"], f.commits["c1"], modPath, []string{"."}, time.Minute, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(search.cands) == 0 {
		t.Fatal("no candidates")
	}
	index, oracle, err := search.findKilling(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if index < 0 {
		t.Fatal("no killing mutant found")
	}
	if label := search.cands[index].label(); !strings.HasPrefix(label, "op1@calc.go:") {
		t.Errorf("first killing candidate = %s, want an op1 mutant (operators run in order)", label)
	}
	want := []TestRef{{Package: "example.com/proj", File: "calc_test.go", Name: "TestClamp"}}
	if !reflect.DeepEqual(oracle, want) {
		t.Errorf("oracle = %+v, want %+v", oracle, want)
	}
	// The tree is restored after every attempt.
	data, err := os.ReadFile(filepath.Join(tree, "calc.go"))
	if err != nil || string(data) != string(search.sources["calc.go"]) {
		t.Errorf("calc.go not restored: %v", err)
	}
}

func TestMutantSearchAttemptLimit(t *testing.T) {
	f := newGenFixture(t)
	tree := f.worktree(t, f.commits["c2"])
	modPath, err := modulePath(context.Background(), tree)
	if err != nil {
		t.Fatal(err)
	}
	search, err := newMutantSearch(context.Background(), tree, f.commits["c1"], f.commits["c2"], modPath, []string{"."}, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := search.limit(); got != 1 {
		t.Fatalf("limit = %d, want 1", got)
	}
	index, oracle, err := search.findKilling(context.Background())
	if err != nil || index != -1 || oracle != nil {
		t.Errorf("an untested change has no killing mutant: %d %v %v", index, oracle, err)
	}
	if len(search.results) != 1 {
		t.Errorf("attempts = %d, want the limit of 1", len(search.results))
	}
}
