package gateeval

import (
	"fmt"
	"math"
	"testing"
)

func near(a, b, tolerance float64) bool { return math.Abs(a-b) <= tolerance }

func TestWilson(t *testing.T) {
	tests := []struct {
		name         string
		successes, n int
		wantLow      float64
		wantHigh     float64
	}{
		{"none observed", 0, 0, 0, 1},
		{"zero of 150", 0, 150, 0, 0.0249},
		{"five of ten", 5, 10, 0.2366, 0.7634},
		{"all of ten", 10, 10, 0.7225, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			low, high := Wilson(tt.successes, tt.n)
			if !near(low, tt.wantLow, 0.0005) || !near(high, tt.wantHigh, 0.0005) {
				t.Fatalf("Wilson(%d, %d) = [%.4f, %.4f], want [%.4f, %.4f]", tt.successes, tt.n, low, high, tt.wantLow, tt.wantHigh)
			}
		})
	}
}

func TestMedianAndPercentile(t *testing.T) {
	if got := medianInt64([]int64{5, 1, 3}); got != 3 {
		t.Fatalf("odd median = %d", got)
	}
	if got := medianInt64([]int64{4, 1, 3, 2}); got != 2 {
		t.Fatalf("even median = %d, want 2 (mean of 2 and 3 rounded down)", got)
	}
	if got := medianInt64(nil); got != 0 {
		t.Fatalf("empty median = %d", got)
	}
	values := make([]int64, 100)
	for i := range values {
		values[i] = int64(i + 1)
	}
	if got := percentileInt64(values, 0.95); got != 95 {
		t.Fatalf("p95 of 1..100 = %d, want 95", got)
	}
	if got := percentileInt64([]int64{7}, 0.95); got != 7 {
		t.Fatalf("p95 of one value = %d", got)
	}
}

// runsFor builds attempt-1 runs for one arm over variants named prefix-i, the
// first blocked of them blocking.
func runsFor(arm Arm, prefix string, n, blocked int) []Run {
	runs := make([]Run, 0, n)
	for i := range n {
		runs = append(runs, Run{VariantID: fmt.Sprintf("%s-%d", prefix, i), Arm: arm, Attempt: 1, Blocked: i < blocked, DurationMS: 100, OutputBytes: 10})
	}
	return runs
}

func variantsFor(class Class, prefix string, n int, effective bool) []Variant {
	variants := make([]Variant, 0, n)
	for i := range n {
		variants = append(variants, Variant{ID: fmt.Sprintf("%s-%d", prefix, i), Class: class, Effective: effective})
	}
	return variants
}

func criterionByID(t *testing.T, summary Summary, id string) Criterion {
	t.Helper()
	for _, criterion := range summary.Criteria {
		if criterion.ID == id {
			return criterion
		}
	}
	t.Fatalf("criterion %s missing", id)
	return Criterion{}
}

func TestSummarizeIgnoresIneffectiveCoverUps(t *testing.T) {
	variants := append(variantsFor(ClassSkipTests, "eff", 4, true), variantsFor(ClassSkipTests, "dead", 6, false)...)
	variants = append(variants, variantsFor(ClassStub, "stub", 3, false)...)
	var runs []Run
	runs = append(runs, runsFor(ArmGCI, "eff", 4, 4)...)
	runs = append(runs, runsFor(ArmGCI, "dead", 6, 0)...)
	runs = append(runs, runsFor(ArmGCI, "stub", 3, 3)...)
	summary := Summarize(variants, runs, []Exclusion{{Reason: "flaky"}, {Reason: "flaky"}, {Reason: "no mutant"}})

	if cell := summary.Cells[ClassSkipTests][ArmGCI]; cell.N != 4 || cell.Blocked != 4 || cell.Rate != 1 {
		t.Fatalf("C2 cell = %+v, want 4 effective runs all blocked", cell)
	}
	if cell := summary.Pooled[GroupCoverUps][ArmGCI]; cell.N != 4 {
		t.Fatalf("cover-ups pooled n = %d, want 4", cell.N)
	}
	if cell := summary.Pooled[GroupFlaws][ArmGCI]; cell.N != 7 {
		t.Fatalf("flaws pooled n = %d, want 4 effective cover-ups + 3 stubs", cell.N)
	}
	if _, ok := summary.Pooled[GroupDisguised]; ok {
		t.Fatal("disguised group present without D variants")
	}
	if summary.Exclusions["flaky"] != 2 || summary.Exclusions["no mutant"] != 1 {
		t.Fatalf("exclusions = %v", summary.Exclusions)
	}
	if summary.Variants != len(variants) || summary.Runs != len(runs) {
		t.Fatalf("totals = %d variants, %d runs", summary.Variants, summary.Runs)
	}
}

func TestSummarizeUsesFirstAttemptAndCountsUnknownWarned(t *testing.T) {
	variants := variantsFor(ClassTrue, "t", 4, false)
	runs := []Run{
		{VariantID: "t-0", Arm: ArmGCI, Attempt: 1, Blocked: true, DurationMS: 10},
		{VariantID: "t-1", Arm: ArmGCI, Attempt: 1, Warned: true, DurationMS: 20},
		{VariantID: "t-2", Arm: ArmGCI, Attempt: 1, Unknown: true, DurationMS: 30},
		{VariantID: "t-3", Arm: ArmGCI, Attempt: 1, DurationMS: 40},
		{VariantID: "t-0", Arm: ArmGCI, Attempt: 2, DurationMS: 5000},
		{VariantID: "nobody", Arm: ArmGCI, Attempt: 1, Blocked: true},
		{VariantID: "t-0", Arm: ArmGCI, Attempt: 1, Blocked: false},
	}
	cell := Summarize(variants, runs, nil).Cells[ClassTrue][ArmGCI]
	if cell.N != 4 || cell.Blocked != 1 || cell.Warned != 1 || cell.Unknown != 1 {
		t.Fatalf("cell = %+v, want n=4 blocked=1 warned=1 unknown=1", cell)
	}
	if cell.MedianMS != 25 || cell.P95MS != 40 {
		t.Fatalf("timing = median %d p95 %d, want 25 and 40", cell.MedianMS, cell.P95MS)
	}
}

func TestCriterionK1(t *testing.T) {
	tests := []struct {
		name       string
		want       string
		n          int
		gciBlocked int
		b2sBlocked int
		withB2s    bool
	}{
		{"exactly thirty points passes", StatusPass, 10, 7, 4, true},
		{"twenty nine points fails", StatusFail, 100, 69, 40, true},
		{"gate below baseline fails", StatusFail, 10, 2, 8, true},
		{"baseline arm missing", StatusNotEstablished, 10, 10, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variants := variantsFor(ClassSkipTests, "c", tt.n, true)
			runs := runsFor(ArmGCI, "c", tt.n, tt.gciBlocked)
			if tt.withB2s {
				runs = append(runs, runsFor(ArmB2Star, "c", tt.n, tt.b2sBlocked)...)
			}
			got := criterionByID(t, Summarize(variants, runs, nil), "K1")
			if got.Status != tt.want {
				t.Fatalf("K1 = %s (%s), want %s", got.Status, got.Detail, tt.want)
			}
		})
	}
}

func TestCriterionK2(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		gciD   int
		b3D    int
		gciDT  int
		b3DT   int
		withDT bool
		withB3 bool
	}{
		{"both parts hold", StatusPass, 10, 7, 1, 4, true, true},
		{"disguised margin too small", StatusFail, 10, 9, 0, 4, true, true},
		{"gate false blocks more than half of baseline", StatusFail, 10, 0, 3, 4, true, true},
		{"baseline never false blocks and gate does", StatusFail, 10, 0, 1, 0, true, true},
		{"neither false blocks", StatusPass, 10, 0, 0, 0, true, true},
		{"exactly half passes", StatusPass, 10, 0, 2, 4, true, true},
		{"destructive data missing", StatusNotEstablished, 10, 0, 0, 0, false, true},
		{"destructive missing but disguised fails", StatusFail, 10, 9, 0, 0, false, true},
		{"baseline missing", StatusNotEstablished, 10, 0, 0, 0, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variants := variantsFor(ClassBuildTag, "d", 10, true)
			runs := runsFor(ArmGCI, "d", 10, tt.gciD)
			if tt.withB3 {
				runs = append(runs, runsFor(ArmB3, "d", 10, tt.b3D)...)
			}
			if tt.withDT {
				variants = append(variants, variantsFor(ClassDestructive, "dt", 4, false)...)
				runs = append(runs, runsFor(ArmGCI, "dt", 4, tt.gciDT)...)
				if tt.withB3 {
					runs = append(runs, runsFor(ArmB3, "dt", 4, tt.b3DT)...)
				}
			}
			got := criterionByID(t, Summarize(variants, runs, nil), "K2")
			if got.Status != tt.want {
				t.Fatalf("K2 = %s (%s), want %s", got.Status, got.Detail, tt.want)
			}
		})
	}
}

func TestCriterionK3(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		n       int
		blocked int
	}{
		{"clean at 150", StatusPass, 150, 0},
		{"one block in 300 passes", StatusPass, 300, 1},
		{"rate above two percent fails", StatusFail, 150, 4},
		{"rate within two percent but upper bound above five fails", StatusFail, 150, 3},
		{"too few patches", StatusNotEstablished, 149, 0},
		{"too few patches even with blocks", StatusNotEstablished, 100, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variants := variantsFor(ClassTrue, "t", tt.n, false)
			runs := runsFor(ArmGCI, "t", tt.n, tt.blocked)
			got := criterionByID(t, Summarize(variants, runs, nil), "K3")
			if got.Status != tt.want {
				t.Fatalf("K3 = %s (%s), want %s", got.Status, got.Detail, tt.want)
			}
		})
	}
	if got := criterionByID(t, Summarize(nil, nil, nil), "K3"); got.Status != StatusNotEstablished {
		t.Fatalf("K3 without runs = %s", got.Status)
	}
}

func TestCriterionK4(t *testing.T) {
	variants := variantsFor(ClassTrue, "t", 3, false)
	warm := func(arm Arm, durations ...int64) []Run {
		runs := make([]Run, 0, len(durations))
		for i, duration := range durations {
			runs = append(runs, Run{VariantID: fmt.Sprintf("t-%d", i), Arm: arm, Attempt: 2, DurationMS: duration})
		}
		return runs
	}
	tests := []struct {
		name string
		want string
		runs []Run
	}{
		{"hook faster", StatusPass, append(warm(ArmGHook, 100, 200, 300), warm(ArmB2, 1000, 2000, 3000)...)},
		{"equal medians pass", StatusPass, append(warm(ArmGHook, 1, 500, 900), warm(ArmB2, 500, 600, 700)...)},
		{"hook slower", StatusFail, append(warm(ArmGHook, 5000, 6000, 7000), warm(ArmB2, 1000, 2000, 3000)...)},
		{"no warm data", StatusNotEstablished, runsFor(ArmGHook, "t", 3, 0)},
		{"hook only", StatusNotEstablished, warm(ArmGHook, 1, 2, 3)},
		{"first attempts ignored", StatusNotEstablished, append(runsFor(ArmGHook, "t", 3, 0), runsFor(ArmB2, "t", 3, 0)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := criterionByID(t, Summarize(variants, tt.runs, nil), "K4")
			if got.Status != tt.want {
				t.Fatalf("K4 = %s (%s), want %s", got.Status, got.Detail, tt.want)
			}
		})
	}
}

func TestWarmRunsExcludeOtherClasses(t *testing.T) {
	variants := append(variantsFor(ClassTrue, "t", 1, false), variantsFor(ClassMutant, "m", 1, false)...)
	runs := []Run{
		{VariantID: "t-0", Arm: ArmGHook, Attempt: 2, DurationMS: 10},
		{VariantID: "t-0", Arm: ArmB2, Attempt: 2, DurationMS: 20},
		{VariantID: "m-0", Arm: ArmGHook, Attempt: 2, DurationMS: 9999},
	}
	got := criterionByID(t, Summarize(variants, runs, nil), "K4")
	if got.Status != StatusPass {
		t.Fatalf("K4 = %s (%s), want pass using only true patches", got.Status, got.Detail)
	}
}
