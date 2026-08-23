package concurrency

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

func init() {
	astutil.RegisterRule("concurrency-03", "unjustified_channel_buffer", finding.SeverityWarning)
	astutil.RegisterRule("concurrency-01", "fire_and_forget_goroutine", finding.SeverityError)
	astutil.RegisterRule("concurrency-06", "background_context_in_goroutine", finding.SeverityError)
	astutil.RegisterRule("concurrency-07", "lock_defer_gap", finding.SeverityError)
	astutil.RegisterRule("concurrency-08", "embedded_sync_primitive", finding.SeverityError)
	astutil.RegisterRule("concurrency-10", "undocumented_concurrency_safety", finding.SeverityInfo)
	astutil.RegisterRule("concurrency-12", "manual_waitgroup_error_channel", finding.SeverityWarning)
	astutil.RegisterRule("concurrency-14", "pool_put_without_reset", finding.SeverityError)
	astutil.RegisterRule("concurrency-15", "compound_state_multiple_atomics", finding.SeverityWarning)
	astutil.RegisterRule("concurrency-04", "undirected_channel_signature", finding.SeverityWarning)
	astutil.RegisterRule("concurrency-05", "async_goroutine_wrapper", finding.SeverityWarning)
	astutil.RegisterRule("concurrency-09", "goroutine_in_init", finding.SeverityWarning)
	astutil.RegisterRule("concurrency-20", "defer_in_loop", finding.SeverityWarning)
}

var Analyzer = &analysis.Analyzer{
	Name:     "concurrency",
	Doc:      "Audits selected goroutine and channel conventions.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	ins, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, fmt.Errorf("inspect result has type %T", pass.ResultOf[inspect.Analyzer])
	}
	atomicByType := make(map[string][]*ast.Field)
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				if fields := atomicFields(pass, st); len(fields) >= 2 {
					atomicByType[ts.Name.Name] = fields
				}
			}
		}
	}
	for _, file := range pass.Files {
		comments := ast.NewCommentMap(pass.Fset, file, file.Comments)
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && unjustifiedBuffer(call, comments, call) {
				lit := call.Args[1].(*ast.BasicLit)
				n, _ := strconv.Atoi(lit.Value)
				astutil.Report(pass, call.Pos(), "concurrency-03", "channel buffer size %d at %s exceeds 1 with no justifying comment", n, pass.Fset.Position(call.Pos()))
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if b, ok := n.(*ast.BlockStmt); ok {
				if found, count := manualWaitGroupErrChanPattern(pass, b); found {
					astutil.Report(pass, b.Pos(), "concurrency-12", "function at %s hand-rolls WaitGroup + error-channel coordination across %d goroutines — use errgroup.Group to propagate the first error and cancel the rest", pass.Fset.Position(b.Pos()), count)
				}
				for _, call := range putWithoutReset(pass, b) {
					astutil.Report(pass, call.Pos(), "concurrency-14", "sync.Pool.Put(%s) at %s has no preceding Reset() call — a pooled item may leak stale data or caller-owned references to the next Get()", types.ExprString(call.Args[0]), pass.Fset.Position(call.Pos()))
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) > 0 {
				if name := receiverName(fd); len(atomicByType[name]) >= 2 && methodMutatesMultipleAtomicsWithoutMutex(pass, fd, atomicByType[name]) {
					astutil.Report(pass, fd.Pos(), "concurrency-15", "method %s at %s mutates %d independent atomic fields with no mutex — compound state needs a mutex, not multiple atomics", fd.Name.Name, pass.Fset.Position(fd.Pos()), len(atomicByType[name]))
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BlockStmt:
				for _, stmt := range lockDeferGapViolations(pass, n) {
					astutil.Report(pass, stmt.Pos(), "concurrency-07", "%s() at %s is not immediately followed by defer %s() — code between lock and defer is a panic window", lockMethodName(stmt), pass.Fset.Position(stmt.Pos()), unlockMethodName(stmt))
				}
			case *ast.StructType:
				for _, field := range embeddedSyncPrimitive(pass, n) {
					astutil.Report(pass, field.Pos(), "concurrency-08", "struct embeds %s anonymously at %s, promoting Lock/Unlock (or Add/Done/Wait) to the public API — use a named field instead", astutil.TypeString(pass, field.Type), pass.Fset.Position(field.Pos()))
				}
			}
			return true
		})
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() || !docNeedsSafety(pass, gd, ts) {
					continue
				}
				astutil.Report(pass, ts.Pos(), "concurrency-10", "exported type %s at %s has synchronization fields but its doc comment does not state whether it is safe for concurrent use", ts.Name.Name, pass.Fset.Position(ts.Pos()))
			}
		}
	}
	ins.Nodes(nil, func(n ast.Node, _ bool) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if isInitWithGoroutine(n) {
				astutil.Report(pass, n.Pos(), "concurrency-09", "init() at %s spawns a goroutine — startup ordering becomes implicit and fragile", pass.Fset.Position(n.Pos()))
			}
			if isAsyncWrapper(n) {
				astutil.Report(pass, n.Pos(), "concurrency-05", "%s fires a goroutine internally instead of letting the caller add concurrency via errgroup or a worker pool", n.Name.Name)
			}
			reportSignatureChannels(pass, n.Type)
		case *ast.FuncLit:
			reportSignatureChannels(pass, n.Type)
		}
		return true
	})
	ins.WithStack(nil, func(_ ast.Node, _ bool, stack []ast.Node) bool {
		if gs, ok := stack[len(stack)-1].(*ast.GoStmt); ok {
			if !strings.HasSuffix(pass.Fset.Position(gs.Pos()).Filename, "_test.go") {
				var block *ast.BlockStmt
				main := false
				for i := len(stack) - 2; i >= 0; i-- {
					switch n := stack[i].(type) {
					case *ast.BlockStmt:
						if block == nil {
							block = n
						}
					case *ast.FuncDecl:
						main = n.Recv == nil && n.Name != nil && n.Name.Name == "main"
					}
				}
				if block != nil && isFireAndForget(pass, gs, block) {
					astutil.Report(pass, gs.Pos(), "concurrency-01", "goroutine spawned at %s has no owner, stop signal, or waiter — wrap it in a Start/Stop-owned worker, errgroup.Group, or a WaitGroup", pass.Fset.Position(gs.Pos()))
				}
				if !main {
					if lit, ok := gs.Call.Fun.(*ast.FuncLit); ok && hasBackgroundInGoroutine(pass, lit) {
						astutil.Report(pass, gs.Pos(), "concurrency-06", "goroutine at %s calls context.Background()/context.TODO() instead of deriving from the parent context — orphans work after client disconnect", pass.Fset.Position(gs.Pos()))
					}
				}
			}
		}
		deferStmt, ok := stack[len(stack)-1].(*ast.DeferStmt)
		if ok {
			if loop, found := deferEnclosingLoop(stack); found {
				kind := "for"
				if _, ok := loop.(*ast.RangeStmt); ok {
					kind = "range"
				}
				astutil.Report(pass, deferStmt.Pos(), "concurrency-20", "defer at %s is directly inside a %s loop with no enclosing function-literal scope — it accumulates until the enclosing function returns instead of running per iteration; wrap the loop body in an immediately-invoked closure (func(){ defer ... }()) or move the deferred call outside the loop", pass.Fset.Position(deferStmt.Pos()), kind)
			}
		}
		return true
	})
	return nil, nil
}

func manualWaitGroupErrChanPattern(pass *analysis.Pass, body *ast.BlockStmt) (bool, int) {
	hasWG, hasErr, count := false, false, 0
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GoStmt:
			count++
		case *ast.ValueSpec:
			for _, name := range v.Names {
				if strings.Contains(astutil.TypeString(pass, name), "sync.WaitGroup") {
					hasWG = true
				}
			}
		case *ast.CallExpr:
			id, ok := v.Fun.(*ast.Ident)
			if ok && id.Name == "make" && len(v.Args) > 0 {
				if ct, ok := v.Args[0].(*ast.ChanType); ok && astutil.TypeString(pass, ct.Value) == "error" {
					hasErr = true
				}
			}
		}
		return true
	})
	return hasWG && hasErr && count >= 2, count
}
func putWithoutReset(pass *analysis.Pass, block *ast.BlockStmt) []*ast.CallExpr {
	var out []*ast.CallExpr
	for i, stmt := range block.List {
		call, ok := astutil.ExprStmtCall(stmt)
		if !ok || len(call.Args) == 0 {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Put" || !strings.Contains(astutil.TypeString(pass, sel.X), "sync.Pool") {
			continue
		}
		typ := pass.TypesInfo.TypeOf(call.Args[0])
		if typ == nil || !hasResetMethod(typ) {
			continue
		}
		if i == 0 || !precedingCallIsReset(block.List[i-1], types.ExprString(call.Args[0])) {
			out = append(out, call)
		}
	}
	return out
}
func hasResetMethod(t types.Type) bool {
	ms := types.NewMethodSet(t)
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Obj().Name() == "Reset" {
			return true
		}
	}
	return false
}
func precedingCallIsReset(stmt ast.Stmt, arg string) bool {
	c, ok := astutil.ExprStmtCall(stmt)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	return ok && s.Sel.Name == "Reset" && types.ExprString(s.X) == arg
}
func atomicFields(pass *analysis.Pass, st *ast.StructType) []*ast.Field {
	var out []*ast.Field
	for _, f := range st.Fields.List {
		if len(f.Names) > 0 && strings.HasPrefix(astutil.TypeString(pass, f.Type), "sync/atomic.") {
			out = append(out, f)
		}
	}
	return out
}
func methodMutatesMultipleAtomicsWithoutMutex(pass *analysis.Pass, fd *ast.FuncDecl, fields []*ast.Field) bool {
	touched := map[string]bool{}
	locked := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if s.Sel.Name == "Lock" || s.Sel.Name == "RLock" {
			locked = true
		}
		x, ok := s.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		for _, f := range fields {
			if x.Sel.Name == f.Names[0].Name && map[string]bool{"Store": true, "Add": true, "Swap": true, "CompareAndSwap": true}[s.Sel.Name] {
				touched[x.Sel.Name] = true
			}
		}
		return true
	})
	return !locked && len(touched) >= 2
}
func receiverName(fd *ast.FuncDecl) string {
	t := fd.Recv.List[0].Type
	if p, ok := t.(*ast.StarExpr); ok {
		t = p.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
func lockDeferGapViolations(pass *analysis.Pass, block *ast.BlockStmt) []ast.Stmt {
	var bad []ast.Stmt
	for i, stmt := range block.List {
		recv, method, ok := lockCall(stmt)
		if !ok {
			continue
		}
		typ := astutil.TypeString(pass, receiverExpr(stmt))
		if !strings.Contains(typ, "sync.Mutex") && !strings.Contains(typ, "sync.RWMutex") {
			continue
		}
		want := "Unlock"
		if method == "RLock" {
			want = "RUnlock"
		}
		if i+1 >= len(block.List) {
			bad = append(bad, stmt)
			continue
		}
		def, ok := block.List[i+1].(*ast.DeferStmt)
		if !ok || !isUnlockCallOn(def.Call, recv, want) {
			bad = append(bad, stmt)
		}
	}
	return bad
}
func lockCall(stmt ast.Stmt) (string, string, bool) {
	c, ok := astutil.ExprStmtCall(stmt)
	if !ok {
		return "", "", false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || (s.Sel.Name != "Lock" && s.Sel.Name != "RLock") {
		return "", "", false
	}
	return types.ExprString(s.X), s.Sel.Name, true
}
func receiverExpr(stmt ast.Stmt) ast.Expr {
	c, _ := astutil.ExprStmtCall(stmt)
	return c.Fun.(*ast.SelectorExpr).X
}
func lockMethodName(stmt ast.Stmt) string { _, method, _ := lockCall(stmt); return method }
func unlockMethodName(stmt ast.Stmt) string {
	_, method, _ := lockCall(stmt)
	if method == "RLock" {
		return "RUnlock"
	}
	return "Unlock"
}
func isUnlockCallOn(call *ast.CallExpr, recv, method string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == method && types.ExprString(sel.X) == recv
}
func embeddedSyncPrimitive(pass *analysis.Pass, st *ast.StructType) []*ast.Field {
	var bad []*ast.Field
	for _, f := range st.Fields.List {
		if len(f.Names) != 0 {
			continue
		}
		switch astutil.TypeString(pass, f.Type) {
		case "sync.Mutex", "sync.RWMutex", "sync.WaitGroup", "*sync.Mutex", "*sync.RWMutex", "*sync.WaitGroup":
			bad = append(bad, f)
		}
	}
	return bad
}
func docNeedsSafety(pass *analysis.Pass, gd *ast.GenDecl, ts *ast.TypeSpec) bool {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return false
	}
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue
		}
		s := astutil.TypeString(pass, f.Type)
		if strings.Contains(s, "sync.Mutex") || strings.Contains(s, "sync.RWMutex") || strings.Contains(s, "sync.WaitGroup") {
			doc := ts.Doc
			if doc == nil {
				doc = gd.Doc
			}
			if doc == nil {
				return true
			}
			text := strings.ToLower(doc.Text())
			for _, kw := range []string{"safe for concurrent", "not safe for concurrent", "goroutine-safe", "concurrency-safe", "thread-safe"} {
				if strings.Contains(text, kw) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func isFireAndForget(pass *analysis.Pass, gs *ast.GoStmt, enclosing *ast.BlockStmt) bool {
	for _, stmt := range enclosing.List {
		call, ok := astutil.ExprStmtCall(stmt)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if sel.Sel.Name == "Add" && strings.Contains(astutil.TypeString(pass, sel.X), "sync.WaitGroup") {
			return false
		}
		if sel.Sel.Name == "Go" && strings.Contains(astutil.TypeString(pass, sel.X), "errgroup.Group") {
			return false
		}
	}
	if lit, ok := gs.Call.Fun.(*ast.FuncLit); ok {
		return !hasDoneSelect(lit.Body)
	}
	for _, arg := range gs.Call.Args {
		if astutil.TypeString(pass, arg) == "context.Context" {
			return false
		}
	}
	return true
}
func hasDoneSelect(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		cc, ok := n.(*ast.CommClause)
		if !ok || cc.Comm == nil {
			return true
		}
		var call *ast.CallExpr
		switch comm := cc.Comm.(type) {
		case *ast.ExprStmt:
			if ue, ok := comm.X.(*ast.UnaryExpr); ok && ue.Op == token.ARROW {
				call, _ = ue.X.(*ast.CallExpr)
			}
		case *ast.AssignStmt:
			if len(comm.Rhs) == 1 {
				if ue, ok := comm.Rhs[0].(*ast.UnaryExpr); ok && ue.Op == token.ARROW {
					call, _ = ue.X.(*ast.CallExpr)
				}
			}
		}
		if call != nil {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Done" {
				found = true
			}
		}
		return true
	})
	return found
}
func hasBackgroundInGoroutine(pass *analysis.Pass, lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && (astutil.IsPkgFunc(pass, call, "context", "Background") || astutil.IsPkgFunc(pass, call, "context", "TODO")) {
			found = true
		}
		return true
	})
	return found
}

func unjustifiedBuffer(call *ast.CallExpr, cmap ast.CommentMap, stmt ast.Node) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "make" || len(call.Args) < 2 {
		return false
	}
	if _, ok := call.Args[0].(*ast.ChanType); !ok {
		return false
	}
	lit, ok := call.Args[1].(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return false
	}
	n, err := strconv.Atoi(lit.Value)
	if err != nil || n <= 1 {
		return false
	}
	return len(cmap[stmt]) == 0
}

func reportSignatureChannels(pass *analysis.Pass, ft *ast.FuncType) {
	if ft == nil {
		return
	}
	for _, fields := range []*ast.FieldList{ft.Params, ft.Results} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			if ct, ok := field.Type.(*ast.ChanType); ok && ct.Dir == ast.SEND|ast.RECV {
				astutil.Report(pass, ct.Pos(), "concurrency-04", "parameter/return channel at %s has no direction (chan T) — declare chan<- T or <-chan T", pass.Fset.Position(ct.Pos()))
			}
		}
	}
}

func containsGoStmt(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			found = true
			return false
		}
		return !found
	})
	return found
}
func isAsyncWrapper(fd *ast.FuncDecl) bool {
	if fd.Name == nil || !strings.HasSuffix(fd.Name.Name, "Async") || fd.Body == nil || !containsGoStmt(fd.Body) {
		return false
	}
	if fd.Type.Results != nil {
		for _, r := range fd.Type.Results.List {
			if _, ok := r.Type.(*ast.ChanType); ok {
				return false
			}
		}
	}
	return true
}
func isInitWithGoroutine(fd *ast.FuncDecl) bool {
	return fd.Recv == nil && fd.Name != nil && fd.Name.Name == "init" && fd.Body != nil && containsGoStmt(fd.Body)
}
func deferEnclosingLoop(stack []ast.Node) (ast.Node, bool) {
	for i := len(stack) - 2; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.ForStmt:
			return n, true
		case *ast.RangeStmt:
			return n, true
		case *ast.FuncLit, *ast.FuncDecl:
			return nil, false
		}
	}
	return nil, false
}
