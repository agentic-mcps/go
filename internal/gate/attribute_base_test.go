package gate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBasePruneKeepsNewestTrees(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mkTree := func(name string, age time.Duration) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return path
	}
	keep := mkTree("current", 3*time.Hour)
	mkTree("newer", time.Minute)
	mkTree("older", 2*time.Hour)
	mkTree(baseTempPrefix+"stale", 2*time.Hour)
	mkTree(baseTempPrefix+"active", time.Minute)
	basePrune(dir, keep)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := []string{baseTempPrefix + "active", "current", "newer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining = %v, want %v", got, want)
	}
}

func TestBaseReuseRequiresCompletionMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tree", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := baseReuse(dir); ok {
		t.Fatal("reused a tree without a completion marker")
	}
	if err := os.WriteFile(filepath.Join(dir, baseCompleteMarker), []byte("tree/sub"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, ok := baseReuse(dir)
	if !ok || root != filepath.Join(dir, "tree", "sub") {
		t.Fatalf("baseReuse = %q, %v", root, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, baseCompleteMarker), []byte("../escape"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := baseReuse(dir); ok {
		t.Fatal("reused a marker pointing outside the tree")
	}
}
