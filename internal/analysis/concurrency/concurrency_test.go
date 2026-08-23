package concurrency

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestUnjustifiedBuffer(t *testing.T) {
	file := parseFile(t, `package p
func f() { _ = make(chan int, 4); _ = make(chan int, 1); _ = make(chan int, size) }
const size = 4`)
	var calls []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			calls = append(calls, c)
		}
		return true
	})
	comments := ast.NewCommentMap(token.NewFileSet(), file, file.Comments)
	if !unjustifiedBuffer(calls[0], comments, calls[0]) || unjustifiedBuffer(calls[1], comments, calls[1]) || unjustifiedBuffer(calls[2], comments, calls[2]) {
		t.Fatal("buffer predicate mismatch")
	}
}

func TestSignatureDirection(t *testing.T) {
	file := parseFile(t, `package p
func bad(c chan int) {}
func good(in <-chan int, out chan<- int) {}`)
	var funcs []*ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			funcs = append(funcs, f)
		}
	}
	if funcs[0].Type.Params.List[0].Type.(*ast.ChanType).Dir != ast.SEND|ast.RECV {
		t.Fatal("expected undirected channel")
	}
	if funcs[1].Type.Params.List[0].Type.(*ast.ChanType).Dir == ast.SEND|ast.RECV || funcs[1].Type.Params.List[1].Type.(*ast.ChanType).Dir == ast.SEND|ast.RECV {
		t.Fatal("directed channel flagged")
	}
}

func TestAsyncAndInitPredicates(t *testing.T) {
	file := parseFile(t, `package p
func ProcessAsync() { go work() }
func ProcessAsyncSignal() <-chan int { go work(); return nil }
func init() { go work() }
func Start() { go work() }`)
	for _, d := range file.Decls {
		f, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch f.Name.Name {
		case "ProcessAsync":
			if !isAsyncWrapper(f) {
				t.Fatal("async wrapper missed")
			}
		case "ProcessAsyncSignal":
			if isAsyncWrapper(f) {
				t.Fatal("awaitable async wrapper flagged")
			}
		case "init":
			if !isInitWithGoroutine(f) {
				t.Fatal("init goroutine missed")
			}
		case "Start":
			if isInitWithGoroutine(f) {
				t.Fatal("regular function flagged as init")
			}
		}
	}
}

func TestDeferEnclosingLoop(t *testing.T) {
	file := parseFile(t, `package p
func f(xs []int) { for range xs { defer closeIt() }; for range xs { func() { defer closeIt() }() } }`)
	var defers []*ast.DeferStmt
	ast.Inspect(file, func(n ast.Node) bool {
		if d, ok := n.(*ast.DeferStmt); ok {
			defers = append(defers, d)
		}
		return true
	})
	if len(defers) != 2 {
		t.Fatalf("got %d defers", len(defers))
	}
	stack := []ast.Node{&ast.FuncDecl{}, &ast.RangeStmt{}, defers[0]}
	if _, ok := deferEnclosingLoop(stack); !ok {
		t.Fatal("direct loop defer missed")
	}
	stack = []ast.Node{&ast.FuncDecl{}, &ast.RangeStmt{}, &ast.FuncLit{}, defers[1]}
	if _, ok := deferEnclosingLoop(stack); ok {
		t.Fatal("closure-scoped defer flagged")
	}
}

func parseFile(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
