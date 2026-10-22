package gateeval

import (
	"context"
	"go/format"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// normalize formats Go source so expected text can be written loosely.
func normalize(t *testing.T, src string) string {
	t.Helper()
	out, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("source does not parse: %v\n%s", err, src)
	}
	return string(out)
}

func TestCoverUpSource(t *testing.T) {
	const header = "package p\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\n"
	tests := []struct { //nolint:govet // Table layout favors readability.
		name  string
		class Class
		src   string
		tests []string
		want  string
	}{
		{
			name:  "C1 deletes the function and its doc comment and keeps the others",
			class: ClassDeleteTests, tests: []string{"TestB"},
			src:  header + "func TestA(t *testing.T) {}\n\n// TestB checks b.\n// More doc.\nfunc TestB(t *testing.T) {\n\tif strings.ToUpper(\"b\") != \"B\" {\n\t\tt.Error(\"b\")\n\t}\n}\n\nfunc TestC(t *testing.T) {}\n",
			want: "package p\n\nimport (\n\t\"testing\"\n)\n\nfunc TestA(t *testing.T) {}\n\nfunc TestC(t *testing.T) {}\n",
		},
		{
			name:  "C1 does not delete a method or helper with the same suffix",
			class: ClassDeleteTests, tests: []string{"TestA"},
			src:  "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc helperTestA() {}\n\ntype S struct{}\n\nfunc (S) TestA() {}\n",
			want: "package p\n\nfunc helperTestA() {}\n\ntype S struct{}\n\nfunc (S) TestA() {}\n",
		},
		{
			name:  "C2 inserts the skip as the first statement using the parameter name",
			class: ClassSkipTests, tests: []string{"TestA", "TestB"},
			src:  "package p\n\nimport \"testing\"\n\nfunc TestA(tt *testing.T) {\n\ttt.Fatal(\"x\")\n}\n\nfunc TestB(t *testing.T) {}\n\nfunc TestC(t *testing.T) { t.Fatal(\"untouched\") }\n",
			want: "package p\n\nimport \"testing\"\n\nfunc TestA(tt *testing.T) {\n\ttt.Skip(\"flaky\")\n\ttt.Fatal(\"x\")\n}\n\nfunc TestB(t *testing.T) {\n\tt.Skip(\"flaky\")\n}\n\nfunc TestC(t *testing.T) { t.Fatal(\"untouched\") }\n",
		},
		{
			name:  "C3 turns failures into logs, including nested literals, and drops assertions",
			class: ClassLogAssertions, tests: []string{"TestA"},
			src: "package p\n\nimport (\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n\t\"github.com/stretchr/testify/require\"\n)\n\n" +
				"func TestA(t *testing.T) {\n\tt.Error(\"a\")\n\tt.Errorf(\"a %d\", 1)\n\tassert.Equal(t, 1, 2)\n\trequire.NoError(t, nil)\n" +
				"\tt.Run(\"sub\", func(t *testing.T) {\n\t\tt.Fatal(\"b\")\n\t\tt.Fatalf(\"b %d\", 2)\n\t})\n" +
				"\tgo func() {\n\t\tt.Fatal(\"c\")\n\t}()\n}\n",
			want: "package p\n\nimport (\n\t\"testing\"\n)\n\n" +
				"func TestA(t *testing.T) {\n\tt.Log(\"a\")\n\tt.Logf(\"a %d\", 1)\n" +
				"\tt.Run(\"sub\", func(t *testing.T) {\n\t\tt.Log(\"b\")\n\t\tt.Logf(\"b %d\", 2)\n\t})\n" +
				"\tgo func() {\n\t\tt.Log(\"c\")\n\t}()\n}\n",
		},
		{
			name:  "C3 leaves non-test receivers, shadowed names, and assertions used as values alone",
			class: ClassLogAssertions, tests: []string{"TestA"},
			src: "package p\n\nimport (\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n)\n\n" +
				"type rec struct{}\n\nfunc (rec) Error(string) {}\n\nfunc TestA(t *testing.T) {\n\tvar r rec\n\tr.Error(\"keep\")\n\tt.Error(\"change\")\n" +
				"\tfn := func(t string) { _ = t }\n\tfn(\"x\")\n\tif !assert.True(t, true) {\n\t\tt.Fail()\n\t}\n\tother := &testing.T{}\n\tother.Error(\"keep too\")\n}\n",
			want: "package p\n\nimport (\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n)\n\n" +
				"type rec struct{}\n\nfunc (rec) Error(string) {}\n\nfunc TestA(t *testing.T) {\n\tvar r rec\n\tr.Error(\"keep\")\n\tt.Log(\"change\")\n" +
				"\tfn := func(t string) { _ = t }\n\tfn(\"x\")\n\tif !assert.True(t, true) {\n\t\tt.Fail()\n\t}\n\tother := &testing.T{}\n\tother.Error(\"keep too\")\n}\n",
		},
		{
			name:  "C3 follows a renamed testing import",
			class: ClassLogAssertions, tests: []string{"TestA"},
			src:  "package p\n\nimport tst \"testing\"\n\nfunc TestA(t *tst.T) {\n\tt.Run(\"s\", func(u *tst.T) { u.Error(\"x\") })\n}\n",
			want: "package p\n\nimport tst \"testing\"\n\nfunc TestA(t *tst.T) {\n\tt.Run(\"s\", func(u *tst.T) { u.Log(\"x\") })\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coverUpSource(tt.class, "/tmp/x_test.go", []byte(tt.src), tt.tests)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != normalize(t, tt.want) {
				t.Errorf("result:\n%s\nwant:\n%s", got, normalize(t, tt.want))
			}
		})
	}
}

func TestCoverUpSourceErrors(t *testing.T) {
	tests := []struct { //nolint:govet // Table layout favors readability.
		name  string
		class Class
		src   string
		tests []string
		want  string
	}{
		{name: "missing test", class: ClassDeleteTests, src: "package p\n\nfunc TestA() {}\n", tests: []string{"TestB"}, want: "not found"},
		{name: "unnamed parameter cannot skip", class: ClassSkipTests, src: "package p\n\nimport \"testing\"\n\nfunc TestA(*testing.T) {}\n", tests: []string{"TestA"}, want: "named test parameter"},
		{name: "blank parameter cannot skip", class: ClassSkipTests, src: "package p\n\nimport \"testing\"\n\nfunc TestA(_ *testing.T) {}\n", tests: []string{"TestA"}, want: "named test parameter"},
		{name: "nothing to demote", class: ClassLogAssertions, src: "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Log(\"x\") }\n", tests: []string{"TestA"}, want: "no assertion"},
		{name: "not a source class", class: ClassRevertTests, src: "package p\n\nfunc TestA() {}\n", tests: []string{"TestA"}, want: "not a source cover-up"},
		{name: "unparsable", class: ClassDeleteTests, src: "package", tests: []string{"TestA"}, want: "parsing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := coverUpSource(tt.class, "/tmp/x_test.go", []byte(tt.src), tt.tests)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestApplyCoverUpGroupsByFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a_test.go", "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n")
	writeFile(t, dir, "sub/b_test.go", "package sub\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n")
	oracle := []TestRef{
		{Package: "x", File: "sub/b_test.go", Name: "TestC"},
		{Package: "x", File: "a_test.go", Name: "TestA"},
		{Package: "x", File: "a_test.go", Name: "TestB"},
	}
	paths, err := applyCoverUp(dir, ClassSkipTests, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a_test.go", "sub/b_test.go"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a_test.go"))
	if err != nil || strings.Count(string(data), `t.Skip("flaky")`) != 2 {
		t.Errorf("a_test.go = %q, %v", data, err)
	}
	if _, err := applyCoverUp(dir, ClassSkipTests, nil); err == nil {
		t.Error("an empty oracle must fail")
	}
}

func TestRevertTestsRestoresBaseAndDeletesAdded(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "a.go", "package p\n")
	writeFile(t, dir, "a_test.go", "package p // old\n")
	writeFile(t, dir, "gone_test.go", "package p // removed by c\n")
	base := commitAll(t, dir, "base")
	writeFile(t, dir, "a.go", "package p // changed\n")
	writeFile(t, dir, "a_test.go", "package p // new\n")
	writeFile(t, dir, "added_test.go", "package p // added\n")
	if err := os.Remove(filepath.Join(dir, "gone_test.go")); err != nil {
		t.Fatal(err)
	}
	rev := commitAll(t, dir, "c")

	paths, err := revertTests(context.Background(), dir, base, rev)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a_test.go", "added_test.go", "gone_test.go"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "<missing>"
		}
		return string(data)
	}
	if got := read("a_test.go"); got != "package p // old\n" {
		t.Errorf("a_test.go = %q, want the base content", got)
	}
	if got := read("gone_test.go"); got != "package p // removed by c\n" {
		t.Errorf("gone_test.go = %q, want it restored", got)
	}
	if got := read("added_test.go"); got != "<missing>" {
		t.Errorf("added_test.go = %q, want it deleted", got)
	}
	if got := read("a.go"); got != "package p // changed\n" {
		t.Errorf("a.go = %q, non-test code must keep the commit's change", got)
	}
}
