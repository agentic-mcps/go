package errors_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/analysis/errors"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
)

func TestRuleSet(t *testing.T) {
	want := []string{"errors-04", "errors-09", "errors-19"}
	if got := astutil.RulesInDomain("errors"); !slices.Equal(got, want) {
		t.Fatalf("registered rules = %v, want %v", got, want)
	}
	disabled := []string{
		"errors-01", "errors-02", "errors-03", "errors-05", "errors-06", "errors-07",
		"errors-10", "errors-11", "errors-12", "errors-13", "errors-14", "errors-15",
		"errors-16", "errors-17",
	}
	if got := astutil.DisabledRulesInDomain("errors"); !slices.Equal(got, disabled) {
		t.Fatalf("disabled rules = %v, want %v", got, disabled)
	}
}

func TestAuditFixtures(t *testing.T) {
	root := filepath.Join("testdata", "audit")
	checks := []struct {
		rule string
		sev  finding.Severity
	}{
		{"errors-04", finding.SeverityError},
		{"errors-09", finding.SeverityError},
		{"errors-19", finding.SeverityError},
	}
	for _, tc := range checks {
		t.Run(tc.rule, func(t *testing.T) {
			ws, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			ws = filepath.Join(ws, root)
			n := strings.TrimPrefix(tc.rule, "errors-")
			pkg := "rule" + n
			result, err := audit.Run(context.Background(), ws, "./"+pkg, []*analysis.Analyzer{errors.Analyzer})
			if err != nil {
				t.Fatal(err)
			}
			wantTotal := 1
			if result.Total != wantTotal || len(result.Findings) != wantTotal {
				t.Fatalf("result = %#v", result)
			}
			got := result.Findings[0]
			if got.Rule != tc.rule || got.Severity != tc.sev || got.Location.File != filepath.ToSlash(filepath.Join(pkg, "violation.go")) {
				t.Fatalf("finding = %#v", got)
			}
			data, err := os.ReadFile(filepath.Join(ws, pkg, "violation.go"))
			if err != nil {
				t.Fatal(err)
			}
			wantLine := 0
			for i, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, "VIOLATION: "+tc.rule) {
					wantLine = i + 1
				}
			}
			if got.Location.Line != wantLine {
				t.Fatalf("line = %d, want %d", got.Location.Line, wantLine)
			}
			for _, f := range result.Findings {
				if strings.HasSuffix(f.Location.File, "compliant.go") {
					t.Fatalf("compliant fixture reported: %#v", f)
				}
			}
		})
	}
}

func TestExitEntrypointsAreSilent(t *testing.T) {
	for _, name := range []string{"audit-main", "audit-cmd"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("testdata", name)
			result, err := audit.Run(context.Background(), root, "./...", []*analysis.Analyzer{errors.Analyzer})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range result.Findings {
				if item.Rule == "errors-19" {
					t.Fatalf("entrypoint exit reported: %#v", item)
				}
			}
		})
	}
}
