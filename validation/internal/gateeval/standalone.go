package gateeval

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"time"
)

// stubBody is the body S1 gives a function.
const stubBody = "{\n\tpanic(\"not implemented\")\n}"

// flawCandidate is one standalone-flaw edit of one file (S1 or S3).
type flawCandidate struct {
	File  string
	Note  string
	Edits []textEdit
}

// stubCandidates returns S1 candidates: each function whose body touches a
// changed line gets the body panic("not implemented"), in source order.
func stubCandidates(file string, src []byte, lines map[int]bool) ([]flawCandidate, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	var out []flawCandidate
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		from, to := fset.Position(fn.Body.Lbrace), fset.Position(fn.Body.Rbrace)
		if !touchesLines(lines, from.Line, to.Line) {
			continue
		}
		body := string(src[from.Offset : to.Offset+1])
		if body == stubBody {
			continue
		}
		out = append(out, flawCandidate{
			File: file, Note: "stub " + fn.Name.Name,
			Edits: []textEdit{{Start: from.Offset, End: to.Offset + 1, Text: stubBody}},
		})
	}
	return out, nil
}

// touchesLines reports whether any line in [from, to] is in lines.
func touchesLines(lines map[int]bool, from, to int) bool {
	for line := from; line <= to; line++ {
		if lines[line] {
			return true
		}
	}
	return false
}

// dropErrCandidates returns S3 candidates: an assignment to err on a changed
// line, directly followed in its block by `if err != nil {...}`; the if is
// deleted and the error assigned to the blank identifier.
func dropErrCandidates(file string, src []byte, lines map[int]bool) ([]flawCandidate, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	var out []flawCandidate
	scan := func(list []ast.Stmt) {
		for i := 0; i+1 < len(list); i++ {
			assign, ok := list[i].(*ast.AssignStmt)
			if !ok {
				continue
			}
			check, ok := list[i+1].(*ast.IfStmt)
			if !ok || !isErrCheck(check) {
				continue
			}
			from, to := fset.Position(assign.Pos()).Line, fset.Position(assign.End()).Line
			if !touchesLines(lines, from, to) {
				continue
			}
			if edits := dropErrEdits(fset, src, assign, check); edits != nil {
				out = append(out, flawCandidate{File: file, Note: fmt.Sprintf("drop error check at line %d", from), Edits: edits})
			}
		}
	}
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BlockStmt:
			scan(node.List)
		case *ast.CaseClause:
			scan(node.Body)
		case *ast.CommClause:
			scan(node.Body)
		}
		return true
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Edits[0].Start < out[j].Edits[0].Start })
	return out, nil
}

// isErrCheck matches `if err != nil { ... }` without an init statement.
func isErrCheck(stmt *ast.IfStmt) bool {
	if stmt.Init != nil {
		return false
	}
	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return false
	}
	left, ok := cond.X.(*ast.Ident)
	if !ok || left.Name != "err" {
		return false
	}
	right, ok := cond.Y.(*ast.Ident)
	return ok && right.Name == "nil"
}

// dropErrEdits builds the S3 edits, or nil when the assignment has no err on
// its left-hand side.
func dropErrEdits(fset *token.FileSet, src []byte, assign *ast.AssignStmt, check *ast.IfStmt) []textEdit {
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	var edits []textEdit
	replaced := false
	allBlank := true
	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		switch {
		case ok && ident.Name == "err" && !replaced:
			replaced = true
			edits = append(edits, textEdit{Start: offset(ident.Pos()), End: offset(ident.End()), Text: "_"})
		case !ok || ident.Name != "_":
			allBlank = false
		}
	}
	if !replaced {
		return nil
	}
	if assign.Tok == token.DEFINE && allBlank {
		edits = append(edits, textEdit{Start: offset(assign.TokPos), End: offset(assign.TokPos) + len(token.DEFINE.String()), Text: "="})
	}
	from, to := wholeLines(src, offset(check.Pos()), offset(check.End()))
	return append(edits, textEdit{Start: from, End: to})
}

// flawSearch tries standalone candidates against a checked-out tree.
type flawSearch struct {
	sources map[string][]byte
	tree    string
	direct  []string
	timeout time.Duration
}

// firstCompiling applies each candidate in order and keeps the first whose
// package builds. The accepted edit stays in the tree; a rejected one is
// undone. It returns the accepted candidate's index, or -1 when none builds.
func (f *flawSearch) firstCompiling(ctx context.Context, cands []flawCandidate) (int, error) {
	if len(f.direct) == 0 {
		return -1, errNoDirect
	}
	for i, c := range cands {
		full := filepath.Join(f.tree, filepath.FromSlash(c.File))
		out, err := applyTextEdits(f.sources[c.File], c.Edits)
		if err == nil {
			out, err = finishEdit(full, f.sources[c.File], out)
		}
		if err != nil {
			continue
		}
		if err := writeKeepingMode(full, out); err != nil {
			return -1, err
		}
		result := runStep(ctx, f.tree, f.timeout, "go", append([]string{"build"}, f.direct...)...)
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		if result.err == nil && result.exit == 0 {
			return i, nil
		}
		if err := writeKeepingMode(full, f.sources[c.File]); err != nil {
			return -1, err
		}
	}
	return -1, nil
}
