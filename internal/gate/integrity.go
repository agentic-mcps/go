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

// integrityCalleeOverlap is the share of a deleted test's callees that a new
// test must also call for the deletion to look like a merge.
const integrityCalleeOverlap = 0.5

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

// integrityCall is a call to code that is not part of the testing machinery.
type integrityCall struct {
	name string
	line int
	// method is true for x.Name(...) calls and false for plain Name(...) calls.
	method bool
}

// integrityFacts summarizes what a function body can do to a test run.
type integrityFacts struct {
	skips         []integritySkip
	callees       []integrityCall
	failing       int
	errorCalls    int
	logCalls      int
	runCalls      int
	earlyReturn   int
	returnLine    int
	disguisedTrue int
	disguisedLoad int
	disguisedLine int
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
	imports     map[string]string
	twin        *integrityFile
	path        string
	altDir      string
	pkgName     string
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
	growth    map[integrityKey]bool
	methods   map[integrityKey]bool
	pairs     []integrityPair
	baseOrder []*integrityFunc
	curOrder  []*integrityFunc
	deleted   []*integrityFunc
	added     []*integrityFunc
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
	index.finish()
	return index
}

// finish derives the lists that need both sides to be complete.
func (index *integrityIndex) finish() {
	for _, fn := range index.baseOrder {
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		if index.baseTests[key] == fn && index.curTests[key] == nil && !index.covered[key] {
			index.deleted = append(index.deleted, fn)
		}
	}
	for _, fn := range index.curOrder {
		key := integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}
		if index.curTests[key] == fn && index.baseTests[key] == nil {
			index.added = append(index.added, fn)
		}
	}
	index.growth = map[integrityKey]bool{}
	index.methods = map[integrityKey]bool{}
	before := map[integrityKey]int{}
	for _, fn := range index.baseOrder {
		if !fn.isTest {
			before[integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}] += len(integrityUnguarded(fn.analyze().skips))
		}
	}
	after := map[integrityKey]int{}
	for _, fn := range index.curOrder {
		if !fn.isTest {
			after[integrityKey{dir: path.Dir(fn.owner.path), name: fn.name}] += len(integrityUnguarded(fn.analyze().skips))
		}
	}
	for key, count := range after {
		if count <= before[key] {
			continue
		}
		if dot := strings.LastIndex(key.name, "."); dot >= 0 {
			index.methods[integrityKey{dir: key.dir, name: key.name[dot+1:]}] = true
			continue
		}
		index.growth[key] = true
	}
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

// integrityGoIgnored reports whether the go tool skips a path: any segment
// that is testdata or starts with an underscore or a dot.
func integrityGoIgnored(filePath string) bool {
	for _, part := range strings.Split(filePath, "/") {
		if part == "testdata" || strings.HasPrefix(part, "_") || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func integrityParsePair(file verification.SourceFile) (integrityPair, bool) {
	pair := integrityPair{file: file}
	if basePath := integrityBasePath(file); file.BaseContent != nil && strings.HasSuffix(basePath, ".go") && !integrityGoIgnored(basePath) {
		parsed, ok := integrityParse(basePath, file.BaseContent)
		if !ok {
			return pair, false
		}
		pair.base = parsed
	}
	if curPath := file.Change.Path; file.CurrentContent != nil && strings.HasSuffix(curPath, ".go") && !integrityGoIgnored(curPath) {
		parsed, ok := integrityParse(curPath, file.CurrentContent)
		if !ok {
			return pair, false
		}
		pair.cur = parsed
	}
	if pair.base != nil && pair.cur != nil {
		pair.base.altDir = path.Dir(pair.cur.path)
		pair.base.twin, pair.cur.twin = pair.cur, pair.base
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
		pkgName:   parsed.Name.Name,
		imports:   map[string]string{},
		generated: ast.IsGenerated(parsed),
	}
	for _, spec := range parsed.Imports {
		name, ok := integrityImportName(spec)
		if !ok {
			continue
		}
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		file.imports[name] = value
		if value == "testing" {
			file.testingName = name
		}
	}
	file.constraint, file.constraintL = integrityConstraint(parsed, fset, filePath)
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

// integrityConstraint returns a normalized description of everything that
// decides whether the go tool builds the file: //go:build and // +build lines
// before the package clause and a GOOS or GOARCH file name suffix. The line is
// the first constraint comment, or zero when there is none.
func integrityConstraint(file *ast.File, fset *token.FileSet, filePath string) (string, int) {
	var parts []string
	line := 0
	for _, group := range file.Comments {
		if group.End() > file.Package {
			break
		}
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) && !constraint.IsPlusBuild(comment.Text) {
				continue
			}
			if line == 0 {
				line = fset.Position(comment.Pos()).Line
			}
			text := strings.TrimSpace(comment.Text)
			if expr, err := constraint.Parse(comment.Text); err == nil {
				text = expr.String()
			}
			parts = append(parts, text)
		}
	}
	if suffix := integrityNameConstraint(filePath); suffix != "" {
		parts = append(parts, "file:"+suffix)
	}
	return strings.Join(parts, "; "), line
}

var (
	integrityKnownOS = map[string]bool{
		"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
		"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true,
		"openbsd": true, "plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
	}
	integrityKnownArch = map[string]bool{
		"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true,
		"arm64be": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true,
		"mips64p32": true, "mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true,
		"riscv": true, "riscv64": true, "s390": true, "s390x": true, "sparc": true, "sparc64": true,
		"wasm": true,
	}
)

// integrityNameConstraint returns the GOOS and GOARCH implied by a file name
// such as x_windows_test.go or x_linux_amd64.go, following go/build's rules.
func integrityNameConstraint(filePath string) string {
	name := strings.TrimSuffix(path.Base(filePath), ".go")
	name = strings.TrimSuffix(name, "_test")
	parts := strings.Split(name, "_")
	n := len(parts)
	if n < 2 {
		return ""
	}
	if n >= 3 && integrityKnownOS[parts[n-2]] && integrityKnownArch[parts[n-1]] {
		return parts[n-2] + "_" + parts[n-1]
	}
	if integrityKnownOS[parts[n-1]] || integrityKnownArch[parts[n-1]] {
		return parts[n-1]
	}
	return ""
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
	return integrityIsTestingSelector(star.X, testingName, kind)
}

func integrityIsTestingSelector(expr ast.Expr, testingName, kind string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
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

// integritySharedGrams counts the token 3-grams two bodies have in common and
// returns the size of each set.
func integritySharedGrams(a, b *integrityFunc) (shared, sizeA, sizeB int) {
	ga, gb := integrityGrams(a.bodyTokens()), integrityGrams(b.bodyTokens())
	for gram := range ga {
		if gb[gram] {
			shared++
		}
	}
	return shared, len(ga), len(gb)
}

// integrityBodySimilarity is the Jaccard similarity of token 3-gram sets;
// identical token sequences score 1.
func integrityBodySimilarity(a, b *integrityFunc) float64 {
	if slices.Equal(a.bodyTokens(), b.bodyTokens()) {
		return 1
	}
	shared, sizeA, sizeB := integritySharedGrams(a, b)
	if sizeA == 0 || sizeB == 0 {
		return 0
	}
	return float64(shared) / float64(sizeA+sizeB-shared)
}

// integrityContainment is the share of the token 3-grams of original that
// also appear in other; identical token sequences score 1. A test folded into
// a larger one scores high even though the two are not similar overall.
func integrityContainment(original, other *integrityFunc) float64 {
	if slices.Equal(original.bodyTokens(), other.bodyTokens()) {
		return 1
	}
	shared, size, _ := integritySharedGrams(original, other)
	if size == 0 {
		return 0
	}
	return float64(shared) / float64(size)
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
	for _, fn := range index.deleted {
		if item, ok := integrityDeletedTest(index, fn, removed); ok {
			items = append(items, item)
		}
	}
	return items
}

// integrityRemovedNames returns, for each declaration the change removed from
// non-test files, the names that must all appear in a test for it to count as
// testing that declaration: the name itself for functions, the type and the
// member name for methods and fields.
func integrityRemovedNames(deleted []verification.ChangedDeclaration) [][]string {
	groups := make([][]string, 0, len(deleted))
	for _, decl := range deleted {
		if decl.BaseLocation != nil && strings.HasSuffix(decl.BaseLocation.File, "_test.go") {
			continue
		}
		parts := strings.Split(decl.Name, ".")
		first, last := parts[0], parts[len(parts)-1]
		switch {
		case last == "":
		case first == last:
			groups = append(groups, []string{last})
		default:
			groups = append(groups, []string{first, last})
		}
	}
	return groups
}

// related reports whether a test in dir could be the deleted test moved or
// renamed: same directory, or the directory its file was renamed into.
func integrityRelated(deleted *integrityFunc, dir string) bool {
	return dir == path.Dir(deleted.owner.path) || (deleted.owner.altDir != "" && dir == deleted.owner.altDir)
}

func integrityDeletedTest(index *integrityIndex, fn *integrityFunc, removed [][]string) (Item, bool) {
	file := fn.owner.path
	if index.renamedTo(fn) != nil {
		return Item{}, false
	}
	if index.hiddenAs(fn) {
		return integrityItem(SeverityBlock, CodeTestHidden, file, fn.line,
			fn.name+" was renamed so go test no longer runs it.",
			"Restore the Test prefix and the *testing.T signature so go test runs it."), true
	}
	if integrityReferences(fn, removed) {
		return integrityItem(SeverityInfo, CodeTestDeleted, file, fn.line,
			fn.name+" was removed together with the code it tested.", ""), true
	}
	if twin := index.exercisedBy(fn); twin != nil {
		return integrityItem(SeverityWarn, CodeTestDeleted, file, fn.line,
			fmt.Sprintf("%s was deleted; %s in this change exercises the same code.", fn.name, twin.name),
			"Check "+twin.name+" still covers what "+fn.name+" did."), true
	}
	return integrityItem(SeverityBlock, CodeTestDeleted, file, fn.line,
		fn.name+" was deleted.",
		"Restore "+fn.name+" or fix the code it covered; deleting a failing test hides the regression."), true
}

// renamedTo returns the new test that contains the body of a deleted test, or
// nil. The best match wins.
func (index *integrityIndex) renamedTo(deleted *integrityFunc) *integrityFunc {
	var best *integrityFunc
	bestScore := 0.0
	for _, cur := range index.added {
		if !integrityRelated(deleted, path.Dir(cur.owner.path)) {
			continue
		}
		if score := integrityContainment(deleted, cur); score >= integritySimilarity && score > bestScore {
			best, bestScore = cur, score
		}
	}
	return best
}

// renamedFrom returns the deleted test whose body a new test contains.
func (index *integrityIndex) renamedFrom(cur *integrityFunc) *integrityFunc {
	var best *integrityFunc
	bestScore := 0.0
	for _, deleted := range index.deleted {
		if !integrityRelated(deleted, path.Dir(cur.owner.path)) {
			continue
		}
		if score := integrityContainment(deleted, cur); score >= integritySimilarity && score > bestScore {
			best, bestScore = deleted, score
		}
	}
	return best
}

// hiddenAs reports whether a deleted test lives on as a function go test does
// not run: a new non-test function, or one with the test's name and the wrong
// signature, with a near identical body.
func (index *integrityIndex) hiddenAs(deleted *integrityFunc) bool {
	for _, cur := range index.curOrder {
		curKey := integrityKey{dir: path.Dir(cur.owner.path), name: cur.name}
		if !integrityRelated(deleted, curKey.dir) || (cur.isTest && strings.HasSuffix(cur.owner.path, "_test.go")) {
			continue
		}
		isNew := !index.baseAll[curKey] || cur.name == deleted.name
		if isNew && integrityBodySimilarity(deleted, cur) >= integritySimilarity {
			return true
		}
	}
	return false
}

// exercisedBy returns a new test that checks something and calls at least half
// of the code the deleted test called, or nil.
func (index *integrityIndex) exercisedBy(deleted *integrityFunc) *integrityFunc {
	wanted := integrityCalleeNames(deleted)
	if len(wanted) == 0 {
		return nil
	}
	var best *integrityFunc
	bestShare := 0.0
	for _, cur := range index.added {
		facts := cur.analyze()
		if !integrityRelated(deleted, path.Dir(cur.owner.path)) || (facts.failing == 0 && facts.runCalls == 0) {
			continue
		}
		have := integrityCalleeNames(cur)
		shared := 0
		for name := range wanted {
			if have[name] {
				shared++
			}
		}
		if share := float64(shared) / float64(len(wanted)); share >= integrityCalleeOverlap && share > bestShare {
			best, bestShare = cur, share
		}
	}
	return best
}

func integrityCalleeNames(fn *integrityFunc) map[string]bool {
	names := map[string]bool{}
	for _, call := range fn.analyze().callees {
		names[call.name] = true
	}
	return names
}

func integrityReferences(fn *integrityFunc, groups [][]string) bool {
	if len(groups) == 0 || fn.decl.Body == nil {
		return false
	}
	seen := map[string]bool{}
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			seen[ident.Name] = true
		}
		return true
	})
	for _, group := range groups {
		if !slices.ContainsFunc(group, func(name string) bool { return !seen[name] }) {
			return true
		}
	}
	return false
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
			"Revert the build constraint or file name change unless the user asked for it."))
	}
	return items
}

// --- rules 3, 4, 5: changes inside tests ---

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
			base := index.baseTests[key]
			if base == nil {
				base = index.renamedFrom(fn)
			}
			items = append(items, integrityTestItems(index, fn, base)...)
		}
	}
	return items
}

func integrityTestItems(index *integrityIndex, cur, base *integrityFunc) []Item {
	if base == nil {
		return integrityNewTestItems(index, cur)
	}
	items := integrityMovedItems(cur, base)
	if slices.Equal(base.bodyTokens(), cur.bodyTokens()) {
		return items
	}
	items = append(items, integritySkipItems(index, cur, base)...)
	assertion := integrityAssertionItems(cur, base)
	items = append(items, assertion...)
	if len(assertion) == 0 {
		items = append(items, integrityEmptyItems(cur)...)
	}
	return items
}

// integrityMovedItems flags a test that moved to a file the go tool builds
// under different conditions than the file it came from.
func integrityMovedItems(cur, base *integrityFunc) []Item {
	from, to := base.owner, cur.owner
	if from.twin == to || to.constraint == "" || from.constraint == to.constraint {
		return nil
	}
	return []Item{integrityItem(SeverityWarn, CodeTestHidden, to.path, cur.line,
		cur.name+" moved to a file with a different build constraint.",
		"Check "+cur.name+" still runs everywhere it ran before.")}
}

func integrityNewTestItems(index *integrityIndex, cur *integrityFunc) []Item {
	items := make([]Item, 0)
	facts := cur.analyze()
	if len(facts.skips) > 0 {
		items = append(items, integrityItem(SeverityInfo, CodeTestSkipAdded, cur.owner.path, facts.skips[0].line,
			"New test "+cur.name+" contains a skip.", ""))
	}
	items = append(items, integrityHelperSkipCalls(index, cur, nil)...)
	return append(items, integrityEmptyItems(cur)...)
}

func integritySkipItems(index *integrityIndex, cur, base *integrityFunc) []Item {
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
	items = append(items, integrityDisguisedItems(cur, now, before)...)
	return append(items, integrityHelperSkipCalls(index, cur, base)...)
}

// integrityDisguisedItems flags an added top-level `if cond { return }`.
func integrityDisguisedItems(cur *integrityFunc, now, before *integrityFacts) []Item {
	switch {
	case now.disguisedTrue > before.disguisedTrue:
		return []Item{integrityItem(SeverityBlock, CodeTestSkipAdded, cur.owner.path, now.disguisedLine,
			cur.name+" returns early under a condition that is always true.",
			"Remove the early return and fix the test or the code under test.")}
	case now.disguisedLoad > before.disguisedLoad:
		return []Item{integrityItem(SeverityWarn, CodeTestSkipAdded, cur.owner.path, now.disguisedLine,
			cur.name+" has a new early return that can skip its assertions.",
			"Check the early return is justified and does not hide a failure.")}
	}
	return nil
}

// integrityHelperSkipCalls flags calls, new in this change, to a function
// whose unguarded skips grew: the skip happens in the helper instead of the test.
func integrityHelperSkipCalls(index *integrityIndex, cur, base *integrityFunc) []Item {
	known := map[string]bool{}
	if base != nil {
		known = integrityCalleeNames(base)
	}
	dir := path.Dir(cur.owner.path)
	items := make([]Item, 0)
	reported := map[string]bool{}
	for _, call := range cur.analyze().callees {
		if known[call.name] || reported[call.name] {
			continue
		}
		table := index.growth
		if call.method {
			table = index.methods
		}
		if !table[integrityKey{dir: dir, name: call.name}] {
			continue
		}
		reported[call.name] = true
		items = append(items, integrityItem(SeverityBlock, CodeTestSkipAdded, cur.owner.path, call.line,
			fmt.Sprintf("%s now calls %s, which skips.", cur.name, call.name),
			"Remove the skip from "+call.name+" or stop calling it, then fix the test."))
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
	errorsDropped := now.errorCalls < before.errorCalls && now.logCalls > before.logCalls
	switch {
	case before.failing > 0 && now.failing == 0 && now.runCalls > 0:
		return []Item{integrityItem(SeverityWarn, CodeAssertionsReduced, cur.owner.path, cur.line,
			cur.name+" no longer asserts directly; only its subtests may.",
			"Check the subtests still assert what "+cur.name+" asserted.")}
	case before.failing > 0 && now.failing == 0:
		return []Item{integrityItem(SeverityBlock, CodeAssertionsRemoved, cur.owner.path, cur.line,
			cur.name+" no longer asserts anything.",
			"Restore the assertions in "+cur.name+" instead of removing them.")}
	case now.failing < before.failing && errorsDropped:
		return []Item{integrityItem(SeverityBlock, CodeAssertionsRemoved, cur.owner.path, cur.line,
			cur.name+" now logs where it used to fail.",
			"Turn the log calls back into Error or Fatal calls.")}
	case errorsDropped:
		return []Item{integrityItem(SeverityWarn, CodeAssertionsReduced, cur.owner.path, cur.line,
			cur.name+" logs where it used to fail.",
			"Check the logged conditions are still asserted.")}
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

// integrityIsStubScope selects production code. Test doubles in packages and
// directories named for them legitimately panic with "not implemented".
func integrityIsStubScope(fn *integrityFunc) bool {
	file := fn.owner
	if !strings.HasSuffix(file.path, ".go") || strings.HasSuffix(file.path, "_test.go") || file.generated || fn.decl.Body == nil {
		return false
	}
	pkg := strings.ToLower(file.pkgName)
	for _, suffix := range []string{"test", "testing", "mock", "mocks", "fake", "fakes"} {
		if strings.HasSuffix(pkg, suffix) {
			return false
		}
	}
	for _, segment := range []string{"testutil", "mocks", "fakes"} {
		if integrityHasSegment(file.path, segment) {
			return false
		}
	}
	return true
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
	// integrityPredeclared lists calls that never reach the code under test.
	integrityPredeclared = map[string]bool{
		"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true,
		"delete": true, "imag": true, "len": true, "make": true, "max": true, "min": true, "new": true,
		"panic": true, "print": true, "println": true, "real": true, "recover": true,
		"any": true, "bool": true, "byte": true, "complex64": true, "complex128": true, "error": true,
		"float32": true, "float64": true, "int": true, "int8": true, "int16": true, "int32": true,
		"int64": true, "rune": true, "string": true, "uint": true, "uint8": true, "uint16": true,
		"uint32": true, "uint64": true, "uintptr": true,
	}
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
	names := integrityTestingParams(fn)
	fn.scanTopLevel(facts, body)
	var stack []ast.Node
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, node)
		if call, ok := node.(*ast.CallExpr); ok {
			fn.recordCallee(facts, call, names)
			fn.countCall(facts, call, names, stack)
		}
		return true
	})
	return facts
}

// scanTopLevel records early returns and `if cond { return }` statements at
// the top level of the body.
func (fn *integrityFunc) scanTopLevel(facts *integrityFacts, body *ast.BlockStmt) {
	for i, stmt := range body.List {
		line := fn.owner.fset.Position(stmt.Pos()).Line
		switch stmt := stmt.(type) {
		case *ast.ReturnStmt:
			if i < len(body.List)-1 {
				facts.earlyReturn++
				if facts.returnLine == 0 {
					facts.returnLine = line
				}
			}
		case *ast.IfStmt:
			if !integrityOnlyReturns(stmt) || fn.owner.guardedIf(stmt) {
				continue
			}
			if value, ok := integrityConstBool(stmt.Cond); ok && value {
				facts.disguisedTrue++
			} else {
				facts.disguisedLoad++
			}
			if facts.disguisedLine == 0 {
				facts.disguisedLine = line
			}
		}
	}
}

func integrityOnlyReturns(stmt *ast.IfStmt) bool {
	if stmt.Else != nil || stmt.Init != nil || len(stmt.Body.List) == 0 {
		return false
	}
	for _, inner := range stmt.Body.List {
		if _, ok := inner.(*ast.ReturnStmt); !ok {
			return false
		}
	}
	return true
}

// integrityConstBool evaluates conditions made only of constants, such as
// true, 1 == 1 or !(2 < 1).
func integrityConstBool(expr ast.Expr) (value, ok bool) {
	switch node := expr.(type) {
	case *ast.Ident:
		switch node.Name {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	case *ast.ParenExpr:
		return integrityConstBool(node.X)
	case *ast.UnaryExpr:
		if inner, ok := integrityConstBool(node.X); ok && node.Op == token.NOT {
			return !inner, true
		}
	case *ast.BinaryExpr:
		return integrityConstBinary(node)
	}
	return false, false
}

func integrityConstBinary(node *ast.BinaryExpr) (value, ok bool) {
	if node.Op == token.LAND || node.Op == token.LOR {
		left, okLeft := integrityConstBool(node.X)
		right, okRight := integrityConstBool(node.Y)
		if !okLeft || !okRight {
			return false, false
		}
		if node.Op == token.LAND {
			return left && right, true
		}
		return left || right, true
	}
	left, okLeft := integrityConstInt(node.X)
	right, okRight := integrityConstInt(node.Y)
	if !okLeft || !okRight {
		return false, false
	}
	switch node.Op {
	case token.EQL:
		return left == right, true
	case token.NEQ:
		return left != right, true
	case token.LSS:
		return left < right, true
	case token.GTR:
		return left > right, true
	case token.LEQ:
		return left <= right, true
	case token.GEQ:
		return left >= right, true
	}
	return false, false
}

func integrityConstInt(expr ast.Expr) (int64, bool) {
	if paren, ok := expr.(*ast.ParenExpr); ok {
		return integrityConstInt(paren.X)
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	value, err := strconv.ParseInt(lit.Value, 0, 64)
	return value, err == nil
}

// integrityTestingParams collects the names of every testing.T, testing.F,
// testing.B or testing.TB parameter in the function, including those of
// subtest closures. Only calls on these names count as test method calls.
func integrityTestingParams(fn *integrityFunc) map[string]bool {
	names := map[string]bool{}
	testing := fn.owner.testingName
	collect := func(ft *ast.FuncType) {
		if ft == nil || ft.Params == nil || testing == "" {
			return
		}
		for _, field := range ft.Params.List {
			if !integrityIsTestingParam(field.Type, testing) {
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

func integrityIsTestingParam(expr ast.Expr, testing string) bool {
	for _, kind := range []string{"T", "F", "B"} {
		if integrityIsTestingPointer(expr, testing, kind) {
			return true
		}
	}
	return integrityIsTestingSelector(expr, testing, "TB")
}

// recordCallee remembers calls into code under test, leaving out testing
// methods, assertion libraries, fmt, standard library packages and builtins.
func (fn *integrityFunc) recordCallee(facts *integrityFacts, call *ast.CallExpr, tNames map[string]bool) {
	line := fn.owner.fset.Position(call.Pos()).Line
	switch callee := call.Fun.(type) {
	case *ast.Ident:
		if !integrityPredeclared[callee.Name] {
			facts.callees = append(facts.callees, integrityCall{name: callee.Name, line: line})
		}
	case *ast.SelectorExpr:
		if recv, ok := callee.X.(*ast.Ident); ok {
			if tNames[recv.Name] {
				return
			}
			if importPath, isPkg := fn.owner.imports[recv.Name]; isPkg {
				if !integrityIsLibraryImport(recv.Name, importPath) {
					facts.callees = append(facts.callees, integrityCall{name: callee.Sel.Name, line: line})
				}
				return
			}
		}
		facts.callees = append(facts.callees, integrityCall{name: callee.Sel.Name, line: line, method: true})
	}
}

// integrityIsLibraryImport reports imports whose calls say nothing about which
// code a test exercises: the standard library and assertion packages.
func integrityIsLibraryImport(name, importPath string) bool {
	if name == "assert" || name == "require" {
		return true
	}
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

func (fn *integrityFunc) countCall(facts *integrityFacts, call *ast.CallExpr, tNames map[string]bool, stack []ast.Node) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if recv, ok := sel.X.(*ast.Ident); ok {
			if _, isPkg := fn.owner.imports[recv.Name]; isPkg {
				if recv.Name == "assert" || recv.Name == "require" {
					facts.failing++
					return
				}
			} else if tNames[recv.Name] && fn.countTestingMethod(facts, sel.Sel.Name, call, stack) {
				return
			}
		}
	}
	for _, arg := range call.Args {
		if ident, ok := arg.(*ast.Ident); ok && tNames[ident.Name] {
			facts.failing++
			return
		}
	}
}

// countTestingMethod classifies a call on a testing value and reports whether
// it was one of the methods the rules care about.
func (fn *integrityFunc) countTestingMethod(facts *integrityFacts, name string, call *ast.CallExpr, stack []ast.Node) bool {
	switch {
	case integrityFailNames[name]:
		facts.failing++
		if integrityErrNames[name] {
			facts.errorCalls++
		}
	case integritySkipNames[name]:
		facts.skips = append(facts.skips, integritySkip{
			line:    fn.owner.fset.Position(call.Pos()).Line,
			guarded: fn.owner.guarded(stack),
		})
	case integrityLogNames[name]:
		facts.logCalls++
	case name == "Run":
		facts.runCalls++
	default:
		return false
	}
	return true
}

// guarded reports whether any enclosing if statement tests the platform, the
// environment or testing.Short.
func (file *integrityFile) guarded(stack []ast.Node) bool {
	for _, node := range stack {
		if stmt, ok := node.(*ast.IfStmt); ok && file.guardedIf(stmt) {
			return true
		}
	}
	return false
}

func (file *integrityFile) guardedIf(stmt *ast.IfStmt) bool {
	return file.referencesGuard(stmt.Init) || file.referencesGuard(stmt.Cond)
}

// referencesGuard resolves import aliases, so goruntime.GOOS counts and a
// local variable named runtime does not.
func (file *integrityFile) referencesGuard(node ast.Node) bool {
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
		switch file.imports[pkg.Name] + "." + sel.Sel.Name {
		case "runtime.GOOS", "runtime.GOARCH", "os.Getenv", "os.LookupEnv", "testing.Short":
			found = true
		}
		return !found
	})
	return found
}
