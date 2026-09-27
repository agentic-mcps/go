package retrieval

import (
	"bytes"
	"context"
	"math"
	"reflect"
	"sort"
	"testing"
)

func TestSearchRanksExactAndStructuralMatches(t *testing.T) {
	cache := NewCache()
	files := []File{{
		Path:   "payments.go",
		Digest: "payments-v1",
		Contents: []byte(`package payments

// ProcessPayment validates and processes an incoming payment request.
func ProcessPayment() {}

// ReconcilePayment compares settled payment records.
func ReconcilePayment() {}
`),
	}}

	result, err := cache.Search(context.Background(), Key{Workspace: "repo", Scope: "./...", Build: "go1.25", Provider: "gopls"}, files, "process incoming payment", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) == 0 || result.Candidates[0].Name != "ProcessPayment" {
		t.Fatalf("Search() candidates = %#v, want ProcessPayment first", result.Candidates)
	}
	if result.Candidates[0].Line != 4 || result.Candidates[0].Column != 6 {
		t.Fatalf("Search() location = %d:%d, want 4:6", result.Candidates[0].Line, result.Candidates[0].Column)
	}
}

func TestSearchKeepsGroupedSpecsAsSeparateFragments(t *testing.T) {
	file := File{Path: "values.go", Contents: []byte(`package fixture

const (
	Alpha = "needle"
	Beta  = "ordinary"
)
`)}
	result, err := NewCache().Search(context.Background(), Key{Workspace: "repo"}, []File{file}, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Name != "Alpha" {
		t.Fatalf("Search() candidates = %#v, want only Alpha", result.Candidates)
	}
}

func TestTokenizePreservesUnicodeAndIdentifierBoundaries(t *testing.T) {
	got := tokenize("ProcessPayment café déjàVu HTTPServer _Leading trailing_ x")
	want := []string{"process", "payment", "café", "déjà", "vu", "httpserver", "_leading", "trailing", "x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenize() = %#v, want %#v", got, want)
	}
}

func TestSearchIsDeterministicAndReusesCachedFragments(t *testing.T) {
	cache := NewCache()
	key := Key{Workspace: "repo", Scope: "./...", Build: "go1.25", Provider: "gopls"}
	files := []File{{Path: "a.go", Digest: "a-v1", Contents: []byte("package fixture\n\nfunc Alpha() {}\n")}, {Path: "b.go", Digest: "b-v1", Contents: []byte("package fixture\n\nfunc Beta() {}\n")}}

	first, err := cache.Search(context.Background(), key, files, "function", 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.Search(context.Background(), key, files, "function", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Candidates, second.Candidates) {
		t.Fatalf("repeated Search() differs:\nfirst=%#v\nsecond=%#v", first.Candidates, second.Candidates)
	}
	if len(cache.entries) != 2 {
		t.Fatalf("cached entries = %d, want 2", len(cache.entries))
	}
}

func TestSearchProfiledReportsColdAndWarmWorkWithoutChangingResults(t *testing.T) {
	cache := NewCache()
	key := Key{Workspace: "repo", Scope: "./...", Build: "go1.25", Provider: "gopls"}
	files := []File{
		{Path: "a.go", Digest: "a-v1", Contents: []byte("package fixture\n\nfunc AlphaWorker() {}\n")},
		{Path: "b.go", Digest: "b-v1", Contents: []byte("package fixture\n\nfunc BetaWorker() {}\n")},
	}

	cold, coldProfile, err := cache.SearchProfiled(context.Background(), key, files, "worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	warm, warmProfile, err := cache.SearchProfiled(context.Background(), key, files, "worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := cache.Search(context.Background(), key, files, "worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cold.Candidates, warm.Candidates) || !reflect.DeepEqual(cold.Candidates, plain.Candidates) {
		t.Fatalf("profiled Search changed candidates:\ncold=%#v\nwarm=%#v\nplain=%#v", cold.Candidates, warm.Candidates, plain.Candidates)
	}
	if coldProfile.FileVisits != 4 || coldProfile.FilesParsed != 2 || coldProfile.CacheHits != 2 {
		t.Fatalf("cold profile = %+v, want two file passes, two parses, and two hits", coldProfile)
	}
	if warmProfile.FileVisits != 4 || warmProfile.FilesParsed != 0 || warmProfile.CacheHits != 4 {
		t.Fatalf("warm profile = %+v, want two file passes and four cache hits", warmProfile)
	}
	if coldProfile.SearchDuration <= 0 || coldProfile.ParseDuration <= 0 || warmProfile.SearchDuration <= 0 || warmProfile.ParseDuration != 0 {
		t.Fatalf("invalid cold/warm durations: cold=%+v warm=%+v", coldProfile, warmProfile)
	}
}

func TestSearchWithTextProfiledAddsBoundedTextCandidatesWithSameScorer(t *testing.T) {
	goFiles := []File{{
		Path: "position.go",
		Contents: []byte("package fixture\n\n// Position converts source offsets.\nfunc Position() {}\n"),
	}}
	textFiles := []File{{
		Path: "contracts.md",
		Contents: []byte("Public locations use one-based UTF-8 byte columns. UTF-16 positions exist only inside the pinned LSP adapter.\n"),
	}}
	key := Key{Workspace: "repo", Scope: "commit", Build: "retrievalbench", Provider: "lexical-declaration-index"}
	query := "UTF-16 LSP adapter positions"

	goOnly, err := NewCache().Search(context.Background(), key, goFiles, query, 10)
	if err != nil {
		t.Fatal(err)
	}
	mixed, profile, err := NewCache().SearchWithTextProfiled(context.Background(), key, goFiles, textFiles, query, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(goOnly.Candidates) != 0 {
		t.Fatalf("Go-only candidates = %#v, want no match for text-only terms", goOnly.Candidates)
	}
	if len(mixed.Candidates) != 1 || mixed.Candidates[0].Path != "contracts.md" || mixed.Candidates[0].Line != 1 || mixed.Candidates[0].Kind != "text.line" {
		t.Fatalf("mixed candidates = %#v, want the source-linked Markdown line", mixed.Candidates)
	}
	if mixed.CandidateCount != 1 || mixed.TextIndexedFiles != 1 || mixed.TextSkippedFiles != 0 || !mixed.Complete {
		t.Fatalf("mixed result = %+v, want one complete text candidate", mixed)
	}
	if profile.FileVisits != 3 {
		t.Fatalf("mixed profile file visits = %d, want two Go passes and one text pass", profile.FileVisits)
	}
}

func TestSearchWithTextProfiledMarksInvalidTextIncomplete(t *testing.T) {
	files := []File{
		{Path: "bad.md", Contents: []byte{0xff}},
		{Path: "misclassified.go", Contents: []byte("package fixture\n\nfunc NotText() {}\n")},
	}
	result, _, err := NewCache().SearchWithTextProfiled(context.Background(), Key{Workspace: "repo"}, nil, files, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.TextIndexedFiles != 0 || result.TextSkippedFiles != 2 {
		t.Fatalf("invalid text result = %+v, want incomplete with two skipped files", result)
	}
}

func TestSearchWithTextProfiledCapsLineCandidateExpansion(t *testing.T) {
	contents := bytes.Repeat([]byte("needle\n"), MaximumTextLineFragments+1)
	result, _, err := NewCache().SearchWithTextProfiled(context.Background(), Key{Workspace: "repo"}, nil, []File{{
		Path: "large.md", Contents: contents,
	}}, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.TextIndexedFiles != 0 || result.TextSkippedFiles != 1 || result.TextIndexedFragments != 0 || len(result.Candidates) != 0 {
		t.Fatalf("capped text result = %+v, want the oversized file skipped as incomplete", result)
	}
}

func TestSearchStreamingTopKMatchesFullRanking(t *testing.T) {
	files := []File{
		{Path: "z.go", Contents: []byte("package fixture\n\nfunc ProcessRequest() {}\nfunc ProcessPayment() {}\n")},
		{Path: "a.go", Contents: []byte("package fixture\n\nfunc ProcessTransfer() {}\nfunc HandlePayment() {}\n")},
		{Path: "m.go", Contents: []byte("package fixture\n\nfunc ProcessRefund() {}\n")},
	}
	queryTerms := tokenize("process payment")
	indexedFiles := make([]indexedFile, 0, len(files))
	documentFrequency := make(map[string]int)
	totalFragments, totalLength := 0, 0
	for _, file := range files {
		indexed, complete := parseFile(file.Path, file.Contents)
		if !complete {
			t.Fatalf("parseFile(%q) was incomplete", file.Path)
		}
		indexedFiles = append(indexedFiles, indexed)
		for _, item := range indexed.fragments {
			totalFragments++
			totalLength += item.length
			for term := range item.terms {
				documentFrequency[term]++
			}
		}
	}
	averageLength := float64(totalLength) / float64(totalFragments)
	all := make([]Candidate, 0)
	for _, indexed := range indexedFiles {
		for _, item := range indexed.fragments {
			candidateScore := score(item, queryTerms, documentFrequency, totalFragments, averageLength)
			if candidateScore <= 0 {
				continue
			}
			all = append(all, Candidate{
				Path: item.path, Line: item.line, Column: item.column,
				Name: item.name, Qualified: item.qualified, Kind: item.kind,
				Package: item.packageName, Score: candidateScore,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return candidateRanksBefore(all[i], all[j]) })

	result, err := NewCache().Search(context.Background(), Key{Workspace: "repo"}, files, "process payment", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := all
	if len(want) > 3 {
		want = want[:3]
	}
	if !reflect.DeepEqual(result.Candidates, want) {
		t.Fatalf("streaming top-k differs from full ranking:\ngot=%#v\nwant=%#v", result.Candidates, want)
	}
	if result.Truncated != (len(all) > 3) {
		t.Fatalf("truncated = %t, want %t for %d candidates", result.Truncated, len(all) > 3, len(all))
	}
	for _, candidate := range result.Candidates {
		if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			t.Fatalf("candidate has non-finite score: %#v", candidate)
		}
	}
}

func TestSearchInvalidatesChangedDigest(t *testing.T) {
	cache := NewCache()
	key := Key{Workspace: "repo", Scope: "./...", Build: "go1.25", Provider: "gopls"}
	oldFile := File{Path: "value.go", Digest: "value-v1", Contents: []byte("package fixture\n\nfunc OldValue() {}\n")}
	old, err := cache.Search(context.Background(), key, []File{oldFile}, "old", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Candidates) != 1 || old.Candidates[0].Name != "OldValue" {
		t.Fatalf("old Search() = %#v", old.Candidates)
	}

	current, err := cache.Search(context.Background(), key, []File{{Path: "value.go", Digest: "value-v2", Contents: []byte("package fixture\n\nfunc NewValue() {}\n")}}, "old", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Candidates) != 0 {
		t.Fatalf("changed Search() returned stale candidates = %#v", current.Candidates)
	}
}
