// Package astutil contains shared, type-aware helpers for analysis passes.
package astutil

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

type ruleInfo struct {
	name     string
	severity finding.Severity
}

var rules = struct {
	m map[string]ruleInfo
	sync.RWMutex
}{m: make(map[string]ruleInfo)}

var (
	ruleIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*-[0-9]{2}$`)
	ruleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// RegisterRule registers one domain rule and panics on conflicting metadata.
func RegisterRule(rule, name string, severity finding.Severity) {
	if !ruleIDPattern.MatchString(rule) {
		panic("invalid rule ID: " + rule)
	}
	if !ruleNamePattern.MatchString(name) {
		panic("invalid rule name: " + name)
	}
	if severity != finding.SeverityError && severity != finding.SeverityWarning && severity != finding.SeverityInfo {
		panic("invalid rule severity: " + string(severity))
	}
	rules.Lock()
	defer rules.Unlock()
	if old, ok := rules.m[rule]; ok {
		if old.name != name || old.severity != severity {
			panic(fmt.Sprintf("conflicting rule registration: %s", rule))
		}
		panic(fmt.Sprintf("duplicate rule registration: %s", rule))
	}
	rules.m[rule] = ruleInfo{name: name, severity: severity}
}

// RuleSeverity returns the registered severity for rule.
func RuleSeverity(rule string) finding.Severity {
	rules.RLock()
	v, ok := rules.m[rule]
	rules.RUnlock()
	if !ok {
		panic("unknown rule: " + rule)
	}
	return v.severity
}

// RuleName returns the registered display name for rule.
func RuleName(rule string) string {
	rules.RLock()
	v, ok := rules.m[rule]
	rules.RUnlock()
	if !ok {
		panic("unknown rule: " + rule)
	}
	return v.name
}

// RulesInDomain returns registered rule IDs in domain order.
func RulesInDomain(domain string) []string {
	rules.RLock()
	defer rules.RUnlock()
	var out []string
	for rule := range rules.m {
		if strings.HasPrefix(rule, domain+"-") {
			out = append(out, rule)
		}
	}
	sort.Strings(out)
	return out
}

// Report emits a diagnostic with validated rule metadata.
func Report(pass *analysis.Pass, pos token.Pos, rule, tmpl string, args ...any) {
	// Resolve both registry fields here so a typo cannot emit an untyped diagnostic.
	_ = RuleName(rule)
	_ = RuleSeverity(rule)
	pass.Report(analysis.Diagnostic{Pos: pos, Message: fmt.Sprintf(tmpl, args...), Category: rule})
}

func objectForCall(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	if pass == nil || call == nil || pass.TypesInfo == nil {
		return nil
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if obj, ok := pass.TypesInfo.Uses[fun].(*types.Func); ok {
			return obj
		}
	case *ast.SelectorExpr:
		if obj, ok := pass.TypesInfo.Uses[fun.Sel].(*types.Func); ok {
			return obj
		}
	}
	return nil
}

// IsPkgFunc reports whether call resolves to a package-level function.
func IsPkgFunc(pass *analysis.Pass, call *ast.CallExpr, pkgPath, name string) bool {
	fn := objectForCall(pass, call)
	return fn != nil && fn.Name() == name && fn.Pkg() != nil && fn.Pkg().Path() == pkgPath && fn.Signature().Recv() == nil
}

// IsMethodOn reports whether call resolves to a method on the named type.
func IsMethodOn(pass *analysis.Pass, call *ast.CallExpr, pkgPath, typeName, methodName string) bool {
	fn := objectForCall(pass, call)
	if fn == nil || fn.Name() != methodName || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return false
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return false
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == typeName && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == pkgPath
}

// ExprStmtCall extracts a call expression from an expression statement.
func ExprStmtCall(n ast.Node) (*ast.CallExpr, bool) {
	s, ok := n.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	c, ok := s.X.(*ast.CallExpr)
	return c, ok
}

// StringLit decodes a literal string expression.
func StringLit(e ast.Expr) (string, bool) {
	l, ok := e.(*ast.BasicLit)
	if !ok || l.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(l.Value)
	return v, err == nil
}

// FuncName returns a stable function or method name.
func FuncName(decl *ast.FuncDecl) string {
	if decl == nil || decl.Name == nil {
		return ""
	}
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}
	t := decl.Recv.List[0].Type
	for {
		if p, ok := t.(*ast.StarExpr); ok {
			t = p.X
			continue
		}
		break
	}
	if id, ok := t.(*ast.Ident); ok {
		return "(*" + id.Name + ")." + decl.Name.Name
	}
	return decl.Name.Name
}

// FilesScanned counts unique compiled files across packages.
func FilesScanned(pkgs []*packages.Package) int {
	seen := make(map[string]struct{})
	for _, p := range pkgs {
		if p == nil {
			continue
		}
		for _, f := range p.CompiledGoFiles {
			seen[f] = struct{}{}
		}
	}
	return len(seen)
}

// TypeString returns the type-checker's string form for e.
func TypeString(pass *analysis.Pass, e ast.Expr) string {
	if pass == nil || pass.TypesInfo == nil || e == nil {
		return ""
	}
	if tv, ok := pass.TypesInfo.Types[e]; ok && tv.Type != nil {
		return types.TypeString(tv.Type, nil)
	}
	if id, ok := e.(*ast.Ident); ok {
		if object := pass.TypesInfo.ObjectOf(id); object != nil && object.Type() != nil {
			return types.TypeString(object.Type(), nil)
		}
	}
	return ""
}
