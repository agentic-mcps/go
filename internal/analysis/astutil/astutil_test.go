package astutil

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"github.com/agentic-mcps/go/internal/finding"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

const (
	testRule         = "astutil-test-01"
	disabledTestRule = "astutil-test-02"
)

func init() {
	RegisterRule(testRule, "test_rule", finding.SeverityWarning)
	RegisterDisabledRule(disabledTestRule, "disabled_test_rule", finding.SeverityInfo, "calibration failure")
}

func TestRuleRegistryAndReport(t *testing.T) {
	if RuleName(testRule) != "test_rule" || RuleSeverity(testRule) != finding.SeverityWarning {
		t.Fatal("registered rule metadata was not preserved")
	}
	if got := RulesInDomain("astutil-test"); len(got) != 1 || got[0] != testRule {
		t.Fatalf("RulesInDomain() = %v", got)
	}
	if got := DisabledRulesInDomain("astutil-test"); len(got) != 1 || got[0] != disabledTestRule {
		t.Fatalf("DisabledRulesInDomain() = %v", got)
	}
	var diagnostic analysis.Diagnostic
	emitted := false
	pass := &analysis.Pass{Report: func(got analysis.Diagnostic) {
		diagnostic = got
		emitted = true
	}}
	Report(pass, token.Pos(1), testRule, "problem %d", 7)
	if diagnostic.Category != testRule || diagnostic.Message != "problem 7" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	emitted = false
	Report(pass, token.Pos(1), disabledTestRule, "suppressed")
	if emitted {
		t.Fatal("disabled rule emitted a diagnostic")
	}
	assertPanics(t, func() { RegisterRule(testRule, "test_rule", finding.SeverityWarning) })
	assertPanics(t, func() { RegisterRule("bad", "test_rule", finding.SeverityWarning) })
	assertPanics(t, func() { RegisterRule("astutil-test-03", "Bad Name", finding.SeverityWarning) })
	assertPanics(t, func() { RegisterRule("astutil-test-03", "bad_severity", finding.Severity("critical")) })
	assertPanics(t, func() { RegisterDisabledRule("astutil-test-04", "disabled", finding.SeverityInfo, "") })
	assertPanics(t, func() { RegisterRule(disabledTestRule, "disabled_test_rule", finding.SeverityInfo) })
	assertPanics(t, func() { RuleSeverity("astutil-test-unknown") })
	assertPanics(t, func() { Report(pass, token.Pos(1), "astutil-test-unknown", "unknown") })
}

func TestCallIdentityHelpers(t *testing.T) {
	pkg := types.NewPackage("example.com/lib", "lib")
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	pass := &analysis.Pass{TypesInfo: info}

	functionSel := ast.NewIdent("Build")
	function := types.NewFunc(token.NoPos, pkg, "Build", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	info.Uses[functionSel] = function
	functionCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("lib"), Sel: functionSel}}
	if !IsPkgFunc(pass, functionCall, "example.com/lib", "Build") || IsPkgFunc(pass, functionCall, "example.com/other", "Build") {
		t.Fatal("IsPkgFunc did not use resolved package identity")
	}

	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Worker", nil), types.NewStruct(nil, nil), nil)
	receiver := types.NewVar(token.NoPos, pkg, "w", types.NewPointer(named))
	methodSel := ast.NewIdent("Stop")
	method := types.NewFunc(token.NoPos, pkg, "Stop", types.NewSignatureType(receiver, nil, nil, nil, nil, false))
	info.Uses[methodSel] = method
	methodCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("w"), Sel: methodSel}}
	if !IsMethodOn(pass, methodCall, "example.com/lib", "Worker", "Stop") || IsPkgFunc(pass, methodCall, "example.com/lib", "Stop") {
		t.Fatal("method/function identity was conflated")
	}
}

func TestSyntaxAndFileHelpers(t *testing.T) {
	literal := &ast.BasicLit{Kind: token.STRING, Value: `"value"`}
	if got, ok := StringLit(literal); !ok || got != "value" {
		t.Fatalf("StringLit() = %q, %v", got, ok)
	}
	receiver := &ast.FuncDecl{Name: ast.NewIdent("Run"), Recv: &ast.FieldList{List: []*ast.Field{{Type: &ast.StarExpr{X: ast.NewIdent("Worker")}}}}}
	if got := FuncName(receiver); got != "(*Worker).Run" {
		t.Fatalf("FuncName() = %q", got)
	}
	pkgs := []*packages.Package{{CompiledGoFiles: []string{"a.go", "b.go"}}, {CompiledGoFiles: []string{"b.go", "c.go"}}}
	if got := FilesScanned(pkgs); got != 3 {
		t.Fatalf("FilesScanned() = %d", got)
	}
	decl := ast.NewIdent("wg")
	info := &types.Info{Defs: map[*ast.Ident]types.Object{decl: types.NewVar(token.NoPos, nil, "wg", types.Typ[types.Int])}}
	if got := TypeString(&analysis.Pass{TypesInfo: info}, decl); got != "int" {
		t.Fatalf("TypeString(declaration) = %q", got)
	}
}

func assertPanics(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	f()
}
