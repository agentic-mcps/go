package gateeval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStubCandidates(t *testing.T) {
	const src = `package p

func Untouched() int { return 1 }

func Changed(a int) int {
	if a > 0 {
		return a
	}
	return 0
}

func (s S) Method() string {
	return "m"
}

type S struct{}

func AlreadyStub() { panic("not implemented") }

func Declared() int
`
	tests := []struct { //nolint:govet // Table layout favors readability.
		name  string
		lines map[int]bool
		want  []string
	}{
		{name: "functions with a changed body line, in source order", lines: map[int]bool{7: true, 12: true}, want: []string{"stub Changed", "stub Method"}},
		{name: "a one-line function with a changed line", lines: map[int]bool{3: true}, want: []string{"stub Untouched"}},
		{name: "changed line outside any body", lines: map[int]bool{15: true, 19: true}},
		{name: "an existing stub or bodiless declaration is not a candidate", lines: map[int]bool{17: true, 19: true}},
		{name: "no changed lines", lines: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cands, err := stubCandidates("p.go", []byte(src), tt.lines)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, c := range cands {
				got = append(got, c.Note)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("candidates = %v, want %v", got, tt.want)
			}
		})
	}
	cands, err := stubCandidates("p.go", []byte(src), map[int]bool{7: true})
	if err != nil || len(cands) != 1 {
		t.Fatalf("%v %v", cands, err)
	}
	out, err := applyTextEdits([]byte(src), cands[0].Edits)
	if err != nil {
		t.Fatal(err)
	}
	if want := "func Changed(a int) int {\n\tpanic(\"not implemented\")\n}\n"; !strings.Contains(string(out), want) {
		t.Errorf("stubbed source:\n%s", out)
	}
	if _, err := stubCandidates("p.go", []byte("package"), map[int]bool{1: true}); err == nil {
		t.Error("unparsable source must be an error")
	}
}

func TestDropErrCandidates(t *testing.T) {
	tests := []struct { //nolint:govet // Table layout favors readability.
		name  string
		body  string
		lines int // changed lines: 1..lines relative to the body start (line 4 of the file)
		want  string
	}{
		{
			name: "define with another value keeps it",
			body: "\tn, err := strconv.Atoi(s)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn n, nil\n",
			want: "\tn, _ := strconv.Atoi(s)\n\treturn n, nil\n",
		},
		{
			name: "define of only err becomes a plain assignment to blank",
			body: "\terr := run()\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn 1, nil\n",
			want: "\t_ = run()\n\treturn 1, nil\n",
		},
		{
			name: "assignment to an existing err",
			body: "\tvar err error\n\terr = run()\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn 1, nil\n",
			want: "\tvar err error\n\t_ = run()\n\treturn 1, nil\n",
		},
		{
			name: "blank on the left stays and := becomes =",
			body: "\t_, err := os.Open(s)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn 1, nil\n",
			want: "\t_, _ = os.Open(s)\n\treturn 1, nil\n",
		},
		{name: "a statement between assignment and check", body: "\tn, err := strconv.Atoi(s)\n\tlog(n)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn n, nil\n"},
		{name: "check of a different condition", body: "\tn, err := strconv.Atoi(s)\n\tif err == nil {\n\t\treturn n, nil\n\t}\n\treturn 0, err\n"},
		{name: "check with an init statement", body: "\tn, err := strconv.Atoi(s)\n\tif e := wrap(err); e != nil {\n\t\treturn 0, e\n\t}\n\treturn n, nil\n"},
		{name: "assignment without err", body: "\tn, ok := lookup(s)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn n, nil\n"},
		{name: "err compared to something else", body: "\tn, err := strconv.Atoi(s)\n\tif err != io.EOF {\n\t\treturn 0, err\n\t}\n\treturn n, nil\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package p\n\nfunc f(s string) (int, error) {\n" + tt.body + "}\n"
			cands, err := dropErrCandidates("p.go", []byte(src), allLines(20))
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if len(cands) != 0 {
					t.Fatalf("expected no candidate, got %v", cands)
				}
				return
			}
			if len(cands) != 1 {
				t.Fatalf("candidates = %d, want 1", len(cands))
			}
			got := mutate(t, src, candidate{Edits: cands[0].Edits})
			if want := normalize(t, "package p\n\nfunc f(s string) (int, error) {\n"+tt.want+"}\n"); got != want {
				t.Errorf("result:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDropErrCandidatesChangedLinesAndOrder(t *testing.T) {
	const src = `package p

func f() {
	if true {
		a, err := one()
		if err != nil {
			return
		}
		_ = a
	}
	b, err := two()
	if err != nil {
		return
	}
	_ = b
}
`
	cands, err := dropErrCandidates("p.go", []byte(src), allLines(20))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || !strings.HasSuffix(cands[0].Note, "line 5") || !strings.HasSuffix(cands[1].Note, "line 11") {
		t.Fatalf("candidates must be in source order: %+v", cands)
	}
	cands, err = dropErrCandidates("p.go", []byte(src), map[int]bool{11: true})
	if err != nil || len(cands) != 1 || !strings.HasSuffix(cands[0].Note, "line 11") {
		t.Fatalf("only the assignment on a changed line qualifies: %+v %v", cands, err)
	}
	if cands, _ = dropErrCandidates("p.go", []byte(src), map[int]bool{12: true}); len(cands) != 0 {
		t.Errorf("a changed if line alone is not enough: %+v", cands)
	}
}

func TestFirstCompilingSkipsBrokenCandidatesAndRestores(t *testing.T) {
	const src = "package p\n\nimport \"strconv\"\n\nfunc a() int { return 1 }\n\nfunc b(s string) int {\n\tn, err := strconv.Atoi(s)\n\tif err != nil {\n\t\treturn 0\n\t}\n\treturn n\n}\n"
	dir := writeModule(t, map[string]string{"p.go": src})
	// The first candidate deletes the declaration of n's use and cannot compile; the second is fine.
	broken := flawCandidate{File: "p.go", Note: "broken", Edits: []textEdit{{Start: strings.Index(src, "return n"), End: strings.Index(src, "return n") + len("return n"), Text: "return undefined"}}}
	good := flawCandidate{File: "p.go", Note: "good", Edits: []textEdit{{Start: strings.Index(src, "return 1"), End: strings.Index(src, "return 1") + len("return 1"), Text: "return 2"}}}
	search := &flawSearch{sources: map[string][]byte{"p.go": []byte(src)}, tree: dir, direct: []string{"."}, timeout: time.Minute}
	index, err := search.firstCompiling(context.Background(), []flawCandidate{broken, good})
	if err != nil || index != 1 {
		t.Fatalf("index = %d, %v; want the compiling candidate", index, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "p.go"))
	if err != nil || !strings.Contains(string(data), "return 2") || strings.Contains(string(data), "undefined") {
		t.Errorf("tree must hold only the accepted edit: %q %v", data, err)
	}
	index, err = search.firstCompiling(context.Background(), []flawCandidate{broken})
	if err != nil || index != -1 {
		t.Fatalf("index = %d, %v; want none", index, err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "p.go"))
	if string(data) != src {
		t.Errorf("a rejected candidate must be undone:\n%s", data)
	}
}

func TestStubFixesUnusedImports(t *testing.T) {
	const src = "package p\n\nimport \"strconv\"\n\nfunc f(s string) int {\n\tn, _ := strconv.Atoi(s)\n\treturn n\n}\n"
	dir := writeModule(t, map[string]string{"p.go": src})
	cands, err := stubCandidates("p.go", []byte(src), allLines(8))
	if err != nil || len(cands) != 1 {
		t.Fatalf("%v %v", cands, err)
	}
	search := &flawSearch{sources: map[string][]byte{"p.go": []byte(src)}, tree: dir, direct: []string{"."}, timeout: time.Minute}
	index, err := search.firstCompiling(context.Background(), cands)
	if err != nil || index != 0 {
		t.Fatalf("a stub that orphans an import must still compile once imports are fixed: %d %v", index, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "p.go"))
	if strings.Contains(string(data), "strconv") || !strings.Contains(string(data), `panic("not implemented")`) {
		t.Errorf("stubbed file:\n%s", data)
	}
}
