package parser

import (
	"strings"
	"testing"
)

func TestParseCoverage(t *testing.T) {
	r, err := ParseCoverage(strings.NewReader("mode: atomic\nz:path.go:3.1,3.4 1 0\nz:path.go:2.1,2.4 1 0\na:path.go:1.1,1.4 3 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 2 || r.Files[0].File != "a:path.go" || r.Files[0].Gaps == nil || len(r.Files[1].Gaps) != 2 {
		t.Fatalf("unexpected report: %+v", r)
	}
	if r.Files[1].Gaps[0].StartLine != 2 {
		t.Fatalf("gaps are not source-sorted: %+v", r.Files[1].Gaps)
	}
	if r.OverallPercent != 60 {
		t.Fatalf("overall percent = %v", r.OverallPercent)
	}
}

func TestParseCoverageRejectsMalformedProfiles(t *testing.T) {
	for _, input := range []string{"", "mode: atomic\n", "mode: nope\na.go:1.1,1.2 1 0\n", "mode: atomic\na.go:1.1,1.2 -1 0\n", "mode: atomic\na.go:2.1,1.2 1 0\n", "mode: atomic\na.go:1.1,1.2 18446744073709551616 0\n"} {
		if _, err := ParseCoverage(strings.NewReader(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
