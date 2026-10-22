package gate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestComputeVerdict(t *testing.T) {
	block := Item{Severity: SeverityBlock, Code: CodeTestDeleted}
	warn := Item{Severity: SeverityWarn, Code: CodeTestFlaky}
	cases := []struct {
		name     string
		want     Verdict
		items    []Item
		complete bool
	}{
		{"empty complete passes", VerdictPass, nil, true},
		{"warnings alone pass", VerdictPass, []Item{warn}, true},
		{"block blocks", VerdictBlock, []Item{warn, block}, true},
		{"block wins over incomplete evidence", VerdictBlock, []Item{block}, false},
		{"incomplete without block is unknown", VerdictUnknown, []Item{warn}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeVerdict(tc.items, tc.complete); got != tc.want {
				t.Fatalf("ComputeVerdict() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSortItemsOrdersBySeverityThenLocation(t *testing.T) {
	items := []Item{
		{Severity: SeverityInfo, Code: "c", File: "a.go", Line: 1},
		{Severity: SeverityWarn, Code: "b", File: "b.go", Line: 2},
		{Severity: SeverityBlock, Code: "z", File: "b.go", Line: 9},
		{Severity: SeverityBlock, Code: "a", File: "b.go", Line: 9},
		{Severity: SeverityBlock, Code: "y", File: "a.go", Line: 30},
	}
	SortItems(items)
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Code)
	}
	want := []string{"y", "a", "z", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SortItems order = %v, want %v", got, want)
	}
}

// TestItemCodesAreDocumented keeps the item code table in the design
// document in step with the Code constants.
func TestItemCodesAreDocumented(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile("../../docs/design/check-gate.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(doc), "### Item codes")
	if !found {
		t.Fatal("docs/design/check-gate.md has no Item codes section")
	}
	codes := 0
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "Code") {
					continue
				}
				code, err := strconv.Unquote(value.Values[i].(*ast.BasicLit).Value)
				if err != nil {
					t.Fatal(err)
				}
				codes++
				if !strings.Contains(table, "| `"+code+"` |") {
					t.Errorf("item code %s (%s) is missing from the Item codes table", code, name.Name)
				}
			}
		}
	}
	if codes == 0 {
		t.Fatal("found no Code constants in types.go")
	}
}
