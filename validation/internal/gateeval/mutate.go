package gateeval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

// Mutation operator numbers, in the protocol's fixed order.
const (
	opNegateIf   = 1
	opFlipRel    = 2
	opZeroReturn = 3
	opDeleteStmt = 4
	opBumpInt    = 5
	opCount      = 5

	// consumerCandidates is how many compiling, direct-test-passing mutants are
	// run against the whole module when looking for a consumer break.
	consumerCandidates = 3
	// consumerTimeoutFactor scales the test timeout for go test ./....
	consumerTimeoutFactor = 5
)

// textEdit replaces src[Start:End] with Text.
type textEdit struct {
	Text       string
	Start, End int
}

// applyTextEdits applies byte-offset edits to src. An edit wholly inside an
// earlier edit's range is dropped; a partial overlap is an error.
func applyTextEdits(src []byte, edits []textEdit) ([]byte, error) {
	sorted := append([]textEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].End < sorted[j].End
	})
	var out bytes.Buffer
	cursor, lastEnd := 0, 0
	for _, e := range sorted {
		if e.Start < 0 || e.End < e.Start || e.End > len(src) {
			return nil, fmt.Errorf("edit [%d,%d) outside source of %d bytes", e.Start, e.End, len(src))
		}
		if e.Start < cursor {
			if e.End <= lastEnd {
				continue
			}
			return nil, fmt.Errorf("overlapping edits at offset %d", e.Start)
		}
		out.Write(src[cursor:e.Start])
		out.WriteString(e.Text)
		cursor, lastEnd = e.End, e.End
	}
	out.Write(src[cursor:])
	return out.Bytes(), nil
}

// wholeLines widens [start, end) to the complete lines it occupies when only
// blanks share them, so deleting a statement leaves no empty line behind.
func wholeLines(src []byte, start, end int) (from, to int) {
	from = start
	for from > 0 && (src[from-1] == ' ' || src[from-1] == '\t') {
		from--
	}
	if from > 0 && src[from-1] != '\n' {
		return start, end
	}
	to = end
	for to < len(src) && (src[to] == ' ' || src[to] == '\t' || src[to] == '\r') {
		to++
	}
	if to < len(src) && src[to] != '\n' {
		return start, end
	}
	if to < len(src) {
		to++
	}
	return from, to
}

// candidate is one mutation of one file.
type candidate struct {
	File   string // slash path relative to the tree root
	Edits  []textEdit
	Op     int
	Line   int
	Offset int
}

// label is the Operator string of a variant.
func (c candidate) label() string {
	return fmt.Sprintf("op%d@%s:%d", c.Op, c.File, c.Line)
}

// sourceFile is a changed non-test Go file with its current-side changed lines.
type sourceFile struct {
	Lines map[int]bool
	Path  string
	Src   []byte
}

// relational operators flip to their complement.
var relationalFlip = map[token.Token]string{
	token.LSS: ">=", token.GEQ: "<",
	token.GTR: "<=", token.LEQ: ">",
	token.EQL: "!=", token.NEQ: "==",
}

// syntaxCandidates returns the purely syntactic mutations (operators 1, 2, 4
// and 5) of src whose edited node starts on a changed line.
func syntaxCandidates(file string, src []byte, lines map[int]bool) ([]candidate, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var out []candidate
	add := func(op int, at token.Pos, edits ...textEdit) {
		if lines[line(at)] {
			out = append(out, candidate{File: file, Op: op, Line: line(at), Offset: offset(at), Edits: edits})
		}
	}
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			add(opNegateIf, node.Cond.Pos(),
				textEdit{Start: offset(node.Cond.Pos()), End: offset(node.Cond.Pos()), Text: "!("},
				textEdit{Start: offset(node.Cond.End()), End: offset(node.Cond.End()), Text: ")"})
		case *ast.BinaryExpr:
			if flipped, ok := relationalFlip[node.Op]; ok {
				start := offset(node.OpPos)
				add(opFlipRel, node.OpPos, textEdit{Start: start, End: start + len(node.Op.String()), Text: flipped})
			}
		case *ast.BlockStmt:
			for _, stmt := range node.List {
				out = appendDeletion(out, file, src, stmt, fset, lines)
			}
		case *ast.CaseClause:
			for _, stmt := range node.Body {
				out = appendDeletion(out, file, src, stmt, fset, lines)
			}
		case *ast.CommClause:
			for _, stmt := range node.Body {
				out = appendDeletion(out, file, src, stmt, fset, lines)
			}
		case *ast.BasicLit:
			if node.Kind == token.INT {
				if next, ok := incrementInt(node.Value); ok {
					add(opBumpInt, node.Pos(), textEdit{Start: offset(node.Pos()), End: offset(node.End()), Text: next})
				}
			}
		}
		return true
	})
	return out, nil
}

// appendDeletion adds operator 4 for a call statement or a plain assignment.
func appendDeletion(out []candidate, file string, src []byte, stmt ast.Stmt, fset *token.FileSet, lines map[int]bool) []candidate {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if _, ok := s.X.(*ast.CallExpr); !ok {
			return out
		}
	case *ast.AssignStmt:
		if s.Tok != token.ASSIGN {
			return out
		}
	default:
		return out
	}
	start := fset.Position(stmt.Pos())
	if !lines[start.Line] {
		return out
	}
	from, to := wholeLines(src, start.Offset, fset.Position(stmt.End()).Offset)
	return append(out, candidate{
		File: file, Op: opDeleteStmt, Line: start.Line, Offset: start.Offset,
		Edits: []textEdit{{Start: from, End: to}},
	})
}

// incrementInt returns the decimal text of an integer literal plus one.
func incrementInt(literal string) (string, bool) {
	value, ok := new(big.Int).SetString(literal, 0)
	if !ok {
		return "", false
	}
	return value.Add(value, big.NewInt(1)).String(), true
}

// typedFile is a parsed and type-checked non-test file.
type typedFile struct {
	fset *token.FileSet
	file *ast.File
	info *types.Info
	pkg  *types.Package
}

// loadTyped type-checks the packages in dirs (slash-form, relative to tree) and
// returns their files by tree-relative path. A package that fails to load or
// check contributes nothing.
func loadTyped(ctx context.Context, tree string, dirs []string) map[string]typedFile {
	typed := map[string]typedFile{}
	if len(dirs) == 0 {
		return typed
	}
	patterns := make([]string, len(dirs))
	for i, dir := range dirs {
		patterns[i] = "./" + dir
		if dir == "." {
			patterns[i] = "."
		}
	}
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Context: ctx, Dir: tree, Env: evalEnv(), Fset: fset,
	}, patterns...)
	if err != nil {
		return typed
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 || pkg.Types == nil || pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			tokFile := fset.File(file.Pos())
			if tokFile == nil {
				continue
			}
			rel, err := filepath.Rel(tree, tokFile.Name())
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			typed[filepath.ToSlash(rel)] = typedFile{fset: fset, file: file, info: pkg.TypesInfo, pkg: pkg.Types}
		}
	}
	return typed
}

// returnCandidates returns operator 3: each returned value replaced by the
// zero value of its type, when the text would change.
func returnCandidates(file string, src []byte, lines map[int]bool, typed typedFile) []candidate {
	var out []candidate
	var stack []ast.Node
	ast.Inspect(typed.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		sig := enclosingSignature(stack, typed.info)
		if sig == nil || sig.Results().Len() != len(ret.Results) {
			return true
		}
		for i, result := range ret.Results {
			start := typed.fset.Position(result.Pos())
			if !lines[start.Line] {
				continue
			}
			zero := zeroValue(sig.Results().At(i).Type(), typed.pkg)
			end := typed.fset.Position(result.End()).Offset
			if zero == "" || string(src[start.Offset:end]) == zero {
				continue
			}
			out = append(out, candidate{
				File: file, Op: opZeroReturn, Line: start.Line, Offset: start.Offset,
				Edits: []textEdit{{Start: start.Offset, End: end, Text: zero}},
			})
		}
		return true
	})
	return out
}

// enclosingSignature finds the signature of the innermost function around the
// last node of stack.
func enclosingSignature(stack []ast.Node, info *types.Info) *types.Signature {
	for i := len(stack) - 1; i >= 0; i-- {
		switch fn := stack[i].(type) {
		case *ast.FuncLit:
			sig, _ := info.TypeOf(fn).(*types.Signature)
			return sig
		case *ast.FuncDecl:
			obj, ok := info.Defs[fn.Name].(*types.Func)
			if !ok {
				return nil
			}
			sig, _ := obj.Type().(*types.Signature)
			return sig
		}
	}
	return nil
}

// zeroValue returns Go source for the zero value of t, or "" when there is no
// simple spelling (type parameters).
func zeroValue(t types.Type, pkg *types.Package) string {
	if _, ok := t.(*types.TypeParam); ok {
		return ""
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "false"
		case u.Info()&types.IsString != 0:
			return `""`
		case u.Info()&types.IsNumeric != 0:
			return "0"
		}
		return "nil"
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil"
	case *types.Struct, *types.Array:
		qualifier := func(other *types.Package) string {
			if other == pkg {
				return ""
			}
			return other.Name()
		}
		return types.TypeString(t, qualifier) + "{}"
	}
	return ""
}

// changedSources reads the changed non-test, non-generated Go files of the
// commit checked out in tree, with their changed line numbers, in path order.
func changedSources(ctx context.Context, tree, base, rev string) ([]sourceFile, error) {
	files, err := diffFiles(ctx, tree, base, rev)
	if err != nil {
		return nil, err
	}
	sources := []sourceFile{}
	for _, f := range files {
		if f.Path == "" || !strings.HasSuffix(f.Path, ".go") || isTestFile(f.Path) || skippedPath(f.Path) || len(f.Lines) == 0 {
			continue
		}
		src, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(f.Path))) //nolint:gosec // Path comes from git diff of the clone.
		if err != nil || isGeneratedSource(src) {
			continue
		}
		sources = append(sources, sourceFile{Path: f.Path, Src: src, Lines: f.Lines})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, nil
}

// buildCandidates lists every mutation of the sources in the protocol order:
// operator by operator, files by path, positions by offset.
func buildCandidates(ctx context.Context, tree string, sources []sourceFile) ([]candidate, error) {
	dirSet := map[string]bool{}
	var all []candidate
	for _, source := range sources {
		cands, err := syntaxCandidates(source.Path, source.Src, source.Lines)
		if err != nil {
			continue // a file that does not parse cannot be mutated
		}
		all = append(all, cands...)
		dirSet[path.Dir(source.Path)] = true
	}
	dirs := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	typed := loadTyped(ctx, tree, dirs)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, source := range sources {
		if tf, ok := typed[source.Path]; ok {
			all = append(all, returnCandidates(source.Path, source.Src, source.Lines, tf)...)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Offset < b.Offset
	})
	return all, nil
}

// testID names a failing top-level test by package import path.
type testID struct{ Package, Name string }

// streamResult is what a go test -json run says about the tests.
type streamResult struct {
	Failed []testID
	// FailedPackages lists packages with a package-level failure or a build
	// failure, whether or not a test in them failed.
	FailedPackages []string
	BuildErr       bool
	TimedOut       bool
}

// streamEvent is the subset of a go test -json event used here.
type streamEvent struct {
	Action     string `json:"Action"`
	Package    string `json:"Package"`
	Test       string `json:"Test"`
	Output     string `json:"Output"`
	ImportPath string `json:"ImportPath"`
}

// scanTestStream extracts failing top-level tests, build failures and test
// timeouts from a go test -json stream.
func scanTestStream(stream []byte) streamResult {
	var result streamResult
	seen := map[testID]bool{}
	for _, line := range bytes.Split(stream, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch {
		case event.Action == "fail" && event.Test != "":
			top, _, _ := strings.Cut(event.Test, "/")
			id := testID{Package: event.Package, Name: top}
			if !seen[id] {
				seen[id] = true
				result.Failed = append(result.Failed, id)
			}
		case event.Action == "fail":
			result.FailedPackages = append(result.FailedPackages, event.Package)
		case event.Action == "build-fail":
			result.BuildErr = true
			path, _, _ := strings.Cut(event.ImportPath, " ")
			result.FailedPackages = append(result.FailedPackages, path)
		case event.Action == "output" && strings.Contains(event.Output, "panic: test timed out"):
			result.TimedOut = true
		}
	}
	sort.Slice(result.Failed, func(i, j int) bool {
		if result.Failed[i].Package != result.Failed[j].Package {
			return result.Failed[i].Package < result.Failed[j].Package
		}
		return result.Failed[i].Name < result.Failed[j].Name
	})
	return result
}

// outcome of running a mutant against the direct packages' tests.
type mutantOutcome int

const (
	mutantUnusable mutantOutcome = iota // compile failure, timeout, or a failure with no failing test
	mutantPassed                        // direct tests still pass
	mutantKilled                        // at least one top-level test fails
)

// mutantResult is the memoized result of one candidate.
type mutantResult struct {
	failed  []testID
	outcome mutantOutcome
}

// mutantSearch runs candidates against one checked-out tree.
type mutantSearch struct {
	sources    map[string][]byte
	results    map[int]mutantResult
	directPkgs map[string]bool
	baseline   *consumerBaseline
	tree       string
	modPath    string
	direct     []string
	cands      []candidate
	timeout    time.Duration
	maxTries   int
}

// newMutantSearch prepares the search for the commit checked out in tree.
func newMutantSearch(ctx context.Context, tree, base, rev, modPath string, direct []string, timeout time.Duration, maxTries int) (*mutantSearch, error) {
	if len(direct) == 0 {
		return nil, errNoDirect
	}
	sources, err := changedSources(ctx, tree, base, rev)
	if err != nil {
		return nil, err
	}
	cands, err := buildCandidates(ctx, tree, sources)
	if err != nil {
		return nil, err
	}
	m := &mutantSearch{
		sources: map[string][]byte{}, results: map[int]mutantResult{}, directPkgs: map[string]bool{},
		tree: tree, modPath: modPath, direct: direct, cands: cands, timeout: timeout, maxTries: maxTries,
	}
	for _, source := range sources {
		m.sources[source.Path] = source.Src
	}
	for _, pattern := range direct {
		m.directPkgs[importPath(modPath, strings.TrimPrefix(pattern, "./"))] = true
	}
	return m, nil
}

// importPath maps a module-relative directory to its import path.
func importPath(modPath, dir string) string {
	if dir == "." || dir == "" {
		return modPath
	}
	return modPath + "/" + dir
}

// dirOf maps an import path inside the module to its directory.
func dirOf(modPath, pkg string) (string, bool) {
	if pkg == modPath {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(pkg, modPath+"/"); ok {
		return rest, true
	}
	return "", false
}

// limit is how many candidates may be tried.
func (m *mutantSearch) limit() int { return min(len(m.cands), m.maxTries) }

// apply writes candidate i into the tree and returns the file it touched.
func (m *mutantSearch) apply(i int) (string, error) {
	c := m.cands[i]
	mutated, err := applyTextEdits(m.sources[c.File], c.Edits)
	if err != nil {
		return "", err
	}
	return c.File, writeKeepingMode(filepath.Join(m.tree, filepath.FromSlash(c.File)), mutated)
}

// restore puts the original text of candidate i's file back.
func (m *mutantSearch) restore(i int) error {
	c := m.cands[i]
	return writeKeepingMode(filepath.Join(m.tree, filepath.FromSlash(c.File)), m.sources[c.File])
}

// writeKeepingMode overwrites a file without changing its permissions.
func writeKeepingMode(full string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(full); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(full, data, mode); err != nil { //nolint:gosec // Path is inside the evaluation worktree.
		return fmt.Errorf("writing %s: %w", full, err)
	}
	return nil
}

// errNoDirect reports a commit without any buildable direct package: running
// go test with no package argument would test the root package by accident.
var errNoDirect = errors.New("no buildable direct package")

// goTestJSON runs go test -json with the evaluation environment. testArgs are
// the flags and package patterns after the fixed ones.
func goTestJSON(ctx context.Context, tree string, timeout time.Duration, factor int, testArgs ...string) stepResult {
	args := append([]string{"test", "-count=1", "-json", "-timeout", timeout.String()}, testArgs...)
	return runStep(ctx, tree, time.Duration(factor)*timeout+30*time.Second, "go", args...)
}

// try runs candidate i against the direct packages. Results are memoized.
func (m *mutantSearch) try(ctx context.Context, i int) (mutantResult, error) {
	if result, ok := m.results[i]; ok {
		return result, nil
	}
	if _, err := m.apply(i); err != nil {
		return mutantResult{}, err
	}
	if len(m.direct) == 0 {
		return mutantResult{}, errNoDirect
	}
	run := goTestJSON(ctx, m.tree, m.timeout, 1, m.direct...)
	if err := m.restore(i); err != nil {
		return mutantResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return mutantResult{}, err
	}
	result := mutantResult{outcome: mutantUnusable}
	if run.err == nil {
		scan := scanTestStream(run.stdout)
		switch {
		case scan.BuildErr || scan.TimedOut:
		case run.exit == 0:
			result.outcome = mutantPassed
		case len(scan.Failed) > 0:
			result.outcome, result.failed = mutantKilled, scan.Failed
		}
	}
	m.results[i] = result
	return result, nil
}

// findKilling returns the first candidate, within the attempt limit, that
// compiles and makes a direct-package test fail, with a resolvable oracle.
func (m *mutantSearch) findKilling(ctx context.Context) (int, []TestRef, error) {
	for i := range m.limit() {
		result, err := m.try(ctx, i)
		if err != nil {
			return 0, nil, err
		}
		if result.outcome != mutantKilled {
			continue
		}
		oracle := oracleOf(m.tree, m.modPath, result.failed)
		if len(oracle) == 0 {
			continue
		}
		confirmed, err := m.confirm(ctx, i, oracle)
		if err != nil {
			return 0, nil, err
		}
		if confirmed {
			return i, oracle, nil
		}
		m.results[i] = mutantResult{outcome: mutantUnusable} // flaky: it did not fail twice
	}
	return -1, nil, nil
}

// confirm reruns the oracle tests of candidate i once and reports whether every
// one of them fails again.
func (m *mutantSearch) confirm(ctx context.Context, i int, oracle []TestRef) (bool, error) {
	names := map[string]bool{}
	dirs := map[string]bool{}
	for _, ref := range oracle {
		dir, ok := dirOf(m.modPath, ref.Package)
		if !ok {
			return false, nil
		}
		names[regexp.QuoteMeta(ref.Name)] = true
		dirs[dir] = true
	}
	pattern := "^(" + strings.Join(sortedKeys(names), "|") + ")$"
	patterns := patternsOf(sortedKeys(dirs))
	if _, err := m.apply(i); err != nil {
		return false, err
	}
	run := goTestJSON(ctx, m.tree, m.timeout, 1, append([]string{"-run", pattern}, patterns...)...)
	if err := m.restore(i); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if run.err != nil {
		return false, nil
	}
	failed := map[testID]bool{}
	for _, id := range scanTestStream(run.stdout).Failed {
		failed[id] = true
	}
	for _, ref := range oracle {
		if !failed[testID{Package: ref.Package, Name: ref.Name}] {
			return false, nil
		}
	}
	return true, nil
}

// sortedKeys returns the keys of a set in order.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// opCounts summarizes how many candidates each operator produced.
func (m *mutantSearch) opCounts() string {
	counts := make([]int, opCount+1)
	for _, c := range m.cands {
		counts[c.Op]++
	}
	parts := make([]string, 0, opCount)
	for op := 1; op <= opCount; op++ {
		parts = append(parts, fmt.Sprintf("op%d=%d", op, counts[op]))
	}
	return strings.Join(parts, ", ")
}

// findConsumerBreak returns the first of at most three compiling candidates
// whose direct tests pass but whose go test ./... fails outside the direct
// packages, with those failing tests as the oracle.
func (m *mutantSearch) findConsumerBreak(ctx context.Context) (int, []TestRef, error) {
	passing := 0
	for i := range m.limit() {
		if passing >= consumerCandidates {
			break
		}
		result, err := m.try(ctx, i)
		if err != nil {
			return 0, nil, err
		}
		if result.outcome != mutantPassed {
			continue
		}
		passing++
		failed, err := m.consumerFailures(ctx, i)
		if err != nil {
			return 0, nil, err
		}
		if oracle := oracleOf(m.tree, m.modPath, failed); len(oracle) > 0 {
			return i, oracle, nil
		}
	}
	return -1, nil, nil
}

// errBaselineUnavailable reports that go test ./... did not complete on the
// unmutated commit, so a consumer break cannot be told from a failure that was
// already there.
var errBaselineUnavailable = errors.New("consumer baseline unavailable")

// consumerBaseline is what fails in go test ./... on the unmutated commit.
type consumerBaseline struct {
	tests  map[testID]bool
	broken map[string]bool // packages that failed with no failing test of their own
}

// loadBaseline runs go test ./... once on the unmutated commit.
func (m *mutantSearch) loadBaseline(ctx context.Context) error {
	if m.baseline != nil {
		return nil
	}
	run := goTestJSON(ctx, m.tree, m.timeout, consumerTimeoutFactor, "./...")
	if err := ctx.Err(); err != nil {
		return err
	}
	if run.err != nil {
		return fmt.Errorf("%w: %w", errBaselineUnavailable, run.err)
	}
	scan := scanTestStream(run.stdout)
	baseline := &consumerBaseline{tests: map[testID]bool{}, broken: map[string]bool{}}
	attributed := map[string]bool{}
	for _, id := range scan.Failed {
		baseline.tests[id] = true
		attributed[id.Package] = true
	}
	for _, pkg := range scan.FailedPackages {
		if !attributed[pkg] {
			baseline.broken[pkg] = true
		}
	}
	m.baseline = baseline
	return nil
}

// consumerFailures runs go test ./... with candidate i applied and returns the
// failing tests outside the direct packages that do not also fail on the
// unmutated commit (neither the same test nor a package that already fails
// without a failing test of its own).
func (m *mutantSearch) consumerFailures(ctx context.Context, i int) ([]testID, error) {
	if err := m.loadBaseline(ctx); err != nil {
		return nil, err
	}
	if _, err := m.apply(i); err != nil {
		return nil, err
	}
	run := goTestJSON(ctx, m.tree, m.timeout, consumerTimeoutFactor, "./...")
	if err := m.restore(i); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var outside []testID
	for _, id := range scanTestStream(run.stdout).Failed {
		if !m.directPkgs[id.Package] && !m.baseline.tests[id] && !m.baseline.broken[id.Package] {
			outside = append(outside, id)
		}
	}
	return outside, nil
}

// modulePath returns the module path of the module rooted at tree.
func modulePath(ctx context.Context, tree string) (string, error) {
	result := runStep(ctx, tree, time.Minute, "go", "list", "-m")
	if result.err != nil {
		return "", result.err
	}
	if result.exit != 0 {
		return "", fmt.Errorf("go list -m: exit %d: %s", result.exit, truncate(string(result.stderr)))
	}
	fields := strings.Fields(string(result.stdout))
	if len(fields) != 1 {
		return "", errors.New("go list -m: expected exactly one module")
	}
	return fields[0], nil
}

// oracleOf resolves failing tests to the test files that declare them. Tests
// outside the module or without a declaration are dropped.
func oracleOf(tree, modPath string, failed []testID) []TestRef {
	oracle := []TestRef{}
	for _, id := range failed {
		dir, ok := dirOf(modPath, id.Package)
		if !ok {
			continue
		}
		file, ok := findTestFile(tree, dir, id.Name)
		if !ok {
			continue
		}
		oracle = append(oracle, TestRef{Package: id.Package, File: file, Name: id.Name})
	}
	return oracle
}

// findTestFile finds the _test.go file in dir that declares the top-level
// function name, returning its tree-relative slash path.
func findTestFile(tree, dir, name string) (string, bool) {
	abs := filepath.Join(tree, filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() || !isTestFile(entry.Name()) {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(abs, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
				return path.Join(dir, entry.Name()), true
			}
		}
	}
	return "", false
}
