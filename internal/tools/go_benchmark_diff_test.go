package tools

import (
	"context"
	"math"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/parser"
)

func TestBenchmarkDiffComparesCommittedFixture(t *testing.T) {
	runtime := newTestRuntime(t)

	_, output, err := runtime.benchmarkDiff(context.Background(), nil, BenchmarkDiffInput{
		Package:          ".",
		Baseline:         "HEAD",
		Count:            1,
		ThresholdPercent: float64Ptr(1000),
	})
	if err != nil {
		t.Fatalf("benchmarkDiff() error = %v", err)
	}
	if len(output.Comparisons) != 1 || output.Comparisons[0].Name != "BenchmarkTrivial" {
		t.Fatalf("comparisons = %+v, want BenchmarkTrivial", output.Comparisons)
	}
	comparison := output.Comparisons[0]
	if comparison.BaselineNsOp <= 0 || comparison.CurrentNsOp <= 0 || comparison.Regression || output.Regressions != 0 {
		t.Fatalf("comparison = %+v, regressions = %d", comparison, output.Regressions)
	}
}

func TestCompareBenchmarksReportsStructuralAndLatencyChanges(t *testing.T) {
	baseline := parser.BenchmarkReport{Benchmarks: []parser.BenchmarkResult{{Name: "BenchmarkGone", Median: 10}, {Name: "BenchmarkShared", Median: 10}}}
	current := parser.BenchmarkReport{Benchmarks: []parser.BenchmarkResult{{Name: "BenchmarkAdded", Median: 5}, {Name: "BenchmarkShared", Median: 12}}}

	output := compareBenchmarks(baseline, current, 10)
	if len(output.Comparisons) != 3 || output.Regressions != 1 {
		t.Fatalf("output = %+v", output)
	}
	if output.Comparisons[0].Name != "BenchmarkAdded" || output.Comparisons[0].Regression {
		t.Fatalf("added comparison = %+v", output.Comparisons[0])
	}
	if output.Comparisons[1].Name != "BenchmarkGone" || output.Comparisons[1].Regression {
		t.Fatalf("removed comparison = %+v", output.Comparisons[1])
	}
	if output.Comparisons[2].Name != "BenchmarkShared" || !output.Comparisons[2].Regression || output.Comparisons[2].DeltaPercent != 20 {
		t.Fatalf("shared comparison = %+v", output.Comparisons[2])
	}
}

func TestNormalizeBenchmarkDiffInput(t *testing.T) {
	input := BenchmarkDiffInput{Package: ".", Baseline: "HEAD", Count: 100}
	if err := normalizeBenchmarkDiffInput(&input); err != nil {
		t.Fatal(err)
	}
	if input.BenchRegex != "." || input.Count != 20 || input.ThresholdPercent == nil || *input.ThresholdPercent != 10 {
		t.Fatalf("normalized input = %+v", input)
	}
	for _, invalid := range []BenchmarkDiffInput{
		{Package: ".", Baseline: "-HEAD"},
		{Package: ".", Baseline: "HEAD", BenchRegex: "["},
		{Package: ".", Baseline: "HEAD", ThresholdPercent: float64Ptr(math.NaN())},
	} {
		if err := normalizeBenchmarkDiffInput(&invalid); err == nil {
			t.Fatalf("accepted invalid input: %+v", invalid)
		}
	}
	zero := BenchmarkDiffInput{Package: ".", Baseline: "HEAD", ThresholdPercent: float64Ptr(0)}
	if err := normalizeBenchmarkDiffInput(&zero); err != nil || *zero.ThresholdPercent != 0 {
		t.Fatalf("explicit zero threshold was not preserved: %+v, %v", zero, err)
	}
}
