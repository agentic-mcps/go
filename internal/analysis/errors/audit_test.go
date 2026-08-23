package errors_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/errors"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
)

func TestAuditFixtures(t *testing.T) {
	root := filepath.Join("testdata", "audit")
	checks := []struct {
		rule string
		sev  finding.Severity
	}{
		{"errors-01", finding.SeverityWarning},
		{"errors-02", finding.SeverityError},
		{"errors-03", finding.SeverityInfo},
		{"errors-04", finding.SeverityError},
		{"errors-05", finding.SeverityWarning},
		{"errors-09", finding.SeverityError},
		{"errors-10", finding.SeverityError},
		{"errors-11", finding.SeverityError},
		{"errors-06", finding.SeverityInfo},
		{"errors-07", finding.SeverityInfo},
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
			if tc.rule == "errors-05" {
				wantTotal = 2
			}
			if result.Total != wantTotal || len(result.Findings) != wantTotal {
				t.Fatalf("result = %#v", result)
			}
			got := result.Findings[0]
			if tc.rule == "errors-05" {
				for _, candidate := range result.Findings {
					if candidate.Rule == tc.rule {
						got = candidate
						break
					}
				}
			}
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
			if tc.rule == "errors-05" {
				foundSuppression := false
				for _, candidate := range result.Findings {
					if candidate.Rule == "errors-04" && strings.HasSuffix(candidate.Location.File, "suppression.go") {
						foundSuppression = true
					}
				}
				if !foundSuppression {
					t.Fatalf("suppression finding missing: %#v", result.Findings)
				}
			}
			for _, f := range result.Findings {
				if strings.HasSuffix(f.Location.File, "compliant.go") {
					t.Fatalf("compliant fixture reported: %#v", f)
				}
			}
		})
	}
}
