package gate

import (
	"reflect"
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
