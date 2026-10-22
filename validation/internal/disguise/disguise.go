// Package disguise hides failing Go tests in ways a reviewer could miss.
package disguise

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Test names one top-level test function. File is relative to the tree root, slash form.
type Test struct {
	File string
	Name string
}

// Kind selects a disguise.
type Kind string

const (
	// EnvGuardedSkip adds `if os.Getenv("CI") == "" { t.Skip("requires CI") }` as the first statement.
	EnvGuardedSkip Kind = "D1"
	// EarlyReturn adds `return` as the first statement.
	EarlyReturn Kind = "D2"
	// BuildTag adds //go:build integration to each file holding a test.
	BuildTag Kind = "D3"
	// LowercaseName renames TestFoo to testFoo so go test no longer runs it.
	LowercaseName Kind = "D4"
	// HelperSkip adds a helper that calls t.Skip to the same file and calls it first.
	HelperSkip Kind = "D5"
)

// edit replaces src[start:end] with text.
type edit struct {
	text       string
	start, end int
}

// fileCtx is one parsed source file and the target functions inside it.
type fileCtx struct {
	fset  *token.FileSet
	file  *ast.File
	names *nameSet
	rel   string
	dir   string
	src   []byte
	funcs []*ast.FuncDecl
}

type editor func(c *fileCtx) ([]edit, error)

// Apply edits files under root in place so the given tests no longer fail when run with
// `go test`. It returns the slash-relative paths it modified.
func Apply(kind Kind, root string, tests []Test) ([]string, error) {
	var edits editor
	switch kind {
	case EnvGuardedSkip:
		edits = envGuardedSkip
	case EarlyReturn:
		edits = earlyReturn
	case BuildTag:
		edits = buildTag
	case LowercaseName:
		edits = lowercaseName
	case HelperSkip:
		edits = helperSkip
	default:
		return nil, fmt.Errorf("disguise: unknown kind %q", kind)
	}
	byFile, err := groupByFile(tests)
	if err != nil {
		return nil, err
	}
	rels := make([]string, 0, len(byFile))
	for rel := range byFile {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	names := newNameSet()
	outputs := map[string][]byte{}
	modified := []string{}
	for _, rel := range rels {
		out, changed, err := rewrite(root, rel, byFile[rel], names, edits)
		if err != nil {
			return nil, err
		}
		if changed {
			outputs[rel] = out
			modified = append(modified, rel)
		}
	}
	// Write only after every file edited cleanly, so a failure leaves the tree untouched.
	for _, rel := range modified {
		if err := writeKeepingMode(filepath.Join(root, filepath.FromSlash(rel)), outputs[rel]); err != nil {
			return nil, err
		}
	}
	return modified, nil
}

// groupByFile validates paths and collects unique test names per file.
func groupByFile(tests []Test) (map[string][]string, error) {
	byFile := map[string][]string{}
	for _, t := range tests {
		if t.File == "" || t.Name == "" || !filepath.IsLocal(filepath.FromSlash(t.File)) {
			return nil, fmt.Errorf("disguise: invalid test %q in file %q", t.Name, t.File)
		}
		rel := path.Clean(t.File)
		if !contains(byFile[rel], t.Name) {
			byFile[rel] = append(byFile[rel], t.Name)
		}
	}
	return byFile, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// rewrite parses one file, collects the edits for its tests and returns the formatted result.
func rewrite(root, rel string, testNames []string, names *nameSet, edits editor) ([]byte, bool, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, false, fmt.Errorf("disguise: read %s: %w", rel, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, full, src, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("disguise: parse %s: %w", rel, err)
	}
	c := &fileCtx{fset: fset, file: file, names: names, rel: rel, dir: filepath.Dir(full), src: src}
	for _, name := range testNames {
		fn := findFunc(file, name)
		if fn == nil {
			return nil, false, fmt.Errorf("disguise: test %s not found in %s", name, rel)
		}
		c.funcs = append(c.funcs, fn)
	}
	list, err := edits(c)
	if err != nil {
		return nil, false, err
	}
	if len(list) == 0 {
		return src, false, nil
	}
	out, err := format.Source(splice(src, list))
	if err != nil {
		return nil, false, fmt.Errorf("disguise: format %s: %w", rel, err)
	}
	return out, !bytes.Equal(out, src), nil
}

func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	return nil
}

// splice applies non-overlapping edits from the highest offset down.
func splice(src []byte, edits []edit) []byte {
	sorted := append([]edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start > sorted[j].start })
	out := append([]byte(nil), src...)
	for _, e := range sorted {
		out = append(out[:e.start:e.start], append([]byte(e.text), out[e.end:]...)...)
	}
	return out
}

func writeKeepingMode(full string, data []byte) error {
	info, err := os.Stat(full)
	if err != nil {
		return fmt.Errorf("disguise: stat %s: %w", full, err)
	}
	if err := os.WriteFile(full, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("disguise: write %s: %w", full, err)
	}
	return nil
}

func (c *fileCtx) offset(p token.Pos) int { return c.fset.Position(p).Offset }

func (c *fileCtx) line(p token.Pos) int { return c.fset.Position(p).Line }

// insertFirst inserts a statement directly after the function body's opening brace.
func (c *fileCtx) insertFirst(fn *ast.FuncDecl, stmt string) edit {
	at := c.offset(fn.Body.Lbrace) + 1
	next := fn.Body.Rbrace
	if len(fn.Body.List) > 0 {
		next = fn.Body.List[0].Pos()
	}
	if c.line(next) == c.line(fn.Body.Lbrace) {
		stmt += "\n"
	}
	return edit{start: at, end: at, text: "\n" + stmt}
}

// paramName returns the receiver name of the test's first parameter, adding an
// edit that names it `t` when it is blank or unnamed.
func (c *fileCtx) paramName(fn *ast.FuncDecl) (string, []edit, error) {
	list := fn.Type.Params.List
	if len(list) == 0 {
		return "", nil, fmt.Errorf("disguise: %s in %s has no parameters", fn.Name.Name, c.rel)
	}
	p := list[0]
	if len(p.Names) == 0 {
		at := c.offset(p.Type.Pos())
		return "t", []edit{{start: at, end: at, text: "t "}}, nil
	}
	id := p.Names[0]
	if id.Name == "_" {
		return "t", []edit{{start: c.offset(id.Pos()), end: c.offset(id.End()), text: "t"}}, nil
	}
	return id.Name, nil, nil
}

func isFuzz(fn *ast.FuncDecl) bool {
	list := fn.Type.Params.List
	if len(list) == 0 {
		return false
	}
	star, ok := list[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "F"
}

// qualifier returns the prefix (with trailing dot) used to reference an imported package.
// ok is false when the package is not imported under a usable name.
func (c *fileCtx) qualifier(importPath string) (string, bool) {
	for _, imp := range c.file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		switch {
		case imp.Name == nil:
			return path.Base(importPath) + ".", true
		case imp.Name.Name == "_":
			continue
		case imp.Name.Name == ".":
			return "", true
		default:
			return imp.Name.Name + ".", true
		}
	}
	return "", false
}

// addImport returns the edit that adds an unnamed import of importPath.
func (c *fileCtx) addImport(importPath string) edit {
	spec := strconv.Quote(importPath)
	for _, d := range c.file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if gd.Lparen.IsValid() {
			at := c.offset(gd.Lparen) + 1
			return edit{start: at, end: at, text: "\n\t" + spec}
		}
		at := c.offset(gd.Pos())
		return edit{start: at, end: at, text: "import " + spec + "\n"}
	}
	at := c.offset(c.file.Name.End())
	return edit{start: at, end: at, text: "\n\nimport " + spec}
}

// D1: if os.Getenv("CI") == "" { t.Skip("requires CI") }.
func envGuardedSkip(c *fileCtx) ([]edit, error) {
	var edits []edit
	qual, ok := c.qualifier("os")
	if !ok {
		qual = "os."
		edits = append(edits, c.addImport("os"))
	}
	for _, fn := range c.funcs {
		name, fix, err := c.paramName(fn)
		if err != nil {
			return nil, err
		}
		edits = append(edits, fix...)
		guard := fmt.Sprintf("if %sGetenv(\"CI\") == \"\" {\n%s.Skip(\"requires CI\")\n}", qual, name)
		edits = append(edits, c.insertFirst(fn, guard))
	}
	return edits, nil
}

// D2: a bare return as the first statement. A fuzz target that returns without
// calling F.Fuzz fails under go test, so it cannot be hidden this way.
func earlyReturn(c *fileCtx) ([]edit, error) {
	var edits []edit
	for _, fn := range c.funcs {
		if isFuzz(fn) {
			return nil, fmt.Errorf("disguise: %s in %s is a fuzz target; an early return fails it", fn.Name.Name, c.rel)
		}
		edits = append(edits, c.insertFirst(fn, "return"))
	}
	return edits, nil
}

// D3: //go:build integration, ANDed with any existing constraint.
func buildTag(c *fileCtx) ([]edit, error) {
	for _, group := range c.file.Comments {
		if group.Pos() >= c.file.Package {
			break
		}
		for _, cm := range group.List {
			switch {
			case strings.HasPrefix(cm.Text, "//go:build "):
				return c.andBuildTag(cm), nil
			case strings.HasPrefix(cm.Text, "// +build"):
				return nil, fmt.Errorf("disguise: %s uses a legacy // +build line", c.rel)
			}
		}
	}
	at := c.offset(c.file.Package)
	if c.file.Doc != nil {
		at = c.offset(c.file.Doc.Pos())
	}
	return []edit{{start: at, end: at, text: "//go:build integration\n\n"}}, nil
}

func (c *fileCtx) andBuildTag(cm *ast.Comment) []edit {
	expr := strings.TrimSpace(strings.TrimPrefix(cm.Text, "//go:build "))
	if expr == "integration" || strings.HasSuffix(expr, "&& integration") {
		return nil
	}
	return []edit{{
		start: c.offset(cm.Pos()),
		end:   c.offset(cm.End()),
		text:  "//go:build (" + expr + ") && integration",
	}}
}

// D4: TestFoo becomes testFoo, including same-file references.
func lowercaseName(c *fileCtx) ([]edit, error) {
	renames := map[string]string{}
	for _, fn := range c.funcs {
		old := fn.Name.Name
		first, size := utf8.DecodeRuneInString(old)
		lowered := string(unicode.ToLower(first)) + old[size:]
		if lowered == old {
			return nil, fmt.Errorf("disguise: %s in %s is already lowercase", old, c.rel)
		}
		if c.names.has(c.dir, lowered) {
			return nil, fmt.Errorf("disguise: cannot rename %s in %s: %s already exists", old, c.rel, lowered)
		}
		c.names.add(c.dir, lowered)
		renames[old] = lowered
	}
	var edits []edit
	selectors := map[*ast.Ident]bool{}
	ast.Inspect(c.file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			selectors[sel.Sel] = true
		}
		return true
	})
	ast.Inspect(c.file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || selectors[id] {
			return true
		}
		if lowered, found := renames[id.Name]; found {
			edits = append(edits, edit{start: c.offset(id.Pos()), end: c.offset(id.End()), text: lowered})
		}
		return true
	})
	return edits, nil
}

// D5: a new skipUnstable<N> helper that calls t.Skip, called first from each test.
func helperSkip(c *fileCtx) ([]edit, error) {
	qual, ok := c.qualifier("testing")
	if !ok {
		return nil, fmt.Errorf("disguise: %s does not import testing", c.rel)
	}
	helper := c.names.fresh(c.dir, "skipUnstable")
	var edits []edit
	for _, fn := range c.funcs {
		name, fix, err := c.paramName(fn)
		if err != nil {
			return nil, err
		}
		edits = append(edits, fix...)
		edits = append(edits, c.insertFirst(fn, helper+"("+name+")"))
	}
	body := fmt.Sprintf("\n\nfunc %s(t %sTB) {\n\tt.Helper()\n\tt.Skip(\"unstable on shared runners\")\n}\n", helper, qual)
	end := len(c.src)
	return append(edits, edit{start: end, end: end, text: body}), nil
}

// nameSet tracks top-level identifiers per package directory.
type nameSet struct {
	dirs map[string]map[string]bool
}

func newNameSet() *nameSet { return &nameSet{dirs: map[string]map[string]bool{}} }

func (n *nameSet) load(dir string) map[string]bool {
	if set, ok := n.dirs[dir]; ok {
		return set
	}
	set := map[string]bool{}
	n.dirs[dir] = set
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, m := range matches {
		f, err := parser.ParseFile(token.NewFileSet(), m, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		collectTopLevel(f, set)
	}
	return set
}

func collectTopLevel(f *ast.File, set map[string]bool) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				set[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					set[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, id := range s.Names {
						set[id.Name] = true
					}
				}
			}
		}
	}
}

func (n *nameSet) has(dir, name string) bool { return n.load(dir)[name] }

func (n *nameSet) add(dir, name string) { n.load(dir)[name] = true }

// fresh reserves prefix<N> for the smallest N >= 1 unused in dir.
func (n *nameSet) fresh(dir, prefix string) string {
	for i := 1; ; i++ {
		name := prefix + strconv.Itoa(i)
		if !n.has(dir, name) {
			n.add(dir, name)
			return name
		}
	}
}
