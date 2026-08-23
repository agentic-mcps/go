// Package errors implements the mechanical error-handling diagnostics.
package errors

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "errors",
	Doc:      "finds precise, mechanical error-handling problems",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func init() {
	astutil.RegisterRule("errors-01", "error_last_return", finding.SeverityWarning)
	astutil.RegisterRule("errors-02", "exported_concrete_error", finding.SeverityError)
	astutil.RegisterRule("errors-03", "happy_path_in_else", finding.SeverityInfo)
	astutil.RegisterRule("errors-06", "failed_to_prefix", finding.SeverityInfo)
	astutil.RegisterRule("errors-07", "error_string_style", finding.SeverityInfo)
}

func run(pass *analysis.Pass) (any, error) {
	n, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, fmt.Errorf("inspect result has type %T", pass.ResultOf[inspect.Analyzer])
	}
	n.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil), (*ast.IfStmt)(nil), (*ast.CallExpr)(nil)}, func(node ast.Node) {
		switch x := node.(type) {
		case *ast.FuncDecl:
			checkReturns(pass, x)
		case *ast.FuncLit:
			checkResultList(pass, x.Type.Results, "function literal")
		case *ast.IfStmt:
			if isErrorCheck(pass, x) && x.Else != nil {
				if _, nested := x.Else.(*ast.IfStmt); !nested {
					astutil.Report(pass, x.Else.Pos(), "errors-03", "error check at %s nests the happy path inside an else block — leave the happy path unindented after the if", pass.Fset.Position(x.Pos()))
				}
			}
		case *ast.CallExpr:
			checkMessage(pass, x)
		}
	})
	return nil, nil
}

func checkReturns(pass *analysis.Pass, fn *ast.FuncDecl) {
	if fn.Type.Results != nil {
		checkResultList(pass, fn.Type.Results, fn.Name.Name)
	}
	if !fn.Name.IsExported() || fn.Type.Results == nil {
		return
	}
	errIface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	for _, field := range fn.Type.Results.List {
		t := pass.TypesInfo.TypeOf(field.Type)
		if t == nil || types.IsInterface(t) || types.Identical(t, types.Universe.Lookup("error").Type()) {
			continue
		}
		if types.Implements(t, errIface) || types.Implements(types.NewPointer(t), errIface) {
			astutil.Report(pass, field.Pos(), "errors-02", "exported function %s returns concrete error type %s — return error and expose a stable exported type for errors.As", fn.Name.Name, types.TypeString(t, nil))
		}
	}
}

func checkResultList(pass *analysis.Pass, results *ast.FieldList, name string) {
	if results == nil {
		return
	}
	for i, f := range results.List {
		if astutil.TypeString(pass, f.Type) == "error" && i != len(results.List)-1 {
			astutil.Report(pass, f.Pos(), "errors-01", "function %s returns error at position %d of %d results, not last — move error to the final return value", name, i+1, len(results.List))
		}
	}
}

func isErrorCheck(pass *analysis.Pass, stmt *ast.IfStmt) bool {
	b, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return false
	}
	left, lok := b.X.(*ast.Ident)
	right, rok := b.Y.(*ast.Ident)
	if !lok || !rok {
		return false
	}
	if right.Name == "nil" && isErrorIdent(pass, left) {
		return true
	}
	return left.Name == "nil" && isErrorIdent(pass, right)
}

func isErrorIdent(pass *analysis.Pass, id *ast.Ident) bool {
	t := pass.TypesInfo.TypeOf(id)
	return t != nil && types.Implements(t, types.Universe.Lookup("error").Type().Underlying().(*types.Interface))
}

func checkMessage(pass *analysis.Pass, call *ast.CallExpr) {
	if len(call.Args) == 0 || (!astutil.IsPkgFunc(pass, call, "fmt", "Errorf") && !astutil.IsPkgFunc(pass, call, "errors", "New")) {
		return
	}
	s, ok := astutil.StringLit(call.Args[0])
	if !ok {
		return
	}
	if strings.HasPrefix(strings.ToLower(s), "failed to") {
		astutil.Report(pass, call.Args[0].Pos(), "errors-06", "error message at %s starts with a \"failed to\" prefix — use a noun phrase instead (e.g. \"fetching user %%s: %%w\")", pass.Fset.Position(call.Args[0].Pos()))
	}
	if reason, bad := stringStyleViolation(s); bad {
		astutil.Report(pass, call.Args[0].Pos(), "errors-07", "error string at %s %s — use lowercase, no trailing punctuation, no capitalized acronyms", pass.Fset.Position(call.Args[0].Pos()), reason)
	}
}

var acronymRe = regexp.MustCompile(`[A-Z]{2,}`)

func stringStyleViolation(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	r := []rune(s)
	switch {
	case unicode.IsUpper(r[0]):
		return "starts with an uppercase letter", true
	case strings.ContainsRune(".!?", r[len(r)-1]):
		return "ends with punctuation", true
	case acronymRe.MatchString(s):
		return "contains a capitalized acronym", true
	default:
		return "", false
	}
}
