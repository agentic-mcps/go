package concurrency

import (
	"fmt"
	"go/ast"
	"go/token"
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
