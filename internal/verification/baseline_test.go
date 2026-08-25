package verification

import (
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

func TestMapUnchangedLineAcrossDiffHunks(t *testing.T) {
	tests := []struct {
		name  string
		line  int
		edits []LineEdit
		want  int
		ok    bool
	}{
		{name: "before insertion", line: 2, edits: []LineEdit{{BaseStart: 2, CurrentStart: 3, CurrentCount: 2}}, want: 2, ok: true},
		{name: "after insertion", line: 3, edits: []LineEdit{{BaseStart: 2, CurrentStart: 3, CurrentCount: 2}}, want: 5, ok: true},
		{name: "inside replacement", line: 4, edits: []LineEdit{{BaseStart: 4, BaseCount: 2, CurrentStart: 4, CurrentCount: 1}}},
		{name: "after deletion", line: 7, edits: []LineEdit{{BaseStart: 4, BaseCount: 2, CurrentStart: 4}}, want: 5, ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := mapUnchangedLine(test.line, test.edits)
			if got != test.want || ok != test.ok {
				t.Fatalf("mapUnchangedLine = %d/%v, want %d/%v", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestCompareAnalyzerFindingsMapsRenameAndRejectsChangedLocations(t *testing.T) {
	base := []finding.Finding{
		{Rule: "errors-09", Message: "same at old.go:10:2", Severity: finding.SeverityError, Location: finding.Location{File: "old.go", Line: 10, Col: 2}},
		{Rule: "errors-09", Message: "changed line", Severity: finding.SeverityError, Location: finding.Location{File: "changed.go", Line: 4, Col: 2}},
	}
	current := []finding.Finding{
		{Rule: "errors-09", Message: "same at new.go:12:2", Severity: finding.SeverityError, Location: finding.Location{File: "new.go", Line: 12, Col: 2}},
		{Rule: "errors-09", Message: "changed line", Severity: finding.SeverityError, Location: finding.Location{File: "changed.go", Line: 4, Col: 2}},
		{Rule: "errors-09", Message: "new", Severity: finding.SeverityError, Location: finding.Location{File: "new.go", Line: 20, Col: 2}},
	}
	files := []SourceFile{
		{Change: ChangedFile{Path: "new.go", PreviousPath: "old.go", Change: ChangeRenamed}, Edits: []LineEdit{{BaseStart: 5, CurrentStart: 6, CurrentCount: 2}}},
		{Change: ChangedFile{Path: "changed.go", Change: ChangeModified}, Edits: []LineEdit{{BaseStart: 4, BaseCount: 1, CurrentStart: 4, CurrentCount: 1}}},
	}

	comparison := compareAnalyzerFindings("errors", base, current, files)
	if comparison.Summary.Existing != 1 || comparison.Summary.Introduced != 1 || comparison.Summary.Unknown != 1 || comparison.Summary.Resolved != 0 {
		t.Fatalf("summary = %#v", comparison.Summary)
	}
	if len(comparison.Introduced) != 1 || comparison.Introduced[0].Message != "new" {
		t.Fatalf("introduced = %#v", comparison.Introduced)
	}
	if len(comparison.Uncertainties) != 1 || comparison.Uncertainties[0].Code != "baseline_unknown" {
		t.Fatalf("uncertainties = %#v", comparison.Uncertainties)
	}
}
