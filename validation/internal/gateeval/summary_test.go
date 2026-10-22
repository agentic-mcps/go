package gateeval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSummary(t *testing.T) {
	variants := append(variantsFor(ClassTrue, "t", 2, false), variantsFor(ClassEarlyReturn, "d", 2, true)...)
	runs := append(runsFor(ArmGCI, "t", 2, 1), runsFor(ArmGCI, "d", 2, 2)...)
	runs = append(runs, runsFor(ArmB2Star, "d", 2, 0)...)
	summary := Summarize(variants, runs, []Exclusion{{Project: "gin", Commit: "abc", Reason: "flake filter"}})

	dir := filepath.Join(t.TempDir(), "out")
	if err := WriteSummary(dir, summary); err != nil {
		t.Fatalf("WriteSummary() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Summary
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("summary.json does not decode: %v", err)
	}
	if decoded.Cells[ClassTrue][ArmGCI].Blocked != 1 || decoded.Variants != 4 || decoded.Runs != 6 {
		t.Fatalf("decoded summary = %+v", decoded)
	}
	markdown, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(markdown)
	criteria := strings.Index(text, "## Kill criteria")
	firstTable := strings.Index(text, "| arm |")
	if criteria < 0 || firstTable < 0 || criteria > firstTable {
		t.Fatalf("kill criteria table must come first:\n%s", text)
	}
	for _, want := range []string{
		"| K1 |", "| K4 | not_established |", "pooled cover-ups (C1–C4, D1–D10)", "pooled seen disguises (D1–D5)", "D2: seen disguise: early return",
		"raw output bytes (B: go test -json + tools; G: --format json)", "--no-cache", "go test timeouts counted as blocks",
		"T: true patches", "| Gci | 2 | 1 | 0.500 |", "| flake filter | 1 |", "95% CI",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("summary.md missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "C1:") {
		t.Errorf("summary.md has a table for a class without runs:\n%s", text)
	}
}
