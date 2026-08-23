package concurrency_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/analysis/concurrency"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
)

func TestRuleSet(t *testing.T) {
	want := []string{
		"concurrency-04", "concurrency-05", "concurrency-08", "concurrency-09",
		"concurrency-10", "concurrency-12", "concurrency-18", "concurrency-19",
	}
	if got := astutil.RulesInDomain("concurrency"); !slices.Equal(got, want) {
		t.Fatalf("registered rules = %v, want %v", got, want)
	}
	disabled := []string{
		"concurrency-01", "concurrency-02", "concurrency-03", "concurrency-06", "concurrency-07",
		"concurrency-14", "concurrency-15", "concurrency-17", "concurrency-20",
	}
	if got := astutil.DisabledRulesInDomain("concurrency"); !slices.Equal(got, disabled) {
		t.Fatalf("disabled rules = %v, want %v", got, disabled)
	}
}

func TestFixtureRules(t *testing.T) {
	for _, tc := range []struct {
		rule string
		dir  string
	}{
		{"concurrency-08", "rule08"},
		{"concurrency-10", "rule10"},
		{"concurrency-12", "rule12"},
		{"concurrency-18", "rule18"},
		{"concurrency-19", "rule19"},
		{"concurrency-19", "rule19defer"},
		{"concurrency-18", "rule18ticker"},
		{"concurrency-04", "rule04"},
		{"concurrency-05", "rule05"},
		{"concurrency-09", "rule09"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			dir := filepath.Join("testdata", tc.dir)
			result, err := audit.Run(context.Background(), dir, "./...", []*analysis.Analyzer{concurrency.Analyzer})
			if err != nil {
				t.Fatal(err)
			}
			var target []finding.Finding
			for _, item := range result.Findings {
				if item.Rule == tc.rule {
					target = append(target, item)
				}
			}
			wantCount := 1
			if len(target) != wantCount {
				t.Fatalf("got %d target findings, want %d (total %d): %#v", len(target), wantCount, result.Total, result.Findings)
			}
			got := target[0]
			wantSeverity := finding.SeverityWarning
			switch tc.rule {
			case "concurrency-01", "concurrency-07", "concurrency-08":
				wantSeverity = finding.SeverityError
			case "concurrency-10":
				wantSeverity = finding.SeverityInfo
			case "concurrency-14", "concurrency-02", "concurrency-19":
				wantSeverity = finding.SeverityError
			}
			if got.Rule != tc.rule || got.Severity != wantSeverity {
				t.Fatalf("got rule/severity %s/%s", got.Rule, got.Severity)
			}
			if got.Location.File != "violation.go" {
				t.Fatalf("got file %q", got.Location.File)
			}
			markerLine := markerLine(t, filepath.Join(dir, "violation.go"), "VIOLATION: "+tc.rule)
			if got.Location.Line != markerLine {
				t.Fatalf("got line %d, marker line %d", got.Location.Line, markerLine)
			}
			if strings.Contains(got.Location.File, "compliant.go") {
				t.Fatal("reported compliant fixture")
			}
		})
	}
}

func markerLine(t *testing.T, path, marker string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, marker) {
			return i + 1
		}
	}
	t.Fatalf("missing marker %q", marker)
	return 0
}
