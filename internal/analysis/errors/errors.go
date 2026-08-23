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
	astutil.RegisterRule("errors-04", "log_and_return", finding.SeverityError)
	astutil.RegisterRule("errors-05", "bare_return_error", finding.SeverityWarning)
	astutil.RegisterRule("errors-09", "discarded_error", finding.SeverityError)
	astutil.RegisterRule("errors-10", "library_panic", finding.SeverityError)
	astutil.RegisterRule("errors-11", "must_outside_startup", finding.SeverityError)
	astutil.RegisterRule("errors-12", "error_string_boundary", finding.SeverityWarning)
	astutil.RegisterRule("errors-13", "multiple_wrap_verbs", finding.SeverityWarning)
	astutil.RegisterRule("errors-17", "error_string_retry", finding.SeverityWarning)
	astutil.RegisterRule("errors-14", "wrapper_without_unwrap", finding.SeverityWarning)
	astutil.RegisterRule("errors-16", "deferred_close_error", finding.SeverityError)
	astutil.RegisterRule("errors-06", "failed_to_prefix", finding.SeverityInfo)
	astutil.RegisterRule("errors-07", "error_string_style", finding.SeverityInfo)
}

func run(pass *analysis.Pass) (any, error) {
	n, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, fmt.Errorf("inspect result has type %T", pass.ResultOf[inspect.Analyzer])
	}
	n.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil), (*ast.IfStmt)(nil), (*ast.CallExpr)(nil), (*ast.TypeSpec)(nil)}, func(node ast.Node) {
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
			if name, ok := errorCheckIdent(pass, x); ok {
				if logCall, ret, matched := logThenReturn(x.Body, name); matched {
					sel := logCall.Fun.(*ast.SelectorExpr)
					astutil.Report(pass, x.Pos(), "errors-04", "%s is logged via %s and then returned at %s — pick one: log, or wrap and return, never both", name.Name, sel.Sel.Name, pass.Fset.Position(ret.Pos()))
				} else if bareReturn(x.Body, name) {
					astutil.Report(pass, x.Pos(), "errors-05", "%s returned bare with no context at %s — wrap at this boundary: fmt.Errorf(\"...: %%w\", %s)", name.Name, pass.Fset.Position(x.Pos()), name.Name)
				}
			}
		case *ast.CallExpr:
			checkMessage(pass, x)
			checkStringMatch(pass, x)
		case *ast.TypeSpec:
			if st, ok := x.Type.(*ast.StructType); ok && wrapperWithoutUnwrap(pass, x, st) {
				astutil.Report(pass, x.Pos(), "errors-14", "%s wraps an error field but has no Unwrap() error method at %s — errors.Is/As stop at this type", x.Name.Name, pass.Fset.Position(x.Pos()))
			}
		}
	})
	for _, file := range pass.Files {
		cmap := ast.NewCommentMap(pass.Fset, file, file.Comments)
		ast.Inspect(file, func(node ast.Node) bool {
			switch x := node.(type) {
			case *ast.AssignStmt:
				if isBareErrDiscard(pass, x) && len(cmap[x]) == 0 {
					astutil.Report(pass, x.Pos(), "errors-09", "error discarded with \"_ = %s\" at %s with no comment explaining why it's safe", errorDiscardName(x), pass.Fset.Position(x.Pos()))
				}
			case *ast.CallExpr:
				if isPanicCall(pass, x) && !startupContext(pass, x.Pos()) {
					astutil.Report(pass, x.Pos(), "errors-10", "panic(...) at %s outside main/init — return an error instead; panic is not control flow in library code", pass.Fset.Position(x.Pos()))
				}
				if name, ok := mustCall(x); ok && !allowedMust(pass, x.Pos(), name) {
					astutil.Report(pass, x.Pos(), "errors-11", "%s called at %s outside main/init/TestMain/package-init — Must* helpers panic and must not run in request or worker paths", name, pass.Fset.Position(x.Pos()))
				}
			case *ast.DeferStmt:
				if bareDeferClose(pass, x, enclosingFunctionResults(pass, x.Pos())) {
					astutil.Report(pass, x.Pos(), "errors-16", "defer %s.Close() at %s drops the close error — use a named return and assign it in the deferred func when err == nil", closeReceiver(x), pass.Fset.Position(x.Pos()))
				}
			}
			return true
		})
	}
	return nil, nil
}

func checkStringMatch(pass *analysis.Pass, call *ast.CallExpr) {
	if !astutil.IsPkgFunc(pass, call, "strings", "Contains") || len(call.Args) != 2 {
		return
	}
	inner, ok := call.Args[0].(*ast.CallExpr)
	if !ok || len(inner.Args) != 0 {
		return
	}
	sel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" || pass.TypesInfo.TypeOf(sel.X) == nil || !types.Implements(pass.TypesInfo.TypeOf(sel.X), types.Universe.Lookup("error").Type().Underlying().(*types.Interface)) {
		return
	}
	if _, ok := astutil.StringLit(call.Args[1]); !ok {
		return
	}
	fn := enclosingFunc(pass, call.Pos())
	rule := "errors-12"
	if fn != nil && fn.Type.Results != nil && len(fn.Type.Results.List) == 1 && astutil.TypeString(pass, fn.Type.Results.List[0].Type) == "bool" {
		rule = "errors-17"
	}
	if rule == "errors-17" {
		astutil.Report(pass, call.Pos(), rule, "retry decision at %s classifies by strings.Contains(err.Error(), ...) — use errors.Is(err, ...) or a typed RetryableError/Temporary() method", pass.Fset.Position(call.Pos()))
	} else {
		astutil.Report(pass, call.Pos(), rule, "error classified by strings.Contains(err.Error(), ...) at %s — use errors.Is/As against a sentinel or typed error for canonical code mapping", pass.Fset.Position(call.Pos()))
	}
}

func checkMultipleWrap(pass *analysis.Pass, call *ast.CallExpr) {
	if !astutil.IsPkgFunc(pass, call, "fmt", "Errorf") || len(call.Args) == 0 {
		return
	}
	s, ok := astutil.StringLit(call.Args[0])
	if !ok {
		return
	}
	if n := countWrapVerbs(s); n > 1 {
		astutil.Report(pass, call.Pos(), "errors-13", "fmt.Errorf at %s carries %d %%w verbs — %%w wraps one cause per call; use errors.Join for independent parallel failures", pass.Fset.Position(call.Pos()), n)
	}
}

func countWrapVerbs(format string) int {
	count := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			continue
		}
		i++
		if format[i] == '%' {
			continue
		}
		for ; i < len(format); i++ {
			if format[i] == 'w' {
				count++
				break
			}
			if (format[i] >= 'a' && format[i] <= 'z') || (format[i] >= 'A' && format[i] <= 'Z') {
				break
			}
		}
	}
	return count
}

func isBareErrDiscard(pass *analysis.Pass, a *ast.AssignStmt) bool {
	if a.Tok != token.ASSIGN || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return false
	}
	l, ok := a.Lhs[0].(*ast.Ident)
	if !ok || l.Name != "_" {
		return false
	}
	id, ok := a.Rhs[0].(*ast.Ident)
	return ok && isErrorIdent(pass, id)
}
func errorDiscardName(a *ast.AssignStmt) string {
	if id, ok := a.Rhs[0].(*ast.Ident); ok {
		return id.Name
	}
	return "error"
}
func isPanicCall(pass *analysis.Pass, c *ast.CallExpr) bool {
	id, ok := c.Fun.(*ast.Ident)
	if !ok || id.Name != "panic" {
		return false
	}
	_, ok = pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok
}
func enclosingFunc(pass *analysis.Pass, pos token.Pos) *ast.FuncDecl {
	for _, f := range pass.Files {
		var found *ast.FuncDecl
		ast.Inspect(f, func(n ast.Node) bool {
			if d, ok := n.(*ast.FuncDecl); ok && d.Pos() <= pos && pos <= d.End() {
				found = d
				return true
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}
func startupContext(pass *analysis.Pass, pos token.Pos) bool {
	f := enclosingFunc(pass, pos)
	return f != nil && pass.Pkg.Name() == "main" && (f.Name.Name == "main" || f.Name.Name == "init")
}
func mustCall(c *ast.CallExpr) (string, bool) {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		if strings.HasPrefix(f.Name, "Must") {
			return f.Name, true
		}
	case *ast.SelectorExpr:
		if strings.HasPrefix(f.Sel.Name, "Must") {
			return f.Sel.Name, true
		}
	}
	return "", false
}
func allowedMust(pass *analysis.Pass, pos token.Pos, name string) bool {
	f := enclosingFunc(pass, pos)
	if f == nil {
		return true
	}
	return f.Name.Name == "main" || f.Name.Name == "init" || f.Name.Name == "TestMain" || strings.HasPrefix(f.Name.Name, "Must")
}

func hasMethodReturning(t types.Type, name, result string) bool {
	ms := types.NewMethodSet(types.NewPointer(t))
	for i := 0; i < ms.Len(); i++ {
		method := ms.At(i).Obj()
		if method.Name() != name {
			continue
		}
		sig, ok := method.Type().(*types.Signature)
		return ok && sig.Params().Len() == 0 && sig.Results().Len() == 1 && types.TypeString(sig.Results().At(0).Type(), nil) == result
	}
	return false
}
func wrapperWithoutUnwrap(pass *analysis.Pass, ts *ast.TypeSpec, st *ast.StructType) bool {
	hasErr := false
	for _, f := range st.Fields.List {
		if len(f.Names) > 0 && astutil.TypeString(pass, f.Type) == "error" {
			hasErr = true
		}
	}
	if !hasErr {
		return false
	}
	obj, ok := pass.TypesInfo.Defs[ts.Name]
	if !ok {
		return false
	}
	named, ok := obj.Type().(*types.Named)
	return ok && hasMethodReturning(named, "Error", "string") && !hasMethodReturning(named, "Unwrap", "error")
}
func bareDeferClose(pass *analysis.Pass, ds *ast.DeferStmt, results *ast.FieldList) bool {
	if results == nil {
		return false
	}
	hasErr := false
	for _, f := range results.List {
		if astutil.TypeString(pass, f.Type) == "error" {
			hasErr = true
		}
	}
	if !hasErr || len(ds.Call.Args) != 0 {
		return false
	}
	sel, ok := ds.Call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return false
	}
	method, ok := pass.TypesInfo.ObjectOf(sel.Sel).(*types.Func)
	if !ok {
		return false
	}
	sig, ok := method.Type().(*types.Signature)
	return ok && sig.Params().Len() == 0 && sig.Results().Len() == 1 && types.TypeString(sig.Results().At(0).Type(), nil) == "error"
}

func enclosingFunctionResults(pass *analysis.Pass, pos token.Pos) *ast.FieldList {
	var results *ast.FieldList
	for _, file := range pass.Files {
		if pos < file.Pos() || pos > file.End() {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Pos() <= pos && pos <= fn.End() {
					results = fn.Type.Results
				}
			case *ast.FuncLit:
				if fn.Pos() <= pos && pos <= fn.End() {
					results = fn.Type.Results
				}
			}
			return true
		})
	}
	return results
}
func closeReceiver(ds *ast.DeferStmt) string {
	if s, ok := ds.Call.Fun.(*ast.SelectorExpr); ok {
		return formatExpr(s.X)
	}
	return "resource"
}
func formatExpr(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "resource"
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
	_, ok := errorCheckIdent(pass, stmt)
	return ok
}

func errorCheckIdent(pass *analysis.Pass, stmt *ast.IfStmt) (*ast.Ident, bool) {
	b, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return nil, false
	}
	left, lok := b.X.(*ast.Ident)
	right, rok := b.Y.(*ast.Ident)
	if !lok || !rok {
		return nil, false
	}
	if right.Name == "nil" && isErrorIdent(pass, left) {
		return left, true
	}
	if left.Name == "nil" && isErrorIdent(pass, right) {
		return right, true
	}
	return nil, false
}

func isErrorIdent(pass *analysis.Pass, id *ast.Ident) bool {
	t := pass.TypesInfo.TypeOf(id)
	return t != nil && types.Implements(t, types.Universe.Lookup("error").Type().Underlying().(*types.Interface))
}

var loggingMethods = map[string]bool{"Error": true, "Errorf": true, "Errorln": true, "ErrorContext": true, "Warn": true, "Warnf": true, "WarnContext": true, "Err": true}

func logThenReturn(block *ast.BlockStmt, name *ast.Ident) (*ast.CallExpr, *ast.ReturnStmt, bool) {
	for i, stmt := range block.List {
		call, ok := astutil.ExprStmtCall(stmt)
		if !ok || !isLoggingCall(call) || !references(call, name.Name) {
			continue
		}
		for _, later := range block.List[i+1:] {
			if ret, ok := later.(*ast.ReturnStmt); ok && references(ret, name.Name) {
				return call, ret, true
			}
		}
	}
	return nil, nil, false
}

func isLoggingCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && loggingMethods[sel.Sel.Name]
}

func references(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func bareReturn(block *ast.BlockStmt, name *ast.Ident) bool {
	for _, stmt := range block.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if ok {
			for _, result := range ret.Results {
				if id, ok := result.(*ast.Ident); ok && id.Name == name.Name {
					return true
				}
			}
		}
	}
	return false
}

func checkMessage(pass *analysis.Pass, call *ast.CallExpr) {
	checkMultipleWrap(pass, call)
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
