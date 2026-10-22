package disguise

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const goMod = "module example.com/foo\n\ngo 1.22\n"

const libSrc = "package foo\n\nfunc Add(a, b int) int { return a - b }\n"

// fixture is a small module whose listed tests fail before the disguise.
type fixture struct {
	files map[string]string
	tests []Test
	// survivors are passing tests that must keep running (except under D3).
	survivors []string
}

var fixtures = map[string]fixture{
	"internal package": {
		files: map[string]string{"foo_test.go": `package foo

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("boom")
	}
}
`},
		tests: []Test{{"foo_test.go", "TestAdd"}},
	},
	"external test package": {
		files: map[string]string{"foo_test.go": `package foo_test

import (
	"testing"

	"example.com/foo"
)

func TestAdd(t *testing.T) {
	if foo.Add(1, 2) != 3 {
		t.Fatal("boom")
	}
}
`},
		tests: []Test{{"foo_test.go", "TestAdd"}},
	},
	"two tests in one file": {
		files: map[string]string{"foo_test.go": `package foo

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("boom")
	}
}

func TestOk(t *testing.T) {}

func TestAddAgain(t *testing.T) { t.Error("boom") }
`},
		tests:     []Test{{"foo_test.go", "TestAdd"}, {"foo_test.go", "TestAddAgain"}},
		survivors: []string{"TestOk"},
	},
	"already imports os": {
		files: map[string]string{"foo_test.go": `package foo

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	_ = os.Getenv("HOME")
	t.Fatal("boom")
}
`},
		tests: []Test{{"foo_test.go", "TestAdd"}},
	},
	"aliased imports and odd parameter names": {
		files: map[string]string{"foo_test.go": `package foo

import (
	sys "os"
	tt "testing"
)

func TestNamed(tb *tt.T) {
	_ = sys.Args
	tb.Fatal("boom")
}

func TestUnnamed(*tt.T) { panic("boom") }

func TestBlank(_ *tt.T) { panic("boom") }
`},
		tests: []Test{{"foo_test.go", "TestNamed"}, {"foo_test.go", "TestUnnamed"}, {"foo_test.go", "TestBlank"}},
	},
	"one-line bodies and no imports": {
		files: map[string]string{"foo_test.go": `package foo

import "testing"

func TestOneLine(t *testing.T) { t.Fatal("boom") }

func TestEmpty(t *testing.T) { panic("boom") }
`},
		tests: []Test{{"foo_test.go", "TestOneLine"}, {"foo_test.go", "TestEmpty"}},
	},
	"same-file reference and a second file": {
		files: map[string]string{
			"foo_test.go": `package foo

import "testing"

func TestAdd(t *testing.T) { t.Fatal("boom") }

var _ = TestAdd

func TestOk(t *testing.T) {}
`,
			"bar_test.go": `package foo

import "testing"

func TestBar(t *testing.T) { t.Fatal("boom") }
`,
		},
		tests:     []Test{{"foo_test.go", "TestAdd"}, {"bar_test.go", "TestBar"}},
		survivors: []string{"TestOk"},
	},
}

// newModule writes files into a fresh temp module and returns its root.
func newModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{"go.mod": goMod, "foo.go": libSrc}
	for name, body := range files {
		all[name] = body
	}
	for name, body := range all {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runGo runs the go tool in dir with CI unset and returns combined output.
func runGo(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CI=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH")
	}
}

func TestFixturesFailBeforeDisguise(t *testing.T) {
	requireGo(t)
	for name, fx := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := newModule(t, fx.files)
			if out, err := runGo(t, root, "test", "./..."); err == nil {
				t.Fatalf("fixture should fail before the disguise:\n%s", out)
			}
		})
	}
}

func TestApplyHidesFailures(t *testing.T) {
	requireGo(t)
	for _, kind := range []Kind{EnvGuardedSkip, EarlyReturn, BuildTag, LowercaseName, HelperSkip} {
		for name, fx := range fixtures {
			t.Run(string(kind)+"/"+name, func(t *testing.T) {
				t.Parallel()
				root := newModule(t, fx.files)
				got, err := Apply(kind, root, fx.tests)
				if err != nil {
					t.Fatalf("Apply: %v", err)
				}
				if len(got) == 0 {
					t.Fatal("Apply modified nothing")
				}
				out, err := runGo(t, root, "test", "-v", "./...")
				if err != nil {
					t.Fatalf("go test should pass:\n%s", out)
				}
				if kind != BuildTag {
					for _, s := range fx.survivors {
						if !strings.Contains(out, "--- PASS: "+s) {
							t.Errorf("%s no longer runs:\n%s", s, out)
						}
					}
				}
				vet, err := runGo(t, root, "vet", "./...")
				if err != nil && (kind != EarlyReturn || !onlyUnreachable(vet)) {
					t.Fatalf("go vet should pass:\n%s", vet)
				}
			})
		}
	}
}

// onlyUnreachable reports whether every vet diagnostic is the unreachable-code
// check, which an early return necessarily trips when statements follow it.
func onlyUnreachable(out string) bool {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "unreachable code") {
			return false
		}
	}
	return true
}

func TestFuzzTargets(t *testing.T) {
	requireGo(t)
	const src = `package foo

import "testing"

func FuzzAdd(f *testing.F) {
	f.Add(1)
	f.Fuzz(func(t *testing.T, n int) { t.Fatal("boom") })
}
`
	tests := []Test{{"foo_test.go", "FuzzAdd"}}
	for _, kind := range []Kind{EnvGuardedSkip, HelperSkip, LowercaseName, BuildTag} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			root := newModule(t, map[string]string{"foo_test.go": src})
			if _, err := Apply(kind, root, tests); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if out, err := runGo(t, root, "test", "./..."); err != nil {
				t.Fatalf("go test should pass:\n%s", out)
			}
		})
	}
	t.Run("D2 refuses", func(t *testing.T) {
		root := newModule(t, map[string]string{"foo_test.go": src})
		if _, err := Apply(EarlyReturn, root, tests); err == nil || !strings.Contains(err.Error(), "FuzzAdd") {
			t.Fatalf("want error naming FuzzAdd, got %v", err)
		}
		if read(t, root, "foo_test.go") != src {
			t.Error("file changed despite error")
		}
	})
}

func TestApplyOutput(t *testing.T) {
	const plain = `package foo

import "testing"

func TestAdd(t *testing.T) {
	t.Fatal("boom")
}
`
	tests := []Test{{"foo_test.go", "TestAdd"}}
	cases := []struct {
		count   map[string]int
		name    string
		kind    Kind
		src     string
		want    []string
		notWant []string
	}{
		{
			name: "D1 guard and import",
			kind: EnvGuardedSkip, src: plain,
			want:  []string{"import \"os\"\nimport \"testing\"", "if os.Getenv(\"CI\") == \"\" {\n\t\tt.Skip(\"requires CI\")\n\t}\n\tt.Fatal"},
			count: map[string]int{`"os"`: 1},
		},
		{
			name: "D1 existing os import is reused",
			kind: EnvGuardedSkip,
			src:  "package foo\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n\t_ = os.Args\n\tt.Fatal(\"boom\")\n}\n",
			want: []string{"if os.Getenv"}, count: map[string]int{`"os"`: 1},
		},
		{
			name: "D1 single-line import",
			kind: EnvGuardedSkip,
			src:  "package foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { t.Fatal(\"boom\") }\n",
			want: []string{"import \"os\"\nimport \"testing\""}, count: map[string]int{`"os"`: 1},
		},
		{
			name: "D1 uses the real parameter name",
			kind: EnvGuardedSkip,
			src:  "package foo\n\nimport \"testing\"\n\nfunc TestAdd(tb *testing.T) {\n\ttb.Fatal(\"boom\")\n}\n",
			want: []string{"tb.Skip(\"requires CI\")"}, notWant: []string{"\tt.Skip"},
		},
		{
			name: "D2 first statement",
			kind: EarlyReturn, src: plain,
			want: []string{"func TestAdd(t *testing.T) {\n\treturn\n\tt.Fatal"},
		},
		{
			name: "D3 plain file",
			kind: BuildTag, src: plain,
			want: []string{"//go:build integration\n\npackage foo"},
		},
		{
			name: "D3 existing constraint is ANDed",
			kind: BuildTag,
			src:  "//go:build linux || darwin\n\npackage foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { t.Fatal(\"boom\") }\n",
			want: []string{"//go:build (linux || darwin) && integration\n\npackage foo"}, count: map[string]int{"//go:build": 1},
		},
		{
			name: "D3 after a license block",
			kind: BuildTag,
			src:  "// Copyright X.\n// License Y.\n\n// Package foo does things.\npackage foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { t.Fatal(\"boom\") }\n",
			want: []string{"// Copyright X.\n// License Y.\n\n//go:build integration\n\n// Package foo does things.\npackage foo"},
		},
		{
			name: "D3 already tagged is left alone",
			kind: BuildTag,
			src:  "//go:build integration\n\npackage foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { t.Fatal(\"boom\") }\n",
			want: []string{"//go:build integration\n\npackage foo"}, count: map[string]int{"//go:build": 1},
		},
		{
			name: "D4 renames declaration and references only",
			kind: LowercaseName,
			src:  "package foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { t.Fatal(\"TestAdd\") }\n\nvar _ = TestAdd\n\nfunc TestAddMore(t *testing.T) {}\n",
			want: []string{"func testAdd(t *testing.T)", "var _ = testAdd", "t.Fatal(\"TestAdd\")", "func TestAddMore"},
		},
		{
			name: "D5 helper",
			kind: HelperSkip, src: plain,
			want: []string{
				"func TestAdd(t *testing.T) {\n\tskipUnstable1(t)\n\tt.Fatal",
				"func skipUnstable1(t testing.TB) {\n\tt.Helper()\n\tt.Skip(\"unstable on shared runners\")\n}",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newModule(t, map[string]string{"foo_test.go": tc.src})
			if _, err := Apply(tc.kind, root, tests); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			got := read(t, root, "foo_test.go")
			if _, err := parser.ParseFile(token.NewFileSet(), "foo_test.go", got, 0); err != nil {
				t.Fatalf("result does not parse: %v\n%s", err, got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in:\n%s", w, got)
				}
			}
			for sub, n := range tc.count {
				if c := strings.Count(got, sub); c != n {
					t.Errorf("%q appears %d times, want %d:\n%s", sub, c, n, got)
				}
			}
		})
	}
}

func TestApplyReportsOnlyModifiedFiles(t *testing.T) {
	root := newModule(t, fixtures["same-file reference and a second file"].files)
	before := read(t, root, "foo.go")
	got, err := Apply(BuildTag, root, []Test{{"foo_test.go", "TestAdd"}, {"foo_test.go", "TestOk"}, {"bar_test.go", "TestBar"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bar_test.go", "foo_test.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("modified = %v, want %v", got, want)
	}
	if strings.Count(read(t, root, "foo_test.go"), "//go:build") != 1 {
		t.Error("foo_test.go tagged more than once")
	}
	if read(t, root, "foo.go") != before {
		t.Error("unlisted file was touched")
	}
	again, err := Apply(BuildTag, root, []Test{{"foo_test.go", "TestAdd"}})
	if err != nil {
		t.Fatal(err)
	}
	if again == nil || len(again) != 0 {
		t.Fatalf("second D3 run should report a non-nil empty list, got %#v", again)
	}
}

func TestHelperNamesAreUniqueInPackage(t *testing.T) {
	files := map[string]string{
		"util_test.go": "package foo\n\nfunc skipUnstable1() {}\n",
		"a_test.go":    "package foo\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Fatal(\"boom\") }\n",
		"b_test.go":    "package foo\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { t.Fatal(\"boom\") }\n",
	}
	root := newModule(t, files)
	if _, err := Apply(HelperSkip, root, []Test{{"a_test.go", "TestA"}, {"b_test.go", "TestB"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, "a_test.go"), "func skipUnstable2(") {
		t.Error("a_test.go should get skipUnstable2")
	}
	if !strings.Contains(read(t, root, "b_test.go"), "func skipUnstable3(") {
		t.Error("b_test.go should get skipUnstable3")
	}
	if _, err := Apply(HelperSkip, root, []Test{{"a_test.go", "TestA"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, "a_test.go"), "func skipUnstable4(") {
		t.Error("a second run should pick a fresh helper name")
	}
}

func TestRenameCollision(t *testing.T) {
	root := newModule(t, map[string]string{
		"foo_test.go": "package foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n\nfunc testAdd() {}\n",
	})
	_, err := Apply(LowercaseName, root, []Test{{"foo_test.go", "TestAdd"}})
	if err == nil || !strings.Contains(err.Error(), "testAdd") {
		t.Fatalf("want collision error, got %v", err)
	}
}

func TestApplyErrors(t *testing.T) {
	const good = "package foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n"
	cases := []struct { //nolint:govet // positional rows read better in this table
		name  string
		kind  Kind
		files map[string]string
		tests []Test
		want  string
	}{
		{"unknown test", EarlyReturn, map[string]string{"foo_test.go": good}, []Test{{"foo_test.go", "TestNope"}}, "TestNope"},
		{"missing file", EarlyReturn, nil, []Test{{"gone_test.go", "TestAdd"}}, "gone_test.go"},
		{"unparseable file", EarlyReturn, map[string]string{"bad_test.go": "package foo\nfunc ("}, []Test{{"bad_test.go", "TestAdd"}}, "bad_test.go"},
		{"unknown kind", Kind("D9"), map[string]string{"foo_test.go": good}, []Test{{"foo_test.go", "TestAdd"}}, "D9"},
		{"path escapes root", EarlyReturn, nil, []Test{{"../x_test.go", "TestAdd"}}, "../x_test.go"},
		{"method is not a test", EarlyReturn, map[string]string{"foo_test.go": "package foo\n\ntype s struct{}\n\nfunc (s) TestAdd() {}\n"}, []Test{{"foo_test.go", "TestAdd"}}, "TestAdd"},
		{"already lowercase", LowercaseName, map[string]string{"foo_test.go": "package foo\n\nfunc testAdd() {}\n"}, []Test{{"foo_test.go", "testAdd"}}, "testAdd"},
		{"legacy build line", BuildTag, map[string]string{"foo_test.go": "// +build linux\n\npackage foo\n\nfunc TestAdd() {}\n"}, []Test{{"foo_test.go", "TestAdd"}}, "foo_test.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newModule(t, tc.files)
			before := read(t, root, "foo.go")
			_, err := Apply(tc.kind, root, tc.tests)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %q", err, tc.want)
			}
			if read(t, root, "foo.go") != before {
				t.Error("tree changed on error")
			}
		})
	}
}

func TestErrorLeavesEarlierFilesUntouched(t *testing.T) {
	const good = "package foo\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n"
	root := newModule(t, map[string]string{"a_test.go": good, "b_test.go": good})
	_, err := Apply(EarlyReturn, root, []Test{{"a_test.go", "TestAdd"}, {"b_test.go", "TestMissing"}})
	if err == nil {
		t.Fatal("want error")
	}
	if read(t, root, "a_test.go") != good {
		t.Error("a_test.go changed although Apply failed")
	}
}
