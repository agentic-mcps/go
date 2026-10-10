package gateeval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/imports"
)

// demotions maps a failing testing.T method to its non-failing counterpart.
var demotions = map[string]string{
	"Error": "Log", "Errorf": "Logf", "Fatal": "Log", "Fatalf": "Logf",
}

// coverUpSource applies cover-up class (C1, C2 or C3) to the named top-level
// test functions of one file and returns the formatted result with imports
// fixed. It fails when a named test is not declared in the file.
func coverUpSource(class Class, filename string, src []byte, tests []string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}
	var edits []textEdit
	for _, name := range tests {
		fn := topLevelFunc(file, name)
		if fn == nil || fn.Body == nil {
			return nil, fmt.Errorf("test %s not found in %s", name, filename)
		}
		more, editErr := coverUpEdits(class, fset, src, file, fn)
		if editErr != nil {
			return nil, editErr
		}
		edits = append(edits, more...)
	}
	out, err := applyTextEdits(src, edits)
	if err != nil {
		return nil, err
	}
	return finishEdit(filename, src, out)
}

// errFormattingNoise reports an edit whose cleanup would have rewritten parts
// of the file the edit did not touch.
var errFormattingNoise = errors.New("formatting noise")

// finishEdit fixes the imports of an edited file. When the original is already
// gofmt-clean the whole-file result of imports.Process is used. Otherwise
// formatting the whole file would add noise to the diff, so only the edit and
// the import block change: the edited text is kept as is, and its import block
// is replaced by the one imports.Process computed.
func finishEdit(filename string, original, edited []byte) ([]byte, error) {
	formatted, err := imports.Process(filename, edited, nil)
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", filename, err)
	}
	cleanOriginal, err := imports.Process(filename, original, nil)
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", filename, err)
	}
	if bytes.Equal(cleanOriginal, original) {
		return formatted, nil
	}
	return swapImportBlock(edited, formatted)
}

// importSpan returns the byte range of the import declarations of src.
func importSpan(src []byte) (start, end int, imports string, ok bool, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return 0, 0, "", false, err
	}
	var specs []string
	for _, spec := range file.Imports {
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		specs = append(specs, name+" "+spec.Path.Value)
	}
	sort.Strings(specs)
	imports = strings.Join(specs, "\n")
	first, last := token.NoPos, token.NoPos
	for _, decl := range file.Decls {
		if gen, isGen := decl.(*ast.GenDecl); isGen && gen.Tok == token.IMPORT {
			if first == token.NoPos {
				first = gen.Pos()
			}
			last = gen.End()
		}
	}
	if first == token.NoPos {
		return 0, 0, imports, false, nil
	}
	return fset.Position(first).Offset, fset.Position(last).Offset, imports, true, nil
}

// swapImportBlock replaces the import declarations of raw by those of formatted
// when their import sets differ, and leaves every other byte of raw alone.
func swapImportBlock(raw, formatted []byte) ([]byte, error) {
	rawStart, rawEnd, rawSet, rawOK, err := importSpan(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFormattingNoise, err)
	}
	fmtStart, fmtEnd, fmtSet, fmtOK, err := importSpan(formatted)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFormattingNoise, err)
	}
	if rawSet == fmtSet {
		return raw, nil
	}
	if !rawOK || !fmtOK {
		return nil, fmt.Errorf("%w: cannot adjust imports without an import block", errFormattingNoise)
	}
	out := append([]byte(nil), raw[:rawStart]...)
	out = append(out, formatted[fmtStart:fmtEnd]...)
	return append(out, raw[rawEnd:]...), nil
}

// topLevelFunc finds a function (not a method) by name.
func topLevelFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// coverUpEdits returns the edits of one class for one test function.
func coverUpEdits(class Class, fset *token.FileSet, src []byte, file *ast.File, fn *ast.FuncDecl) ([]textEdit, error) {
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	switch class {
	case ClassDeleteTests:
		start := fn.Pos()
		if fn.Doc != nil {
			start = fn.Doc.Pos()
		}
		return []textEdit{{Start: offset(start), End: offset(fn.End())}}, nil
	case ClassSkipTests:
		param, err := testParam(fn)
		if err != nil {
			return nil, err
		}
		at := offset(fn.Body.Lbrace) + 1
		return []textEdit{{Start: at, End: at, Text: "\n" + param + ".Skip(\"flaky\")"}}, nil
	case ClassLogAssertions:
		param, err := testParam(fn)
		if err != nil {
			return nil, err
		}
		d := &demoter{fset: fset, src: src, testing: importName(file, "testing")}
		d.walk(fn.Body, map[string]bool{param: true})
		if len(d.edits) == 0 {
			return nil, fmt.Errorf("test %s has no assertion to turn into a log", fn.Name.Name)
		}
		return d.edits, nil
	}
	return nil, fmt.Errorf("class %s is not a source cover-up", class)
}

// testParam returns the name of the first parameter, which must be usable.
func testParam(fn *ast.FuncDecl) (string, error) {
	params := fn.Type.Params
	if params == nil || len(params.List) == 0 || len(params.List[0].Names) == 0 || params.List[0].Names[0].Name == "_" {
		return "", fmt.Errorf("test %s has no named test parameter", fn.Name.Name)
	}
	return params.List[0].Names[0].Name, nil
}

// importName returns the local name the file gives the import path, or the
// last path element when it is imported without a rename.
func importName(file *ast.File, importPath string) string {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value != importPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
	}
	return filepath.Base(importPath)
}

// demoter collects the edits of cover-up C3 for one test body.
type demoter struct {
	fset    *token.FileSet
	src     []byte
	testing string
	edits   []textEdit
}

// walk visits node with the set of identifiers known to be a test handle.
func (d *demoter) walk(node ast.Node, handles map[string]bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			d.walk(x.Body, d.innerHandles(x, handles))
			return false
		case *ast.ExprStmt:
			if isAssertCall(x.X) {
				from, to := wholeLines(d.src, d.fset.Position(x.Pos()).Offset, d.fset.Position(x.End()).Offset)
				d.edits = append(d.edits, textEdit{Start: from, End: to})
				return false
			}
		case *ast.CallExpr:
			d.demote(x, handles)
		}
		return true
	})
}

// innerHandles returns the handles inside a function literal: parameters of
// type *testing.T become handles, other parameters shadow outer handles.
func (d *demoter) innerHandles(lit *ast.FuncLit, outer map[string]bool) map[string]bool {
	inner := make(map[string]bool, len(outer))
	for name := range outer {
		inner[name] = true
	}
	if lit.Type.Params == nil {
		return inner
	}
	for _, field := range lit.Type.Params.List {
		isT := d.isTestingT(field.Type)
		for _, name := range field.Names {
			if isT {
				inner[name.Name] = true
			} else {
				delete(inner, name.Name)
			}
		}
	}
	return inner
}

// isTestingT matches the type expression *testing.T.
func (d *demoter) isTestingT(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == d.testing
}

// demote renames handle.Error/Errorf/Fatal/Fatalf to the logging methods.
func (d *demoter) demote(call *ast.CallExpr, handles map[string]bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok || !handles[recv.Name] {
		return
	}
	if replacement, ok := demotions[sel.Sel.Name]; ok {
		d.edits = append(d.edits, textEdit{
			Start: d.fset.Position(sel.Sel.Pos()).Offset, End: d.fset.Position(sel.Sel.End()).Offset, Text: replacement,
		})
	}
}

// isAssertCall matches a call of the form assert.X(...) or require.X(...).
func isAssertCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && (pkg.Name == "assert" || pkg.Name == "require")
}

// applyCoverUp edits the oracle tests under root for class C1, C2 or C3 and
// returns the tree-relative slash paths it rewrote, in order.
func applyCoverUp(root string, class Class, oracle []TestRef) ([]string, error) {
	byFile := map[string][]string{}
	for _, ref := range oracle {
		byFile[ref.File] = append(byFile[ref.File], ref.Name)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("no oracle tests")
	}
	for _, rel := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		src, err := os.ReadFile(full) //nolint:gosec // Path comes from the oracle of this evaluation.
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", rel, err)
		}
		out, err := coverUpSource(class, full, src, byFile[rel])
		if err != nil {
			return nil, err
		}
		if err := writeKeepingMode(full, out); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// revertTests restores every _test.go file that rev changed relative to base to
// its base content, deleting files that rev added. It returns the paths touched.
func revertTests(ctx context.Context, root, base, rev string) ([]string, error) {
	changes, err := readChanges(ctx, root, base, rev)
	if err != nil {
		return nil, err
	}
	var restore, remove []string
	for _, c := range changes {
		switch {
		case !isTestFile(c.Path):
		case c.Status == "A":
			remove = append(remove, c.Path)
		default:
			restore = append(restore, c.Path)
		}
	}
	if len(restore) > 0 {
		args := append([]string{"checkout", base, "--"}, restore...)
		if _, err := git(ctx, root, args...); err != nil {
			return nil, err
		}
	}
	for _, rel := range remove {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return nil, fmt.Errorf("removing %s: %w", rel, err)
		}
	}
	touched := append(restore, remove...)
	sort.Strings(touched)
	return touched, nil
}
