package gate

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/agentic-mcps/go/internal/verification"
)

// integritySimilarity is the body similarity at or above which two functions
// are treated as the same code under a different name.
const integritySimilarity = 0.8

var integrityStubPanic = regexp.MustCompile(`(?i)not implemented|unimplemented|not yet implemented|implement me|\btodo\b`)

// CheckIntegrity reports signs that a change hides a problem instead of
// fixing it: deleted, skipped, hidden or weakened tests, stubbed or gutted
// code, rewritten golden files, and edits to gate or CI configuration. It
// compares the base and current content of each file and reports only what
// the change introduced. Files that do not parse on either side are skipped
// because syntax is checked elsewhere. Precision is preferred over recall, so
// ambiguous cases are reported at a lower severity or not at all.
func CheckIntegrity(files []verification.SourceFile, deletedDecls []verification.ChangedDeclaration) []Item {
	items := make([]Item, 0)
	items = append(items, integrityPathItems(files)...)
	index := integrityLoad(files)
	items = append(items, integrityDeletedTests(index, deletedDecls)...)
	items = append(items, integrityBuildConstraints(index)...)
	items = append(items, integrityTestChanges(index)...)
	items = append(items, integrityHelperSkips(index)...)
	items = append(items, integrityStubs(index)...)
	SortItems(items)
	return items
}

// integrityKey identifies a function across files of one directory.
type integrityKey struct {
	dir  string
	name string
}

// integrityFacts summarizes what a function body can do to a test run.
type integrityFacts struct {
	skips       []integritySkip
	failing     int
	errorCalls  int
	logCalls    int
	runCalls    int
	earlyReturn int
	returnLine  int
}

type integritySkip struct {
	line    int
	guarded bool
}

type integrityFunc struct {
	owner  *integrityFile
	decl   *ast.FuncDecl
	facts  *integrityFacts
	name   string
	tokens []string
	line   int
	isTest bool
}

type integrityFile struct {
	fset        *token.FileSet
	ast         *ast.File
	imports     map[string]bool
	path        string
	altDir      string
	testingName string
	constraint  string
	src         []byte
	funcs       []*integrityFunc
	constraintL int
	generated   bool
}

// integrityPair is one source file with both sides parsed when present.
type integrityPair struct {
	base *integrityFile
	cur  *integrityFile
	file verification.SourceFile
}

type integrityIndex struct {
	baseTests map[integrityKey]*integrityFunc
	curTests  map[integrityKey]*integrityFunc
	baseAll   map[integrityKey]bool
	covered   map[integrityKey]bool
	pairs     []integrityPair
	baseOrder []*integrityFunc
	curOrder  []*integrityFunc
}

func integrityLoad(files []verification.SourceFile) *integrityIndex {
	index := &integrityIndex{
		baseTests: map[integrityKey]*integrityFunc{},
		curTests:  map[integrityKey]*integrityFunc{},
		baseAll:   map[integrityKey]bool{},
		covered:   map[integrityKey]bool{},
	}
	for _, file := range files {
		pair, ok := integrityParsePair(file)
		if !ok {
			continue
		}
		index.pairs = append(index.pairs, pair)
		index.addSide(pair, pair.base, true)
		index.addSide(pair, pair.cur, false)
	}
	return index
}

// integrityRenamedFromTest reports whether a file lost its _test.go suffix; the
// tests it held are reported once by the path rule instead of one by one.
func integrityRenamedFromTest(file verification.SourceFile) bool {
	prev := file.Change.PreviousPath
	return strings.HasSuffix(prev, "_test.go") && !strings.HasSuffix(file.Change.Path, "_test.go")
}

func integrityBasePath(file verification.SourceFile) string {
	if file.Change.PreviousPath != "" {
		return file.Change.PreviousPath
	}
	return file.Change.Path
}

func integrityParsePair(file verification.SourceFile) (integrityPair, bool) {
	pair := integrityPair{file: file}
	if file.BaseContent != nil && strings.HasSuffix(integrityBasePath(file), ".go") {
		parsed, ok := integrityParse(integrityBasePath(file), file.BaseContent)
		if !ok {
			return pair, false
		}
		pair.base = parsed
	}
	if file.CurrentContent != nil && strings.HasSuffix(file.Change.Path, ".go") {
		parsed, ok := integrityParse(file.Change.Path, file.CurrentContent)
		if !ok {
			return pair, false
		}
		pair.cur = parsed
	}
	if pair.base != nil && pair.cur != nil {
		pair.base.altDir = path.Dir(pair.cur.path)
	}
	return pair, pair.base != nil || pair.cur != nil
}

func (index *integrityIndex) addSide(pair integrityPair, side *integrityFile, isBase bool) {
	if side == nil {
		return
	}
	dir := path.Dir(side.path)
	isTestFile := strings.HasSuffix(side.path, "_test.go")
	for _, fn := range side.funcs {
		key := integrityKey{dir: dir, name: fn.name}
		switch {
		case isBase:
			index.baseOrder = append(index.baseOrder, fn)
			index.baseAll[key] = true
			if fn.isTest && isTestFile {
				index.addTest(index.baseTests, key, fn)
				if integrityRenamedFromTest(pair.file) {
					index.covered[key] = true
				}
			}
		default:
			index.curOrder = append(index.curOrder, fn)
			if fn.isTest && isTestFile {
				index.addTest(index.curTests, key, fn)
			}
		}
	}
}

func (index *integrityIndex) addTest(into map[integrityKey]*integrityFunc, key integrityKey, fn *integrityFunc) {
	if _, exists := into[key]; !exists {
		into[key] = fn
	}
}

func integrityParse(filePath string, src []byte) (*integrityFile, bool) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filePath, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	file := &integrityFile{
		fset:      fset,
		ast:       parsed,
		src:       src,
		path:      filePath,
		imports:   map[string]bool{},
		generated: ast.IsGenerated(parsed),
	}
	for _, spec := range parsed.Imports {
		name, ok := integrityImportName(spec)
		if !ok {
			continue
		}
		file.imports[name] = true
		if value, err := strconv.Unquote(spec.Path.Value); err == nil && value == "testing" {
			file.testingName = name
		}
	}
	file.constraint, file.constraintL = integrityBuildLine(parsed, fset)
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		file.funcs = append(file.funcs, &integrityFunc{
			owner:  file,
			decl:   fn,
			name:   integrityFuncName(fn),
			line:   fset.Position(fn.Pos()).Line,
			isTest: integrityIsTestFunc(fn, file.testingName),
		})
	}
	return file, true
}

func integrityImportName(spec *ast.ImportSpec) (string, bool) {
	if spec.Name != nil {
		if spec.Name.Name == "_" || spec.Name.Name == "." {
			return "", false
		}
		return spec.Name.Name, true
	}
	value, err := strconv.Unquote(spec.Path.Value)
	if err != nil || value == "" {
		return "", false
	}
	parts := strings.Split(value, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && integrityVersionSuffix(last) {
		last = parts[len(parts)-2]
	}
	return last, true
}

func integrityVersionSuffix(element string) bool {
	if len(element) < 2 || element[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(element[1:])
	return err == nil
}

// integrityBuildLine returns the normalized //go:build expression that appears
// before the package clause and its line, or empty when there is none.
func integrityBuildLine(file *ast.File, fset *token.FileSet) (string, int) {
	for _, group := range file.Comments {
		if group.End() > file.Package {
			break
		}
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) {
				continue
			}
			line := fset.Position(comment.Pos()).Line
			expr, err := constraint.Parse(comment.Text)
			if err != nil {
				return strings.TrimSpace(comment.Text), line
			}
			return expr.String(), line
		}
	}
	return "", 0
}

func integrityFuncName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return integrityReceiverName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func integrityReceiverName(expr ast.Expr) string {
	switch typ := expr.(type) {
	case *ast.StarExpr:
		return integrityReceiverName(typ.X)
	case *ast.ParenExpr:
		return integrityReceiverName(typ.X)
	case *ast.IndexExpr:
		return integrityReceiverName(typ.X)
	case *ast.IndexListExpr:
		return integrityReceiverName(typ.X)
	case *ast.Ident:
		return typ.Name
	default:
		return "?"
	}
}

// integrityIsTestFunc applies go test's rules: a top-level function named
// TestX or FuzzX whose next rune is not lowercase, with a single *testing.T
// (or *testing.F for fuzz tests) parameter.
func integrityIsTestFunc(fn *ast.FuncDecl, testingName string) bool {
	if fn.Recv != nil || fn.Body == nil || testingName == "" || fn.Type.TypeParams != nil {
		return false
	}
	var kind string
	switch {
	case strings.HasPrefix(fn.Name.Name, "Test"):
		kind = "T"
	case strings.HasPrefix(fn.Name.Name, "Fuzz"):
		kind = "F"
	default:
		return false
	}
	rest := fn.Name.Name[len("Test"):] // Test and Fuzz have the same length
	if r, _ := utf8.DecodeRuneInString(rest); rest != "" && unicode.IsLower(r) {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return false
	}
	return integrityIsTestingPointer(params[0].Type, testingName, kind)
}

func integrityIsTestingPointer(expr ast.Expr, testingName, kind string) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == testingName && sel.Sel.Name == kind
}

// bodyTokens returns the comment-free token sequence of a function body.
func (fn *integrityFunc) bodyTokens() []string {
	if fn.tokens != nil {
		return fn.tokens
	}
	fn.tokens = []string{}
	body := fn.decl.Body
	if body == nil {
		return fn.tokens
	}
	fset := fn.owner.fset
	start := fset.Position(body.Lbrace).Offset
	end := fset.Position(body.Rbrace).Offset + 1
	if start < 0 || end > len(fn.owner.src) || start >= end {
		return fn.tokens
	}
	src := fn.owner.src[start:end]
	scanFset := token.NewFileSet()
	file := scanFset.AddFile("", scanFset.Base(), len(src))
	var scan scanner.Scanner
	scan.Init(file, src, nil, 0)
	for {
		_, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON || lit == "" {
			fn.tokens = append(fn.tokens, tok.String())
			continue
		}
		fn.tokens = append(fn.tokens, lit)
	}
	return fn.tokens
}

func integrityGrams(tokens []string) map[string]bool {
	grams := map[string]bool{}
	for i := 0; i+3 <= len(tokens); i++ {
		grams[strings.Join(tokens[i:i+3], "\x00")] = true
	}
	return grams
}

// integrityBodySimilarity is the Jaccard similarity of token 3-gram sets;
// identical token sequences score 1.
func integrityBodySimilarity(a, b *integrityFunc) float64 {
	ta, tb := a.bodyTokens(), b.bodyTokens()
	if slices.Equal(ta, tb) {
		return 1
	}
	ga, gb := integrityGrams(ta), integrityGrams(tb)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	shared := 0
	for gram := range ga {
		if gb[gram] {
			shared++
		}
	}
	return float64(shared) / float64(len(ga)+len(gb)-shared)
}

func integrityItem(severity Severity, code, file string, line int, message, fix string) Item {
	return Item{Severity: severity, Code: code, File: file, Line: line, Message: message, Fix: fix}
}

// --- rules 2c, 7, 8: path based checks ---

func integrityPathItems(files []verification.SourceFile) []Item {
	items := make([]Item, 0)
	hasCode := false
	for _, file := range files {
		if integrityIsProductionGo(file.Change.Path) {
			hasCode = true
		}
	}
	for _, file := range files {
		change := file.Change
		if prev := change.PreviousPath; strings.HasSuffix(prev, "_test.go") && !strings.HasSuffix(change.Path, "_test.go") {
			items = append(items, integrityItem(SeverityBlock, CodeTestHidden, change.Path, 0,
				fmt.Sprintf("%s was renamed to %s, so go test no longer runs its tests.", prev, change.Path),
				"Keep the _test.go suffix on test files."))
		}
		if hasCode && integrityIsGolden(change.Path) {
			items = append(items, integrityItem(SeverityWarn, CodeGoldenModified, change.Path, 0,
				fmt.Sprintf("%s is test data that changed together with code.", change.Path),
				"Check the new expected output is correct instead of regenerating it to pass."))
		}
		for _, candidate := range []string{change.Path, change.PreviousPath} {
			if candidate != "" && integrityIsConfig(candidate) {
				items = append(items, integrityItem(SeverityBlock, CodeConfigModified, candidate, 0,
					candidate+" changes gate or CI configuration; this is always reported to the user.",
					"Revert the change unless the user asked for it."))
				break
			}
		}
	}
	return items
}

func integrityIsProductionGo(filePath string) bool {
	return strings.HasSuffix(filePath, ".go") && !strings.HasSuffix(filePath, "_test.go") && !integrityHasSegment(filePath, "testdata")
}

func integrityHasSegment(filePath, segment string) bool {
	for _, part := range strings.Split(filePath, "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func integrityIsGolden(filePath string) bool {
	return integrityHasSegment(filePath, "testdata") || strings.HasSuffix(filePath, ".golden")
}

func integrityIsConfig(filePath string) bool {
	switch filePath {
	case ".claude/settings.json", ".claude/settings.local.json":
		return true
	}
	if strings.HasPrefix(filePath, ".codex/") || strings.HasPrefix(filePath, ".github/workflows/") {
		return true
	}
	base := path.Base(filePath)
	if strings.HasPrefix(base, ".agentic-go") {
		return true
	}
	switch base {
	case ".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json":
		return true
	}
	return false
}

// --- rules 1 and 2a: deleted and hidden tests ---

func integrityDeletedTests(index *integrityIndex, deletedDecls []verification.ChangedDeclaration) []Item {
	items := make([]Item, 0)
	removed := integrityRemovedNames(deletedDecls)
	for _, pair := range index.pairs {
		if pair.base == nil || !strings.HasSuffix(pair.base.path, "_test.go") {
			continue
		}
		for _, fn := range pair.base.funcs {
			key := integrityKey{dir: path.Dir(pair.base.path), name: fn.name}
			if !fn.isTest || index.covered[key] || index.curTests[key] != nil || index.baseTests[key] != fn {
				continue
			}
			if item, ok := integrityDeletedTest(index, pair, fn, removed); ok {
				items = append(items, item)
			}
		}
	}
	return items
}

// integrityRemovedNames returns the last name segment of each declaration the
// change removed from non-test files.
func integrityRemovedNames(deleted []verification.ChangedDeclaration) map[string]bool {
	names := map[string]bool{}
	for _, decl := range deleted {
		if decl.BaseLocation != nil && strings.HasSuffix(decl.BaseLocation.File, "_test.go") {
			continue
		}
		name := decl.Name
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
		if name != "" {
			names[name] = true
		}
	}
	return names
}

func integrityDeletedTest(index *integrityIndex, pair integrityPair, fn *integrityFunc, removed map[string]bool) (Item, bool) {
	dirs := map[string]bool{path.Dir(pair.base.path): true}
	if pair.base.altDir != "" {
		dirs[pair.base.altDir] = true
	}
	var hidden bool
	for _, cur := range index.curOrder {
		curKey := integrityKey{dir: path.Dir(cur.owner.path), name: cur.name}
		isNew := !index.baseAll[curKey] || cur.name == fn.name
		if !dirs[curKey.dir] || !isNew || integrityBodySimilarity(fn, cur) < integritySimilarity {
			continue
		}
		if cur.isTest && strings.HasSuffix(cur.owner.path, "_test.go") {
			return Item{}, false
		}
		hidden = true
	}
	if hidden {
		return integrityItem(SeverityBlock, CodeTestHidden, pair.base.path, fn.line,
			fn.name+" was renamed so go test no longer runs it.",
			"Restore the Test prefix and the *testing.T signature so go test runs it."), true
	}
	if integrityReferences(fn, removed) {
		return integrityItem(SeverityInfo, CodeTestDeleted, pair.base.path, fn.line,
			fn.name+" was removed together with the code it tested.", ""), true
	}
	return integrityItem(SeverityBlock, CodeTestDeleted, pair.base.path, fn.line,
		fn.name+" was deleted.",
		"Restore "+fn.name+" or fix the code it covered; deleting a failing test hides the regression."), true
}

func integrityReferences(fn *integrityFunc, names map[string]bool) bool {
	if len(names) == 0 || fn.decl.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && names[ident.Name] {
			found = true
		}
		return !found
	})
	return found
}

// --- rule 2b: build constraints ---

func integrityBuildConstraints(index *integrityIndex) []Item {
	items := make([]Item, 0)
	for _, pair := range index.pairs {
		if pair.base == nil || pair.cur == nil || !strings.HasSuffix(pair.cur.path, "_test.go") {
			continue
		}
		if pair.cur.constraint == "" || pair.cur.constraint == pair.base.constraint {
			continue
		}
		items = append(items, integrityItem(SeverityBlock, CodeTestHidden, pair.cur.path, pair.cur.constraintL,
			"The build constraint of "+pair.cur.path+" changed, so go test may no longer run its tests.",
			"Revert the //go:build line unless the user asked for it."))
	}
	return items
}

// --- rules 3, 4, 5: changes inside tests that exist on both sides ---

func integrityTestChanges(index *integrityIndex) []Item {
	items := make([]Item, 0)
	for _, pair := range index.pairs {
		if pair.cur == nil || !strings.HasSuffix(pair.cur.path, "_test.go") {
			continue
		}
		for _, fn := range pair.cur.funcs {
			key := integrityKey{dir: path.Dir(pair.cur.path), name: fn.name}
			if !fn.isTest || index.curTests[key] != fn {
				continue
			}
			items = append(items, integrityTestItems(fn, index.baseTests[key])...)
		}
	}
	return items
}

func integrityTestItems(cur, base *integrityFunc) []Item {
	if base == nil {
		return integrityNewTestItems(cur)
	}
	if slices.Equal(base.bodyTokens(), cur.bodyTokens()) {
		return nil
	}
	items := integritySkipItems(cur, base)
	assertion := integrityAssertionItems(cur, base)
	items = append(items, assertion...)
	if len(assertion) == 0 {
		items = append(items, integrityEmptyItems(cur)...)
	}
	return items
}

func integrityNewTestItems(cur *integrityFunc) []Item {
	items := make([]Item, 0)
	if facts := cur.analyze(); len(facts.skips) > 0 {
		items = append(items, integrityItem(SeverityInfo, CodeTestSkipAdded, cur.owner.path, facts.skips[0].line,
			"New test "+cur.name+" contains a skip.", ""))
	}
	return append(items, integrityEmptyItems(cur)...)
}

func integritySkipItems(cur, base *integrityFunc) []Item {
	now, before := cur.analyze(), base.analyze()
	items := make([]Item, 0)
	unguardedNow, unguardedBefore := integrityUnguarded(now.skips), integrityUnguarded(before.skips)
	switch {
	case len(unguardedNow) > len(unguardedBefore):
		line := unguardedNow[len(unguardedBefore)].line
		items = append(items, integrityItem(SeverityBlock, CodeTestSkipAdded, cur.owner.path, line,
			cur.name+" now skips unconditionally.",
			"Remove the skip and fix the test or the code under test."))
	case len(now.skips) > len(before.skips):
		line := now.skips[len(before.skips)].line
		items = append(items, integrityItem(SeverityWarn, CodeTestSkipAdded, cur.owner.path, line,
			cur.name+" now skips under a condition.",
			"Check the skip condition is justified and does not hide a failure."))
	}
	if now.earlyReturn > 0 && before.earlyReturn == 0 {
		items = append(items, integrityItem(SeverityBlock, CodeTestSkipAdded, cur.owner.path, now.returnLine,
			cur.name+" returns early, so the rest of the test never runs.",
			"Remove the early return and fix the test or the code under test."))
	}
	return items
}

func integrityUnguarded(skips []integritySkip) []integritySkip {
	out := make([]integritySkip, 0, len(skips))
	for _, skip := range skips {
		if !skip.guarded {
			out = append(out, skip)
		}
	}
	return out
}

func integrityAssertionItems(cur, base *integrityFunc) []Item {
	now, before := cur.analyze(), base.analyze()
	switch {
	case before.failing > 0 && now.failing == 0:
		return []Item{integrityItem(SeverityBlock, CodeAssertionsRemoved, cur.owner.path, cur.line,
			cur.name+" no longer asserts anything.",
			"Restore the assertions in "+cur.name+" instead of removing them.")}
	case now.failing < before.failing && now.errorCalls < before.errorCalls && now.logCalls > before.logCalls:
		return []Item{integrityItem(SeverityBlock, CodeAssertionsRemoved, cur.owner.path, cur.line,
			cur.name+" now logs where it used to fail.",
			"Turn the log calls back into Error or Fatal calls.")}
	case now.failing < before.failing:
		return []Item{integrityItem(SeverityWarn, CodeAssertionsReduced, cur.owner.path, cur.line,
			fmt.Sprintf("%s has fewer assertions than before (%d to %d).", cur.name, before.failing, now.failing),
			"Check the removed assertions are still covered.")}
	}
	return nil
}

func integrityEmptyItems(cur *integrityFunc) []Item {
	facts := cur.analyze()
	if facts.failing > 0 || facts.runCalls > 0 {
		return nil
	}
	return []Item{integrityItem(SeverityWarn, CodeTestEmpty, cur.owner.path, cur.line,
		cur.name+" has no assertions and no subtests.",
		"Add an assertion that fails when the behavior is wrong.")}
}

// --- rule 3 for helpers in test files ---

type integrityHelperCount struct {
	file  string
	line  int
	count int
}

func integrityHelperSkips(index *integrityIndex) []Item {
	before := map[integrityKey]int{}
	for _, fn := range index.baseOrder {
		if integrityIsTestFileHelper(fn) {
			before[integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}] += len(fn.analyze().skips)
		}
	}
	after := map[integrityKey]*integrityHelperCount{}
	order := make([]integrityKey, 0)
	for _, fn := range index.curOrder {
		if !integrityIsTestFileHelper(fn) {
			continue
		}
		skips := fn.analyze().skips
		if len(skips) == 0 {
			continue
		}
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		if after[key] == nil {
			after[key] = &integrityHelperCount{file: fn.owner.path, line: skips[0].line}
			order = append(order, key)
		}
		after[key].count += len(skips)
	}
	items := make([]Item, 0)
	for _, key := range order {
		if after[key].count <= before[key] {
			continue
		}
		items = append(items, integrityItem(SeverityWarn, CodeTestSkipAdded, after[key].file, after[key].line,
			"Skip added in helper "+key.name+".",
			"Check the skip is justified and does not hide a failure."))
	}
	return items
}

func integrityIsTestFileHelper(fn *integrityFunc) bool {
	return !fn.isTest && strings.HasSuffix(fn.owner.path, "_test.go") && fn.decl.Body != nil
}

// --- rule 6: stubs and gutted functions ---

type integrityStubSite struct {
	file string
	line int
}

func integrityStubs(index *integrityIndex) []Item {
	baseStubs := map[integrityKey]int{}
	baseBodies := map[integrityKey]int{}
	for _, fn := range index.baseOrder {
		if !integrityIsStubScope(fn) {
			continue
		}
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		baseStubs[key] += len(integrityStubPanics(fn))
		baseBodies[key]++
	}
	curStubs := map[integrityKey][]integrityStubSite{}
	curBodies := map[integrityKey]int{}
	order := make([]integrityKey, 0)
	for _, fn := range index.curOrder {
		if !integrityIsStubScope(fn) {
			continue
		}
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		curBodies[key]++
		if sites := integrityStubPanics(fn); len(sites) > 0 {
			if curStubs[key] == nil {
				order = append(order, key)
			}
			curStubs[key] = append(curStubs[key], sites...)
		}
	}
	items := make([]Item, 0)
	for _, key := range order {
		if len(curStubs[key]) <= baseStubs[key] {
			continue
		}
		site := curStubs[key][baseStubs[key]]
		items = append(items, integrityItem(SeverityBlock, CodeStub, site.file, site.line,
			key.name+" panics with a not-implemented message instead of doing its job.",
			"Implement "+key.name+" instead of leaving a placeholder."))
	}
	return append(items, integrityGutted(index, baseBodies, curBodies)...)
}

func integrityIsStubScope(fn *integrityFunc) bool {
	file := fn.owner
	return strings.HasSuffix(file.path, ".go") && !strings.HasSuffix(file.path, "_test.go") && !file.generated && fn.decl.Body != nil
}

func integrityStubPanics(fn *integrityFunc) []integrityStubSite {
	sites := make([]integrityStubSite, 0)
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !integrityIsStubPanic(call) {
			return true
		}
		sites = append(sites, integrityStubSite{file: fn.owner.path, line: fn.owner.fset.Position(call.Pos()).Line})
		return true
	})
	return sites
}

func integrityIsStubPanic(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "panic" || len(call.Args) != 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	text, err := strconv.Unquote(lit.Value)
	return err == nil && integrityStubPanic.MatchString(text)
}

func integrityGutted(index *integrityIndex, baseBodies, curBodies map[integrityKey]int) []Item {
	base := map[integrityKey]*integrityFunc{}
	for _, fn := range index.baseOrder {
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		if integrityIsStubScope(fn) && baseBodies[key] == 1 {
			base[key] = fn
		}
	}
	items := make([]Item, 0)
	for _, fn := range index.curOrder {
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		before := base[key]
		if !integrityIsStubScope(fn) || curBodies[key] != 1 || before == nil {
			continue
		}
		if len(before.decl.Body.List) > 2 && integrityIsZeroReturn(fn.decl.Body) {
			items = append(items, integrityItem(SeverityWarn, CodeGutted, fn.owner.path, fn.line,
				fn.name+" now only returns zero values.",
				"Restore the logic of "+fn.name+" or delete it if it is unused."))
		}
	}
	return items
}

func integrityIsZeroReturn(body *ast.BlockStmt) bool {
	if len(body.List) != 1 {
		return false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, result := range ret.Results {
		if !integrityIsZeroValue(result) {
			return false
		}
	}
	return true
}

func integrityIsZeroValue(expr ast.Expr) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name == "nil" || value.Name == "false"
	case *ast.BasicLit:
		return (value.Kind == token.INT && value.Value == "0") || (value.Kind == token.STRING && (value.Value == `""` || value.Value == "``"))
	case *ast.CompositeLit:
		return len(value.Elts) == 0
	}
	return false
}

// --- body analysis shared by the test rules ---

var (
	integrityFailNames = map[string]bool{"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true, "Fail": true, "FailNow": true}
	integrityErrNames  = map[string]bool{"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true}
	integritySkipNames = map[string]bool{"Skip": true, "Skipf": true, "SkipNow": true}
	integrityLogNames  = map[string]bool{"Log": true, "Logf": true}
)

func (fn *integrityFunc) analyze() *integrityFacts {
	if fn.facts != nil {
		return fn.facts
	}
	facts := &integrityFacts{}
	fn.facts = facts
	body := fn.decl.Body
	if body == nil {
		return facts
	}
	for i, stmt := range body.List {
		if _, ok := stmt.(*ast.ReturnStmt); ok && i < len(body.List)-1 {
			facts.earlyReturn++
			if facts.returnLine == 0 {
				facts.returnLine = fn.owner.fset.Position(stmt.Pos()).Line
			}
		}
	}
	names := integrityTestingParams(fn)
	var stack []ast.Node
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, node)
		if call, ok := node.(*ast.CallExpr); ok {
			fn.countCall(facts, call, names, stack)
		}
		return true
	})
	return facts
}

// integrityTestingParams collects the names of every *testing.T or *testing.F
// parameter in the function, including those of subtest closures.
func integrityTestingParams(fn *integrityFunc) map[string]bool {
	names := map[string]bool{}
	testing := fn.owner.testingName
	collect := func(ft *ast.FuncType) {
		if ft == nil || ft.Params == nil || testing == "" {
			return
		}
		for _, field := range ft.Params.List {
			if !integrityIsTestingPointer(field.Type, testing, "T") && !integrityIsTestingPointer(field.Type, testing, "F") {
				continue
			}
			for _, name := range field.Names {
				if name.Name != "_" {
					names[name.Name] = true
				}
			}
		}
	}
	collect(fn.decl.Type)
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		if lit, ok := node.(*ast.FuncLit); ok {
			collect(lit.Type)
		}
		return true
	})
	return names
}

func (fn *integrityFunc) countCall(facts *integrityFacts, call *ast.CallExpr, tNames map[string]bool, stack []ast.Node) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if isSel {
		recv, _ := sel.X.(*ast.Ident)
		isPkg := recv != nil && fn.owner.imports[recv.Name]
		name := sel.Sel.Name
		switch {
		case isPkg && (recv.Name == "assert" || recv.Name == "require"):
			facts.failing++
			return
		case isPkg:
			// Package-level calls such as fmt.Errorf never fail a test.
		case integrityIsFailureCall(name, len(call.Args)):
			facts.failing++
			if integrityErrNames[name] {
				facts.errorCalls++
			}
			return
		case integritySkipNames[name]:
			facts.skips = append(facts.skips, integritySkip{
				line:    fn.owner.fset.Position(call.Pos()).Line,
				guarded: integrityIsGuarded(stack),
			})
			return
		case integrityLogNames[name]:
			facts.logCalls++
			return
		case name == "Run":
			facts.runCalls++
			return
		}
	}
	for _, arg := range call.Args {
		if ident, ok := arg.(*ast.Ident); ok && tNames[ident.Name] {
			facts.failing++
			return
		}
	}
}

// integrityIsFailureCall tells t.Error(...) from err.Error(): the testing
// methods that report a failure take arguments, except Fail and FailNow.
func integrityIsFailureCall(name string, args int) bool {
	if !integrityFailNames[name] {
		return false
	}
	if name == "Fail" || name == "FailNow" {
		return args == 0
	}
	return args > 0
}

func integrityIsGuarded(stack []ast.Node) bool {
	for _, node := range stack {
		stmt, ok := node.(*ast.IfStmt)
		if !ok {
			continue
		}
		if integrityReferencesGuard(stmt.Init) || integrityReferencesGuard(stmt.Cond) {
			return true
		}
	}
	return false
}

func integrityReferencesGuard(node ast.Node) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		sel, ok := inner.(*ast.SelectorExpr)
		if !ok {
			return !found
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return !found
		}
		switch pkg.Name + "." + sel.Sel.Name {
		case "runtime.GOOS", "runtime.GOARCH", "os.Getenv", "os.LookupEnv", "testing.Short":
			found = true
		}
		return !found
	})
	return found
}
