// Package retrieval provides bounded, deterministic discovery over observed Go
// source. It deliberately returns source coordinates only; the intelligence
// layer remains responsible for resolving them through the active semantic
// provider before exposing evidence.
package retrieval

import (
	"container/list"
	"context"
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	defaultMaximumCacheBytes = 64 << 20
	bm25K1                   = 1.2
	bm25B                    = 0.75
	qualifiedBoost           = 12.0
	nameBoost                = 8.0
	receiverBoost            = 3.0
	packageBoost             = 2.0
	kindBoost                = 1.5
	identifierBoost          = 2.0
)

// Key identifies the source interpretation under which fragments were built.
// The caller supplies values from the active snapshot rather than an absolute
// filesystem path so cached fragments cannot become public evidence by
// themselves.
type Key struct {
	Workspace string
	Scope     string
	Build     string
	Provider  string
}

// File is one contained file from an already validated workspace observation.
type File struct {
	Path     string
	Digest   string
	Contents []byte
}

// Candidate is a ranked source coordinate for semantic-provider resolution.
// Line and Column are one-based UTF-8 byte coordinates.
type Candidate struct {
	Path      string
	Line      int
	Column    int
	Name      string
	Qualified string
	Kind      string
	Package   string
	Score     float64
}

// Result reports ranked candidates and whether every eligible Go file was
// structurally indexed. A partial result is still useful for discovery, but
// callers must preserve the incompleteness as uncertainty.
type Result struct {
	Candidates   []Candidate
	IndexedFiles int
	SkippedFiles int
	Complete     bool
	Truncated    bool
}

type fileKey struct {
	workspace string
	scope     string
	build     string
	provider  string
	path      string
	digest    string
}

type cacheEntry struct {
	key      fileKey
	index    indexedFile
	complete bool
	size     int
}

// Cache reuses parsed per-file fragments across observations while retaining
// only bounded derived metadata in memory. Eviction only causes reparsing.
type Cache struct {
	mu      sync.Mutex
	maximum int
	bytes   int
	entries map[fileKey]*list.Element
	order   *list.List
}

// NewCache constructs the process-local retrieval cache.
func NewCache() *Cache {
	return &Cache{
		maximum: defaultMaximumCacheBytes,
		entries: make(map[fileKey]*list.Element),
		order:   list.New(),
	}
}

// Search ranks declarations from the supplied observed files. It never reads
// from disk and never returns a cached result without rebuilding the current
// file set's aggregate statistics.
func (c *Cache) Search(ctx context.Context, key Key, files []File, query string, limit int) (Result, error) {
	if c == nil {
		c = NewCache()
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return Result{Candidates: []Candidate{}, Complete: true}, nil
	}
	if limit < 1 {
		limit = 20
	}

	fragments := make([]fragment, 0)
	result := Result{Candidates: []Candidate{}, Complete: true}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if !strings.HasSuffix(strings.ToLower(file.Path), ".go") {
			continue
		}
		indexed, complete := c.fileIndex(key, file)
		if !complete {
			result.Complete = false
			result.SkippedFiles++
		}
		if len(indexed.fragments) == 0 {
			continue
		}
		result.IndexedFiles++
		fragments = append(fragments, indexed.fragments...)
	}

	if len(fragments) == 0 {
		return result, nil
	}

	documentFrequency := make(map[string]int)
	totalLength := 0
	for _, item := range fragments {
		seen := make(map[string]struct{}, len(item.terms))
		for term := range item.terms {
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			documentFrequency[term]++
		}
		totalLength += item.length
	}
	averageLength := float64(totalLength) / float64(len(fragments))
	if averageLength == 0 {
		averageLength = 1
	}

	for _, item := range fragments {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		score := score(item, terms, documentFrequency, len(fragments), averageLength)
		if score <= 0 {
			continue
		}
		result.Candidates = append(result.Candidates, Candidate{
			Path: item.path, Line: item.line, Column: item.column,
			Name: item.name, Qualified: item.qualified, Kind: item.kind,
			Package: item.packageName, Score: score,
		})
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		left, right := result.Candidates[i], result.Candidates[j]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		return left.Name < right.Name
	})
	if len(result.Candidates) > limit {
		result.Candidates = result.Candidates[:limit]
		result.Truncated = true
	}
	return result, nil
}

type indexedFile struct {
	fragments []fragment
}

type fragment struct {
	path           string
	line           int
	column         int
	name           string
	qualified      string
	kind           string
	packageName    string
	receiver       string
	terms          map[string]int
	nameTerms      []string
	qualifiedTerms []string
	receiverTerms  []string
	packageTerms   []string
	kindTerms      []string
	length         int
}

func (c *Cache) fileIndex(key Key, file File) (indexedFile, bool) {
	digest := file.Digest
	if digest == "" {
		digestBytes := sha256.Sum256(file.Contents)
		digest = string(digestBytes[:])
	}
	cacheKey := fileKey{
		workspace: key.Workspace, scope: key.Scope, build: key.Build,
		provider: key.Provider, path: file.Path, digest: digest,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[cacheKey]; ok {
		c.order.MoveToFront(element)
		entry := element.Value.(cacheEntry)
		return entry.index, entry.complete
	}

	indexed, complete := parseFile(file.Path, file.Contents)
	entry := cacheEntry{key: cacheKey, index: indexed, complete: complete, size: estimateSize(indexed)}
	if entry.size <= c.maximum {
		for c.bytes+entry.size > c.maximum && c.order.Len() > 0 {
			oldest := c.order.Back()
			if oldest == nil {
				break
			}
			removed := oldest.Value.(cacheEntry)
			delete(c.entries, removed.key)
			c.bytes -= removed.size
			c.order.Remove(oldest)
		}
		element := c.order.PushFront(entry)
		c.entries[cacheKey] = element
		c.bytes += entry.size
	}
	return indexed, complete
}

func parseFile(path string, contents []byte) (indexedFile, bool) {
	fileSet := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fileSet, path, contents, parser.ParseComments|parser.AllErrors)
	if parsed == nil {
		return indexedFile{}, false
	}
	complete := parseErr == nil
	fragments := make([]fragment, 0)
	for _, declaration := range parsed.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			name := declaration.Name.Name
			receiver := receiverName(declaration.Recv)
			kind := "go.function"
			qualified := parsed.Name.Name + "." + name
			if receiver != "" {
				kind = "go.method"
				qualified = parsed.Name.Name + "." + receiver + "." + name
			}
			fragments = append(fragments, makeFragment(fileSet, contents, path, declaration.Pos(), declaration.End(), declaration.Name.Pos(), name, qualified, kind, parsed.Name.Name, receiver, declaration.Doc))
		case *ast.GenDecl:
			fragments = append(fragments, fragmentsFromGenDecl(fileSet, contents, path, parsed.Name.Name, declaration)...)
		}
	}
	return indexedFile{fragments: fragments}, complete
}

func fragmentsFromGenDecl(fileSet *token.FileSet, contents []byte, path, packageName string, declaration *ast.GenDecl) []fragment {
	fragments := make([]fragment, 0, len(declaration.Specs))
	for _, specification := range declaration.Specs {
		switch specification := specification.(type) {
		case *ast.TypeSpec:
			kind := "go.type"
			if specification.Assign.IsValid() {
				kind = "go.type_alias"
			}
			fragments = append(fragments, makeFragment(fileSet, contents, path, declaration.Pos(), specification.End(), specification.Name.Pos(), specification.Name.Name, packageName+"."+specification.Name.Name, kind, packageName, "", firstComment(specification.Doc, declaration.Doc)))
		case *ast.ValueSpec:
			kind := "go.var"
			if declaration.Tok == token.CONST {
				kind = "go.const"
			}
			for _, name := range specification.Names {
				fragments = append(fragments, makeFragment(fileSet, contents, path, declaration.Pos(), specification.End(), name.Pos(), name.Name, packageName+"."+name.Name, kind, packageName, "", firstComment(specification.Doc, declaration.Doc)))
			}
		}
	}
	return fragments
}

func firstComment(primary, fallback *ast.CommentGroup) *ast.CommentGroup {
	if primary != nil {
		return primary
	}
	return fallback
}

func makeFragment(fileSet *token.FileSet, contents []byte, path string, start, end, namePosition token.Pos, name, qualified, kind, packageName, receiver string, doc *ast.CommentGroup) fragment {
	startOffset := fileSet.PositionFor(start, false).Offset
	endOffset := fileSet.PositionFor(end, false).Offset
	if startOffset < 0 {
		startOffset = 0
	}
	if endOffset < startOffset || endOffset > len(contents) {
		endOffset = len(contents)
	}
	position := fileSet.PositionFor(namePosition, false)
	text := string(contents[startOffset:endOffset])
	if doc != nil {
		text += "\n" + doc.Text()
	}
	text += "\n" + name + "\n" + qualified + "\n" + kind + "\n" + packageName + "\n" + receiver
	terms := countTerms(tokenize(text))
	return fragment{
		path: path, line: position.Line, column: position.Column,
		name: name, qualified: qualified, kind: kind, packageName: packageName,
		receiver: receiver, terms: terms, nameTerms: tokenize(name),
		qualifiedTerms: tokenize(qualified), receiverTerms: tokenize(receiver),
		packageTerms: tokenize(packageName), kindTerms: tokenize(kind), length: termCount(terms),
	}
}

func receiverName(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	var expression ast.Expr = fields.List[0].Type
	for {
		switch typed := expression.(type) {
		case *ast.StarExpr:
			expression = typed.X
		case *ast.ParenExpr:
			expression = typed.X
		case *ast.IndexExpr:
			expression = typed.X
		case *ast.IndexListExpr:
			expression = typed.X
		case *ast.Ident:
			return typed.Name
		default:
			return ""
		}
	}
}

func score(item fragment, query []string, documentFrequency map[string]int, documentCount int, averageLength float64) float64 {
	queryCounts := countTerms(query)
	score := 0.0
	for term, queryFrequency := range queryCounts {
		frequency := item.terms[term]
		if frequency == 0 {
			continue
		}
		documentFrequencyForTerm := documentFrequency[term]
		inverseFrequency := math.Log(1 + (float64(documentCount-documentFrequencyForTerm)+0.5)/(float64(documentFrequencyForTerm)+0.5))
		normalizedLength := float64(item.length)
		termFrequency := float64(frequency)
		bm25 := inverseFrequency * (termFrequency * (bm25K1 + 1)) /
			(termFrequency + bm25K1*(1-bm25B+bm25B*normalizedLength/averageLength))
		score += bm25 * float64(queryFrequency)
	}
	if containsSequence(query, item.qualifiedTerms) {
		score += qualifiedBoost
	} else if containsSequence(query, item.nameTerms) {
		score += nameBoost
	}
	if containsSequence(query, item.receiverTerms) {
		score += receiverBoost
	}
	if overlaps(query, item.packageTerms) {
		score += packageBoost
	}
	if overlaps(query, item.kindTerms) {
		score += kindBoost
	}
	if overlaps(query, item.nameTerms) || overlaps(query, item.qualifiedTerms) {
		score += identifierBoost
	}
	return score
}

func estimateSize(index indexedFile) int {
	size := 0
	for _, item := range index.fragments {
		size += len(item.path) + len(item.name) + len(item.qualified) + len(item.kind) + len(item.packageName) + len(item.receiver)
		for term := range item.terms {
			size += len(term) + 8
		}
	}
	return size
}

func tokenize(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	runes := []rune(value)
	result := make([]string, 0, len(runes)/2)
	start := -1
	flush := func(end int) {
		if start < 0 || end <= start {
			start = -1
			return
		}
		result = append(result, strings.ToLower(string(runes[start:end])))
		start = -1
	}
	for index, current := range runes {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) && current != '_' {
			flush(index)
			continue
		}
		if start < 0 {
			start = index
			continue
		}
		previous := runes[index-1]
		if current == '_' {
			flush(index)
			continue
		}
		if unicode.IsUpper(current) && unicode.IsLower(previous) {
			flush(index)
			start = index
		}
	}
	flush(len(runes))
	return result
}

func countTerms(terms []string) map[string]int {
	counts := make(map[string]int, len(terms))
	for _, term := range terms {
		counts[term]++
	}
	return counts
}

func termCount(terms map[string]int) int {
	count := 0
	for _, frequency := range terms {
		count += frequency
	}
	return count
}

func overlaps(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		set[value] = struct{}{}
	}
	for _, value := range left {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func containsSequence(haystack, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for start := 0; start <= len(haystack)-len(needle); start++ {
		match := true
		for offset, value := range needle {
			if haystack[start+offset] != value {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
