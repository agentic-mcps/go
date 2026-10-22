package heldout

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const buggySource = `package calc

// Add is deliberately wrong: it subtracts.
func Add(a, b int) int { return a - b }

// Double is correct.
func Double(a int) int { return a * 2 }
`

// fixture is a small module whose tests fail because Add is buggy.
type fixture struct {
	files map[string]string
	name  string
	tests []Test
}

func fixtures() []fixture {
	return []fixture{
		{
			name: "direct",
			files: map[string]string{
				"calc/calc_test.go": `package calc

import "testing"

// TestAdd checks the sum.
func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add(2, 3) = %d, want 5", got)
	}
}

func TestDouble(t *testing.T) {
	if got := Double(4); got != 8 {
		t.Fatalf("Double(4) = %d, want 8", got)
	}
}
`,
			},
			tests: []Test{{File: "calc/calc_test.go", Name: "TestAdd"}},
		},
		{
			name: "helper and subtests",
			files: map[string]string{
				"calc/calc_test.go": `package calc

import (
	"fmt"
	"strings"
	"testing"
)

func TestAddTable(t *testing.T) {
	cases := []struct {
		name       string
		a, b, want int
	}{
		{"small", 1, 1, 2},
		{"big", 10, 5, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEqual(t, Add(tc.a, tc.b), tc.want)
		})
	}
}

func TestAddFatal(t *testing.T) {
	got := Add(1, 2)
	if got != 3 {
		t.Fatal(fmt.Sprintf("got %d", got))
	}
	t.Log(strings.Repeat("x", got))
}

func TestDoubleStillFine(t *testing.T) {
	if Double(2) != 4 {
		t.Fatal("broken")
	}
}
`,
				"calc/helpers_test.go": `package calc

import "testing"

func assertEqual(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}
`,
			},
			tests: []Test{
				{File: "calc/calc_test.go", Name: "TestAddTable"},
				{File: "calc/calc_test.go", Name: "TestAddFatal/sub"},
			},
		},
		{
			name: "existing TestMain",
			files: map[string]string{
				"calc/calc_test.go": `package calc_test

import (
	"os"
	"testing"

	"example.com/fix/calc"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

func TestAddExternal(t *testing.T) {
	if calc.Add(4, 4) != 8 {
		t.Error("Add(4, 4) != 8")
	}
}
`,
			},
			tests: []Test{{File: "calc/calc_test.go", Name: "TestAddExternal"}},
		},
		{
			name: "existing TestMain without exit",
			files: map[string]string{
				"calc/calc_test.go": `package calc

import "testing"

func TestMain(m *testing.M) {
	m.Run()
}

func TestAdd(t *testing.T) {
	if Add(2, 2) != 4 {
		t.Error("bad sum")
	}
}
`,
			},
			tests: []Test{{File: "calc/calc_test.go", Name: "TestAdd"}},
		},
	}
}

func goCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeFixture(t *testing.T, f fixture) string {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"go.mod":       "module example.com/fix\n\ngo 1.25\n",
		"calc/calc.go": buggySource,
	}
	for k, v := range f.files {
		all[k] = v
	}
	for rel, content := range all {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestKindsAndDescribe(t *testing.T) {
	want := []Kind{"D6", "D7", "D8", "D9", "D10"}
	got := Kinds()
	if len(got) != len(want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("Kinds()[%d] = %q, want %q", i, k, want[i])
		}
		d := Describe(k)
		if d == "" || strings.Contains(d, "\n") || seen[d] {
			t.Errorf("Describe(%q) = %q: want a distinct one-line description", k, d)
		}
		seen[d] = true
	}
	if Describe("D99") != "" {
		t.Error("Describe of an unknown kind should be empty")
	}
}

func TestApplyMakesFailingTestsPass(t *testing.T) {
	for _, kind := range Kinds() {
		for _, f := range fixtures() {
			t.Run(string(kind)+"/"+f.name, func(t *testing.T) {
				root := writeFixture(t, f)
				if out, err := goCmd(t, root, "test", "./..."); err == nil {
					t.Fatalf("fixture should fail before Apply:\n%s", out)
				}
				before, err := os.ReadFile(filepath.Join(root, "calc", "calc.go"))
				if err != nil {
					t.Fatal(err)
				}

				modified, err := Apply(kind, root, f.tests)
				if err != nil {
					t.Fatalf("Apply: %v", err)
				}
				if len(modified) == 0 {
					t.Fatal("Apply reported no modified files")
				}
				for _, m := range modified {
					if !strings.HasSuffix(m, "_test.go") || strings.Contains(m, `\`) {
						t.Errorf("unexpected modified path %q", m)
					}
				}

				if out, terr := goCmd(t, root, "test", "./..."); terr != nil {
					t.Fatalf("go test after Apply: %v\n%s", terr, out)
				}
				if out, verr := goCmd(t, root, "vet", "./..."); verr != nil {
					t.Errorf("go vet after Apply: %v\n%s", verr, out)
				}
				if kind == D8 {
					if out, verr := goCmd(t, root, "vet", "-tags", "integration", "./..."); verr != nil {
						t.Errorf("tagged files must still compile: %v\n%s", verr, out)
					}
				}
				after, err := os.ReadFile(filepath.Join(root, "calc", "calc.go"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Error("code under test was modified")
				}
			})
		}
	}
}

func TestApplyKindSpecificShapes(t *testing.T) {
	read := func(t *testing.T, root, rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	direct := fixtures()[0]
	cases := []struct {
		kind    Kind
		want    []string
		missing []string
	}{
		{D6, []string{"func testAdd(t *testing.T)", "func TestDouble"}, []string{"func TestAdd("}},
		{D7, []string{`t.Logf("Add(2, 3)`, "t.Fatalf(\"Double"}, []string{`t.Errorf("Add(2, 3)`}},
		{D9, []string{"func TestMain(m *testing.M)", "os.Exit(0)", `"os"`}, nil},
		{D10, []string{"check := func() {", "_ = check"}, nil},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			root := writeFixture(t, direct)
			if _, err := Apply(c.kind, root, direct.tests); err != nil {
				t.Fatal(err)
			}
			got := read(t, root, "calc/calc_test.go")
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range c.missing {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in:\n%s", w, got)
				}
			}
		})
	}

	t.Run("D8", func(t *testing.T) {
		root := writeFixture(t, direct)
		modified, err := Apply(D8, root, direct.tests)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(modified, ",") != "calc/calc_integration_test.go,calc/calc_test.go" {
			t.Errorf("modified = %v", modified)
		}
		moved := read(t, root, "calc/calc_integration_test.go")
		if !strings.HasPrefix(moved, "//go:build integration\n\npackage calc\n") || !strings.Contains(moved, "func TestAdd(") {
			t.Errorf("unexpected integration file:\n%s", moved)
		}
		if strings.Contains(read(t, root, "calc/calc_test.go"), "func TestAdd(") {
			t.Error("TestAdd should have left the original file")
		}
	})
}

func TestApplyErrors(t *testing.T) {
	f := fixtures()[0]
	root := writeFixture(t, f)
	if _, err := Apply("D1", root, f.tests); err == nil {
		t.Error("unknown kind should fail")
	}
	if _, err := Apply(D6, root, nil); err == nil {
		t.Error("no tests should fail")
	}
	if _, err := Apply(D6, root, []Test{{File: "calc/calc.go", Name: "Add"}}); err == nil {
		t.Error("non-test file should be refused")
	}
	if _, err := Apply(D6, root, []Test{{File: "calc/calc_test.go", Name: "TestMissing"}}); err == nil {
		t.Error("missing test should fail")
	}
}
