package audit

import (
	"context"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/analysis/astutil"
	"github.com/agentic-mcps/go/internal/finding"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

func init() {
	astutil.RegisterRule("audit-test-01", "test_rule", finding.SeverityWarning)
}

func TestRunRecoversAnalyzerPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/panic\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "panic.go"), []byte("package panicfixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	analyzer := &analysis.Analyzer{Name: "panic_test", Doc: "panics for recovery testing", Run: func(*analysis.Pass) (any, error) { panic("boom") }}
	_, err := Run(context.Background(), dir, ".", []*analysis.Analyzer{analyzer})
	if err == nil || !strings.Contains(err.Error(), "panic in analyzer predicate: boom") {
		t.Fatalf("Run() error = %v, want recovered panic", err)
	}
}

func TestRunDoesNotReturnSuccessAfterCancellation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/cancel\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cancel.go"), []byte("package cancelfixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	analyzer := &analysis.Analyzer{
		Name: "cancel_test",
		Doc:  "cancels its request during analysis",
		Run: func(*analysis.Pass) (any, error) {
			cancel()
			return nil, nil
		},
	}
	_, err := Run(ctx, dir, ".", []*analysis.Analyzer{analyzer})
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Run() error = %v, want cancellation", err)
	}
}

func TestRunCollectsAndNormalizesDiagnostics(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/audit\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package audit\n\nfunc f() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package audit\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &analysis.Analyzer{
		Name: "audit_test", Doc: "reports test diagnostics", Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			in := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			in.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
				pass.Report(analysis.Diagnostic{Pos: n.Pos(), Category: "audit-test-01", Message: fmt.Sprintf("test diagnostic at %s", pass.Fset.Position(n.Pos()))})
			})
			return nil, nil
		},
	}
	got, err := Run(context.Background(), dir, ".", []*analysis.Analyzer{a})
	if err != nil {
		t.Fatal(err)
	}
	if got.Findings == nil || got.CountsBySeverity == nil {
		t.Fatal("result collections must be non-nil")
	}
	if got.Total != 2 || len(got.Findings) != 2 {
		t.Fatalf("findings = %#v", got.Findings)
	}
	if got.Findings[0].Location.File != "main.go" {
		t.Fatalf("location = %#v", got.Findings[0].Location)
	}
	if strings.Contains(got.Findings[0].Message, dir) || !strings.Contains(got.Findings[0].Message, "main.go:") {
		t.Fatalf("message leaked or lost location: %q", got.Findings[0].Message)
	}
	if got.CountsBySeverity[finding.SeverityWarning] != 2 {
		t.Fatalf("counts = %#v", got.CountsBySeverity)
	}
}

func TestClosedWorldEnvOverridesNetworkSettings(t *testing.T) {
	got := closedWorldEnv([]string{"PATH=/bin", "GOPROXY=https://proxy.example", "GOSUMDB=sum.example", "GOTOOLCHAIN=auto"})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"PATH=/bin", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment %q missing %q", joined, want)
		}
	}
	for _, unwanted := range []string{"proxy.example", "sum.example", "auto"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("environment %q retained %q", joined, unwanted)
		}
	}
}
