// Package retrieval provides bounded, deterministic discovery over observed Go
// source. It deliberately returns source coordinates only; the intelligence
// layer remains responsible for resolving them through the active semantic
// provider before exposing evidence.
package retrieval

import (
	"bytes"
	"container/heap"
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
	"time"
	"unicode"
	"unicode/utf8"
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

// MaximumTextLineFragments bounds transient text candidate expansion in the
// evaluation-only mixed retrieval path.
const MaximumTextLineFragments = 50_000

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
	Name      string
	Qualified string
	Kind      string
	Package   string
	Score     float64
	Line      int
	Column    int
}

// Result reports ranked candidates and whether every eligible Go file was
// structurally indexed. A partial result is still useful for discovery, but
// callers must preserve the incompleteness as uncertainty.
type Result struct {
	Candidates           []Candidate
	CandidateCount       int
	IndexedFiles         int
	SkippedFiles         int
	TextIndexedFiles     int
	TextSkippedFiles     int
	TextIndexedFragments int
	Complete             bool
	Truncated            bool
}

// SearchProfile reports non-overlapping work performed by one SearchProfiled
// call. It is internal diagnostic data and is not part of any MCP contract.
type SearchProfile struct {
	SearchDuration    time.Duration
	ParseDuration     time.Duration
	AggregateDuration time.Duration
	RankDuration      time.Duration
	FileVisits        int
	CacheHits         int
	FilesParsed       int
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
	size     int
	complete bool
}

// Cache reuses parsed per-file fragments across observations while retaining
// only bounded derived metadata in memory. Eviction only causes reparsing.
type Cache struct {
	entries map[fileKey]*list.Element
	order   *list.List
	mu      sync.Mutex
	maximum int
	bytes   int
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
	result, _, err := c.SearchProfiled(ctx, key, files, query, limit)
	return result, err
}

// SearchProfiled ranks declarations like Search and returns stage timings for
// benchmark and diagnostic use. It never reads from disk and does not alter
// the search result or cache policy.
func (c *Cache) SearchProfiled(ctx context.Context, key Key, files []File, query string, limit int) (Result, SearchProfile, error) {
	return c.searchProfiled(ctx, key, files, nil, query, limit)
}

// SearchWithTextProfiled runs the declaration scorer over Go declaration
// anchors plus bounded caller-supplied text-line fragments. It does not read
// files from disk. The live intelligence path continues to call
// SearchProfiled, which indexes Go declarations only.
func (c *Cache) SearchWithTextProfiled(ctx context.Context, key Key, goFiles, textFiles []File, query string, limit int) (Result, SearchProfile, error) {
	return c.searchProfiled(ctx, key, goFiles, textFiles, query, limit)
}

func (c *Cache) searchProfiled(ctx context.Context, key Key, files, textFiles []File, query string, limit int) (Result, SearchProfile, error) {
	started := time.Now()
	profile := SearchProfile{}
	if c == nil {
		c = NewCache()
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		profile.SearchDuration = time.Since(started)
		return Result{Candidates: []Candidate{}, Complete: true}, profile, nil
	}
	if limit < 1 {
		limit = 20
	}

	result := Result{Candidates: []Candidate{}, Complete: true}
	collectStarted := time.Now()
	queryCounts := countTerms(terms)
	documentFrequency := make(map[string]int, len(queryCounts))
	totalFragments := 0
	totalLength := 0
	textFragments := make([]fragment, 0)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			collectDuration := time.Since(collectStarted)
			profile.AggregateDuration += collectDuration - profile.ParseDuration
			profile.SearchDuration = time.Since(started)
			return Result{}, profile, err
		}
		if !strings.HasSuffix(strings.ToLower(file.Path), ".go") {
			continue
		}
		profile.FileVisits++
		indexed, complete, cacheHit, parseDuration := c.fileIndex(key, file)
		if cacheHit {
			profile.CacheHits++
		} else {
			profile.FilesParsed++
			profile.ParseDuration += parseDuration
		}
		if !complete {
			result.Complete = false
			result.SkippedFiles++
		}
		if len(indexed.fragments) == 0 {
			continue
		}
		result.IndexedFiles++
		for _, item := range indexed.fragments {
			totalFragments++
			totalLength += item.length
			for term := range item.terms {
				if _, queried := queryCounts[term]; queried {
					documentFrequency[term]++
				}
			}
		}
	}
	for fileIndex, file := range textFiles {
		if err := ctx.Err(); err != nil {
			collectDuration := time.Since(collectStarted)
			profile.AggregateDuration += collectDuration - profile.ParseDuration
			profile.SearchDuration = time.Since(started)
			return Result{}, profile, err
		}
		profile.FileVisits++
		remaining := MaximumTextLineFragments - result.TextIndexedFragments
		if remaining <= 0 {
			result.Complete = false
			result.TextSkippedFiles += len(textFiles) - fileIndex
			break
		}
		fragments, valid, fullyIndexed := indexTextLines(file, remaining)
		if !valid {
			result.Complete = false
			result.TextSkippedFiles++
			continue
		}
		if !fullyIndexed {
			result.Complete = false
			result.TextSkippedFiles += len(textFiles) - fileIndex
			break
		}
		result.TextIndexedFiles++
		result.TextIndexedFragments += len(fragments)
		for _, item := range fragments {
			textFragments = append(textFragments, item)
			totalFragments++
			totalLength += item.length
			for term := range item.terms {
				if _, queried := queryCounts[term]; queried {
					documentFrequency[term]++
				}
			}
		}
	}
	profile.AggregateDuration += time.Since(collectStarted) - profile.ParseDuration

	if totalFragments == 0 {
		profile.SearchDuration = time.Since(started)
		return result, profile, nil
	}

	averageLength := float64(totalLength) / float64(totalFragments)
	if averageLength == 0 {
		averageLength = 1
	}

	rankStarted := time.Now()
	top := candidateHeap{}
	heap.Init(&top)
	candidateCount := 0
	secondPassParseDuration := time.Duration(0)
	for _, file := range files {
		if !strings.HasSuffix(strings.ToLower(file.Path), ".go") {
			continue
		}
		profile.FileVisits++
		if err := ctx.Err(); err != nil {
			profile.RankDuration += time.Since(rankStarted) - secondPassParseDuration
			profile.SearchDuration = time.Since(started)
			return Result{}, profile, err
		}
		indexed, _, cacheHit, parseDuration := c.fileIndex(key, file)
		if cacheHit {
			profile.CacheHits++
		} else {
			profile.FilesParsed++
			profile.ParseDuration += parseDuration
			secondPassParseDuration += parseDuration
		}
		for _, item := range indexed.fragments {
			candidateScore := scoreWithQueryCounts(item, terms, queryCounts, documentFrequency, totalFragments, averageLength)
			if candidateScore <= 0 {
				continue
			}
			candidateCount++
			candidate := Candidate{
				Path: item.path, Line: item.line, Column: item.column,
				Name: item.name, Qualified: item.qualified, Kind: item.kind,
				Package: item.packageName, Score: candidateScore,
			}
			if top.Len() < limit {
				heap.Push(&top, candidate)
			} else if candidateRanksBefore(candidate, top[0]) {
				top[0] = candidate
				heap.Fix(&top, 0)
			}
		}
	}
	for _, item := range textFragments {
		if err := ctx.Err(); err != nil {
			profile.RankDuration += time.Since(rankStarted) - secondPassParseDuration
			profile.SearchDuration = time.Since(started)
			return Result{}, profile, err
		}
		candidateScore := scoreWithQueryCounts(item, terms, queryCounts, documentFrequency, totalFragments, averageLength)
		if candidateScore <= 0 {
			continue
		}
		candidateCount++
		candidate := Candidate{
			Path: item.path, Line: item.line, Column: item.column,
			Name: item.name, Qualified: item.qualified, Kind: item.kind,
			Package: item.packageName, Score: candidateScore,
		}
		if top.Len() < limit {
			heap.Push(&top, candidate)
		} else if candidateRanksBefore(candidate, top[0]) {
			top[0] = candidate
			heap.Fix(&top, 0)
		}
	}
	profile.RankDuration += time.Since(rankStarted) - secondPassParseDuration
	result.CandidateCount = candidateCount
	result.Truncated = candidateCount > limit
	result.Candidates = append(result.Candidates, top...)
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		return candidateRanksBefore(result.Candidates[i], result.Candidates[j])
	})
	profile.SearchDuration = time.Since(started)
	return result, profile, nil
}

func indexTextLines(file File, maximumFragments int) ([]fragment, bool, bool) {
	if strings.HasSuffix(strings.ToLower(file.Path), ".go") || !utf8.Valid(file.Contents) || bytes.IndexByte(file.Contents, 0) >= 0 {
		return nil, false, false
	}
	fragments := make([]fragment, 0)
	lineNumber := 1
	for start := 0; start < len(file.Contents); {
		end := bytes.IndexByte(file.Contents[start:], '\n')
		var line []byte
		if end < 0 {
			line = file.Contents[start:]
			start = len(file.Contents)
		} else {
			line = file.Contents[start : start+end]
			start += end + 1
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})
		terms := countTerms(tokenize(string(line)))
		if len(terms) != 0 {
			if len(fragments) >= maximumFragments {
				return nil, true, false
			}
			fragments = append(fragments, fragment{
				path: file.Path, line: lineNumber, kind: "text.line",
				terms: terms, length: termCount(terms),
			})
		}
		lineNumber++
	}
	return fragments, true, true
}

// candidateHeap keeps the worst selected candidate at the root so ranking can
// retain only the requested result count while scanning large workspaces.
type candidateHeap []Candidate

func (candidates candidateHeap) Len() int { return len(candidates) }

func (candidates candidateHeap) Less(left, right int) bool {
	return candidateRanksBefore(candidates[right], candidates[left])
}

func (candidates candidateHeap) Swap(left, right int) {
	candidates[left], candidates[right] = candidates[right], candidates[left]
}

func (candidates *candidateHeap) Push(value any) {
	*candidates = append(*candidates, value.(Candidate))
}

func (candidates *candidateHeap) Pop() any {
	items := *candidates
	last := len(items) - 1
	value := items[last]
	items[last] = Candidate{}
	*candidates = items[:last]
	return value
}

func candidateRanksBefore(left, right Candidate) bool {
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
}

type indexedFile struct {
	fragments []fragment
}

type fragment struct {
	path           string
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
	line           int
	column         int
}

func (c *Cache) fileIndex(key Key, file File) (indexedFile, bool, bool, time.Duration) {
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
		return entry.index, entry.complete, true, 0
	}

	started := time.Now()
	indexed, complete := parseFile(file.Path, file.Contents)
	parseDuration := time.Since(started)
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
	return indexed, complete, false, parseDuration
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
			fragments = append(fragments, makeFragment(fileSet, contents, path, specification.Pos(), specification.End(), specification.Name.Pos(), specification.Name.Name, packageName+"."+specification.Name.Name, kind, packageName, "", firstComment(specification.Doc, declaration.Doc)))
		case *ast.ValueSpec:
			kind := "go.var"
			if declaration.Tok == token.CONST {
				kind = "go.const"
			}
			for _, name := range specification.Names {
				fragments = append(fragments, makeFragment(fileSet, contents, path, specification.Pos(), specification.End(), name.Pos(), name.Name, packageName+"."+name.Name, kind, packageName, "", firstComment(specification.Doc, declaration.Doc)))
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
	var text strings.Builder
	text.Grow(endOffset - startOffset + len(name) + len(qualified) + len(kind) + len(packageName) + len(receiver) + 8)
	text.Write(contents[startOffset:endOffset])
	if doc != nil {
		text.WriteByte('\n')
		text.WriteString(doc.Text())
	}
	text.WriteByte('\n')
	text.WriteString(name)
	text.WriteByte('\n')
	text.WriteString(qualified)
	text.WriteByte('\n')
	text.WriteString(kind)
	text.WriteByte('\n')
	text.WriteString(packageName)
	text.WriteByte('\n')
	text.WriteString(receiver)
	terms := countTerms(tokenize(text.String()))
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
	expression := fields.List[0].Type
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
	return scoreWithQueryCounts(item, query, countTerms(query), documentFrequency, documentCount, averageLength)
}

func scoreWithQueryCounts(item fragment, query []string, queryCounts map[string]int, documentFrequency map[string]int, documentCount int, averageLength float64) float64 {
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
		// Account conservatively for the fragment, term map buckets, and token
		// slice headers. The cache limit is intended to bound retained metadata,
		// so counting only string payloads materially understates its footprint.
		size += 256
		size += len(item.path) + len(item.name) + len(item.qualified) + len(item.kind) + len(item.packageName) + len(item.receiver)
		size += len(item.terms) * 64
		for term := range item.terms {
			size += len(term)
		}
		size = estimateTerms(size, item.nameTerms)
		size = estimateTerms(size, item.qualifiedTerms)
		size = estimateTerms(size, item.receiverTerms)
		size = estimateTerms(size, item.packageTerms)
		size = estimateTerms(size, item.kindTerms)
	}
	return size
}

func estimateTerms(size int, terms []string) int {
	size += len(terms) * 16
	for _, term := range terms {
		size += len(term)
	}
	return size
}

func tokenize(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	result := make([]string, 0, len(value)/8)
	var normalized strings.Builder
	ends := make([]int, 0, len(value)/8)
	start := -1
	flush := func(end int) {
		if start < 0 || end <= start {
			start = -1
			return
		}
		normalized.WriteString(strings.ToLower(value[start:end]))
		ends = append(ends, normalized.Len())
		normalized.WriteByte(0)
		start = -1
	}
	var previous rune
	for index := 0; index < len(value); {
		current, size := utf8.DecodeRuneInString(value[index:])
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) && current != '_' {
			flush(index)
			previous = 0
			index += size
			continue
		}
		if start < 0 {
			start = index
			previous = current
			index += size
			continue
		}
		if current == '_' {
			flush(index)
			previous = 0
			index += size
			continue
		}
		if unicode.IsUpper(current) && unicode.IsLower(previous) {
			flush(index)
			start = index
		}
		previous = current
		index += size
	}
	flush(len(value))
	if len(ends) == 0 {
		return result[:0]
	}
	packed := normalized.String()
	start = 0
	for _, end := range ends {
		result = append(result, packed[start:end])
		start = end + 1
	}
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
