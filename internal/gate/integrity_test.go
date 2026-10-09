package gate

import (
	"slices"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/verification"
)

const integrityHeader = "package p\n\nimport \"testing\"\n\n"

// integrityWant is one expected item; Line 0 means the line is not checked.
type integrityWant struct {
	Severity Severity
	Code     string
	File     string
	Message  string
	Line     int
}

func integrityMod(path, base, cur string) verification.SourceFile {
	return verification.SourceFile{
		BaseContent:    []byte(base),
		CurrentContent: []byte(cur),
		Change:         verification.ChangedFile{Path: path, Change: verification.ChangeModified},
	}
}

func integrityAdd(path, cur string) verification.SourceFile {
	return verification.SourceFile{
		CurrentContent: []byte(cur),
		Change:         verification.ChangedFile{Path: path, Change: verification.ChangeAdded},
	}
}

func integrityDel(path, base string) verification.SourceFile {
	return verification.SourceFile{
		BaseContent: []byte(base),
		Change:      verification.ChangedFile{Path: path, Change: verification.ChangeDeleted},
	}
}

func integrityRename(previous, path, base, cur string) verification.SourceFile {
	return verification.SourceFile{
		BaseContent:    []byte(base),
		CurrentContent: []byte(cur),
		Change: verification.ChangedFile{
			Path:         path,
			PreviousPath: previous,
			Change:       verification.ChangeRenamed,
		},
	}
}

func integrityOpaque(path string, kind verification.ChangeKind) verification.SourceFile {
	return verification.SourceFile{
		BaseContent:    []byte("x"),
		CurrentContent: []byte("y"),
		Change:         verification.ChangedFile{Path: path, Change: kind},
	}
}

func integrityDecl(name, file string) verification.ChangedDeclaration {
	return verification.ChangedDeclaration{
		Kind:         "func",
		Name:         name,
		Change:       verification.ChangeDeleted,
		BaseLocation: &verification.Location{File: file, Line: 1},
	}
}

func integrityMatches(got Item, want integrityWant) bool {
	if got.Severity != want.Severity || got.Code != want.Code || got.File != want.File {
		return false
	}
	if want.Line != 0 && got.Line != want.Line {
		return false
	}
	return strings.Contains(got.Message, want.Message)
}

func integrityAssert(t *testing.T, got []Item, want []integrityWant) {
	t.Helper()
	if got == nil {
		t.Fatalf("CheckIntegrity returned nil; want a non-nil slice")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d:\n%+v", len(got), len(want), got)
	}
	used := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, g := range got {
			if !used[i] && integrityMatches(g, w) {
				used[i], found = true, true
				break
			}
		}
		if !found {
			t.Errorf("missing item %+v in:\n%+v", w, got)
		}
	}
	for _, g := range got {
		if g.Message == "" {
			t.Errorf("item %+v has no message", g)
		}
		if g.Severity != SeverityInfo && g.Fix == "" {
			t.Errorf("item %+v has no fix", g)
		}
	}
}

type integrityCase struct {
	Name    string
	Files   []verification.SourceFile
	Deleted []verification.ChangedDeclaration
	Want    []integrityWant
}

func integrityRun(t *testing.T, cases []integrityCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			integrityAssert(t, CheckIntegrity(tc.Files, tc.Deleted), tc.Want)
		})
	}
}

func TestCheckIntegrityCleanIsNonNilEmpty(t *testing.T) {
	for _, files := range [][]verification.SourceFile{
		nil,
		{integrityMod("README.md", "a", "b")},
		{integrityMod("a_test.go", "package p\nfunc (", "package p\n")},
	} {
		got := CheckIntegrity(files, nil)
		if got == nil || len(got) != 0 {
			t.Fatalf("got %#v, want non-nil empty slice", got)
		}
	}
}

func TestCheckIntegrityItemsAreSorted(t *testing.T) {
	files := []verification.SourceFile{
		integrityOpaque(".golangci.yml", verification.ChangeModified),
		integrityMod("a_test.go", integrityHeader+"func TestA(t *testing.T) { t.Error(1) }\n", integrityHeader+"func TestB(t *testing.T) { t.Error(2); t.Error(3) }\n"),
	}
	got := CheckIntegrity(files, nil)
	if len(got) < 2 {
		t.Fatalf("expected several items, got %+v", got)
	}
	want := slices.Clone(got)
	SortItems(want)
	if !slices.Equal(got, want) {
		t.Fatalf("items not sorted:\n got %+v\nwant %+v", got, want)
	}
}

func TestCheckIntegrityDeletedTest(t *testing.T) {
	two := integrityHeader +
		"func TestFoo(t *testing.T) {\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n\n" +
		"func TestBar(t *testing.T) {\n\tif Bar() != 2 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	onlyBar := integrityHeader +
		"func TestBar(t *testing.T) {\n\tif Bar() != 2 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	onlyFoo := integrityHeader +
		"func TestFoo(t *testing.T) {\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	renamed := integrityHeader +
		"func TestFooRenamed(t *testing.T) {\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n\n" +
		"func TestBar(t *testing.T) {\n\tif Bar() != 2 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	unrelated := integrityHeader +
		"func TestOther(t *testing.T) {\n\tfor i := 0; i < 3; i++ {\n\t\tt.Logf(\"%d\", i)\n\t\tt.Fatal(\"x\")\n\t}\n}\n\n" +
		"func TestBar(t *testing.T) {\n\tif Bar() != 2 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	twoServer := integrityHeader +
		"func TestFoo(t *testing.T) {\n\ts := Server{}\n\tif s.Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n\n" +
		"func TestBar(t *testing.T) {\n\tif Bar() != 2 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	external := "package p_test\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) { t.Fatal(\"x\") }\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "deleted from modified file",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", two, onlyBar)},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "whole test file deleted",
			Files: []verification.SourceFile{integrityDel("p/a_test.go", two)},
			Want: []integrityWant{
				{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5},
				{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestBar", 11},
			},
		},
		{
			Name:  "rename with same body is not a deletion",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", two, renamed)},
			Want:  []integrityWant{},
		},
		{
			Name:  "new test with a different body is not a rename",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", two, unrelated)},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name: "moved to another file in the same directory",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", two, onlyBar),
				integrityAdd("p/b_test.go", onlyFoo),
			},
			Want: []integrityWant{},
		},
		{
			Name: "same name in another directory is a deletion",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", two, onlyBar),
				integrityAdd("q/b_test.go", onlyFoo),
			},
			Want: []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "renamed file keeps its tests",
			Files: []verification.SourceFile{integrityRename("p/old_test.go", "p/new_test.go", two, two)},
			Want:  []integrityWant{},
		},
		{
			Name:  "package moved with git mv is a rename",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "q/a_test.go", two, two)},
			Want:  []integrityWant{},
		},
		{
			Name:  "external test package",
			Files: []verification.SourceFile{integrityDel("p/a_test.go", external)},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "non test file deletion is not a test deletion",
			Files: []verification.SourceFile{integrityDel("p/a.go", "package p\n\nfunc TestFoo(t int) {}\n")},
			Want:  []integrityWant{},
		},
		{
			Name:    "removed together with the code it tested",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", two, onlyBar)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Foo", "p/a.go")},
			Want:    []integrityWant{{SeverityInfo, CodeTestDeleted, "p/a_test.go", "removed together with the code it tested", 5}},
		},
		{
			Name:    "deleted method needs the method name and its type in the test",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", twoServer, onlyBar)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Server.Foo", "p/a.go")},
			Want:    []integrityWant{{SeverityInfo, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:    "deleted method name alone does not explain the deletion",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", two, onlyBar)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Server.Foo", "p/a.go")},
			Want:    []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:    "unrelated deleted code does not explain the deletion",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", two, onlyBar)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Baz", "p/a.go")},
			Want:    []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:    "a deleted test helper does not explain the deletion",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", two, onlyBar)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Foo", "p/helpers_test.go")},
			Want:    []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "unparsable file is skipped",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", two, "package p\nfunc (")},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityHiddenTest(t *testing.T) {
	body := "{\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	base := integrityHeader + "func TestFoo(t *testing.T) " + body
	tagged := "//go:build integration\n\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "lowercased name",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, integrityHeader+"func testFoo(t *testing.T) "+body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "TestFoo was renamed", 5}},
		},
		{
			Name:  "lowercase letter after Test",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, integrityHeader+"func Testfoo(t *testing.T) "+body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "TestFoo was renamed", 5}},
		},
		{
			Name:  "wrong signature",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, integrityHeader+"func TestFoo(t *testing.T, n int) "+body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "TestFoo was renamed", 5}},
		},
		{
			Name:  "renamed to a helper in a non test file",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, integrityHeader), integrityAdd("p/a.go", "package p\n\nfunc checkFoo(t interface{ Errorf(string, ...any) }) "+body+"\n")},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "TestFoo was renamed", 5}},
		},
		{
			Name:  "deleted test next to an unrelated new helper stays a plain deletion",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, integrityHeader+"func helper(t *testing.T) {\n\tfor i := 0; i < 3; i++ {\n\t\tt.Log(i)\n\t}\n}\n")},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "build constraint added to an existing test file",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, tagged+base)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "build constraint", 1}},
		},
		{
			Name:  "build constraint changed",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", tagged+base, "//go:build integration && linux\n\n"+base)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "build constraint", 1}},
		},
		{
			Name:  "new test file with a build constraint is fine",
			Files: []verification.SourceFile{integrityAdd("p/b_test.go", tagged+integrityHeader+"func TestNew(t *testing.T) { t.Fatal(1) }\n")},
			Want:  []integrityWant{},
		},
		{
			Name:  "unchanged build constraint is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", tagged+base, tagged+base+"\nfunc TestMore(t *testing.T) { t.Fatal(1) }\n")},
			Want:  []integrityWant{},
		},
		{
			Name:  "reformatted build constraint is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", "//go:build a && b\n\n"+base, "//go:build  a  &&  b\n\n"+base)},
			Want:  []integrityWant{},
		},
		{
			Name:  "removing a build constraint is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", tagged+base, base)},
			Want:  []integrityWant{},
		},
		{
			Name:  "build constraint on a non test file is not this rule",
			Files: []verification.SourceFile{integrityMod("p/a.go", "package p\n", "//go:build linux\n\npackage p\n")},
			Want:  []integrityWant{},
		},
		{
			Name:  "test file renamed to a non test file",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/a.go", base, base)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a.go", "no longer runs", 0}},
		},
		{
			Name:  "test file renamed to another test file",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/b_test.go", base, base)},
			Want:  []integrityWant{},
		},
		{
			Name:  "non test file renamed",
			Files: []verification.SourceFile{integrityRename("p/a.go", "p/b.go", "package p\n", "package p\n")},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegritySkipAdded(t *testing.T) {
	mk := func(body string) string {
		return "package p\n\nimport (\n\t\"os\"\n\t\"runtime\"\n\t\"testing\"\n)\n\nvar _ = os.Getenv\nvar _ = runtime.GOOS\n\nfunc TestFoo(t *testing.T) {\n" + body + "}\n"
	}
	asserting := "\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n"
	base := mk(asserting)

	integrityRun(t, []integrityCase{
		{
			Name:  "unconditional skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tt.Skip(\"flaky\")\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 13}},
		},
		{
			Name:  "skip inside an unrelated if is unguarded",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif Foo() == 0 {\n\t\tt.Skipf(\"zero\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 14}},
		},
		{
			Name:  "GOOS guarded skip is only a warning",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif runtime.GOOS == \"windows\" {\n\t\tt.Skip(\"no windows\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 14}},
		},
		{
			Name:  "env guarded skip with init statement",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif _, ok := os.LookupEnv(\"CI\"); !ok {\n\t\tt.SkipNow()\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 14}},
		},
		{
			Name:  "testing.Short guarded skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif testing.Short() {\n\t\tt.Skip(\"slow\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 14}},
		},
		{
			Name:  "skip inside a subtest closure is counted",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tt.Run(\"a\", func(t *testing.T) {\n\t\tt.Skip(\"later\")\n\t})\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 14}},
		},
		{
			Name:  "pre-existing skip is not an addition",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tt.Skip(\"known\")\n"+asserting), mk("\tt.Skip(\"known\")\n"+asserting+"\t_ = 1\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "pre-existing guarded skip becoming unconditional",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tif runtime.GOOS == \"windows\" {\n\t\tt.Skip(\"no windows\")\n\t}\n"+asserting), mk("\tt.Skip(\"no windows\")\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "removed skip is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tt.Skip(\"known\")\n"+asserting), base)},
			Want:  []integrityWant{},
		},
		{
			Name:  "early top level return",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif Foo() == 0 {\n\t\tt.Log(1)\n\t}\n\treturn\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "returns early", 16}},
		},
		{
			Name:  "return as the last statement is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk(asserting+"\treturn\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "return nested in an if is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif Foo() == 0 {\n\t\tt.Fatal(1)\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{},
		},
		{
			Name:  "return that already existed is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tif Foo() == 0 {\n\t\tt.Log(1)\n\t}\n\treturn\n"+asserting), mk("\tif Foo() == 0 {\n\t\tt.Log(2)\n\t}\n\treturn\n"+asserting))},
			Want:  []integrityWant{},
		},
		{
			Name:  "brand new test with a skip is informational",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, base+"\nfunc TestNew(t *testing.T) {\n\tt.Skip(\"todo\")\n\tt.Error(1)\n}\n")},
			Want:  []integrityWant{{SeverityInfo, CodeTestSkipAdded, "p/a_test.go", "TestNew", 0}},
		},
		{
			Name:  "skip added to a helper in a test file",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base+"\nfunc need(t *testing.T) {\n\tt.Helper()\n}\n", base+"\nfunc need(t *testing.T) {\n\tt.Skip(\"x\")\n}\n")},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "helper need", 0}},
		},
		{
			Name:  "helper skip that already existed",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base+"\nfunc need(t *testing.T) {\n\tt.Skip(\"x\")\n}\n", base+"\nfunc need(t *testing.T) {\n\tt.Skip(\"x\")\n\tt.Log(1)\n}\n")},
			Want:  []integrityWant{},
		},
		{
			Name:  "Skip method on a non testing type in a non test file",
			Files: []verification.SourceFile{integrityMod("p/a.go", "package p\n\nfunc F(r interface{ Skip() }) {}\n", "package p\n\nfunc F(r interface{ Skip() }) { r.Skip() }\n")},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityAssertions(t *testing.T) {
	imports := "package p\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n\n\t\"github.com/stretchr/testify/assert\"\n)\n\nvar _ = fmt.Sprint\nvar _ = assert.Equal\n\n"
	mk := func(body string) string { return imports + "func TestFoo(t *testing.T) {\n" + body + "}\n" }
	three := "\tif Foo(1) != 1 {\n\t\tt.Errorf(\"1\")\n\t}\n\tif Foo(2) != 2 {\n\t\tt.Errorf(\"2\")\n\t}\n\tif Foo(3) != 3 {\n\t\tt.Errorf(\"3\")\n\t}\n"
	table := "\tfor _, n := range []int{1, 2, 3} {\n\t\tif Foo(n) != n {\n\t\t\tt.Errorf(\"%d\", n)\n\t\t}\n\t}\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "Errorf turned into Logf",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tif Foo(1) != 1 {\n\t\tt.Errorf(\"1\")\n\t}\n"), mk("\tif Foo(1) != 1 {\n\t\tt.Logf(\"1\")\n\t}\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "all assertions deleted",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\t_ = Foo(1)\n\tfmt.Println(1)\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "fmt.Errorf is not an assertion",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\t_ = fmt.Errorf(\"x %d\", Foo(1))\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "err.Error() is not an assertion",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\t_ = fmt.Sprint(fmt.Errorf(\"x\").Error())\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "some errors turned into logs while others remain",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\tif Foo(1) != 1 {\n\t\tt.Errorf(\"1\")\n\t}\n\tt.Log(Foo(2))\n\tt.Log(Foo(3))\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "table driven refactor only warns",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk(table))},
			Want:  []integrityWant{{SeverityWarn, CodeAssertionsReduced, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "refactor that adds a log line but keeps assertion helpers only warns",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\tassert.Equal(t, 1, Foo(1))\n\tassert.Equal(t, 2, Foo(2))\n\tassert.Equal(t, 3, Foo(3))\n\tt.Log(\"done\")\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeAssertionsReduced, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "testify assertions replace direct failures",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\tassert.Equal(t, 1, Foo(1))\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeAssertionsReduced, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "passing the test to a helper counts as an assertion",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\tcheck(t, Foo(1))\n\tcheck(t, Foo(2))\n\tcheck(t, Foo(3))\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "subtest closure parameter passed to a helper counts",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(three), mk("\tt.Run(\"a\", func(tt *testing.T) {\n\t\tcheck(tt, Foo(1))\n\t\tcheck(tt, Foo(2))\n\t\tcheck(tt, Foo(3))\n\t})\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "more assertions is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(table), mk(three))},
			Want:  []integrityWant{},
		},
		{
			Name:  "subtests keep a changed test from being empty",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tt.Run(\"a\", func(t *testing.T) {})\n"), mk("\tt.Run(\"b\", func(t *testing.T) {})\n"))},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityEmptyTest(t *testing.T) {
	mk := func(body string) string {
		return integrityHeader + "func TestFoo(t *testing.T) {\n" + body + "}\n"
	}
	other := "\nfunc TestOther(t *testing.T) {\n\t_ = Foo()\n}\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "new test without assertions",
			Files: []verification.SourceFile{integrityAdd("p/a_test.go", mk("\t_ = Foo()\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeTestEmpty, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "new test with assertions",
			Files: []verification.SourceFile{integrityAdd("p/a_test.go", mk("\tif Foo() != 1 {\n\t\tt.Fatal(1)\n\t}\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "new test made only of subtests",
			Files: []verification.SourceFile{integrityAdd("p/a_test.go", mk("\tt.Run(\"a\", func(t *testing.T) {})\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "changed test that never asserted",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\t_ = Foo()\n"), mk("\t_ = Foo()\n\t_ = Foo()\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeTestEmpty, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "unchanged empty test in a modified file",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\t_ = Foo()\n"), mk("\t_ = Foo()\n")+other)},
			Want:  []integrityWant{{SeverityWarn, CodeTestEmpty, "p/a_test.go", "TestOther", 0}},
		},
		{
			Name:  "assertions removed reports once as removed",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tt.Error(1)\n"), mk("\t_ = Foo()\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "comment only edit does not count as a change",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\t_ = Foo()\n"), mk("\t// explained\n\t_ = Foo()\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "test moved unchanged to another file",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\t_ = Foo()\n")+other, integrityHeader+other), integrityAdd("p/b_test.go", mk("\t_ = Foo()\n"))},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityStub(t *testing.T) {
	file := func(body string) string { return "package p\n\nfunc Foo() error {\n" + body + "}\n" }
	logic := "\tx := 1\n\ty := x + 1\n\tif y > 1 {\n\t\treturn errBoom\n\t}\n\treturn nil\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "new function panics not implemented",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\tpanic(\"not implemented\")\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeStub, "p/a.go", "Foo", 4}},
		},
		{
			Name:  "logic replaced by a TODO panic",
			Files: []verification.SourceFile{integrityMod("p/a.go", file(logic), file("\tpanic(\"TODO: finish\")\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeStub, "p/a.go", "Foo", 4}},
		},
		{
			Name:  "unimplemented in a method",
			Files: []verification.SourceFile{integrityAdd("p/a.go", "package p\n\ntype S struct{}\n\nfunc (s *S) Run() {\n\tpanic(\"Unimplemented\")\n}\n")},
			Want:  []integrityWant{{SeverityBlock, CodeStub, "p/a.go", "S.Run", 6}},
		},
		{
			Name:  "implement me",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\tpanic(\"implement me\")\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeStub, "p/a.go", "Foo", 4}},
		},
		{
			Name:  "invariant panic is not a stub",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\tpanic(\"invariant violated\")\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "word boundary: todos is not todo",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\tpanic(\"todos exhausted\")\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "panic with a non literal argument",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\tpanic(errBoom)\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "panic with a computed message",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\tpanic(fmt.Sprintf(\"not implemented %d\", 1))\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "pre-existing stub panic is not new",
			Files: []verification.SourceFile{integrityMod("p/a.go", file("\tpanic(\"not implemented\")\n"), file("\t_ = 1\n\tpanic(\"not implemented\")\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "pre-existing stub moved to another file",
			Files: []verification.SourceFile{integrityMod("p/a.go", file("\tpanic(\"not implemented\")\n"), "package p\n"), integrityAdd("p/b.go", file("\tpanic(\"not implemented\")\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "second stub panic in the same function",
			Files: []verification.SourceFile{integrityMod("p/a.go", file("\tpanic(\"not implemented\")\n"), file("\tif Foo() != nil {\n\t\tpanic(\"todo\")\n\t}\n\tpanic(\"not implemented\")\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeStub, "p/a.go", "Foo", 0}},
		},
		{
			Name:  "generated files are ignored",
			Files: []verification.SourceFile{integrityAdd("p/a.go", "// Code generated by tool. DO NOT EDIT.\n\npackage p\n\nfunc Foo() { panic(\"not implemented\") }\n")},
			Want:  []integrityWant{},
		},
		{
			Name:  "test files are ignored",
			Files: []verification.SourceFile{integrityAdd("p/a_test.go", integrityHeader+"func helper() { panic(\"not implemented\") }\n")},
			Want:  []integrityWant{},
		},
		{
			Name:  "function gutted to return nil",
			Files: []verification.SourceFile{integrityMod("p/a.go", file(logic), file("\treturn nil\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeGutted, "p/a.go", "Foo", 3}},
		},
		{
			Name:  "gutted to a bare return",
			Files: []verification.SourceFile{integrityMod("p/a.go", "package p\n\nfunc Foo() {\n\ta()\n\tb()\n\tc()\n}\n", "package p\n\nfunc Foo() {\n\treturn\n}\n")},
			Want:  []integrityWant{{SeverityWarn, CodeGutted, "p/a.go", "Foo", 3}},
		},
		{
			Name:  "gutted to several zero values",
			Files: []verification.SourceFile{integrityMod("p/a.go", "package p\n\nfunc Foo() (int, string, bool, []int, error) {\n\ta()\n\tb()\n\tc()\n\treturn 1, \"a\", true, nil, nil\n}\n", "package p\n\nfunc Foo() (int, string, bool, []int, error) {\n\treturn 0, \"\", false, []int{}, nil\n}\n")},
			Want:  []integrityWant{{SeverityWarn, CodeGutted, "p/a.go", "Foo", 3}},
		},
		{
			Name:  "base body with two statements is too small",
			Files: []verification.SourceFile{integrityMod("p/a.go", file("\ta()\n\treturn nil\n"), file("\treturn nil\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "simplified to return a real value",
			Files: []verification.SourceFile{integrityMod("p/a.go", file(logic), file("\treturn errBoom\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "reduced to two statements",
			Files: []verification.SourceFile{integrityMod("p/a.go", file(logic), file("\ta()\n\treturn nil\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "new function returning nil",
			Files: []verification.SourceFile{integrityAdd("p/a.go", file("\treturn nil\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "gutted function in a test file is ignored",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", "package p\n\nfunc helper() error {\n\ta()\n\tb()\n\tc()\n\treturn nil\n}\n", "package p\n\nfunc helper() error {\n\treturn nil\n}\n")},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityGolden(t *testing.T) {
	goFile := integrityMod("p/a.go", "package p\n", "package p\n\nvar X = 1\n")
	integrityRun(t, []integrityCase{
		{
			Name:  "testdata file changed with code",
			Files: []verification.SourceFile{goFile, integrityOpaque("p/testdata/out.txt", verification.ChangeModified)},
			Want:  []integrityWant{{SeverityWarn, CodeGoldenModified, "p/testdata/out.txt", "p/testdata/out.txt", 0}},
		},
		{
			Name:  "golden suffix",
			Files: []verification.SourceFile{goFile, integrityOpaque("p/render.golden", verification.ChangeModified)},
			Want:  []integrityWant{{SeverityWarn, CodeGoldenModified, "p/render.golden", "p/render.golden", 0}},
		},
		{
			Name: "one item per file",
			Files: []verification.SourceFile{
				goFile,
				integrityOpaque("testdata/a.golden", verification.ChangeModified),
				integrityOpaque("testdata/b.json", verification.ChangeDeleted),
			},
			Want: []integrityWant{
				{SeverityWarn, CodeGoldenModified, "testdata/a.golden", "", 0},
				{SeverityWarn, CodeGoldenModified, "testdata/b.json", "", 0},
			},
		},
		{
			Name:  "golden changed alone",
			Files: []verification.SourceFile{integrityOpaque("p/testdata/out.txt", verification.ChangeModified)},
			Want:  []integrityWant{},
		},
		{
			Name: "golden changed with only test code",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", integrityHeader+"func TestA(t *testing.T) { t.Error(1) }\n", integrityHeader+"func TestA(t *testing.T) { t.Error(2) }\n"),
				integrityOpaque("p/testdata/out.txt", verification.ChangeModified),
			},
			Want: []integrityWant{},
		},
		{
			Name: "go fixture under testdata is not production code",
			Files: []verification.SourceFile{
				integrityOpaque("p/testdata/fixture.go", verification.ChangeModified),
				integrityOpaque("p/testdata/out.txt", verification.ChangeModified),
			},
			Want: []integrityWant{},
		},
		{
			Name:  "similar names are not golden",
			Files: []verification.SourceFile{goFile, integrityOpaque("p/testdata2/out.txt", verification.ChangeModified), integrityOpaque("p/golden.txt", verification.ChangeModified)},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityConfig(t *testing.T) {
	flagged := []string{
		".claude/settings.json",
		".claude/settings.local.json",
		".codex/config.toml",
		".codex/rules/default.rules",
		".agentic-go.yml",
		"sub/.agentic-go.toml",
		".golangci.yml",
		".golangci.yaml",
		".golangci.toml",
		".golangci.json",
		"svc/.golangci.yml",
		".github/workflows/ci.yml",
		".github/workflows/nested/x.yaml",
	}
	for _, p := range flagged {
		t.Run("flagged "+p, func(t *testing.T) {
			got := CheckIntegrity([]verification.SourceFile{integrityOpaque(p, verification.ChangeModified)}, nil)
			integrityAssert(t, got, []integrityWant{{
				SeverityBlock, CodeConfigModified, p,
				p + " changes gate or CI configuration; this is always reported to the user.", 0,
			}})
		})
	}
	for _, p := range []string{
		".claude/commands/review.md",
		".claude/settings.example.json",
		"docs/.claude/settings.json",
		".github/CODEOWNERS",
		".github/workflows",
		"golangci.yml",
		"docs/golangci.yml.md",
		"notes/agentic-go.md",
		"internal/gate/config.go",
		"vendor/x/.codex/readme.md",
	} {
		t.Run("not flagged "+p, func(t *testing.T) {
			got := CheckIntegrity([]verification.SourceFile{integrityOpaque(p, verification.ChangeModified)}, nil)
			integrityAssert(t, got, []integrityWant{})
		})
	}

	t.Run("deleted and renamed away", func(t *testing.T) {
		files := []verification.SourceFile{
			integrityOpaque(".golangci.yml", verification.ChangeDeleted),
			integrityRename(".github/workflows/ci.yml", "ci.yml", "a", "a"),
		}
		got := CheckIntegrity(files, nil)
		integrityAssert(t, got, []integrityWant{
			{SeverityBlock, CodeConfigModified, ".golangci.yml", "", 0},
			{SeverityBlock, CodeConfigModified, ".github/workflows/ci.yml", "", 0},
		})
	})
}

func TestCheckIntegrityRenameCoverUp(t *testing.T) {
	body := "{\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	base := integrityHeader + "func TestFoo(t *testing.T) " + body
	renamed := func(inner string) string {
		return integrityHeader + "func TestFooDisabled(t *testing.T) {\n" + inner + "\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	}
	guardedHead := "package p\n\nimport (\n\t\"runtime\"\n\t\"testing\"\n)\n\nvar _ = runtime.GOOS\n\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "rename plus unguarded skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, renamed("\tt.Skip(\"x\")\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFooDisabled", 0}},
		},
		{
			Name: "rename plus guarded skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base,
				guardedHead+"func TestFooDisabled(t *testing.T) {\n\tif runtime.GOOS == \"windows\" {\n\t\tt.Skip(\"x\")\n\t}\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n")},
			Want: []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFooDisabled", 0}},
		},
		{
			Name: "rename plus Errorf turned into Logf",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base,
				integrityHeader+"func TestFooDisabled(t *testing.T) {\n\tif Foo() != 1 {\n\t\tt.Logf(\"bad\")\n\t}\n}\n")},
			Want: []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFooDisabled", 0}},
		},
		{
			Name: "rename plus early return",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base,
				integrityHeader+"func TestFooDisabled(t *testing.T) {\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n\treturn\n\t_ = 1\n}\n")},
			Want: []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "returns early", 0}},
		},
		{
			Name:  "rename that also removes the assertion is a deletion",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, integrityHeader+"func TestFooDisabled(t *testing.T) {\n\t_ = Foo()\n}\n")},
			Want: []integrityWant{
				{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5},
				{SeverityWarn, CodeTestEmpty, "p/a_test.go", "TestFooDisabled", 0},
			},
		},
		{
			Name:  "rename that adds an assertion is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, renamed("\tif Foo() == 0 {\n\t\tt.Errorf(\"zero\")\n\t}\n"))},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegritySkipViaHelper(t *testing.T) {
	body := "\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n"
	test := func(call string) string {
		return integrityHeader + "func TestFoo(t *testing.T) {\n" + call + body + "}\n"
	}
	helper := "\nfunc skipFlaky(t *testing.T) {\n\tt.Skip(\"flaky\")\n}\n"
	guardedHelper := "\nfunc skipFlaky(t *testing.T) {\n\tif testing.Short() {\n\t\tt.Skip(\"slow\")\n\t}\n}\n"
	plainHelper := "\nfunc skipFlaky(t *testing.T) {\n\tt.Helper()\n}\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "test newly calls a new skipping helper",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", test(""), test("\tskipFlaky(t)\n")+helper)},
			Want: []integrityWant{
				{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo now calls skipFlaky", 0},
				{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "helper skipFlaky", 0},
			},
		},
		{
			Name: "helper lives in another changed file",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", test(""), test("\tskipFlaky(t)\n")),
				integrityAdd("p/h.go", "package p\n\nimport \"testing\"\n\nfunc skipFlaky(t *testing.T) {\n\tt.Skip(\"flaky\")\n}\n"),
			},
			Want: []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo now calls skipFlaky", 0}},
		},
		{
			Name:  "helper that gained a skip is not newly called",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", test("\tskipFlaky(t)\n")+plainHelper, test("\tskipFlaky(t)\n")+helper)},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "helper skipFlaky", 0}},
		},
		{
			Name:  "test newly calls a helper that gained a skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", test("")+plainHelper, test("\tskipFlaky(t)\n")+helper)},
			Want: []integrityWant{
				{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo now calls skipFlaky", 0},
				{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "helper skipFlaky", 0},
			},
		},
		{
			Name:  "helper that already skipped at base",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", test("")+helper, test("\tskipFlaky(t)\n")+helper)},
			Want:  []integrityWant{},
		},
		{
			Name:  "new helper with a guarded skip only warns",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", test(""), test("\tskipFlaky(t)\n")+guardedHelper)},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "helper skipFlaky", 0}},
		},
		{
			Name:  "new helper that does not skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", test(""), test("\tskipFlaky(t)\n")+plainHelper)},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityIgnoredDirectories(t *testing.T) {
	src := integrityHeader + "func TestFoo(t *testing.T) {\n\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n}\n"
	for _, moved := range []string{"p/testdata/a_test.go", "p/_old/a_test.go", "p/.hidden/a_test.go", "_x/a_test.go"} {
		t.Run("moved to "+moved, func(t *testing.T) {
			got := CheckIntegrity([]verification.SourceFile{integrityRename("p/a_test.go", moved, src, src)}, nil)
			integrityAssert(t, got, []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}})
		})
	}
	integrityRun(t, []integrityCase{
		{
			Name:  "added copy in an ignored directory does not replace the test",
			Files: []verification.SourceFile{integrityDel("p/a_test.go", src), integrityAdd("p/testdata/a_test.go", src)},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 5}},
		},
		{
			Name:  "directory names that only look hidden are fine",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/my_dir/a_test.go", src, src)},
			Want:  []integrityWant{},
		},
		{
			Name:  "fixture tests under testdata are not real tests",
			Files: []verification.SourceFile{integrityDel("p/testdata/x_test.go", src)},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityBuildConstraintVariants(t *testing.T) {
	body := integrityHeader + "func TestFoo(t *testing.T) { t.Fatal(1) }\nfunc TestBar(t *testing.T) { t.Fatal(2) }\n"
	onlyBar := integrityHeader + "func TestBar(t *testing.T) { t.Fatal(2) }\n"
	onlyFoo := integrityHeader + "func TestFoo(t *testing.T) { t.Fatal(1) }\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "plus build ignore added",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", body, "// +build ignore\n\n"+body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_test.go", "build constraint", 1}},
		},
		{
			Name:  "plus build line unchanged",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", "// +build linux\n\n"+body, "// +build linux\n\n"+body+"\n")},
			Want:  []integrityWant{},
		},
		{
			Name: "test moved into a new file with a different constraint",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", body, onlyBar),
				integrityAdd("p/b_test.go", "//go:build integration\n\n"+onlyFoo),
			},
			Want: []integrityWant{{SeverityWarn, CodeTestHidden, "p/b_test.go", "TestFoo", 0}},
		},
		{
			Name: "test moved into a new file that is not constrained",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", body, onlyBar),
				integrityAdd("p/b_test.go", onlyFoo),
			},
			Want: []integrityWant{},
		},
		{
			Name: "test moved between files with the same constraint",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", "//go:build integration\n\n"+body, "//go:build integration\n\n"+onlyBar),
				integrityAdd("p/b_test.go", "//go:build integration\n\n"+onlyFoo),
			},
			Want: []integrityWant{},
		},
		{
			Name: "test moved into a new file with an OS suffix",
			Files: []verification.SourceFile{
				integrityMod("p/a_test.go", body, onlyBar),
				integrityAdd("p/b_windows_test.go", onlyFoo),
			},
			Want: []integrityWant{{SeverityWarn, CodeTestHidden, "p/b_windows_test.go", "TestFoo", 0}},
		},
		{
			Name:  "renamed to an OS suffix",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/a_windows_test.go", body, body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_windows_test.go", "build constraint", 0}},
		},
		{
			Name:  "renamed to an arch suffix",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/a_arm64_test.go", body, body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_arm64_test.go", "build constraint", 0}},
		},
		{
			Name:  "renamed to an OS and arch suffix",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/a_linux_amd64_test.go", body, body)},
			Want:  []integrityWant{{SeverityBlock, CodeTestHidden, "p/a_linux_amd64_test.go", "build constraint", 0}},
		},
		{
			Name:  "renamed to an unknown suffix",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/a_plan9x_test.go", body, body)},
			Want:  []integrityWant{},
		},
		{
			Name:  "renamed to a bare OS name is not a suffix",
			Files: []verification.SourceFile{integrityRename("p/a_test.go", "p/windows_test.go", body, body)},
			Want:  []integrityWant{},
		},
		{
			Name:  "renamed keeping the same OS suffix",
			Files: []verification.SourceFile{integrityRename("p/a_windows_test.go", "p/b_windows_test.go", body, body)},
			Want:  []integrityWant{},
		},
		{
			Name:  "new test file with an OS suffix",
			Files: []verification.SourceFile{integrityAdd("p/b_windows_test.go", onlyFoo)},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityAssertionRefinements(t *testing.T) {
	head := "package p\n\nimport (\n\t\"encoding/xml\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nvar _ = xml.NewDecoder\nvar _ = exec.Command\n\n"
	mk := func(body string) string { return head + "func TestFoo(t *testing.T) {\n" + body + "}\n" }
	two := "\tif Foo(1) != 1 {\n\t\tt.Errorf(\"1\")\n\t}\n\tif Foo(2) != 2 {\n\t\tt.Errorf(\"2\")\n\t}\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "Skip on a decoder is not a test skip",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(two), mk(two+"\td := xml.NewDecoder(nil)\n\t_ = d.Skip()\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "cmd.Run is not a subtest",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(two), mk("\tcmd := exec.Command(\"x\")\n\t_ = cmd.Run()\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "Error and Log on a non testing receiver",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(two), mk("\tvar l interface{ Errorf(string); Log(string) }\n\tl.Errorf(\"x\")\n\tl.Log(\"y\")\n"))},
			Want:  []integrityWant{{SeverityBlock, CodeAssertionsRemoved, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "testing.TB parameter of a closure is a testing receiver",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(two), mk("\tcheck := func(tb testing.TB) {\n\t\ttb.Errorf(\"1\")\n\t\ttb.Errorf(\"2\")\n\t}\n\tcheck(t)\n"))},
			Want:  []integrityWant{},
		},
		{
			Name:  "method value subtests only warn",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk(two), mk("\tfor _, tc := range cases {\n\t\tt.Run(tc.name, tc.check)\n\t}\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeAssertionsReduced, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "Errorf swapped for Logf while an equal helper count remains only warns",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tif Foo(1) != 1 {\n\t\tt.Errorf(\"1\")\n\t}\n"), mk("\tcheck(t, Foo(1))\n\tt.Log(\"x\")\n"))},
			Want:  []integrityWant{{SeverityWarn, CodeAssertionsReduced, "p/a_test.go", "TestFoo", 0}},
		},
	})
}

func TestCheckIntegrityDisguisedEarlyReturn(t *testing.T) {
	mk := func(body string) string {
		return "package p\n\nimport (\n\t\"os\"\n\t\"runtime\"\n\t\"testing\"\n)\n\nvar _ = os.Args\nvar _ = runtime.GOOS\n\nfunc TestFoo(t *testing.T) {\n" + body + "}\n"
	}
	asserting := "\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n"
	base := mk(asserting)

	integrityRun(t, []integrityCase{
		{
			Name:  "if true return",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif true {\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 13}},
		},
		{
			Name:  "if one equals one return",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif 1 == 1 {\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 13}},
		},
		{
			Name:  "if not false return",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif !(2 < 1) {\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 13}},
		},
		{
			Name:  "other condition only warns",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif len(os.Args) > 5 {\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 13}},
		},
		{
			Name:  "guard condition is fine",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif runtime.GOOS == \"windows\" {\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{},
		},
		{
			Name:  "body that also fails is a normal guard clause",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif Foo() == 0 {\n\t\tt.Fatal(1)\n\t\treturn\n\t}\n"+asserting))},
			Want:  []integrityWant{},
		},
		{
			Name:  "if with else is not matched",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", base, mk("\tif Foo() == 0 {\n\t\treturn\n\t} else {\n\t\tt.Log(1)\n\t}\n"+asserting))},
			Want:  []integrityWant{},
		},
		{
			Name:  "pre-existing disguised return",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", mk("\tif true {\n\t\treturn\n\t}\n"+asserting), mk("\tif true {\n\t\treturn\n\t}\n"+asserting+"\tt.Log(1)\n"))},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityGuardAliases(t *testing.T) {
	src := func(body string) string {
		return "package p\n\nimport (\n\tgoruntime \"runtime\"\n\tgoos \"os\"\n\ttt \"testing\"\n)\n\nvar _ = goruntime.GOOS\nvar _ = goos.Args\n\nfunc TestFoo(t *tt.T) {\n" + body + "}\n"
	}
	asserting := "\tif Foo() != 1 {\n\t\tt.Errorf(\"bad\")\n\t}\n"
	integrityRun(t, []integrityCase{
		{
			Name:  "aliased runtime guard",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", src(asserting), src("\tif goruntime.GOOS == \"windows\" {\n\t\tt.Skip(\"x\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "aliased os guard",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", src(asserting), src("\tif goos.Getenv(\"CI\") == \"\" {\n\t\tt.Skip(\"x\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "aliased testing guard",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", src(asserting), src("\tif tt.Short() {\n\t\tt.Skip(\"x\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityWarn, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:  "a local variable named runtime is not the package",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", src(asserting), src("\truntime := struct{ GOOS string }{}\n\tif runtime.GOOS == \"windows\" {\n\t\tt.Skip(\"x\")\n\t}\n"+asserting))},
			Want:  []integrityWant{{SeverityBlock, CodeTestSkipAdded, "p/a_test.go", "TestFoo", 0}},
		},
	})
}

func TestCheckIntegrityDeletedTestMerging(t *testing.T) {
	deleted := integrityHeader + "func TestA(t *testing.T) {\n\tgot := Parse(\"a\")\n\tif got != Render(1) {\n\t\tt.Errorf(\"bad %v\", got)\n\t}\n}\n"
	table := func(inner string) string {
		return integrityHeader + "func TestTable(t *testing.T) {\n\tfor _, c := range cases {\n\t\t" + inner + "\n\t}\n}\n"
	}
	superset := integrityHeader + "func TestBoth(t *testing.T) {\n\tgot := Parse(\"a\")\n\tif got != Render(1) {\n\t\tt.Errorf(\"bad %v\", got)\n\t}\n\tif Parse(\"b\") != Render(2) {\n\t\tt.Errorf(\"bad b\")\n\t}\n}\n"

	integrityRun(t, []integrityCase{
		{
			Name:  "merged into a table test that calls the same code",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", deleted, table("if Parse(c.in) != Render(c.n) {\n\t\t\tt.Errorf(\"%v\", c)\n\t\t}"))},
			Want:  []integrityWant{{SeverityWarn, CodeTestDeleted, "p/a_test.go", "TestTable in this change exercises the same code", 5}},
		},
		{
			Name:  "new test calls half of the code",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", deleted, table("if Parse(c.in) != c.n {\n\t\t\tt.Errorf(\"%v\", c)\n\t\t}"))},
			Want:  []integrityWant{{SeverityWarn, CodeTestDeleted, "p/a_test.go", "TestTable", 5}},
		},
		{
			Name:  "new test calls only standard library and testing helpers",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", deleted, table("if strings.TrimSpace(c.in) != c.n {\n\t\t\tt.Errorf(\"%v\", c)\n\t\t\tfmt.Println(c)\n\t\t}"))},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestA", 5}},
		},
		{
			Name:  "new test calls unrelated code",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", deleted, table("if Other(c.in) != c.n {\n\t\t\tt.Errorf(\"%v\", c)\n\t\t}"))},
			Want:  []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestA", 5}},
		},
		{
			Name:  "deleted test body contained in a larger new test",
			Files: []verification.SourceFile{integrityMod("p/a_test.go", deleted, superset)},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityStubScope(t *testing.T) {
	stub := func(pkg string) string { return "package " + pkg + "\n\nfunc F() { panic(\"not implemented\") }\n" }
	for _, tc := range []struct{ path, pkg string }{
		{"p/m.go", "mocks"},
		{"p/m.go", "mock"},
		{"p/m.go", "fakes"},
		{"p/m.go", "fake"},
		{"p/m.go", "fooMock"},
		{"p/m.go", "enginetest"},
		{"p/m.go", "testing"},
		{"p/testutil/a.go", "p"},
		{"p/mocks/a.go", "p"},
		{"p/fakes/a.go", "p"},
	} {
		t.Run("exempt "+tc.path+" "+tc.pkg, func(t *testing.T) {
			got := CheckIntegrity([]verification.SourceFile{integrityAdd(tc.path, stub(tc.pkg))}, nil)
			integrityAssert(t, got, []integrityWant{})
		})
	}
	integrityRun(t, []integrityCase{
		{
			Name:  "ordinary package still blocks",
			Files: []verification.SourceFile{integrityAdd("p/mocksmith/a.go", stub("p"))},
			Want:  []integrityWant{{SeverityBlock, CodeStub, "p/mocksmith/a.go", "F", 3}},
		},
		{
			Name:  "gutted function in a mock package is exempt",
			Files: []verification.SourceFile{integrityMod("p/a.go", "package mocks\n\nfunc F() error {\n\ta()\n\tb()\n\tc()\n\treturn nil\n}\n", "package mocks\n\nfunc F() error {\n\treturn nil\n}\n")},
			Want:  []integrityWant{},
		},
	})
}

func TestCheckIntegrityDeletedDeclarationMatching(t *testing.T) {
	test := func(inner string) string {
		return integrityHeader + "func TestFoo(t *testing.T) {\n" + inner + "\tt.Fatal(1)\n}\n\nfunc TestKeep(t *testing.T) { t.Fatal(1) }\n"
	}
	keep := integrityHeader + "func TestKeep(t *testing.T) { t.Fatal(1) }\n"
	integrityRun(t, []integrityCase{
		{
			Name:    "method with its receiver type",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", test("\tvar s Server\n\ts.Handle()\n"), keep)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Server.Handle", "p/a.go")},
			Want:    []integrityWant{{SeverityInfo, CodeTestDeleted, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:    "method name only",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", test("\tvar s Other\n\ts.Handle()\n"), keep)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Server.Handle", "p/a.go")},
			Want:    []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:    "receiver type only",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", test("\tvar s Server\n\t_ = s\n"), keep)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Server.Handle", "p/a.go")},
			Want:    []integrityWant{{SeverityBlock, CodeTestDeleted, "p/a_test.go", "TestFoo", 0}},
		},
		{
			Name:    "top level function by name",
			Files:   []verification.SourceFile{integrityMod("p/a_test.go", test("\tHandle()\n"), keep)},
			Deleted: []verification.ChangedDeclaration{integrityDecl("Handle", "p/a.go")},
			Want:    []integrityWant{{SeverityInfo, CodeTestDeleted, "p/a_test.go", "TestFoo", 0}},
		},
	})
}
