package retrieval

import (
	"context"
	"reflect"
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
