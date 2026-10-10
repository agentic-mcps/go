package verification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestCoverageEvidenceSeparatesLostCoverageFromProfileErrors(t *testing.T) {
	root := t.TempDir()
	source := "package calc\n\nfunc Sign(value int) int {\n\tif value < 0 {\n\t\treturn -1\n\t}\n\treturn 1\n}\n"
	for path, content := range map[string]string{"go.mod": "module example.test/verify\n\ngo 1.25.0\n", "calc/calc.go": source} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{workspace: ws}
	changed := ChangedFile{Path: "calc/calc.go", Change: ChangeModified, CurrentRanges: []LineRange{{Start: 4, End: 6}}}
	analysis := ChangeAnalysis{
		Files:    []SourceFile{{Change: changed, CurrentContent: []byte(source)}},
		Packages: []ExecutionTarget{{ID: "example.test/verify/calc", Dir: filepath.Join(ws.Root(), "calc")}},
	}
	lostImporter := testRunFacts{coverageLost: map[string]struct{}{"example.test/verify/api": {}}}
	tests := []struct {
		name        string
		wantStatus  EvidenceStatus
		wantSummary string
		wantError   string
		run         affectedRun
	}{
		{
			name:       "unrelated profile error keeps its own text",
			run:        affectedRun{facts: lostImporter, coverageErr: errors.New("changed coverage profile exceeds 8388608 bytes")},
			wantStatus: EvidenceError, wantSummary: "changed coverage could not be calculated", wantError: "exceeds 8388608 bytes",
		},
		{
			name:       "empty profile after a lost package is unavailable",
			run:        affectedRun{facts: lostImporter, coverageErr: errors.New("parsing changed coverage profile: coverage: profile has no blocks"), coverageEmpty: true},
			wantStatus: EvidenceError, wantSummary: "coverage unavailable: tests in example.test/verify/api failed",
		},
		{
			name: "zero covered statements after a lost package is unavailable",
			run: affectedRun{facts: lostImporter, profile: []parser.CoverageBlock{
				{File: "example.test/verify/calc/calc.go", StartLine: 4, StartCol: 2, EndLine: 6, EndCol: 3, Statements: 2},
			}},
			wantStatus: EvidenceError, wantSummary: "coverage unavailable: tests in example.test/verify/api failed",
		},
		{
			name: "zero covered statements without lost packages is measured",
			run: affectedRun{facts: testRunFacts{coverageLost: map[string]struct{}{}}, profile: []parser.CoverageBlock{
				{File: "example.test/verify/calc/calc.go", StartLine: 4, StartCol: 2, EndLine: 6, EndCol: 3, Statements: 2},
			}},
			wantStatus: EvidencePassed, wantSummary: "0.0% of changed statements covered",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, _ := engine.coverageEvidence(analysis, test.run)
			if evidence.Status != test.wantStatus || evidence.Summary != test.wantSummary || !strings.Contains(evidence.Error, test.wantError) {
				t.Fatalf("evidence = %#v, want %s %q with error %q", evidence, test.wantStatus, test.wantSummary, test.wantError)
			}
		})
	}
}
