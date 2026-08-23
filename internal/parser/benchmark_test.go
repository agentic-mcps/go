package parser

import (
	"strings"
	"testing"
)

func TestParseBenchmarksAggregatesAndSortsByMedian(t *testing.T) {
	r, err := ParseBenchmarks(strings.NewReader("go test output\nBenchmarkZ-8 1 2e2 ns/op 3 B/op\nBenchmarkA-8 1 3.5 ns/op\nBenchmarkZ-8 1 100 ns/op\nBenchmarkA-8 1 4 ns/op\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Benchmarks) != 2 || r.Benchmarks[0].Name != "BenchmarkA" || r.Benchmarks[1].Name != "BenchmarkZ" {
		t.Fatalf("unexpected benchmarks: %+v", r.Benchmarks)
	}
	if r.Benchmarks[0].Median != 3.75 || r.Benchmarks[1].Median != 150 {
		t.Fatalf("unexpected medians: %+v", r.Benchmarks)
	}
	if r.Benchmarks[0].Samples == nil || r.Benchmarks[1].Samples == nil {
		t.Fatalf("samples must be non-nil: %+v", r.Benchmarks)
	}
}

func TestParseBenchmarksRejectsInvalidOrMissingMeasurements(t *testing.T) {
	for _, input := range []string{
		"",
		"ok test output only\n",
		"BenchmarkBad-1 1 0 ns/op\n",
		"BenchmarkBad-1 1 NaN ns/op\n",
		"BenchmarkBad-1 1 +Inf ns/op\n",
		"BenchmarkBad malformed\nBenchmarkGood-1 1 1 ns/op\n",
	} {
		if _, err := ParseBenchmarks(strings.NewReader(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestParseBenchmarksRejectsScannerOverflow(t *testing.T) {
	line := "BenchmarkHuge-1 1 1 ns/op " + strings.Repeat("x", maxBenchmarkLine)
	if _, err := ParseBenchmarks(strings.NewReader(line)); err == nil {
		t.Fatal("accepted overlong benchmark line")
	}
}
