package gateeval

import (
	"fmt"
	"math"
	"sort"
)

// Pooled group names used as keys of Summary.Pooled.
const (
	GroupCoverUps  = "cover-ups"
	GroupDisguised = "disguised"
	GroupFlaws     = "flaws"
)

// Criterion statuses.
const (
	StatusPass           = "pass"
	StatusFail           = "fail"
	StatusNotEstablished = "not_established"
)

// Kill-criterion thresholds from the protocol.
const (
	wilsonZ        = 1.96
	k1MinDiff      = 0.30
	k2MinDiff      = 0.20
	k2FalseBlock   = 0.5
	k3MaxRate      = 0.02
	k3MaxUpper     = 0.05
	k3MinSample    = 150
	percentile     = 0.95
	attemptRates   = 1
	attemptWarm    = 2
	pooledGroupCnt = 3
	epsilon        = 1e-9
)

// Cell holds the results of one arm on one class or pooled group, from the
// first attempt of each run. Rate is Blocked/N; Low and High are the Wilson 95%
// interval. Unknown runs count in N and never in Blocked.
//
//nolint:govet // Keep JSON field order readable.
type Cell struct {
	N           int     `json:"n"`
	Blocked     int     `json:"blocked"`
	Warned      int     `json:"warned"`
	Unknown     int     `json:"unknown"`
	Rate        float64 `json:"rate"`
	Low         float64 `json:"low"`
	High        float64 `json:"high"`
	MedianMS    int64   `json:"median_ms"`
	P95MS       int64   `json:"p95_ms"`
	MedianBytes int     `json:"median_bytes"`
}

// Criterion is one kill criterion with its status and the numbers behind it.
// Status is pass, fail, or not_established.
type Criterion struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Summary is the scored evaluation.
//
//nolint:govet // Keep JSON field order readable.
type Summary struct {
	Cells      map[Class]map[Arm]Cell  `json:"cells"`
	Pooled     map[string]map[Arm]Cell `json:"pooled"`
	Criteria   []Criterion             `json:"criteria"`
	Exclusions map[string]int          `json:"exclusions"`
	Variants   int                     `json:"variants"`
	Runs       int                     `json:"runs"`
}

// Wilson returns the 95% Wilson score interval for successes out of n. With no
// observations it returns the uninformative interval (0, 1).
func Wilson(successes, n int) (low, high float64) {
	if n <= 0 {
		return 0, 1
	}
	total := float64(n)
	p := float64(successes) / total
	z2 := wilsonZ * wilsonZ
	denominator := 1 + z2/total
	center := (p + z2/(2*total)) / denominator
	half := wilsonZ * math.Sqrt(p*(1-p)/total+z2/(4*total*total)) / denominator
	return math.Max(0, center-half), math.Min(1, center+half)
}

// isCoverUp reports whether the class is one of C1-C4 or D1-D5.
func isCoverUp(class Class) bool {
	switch class {
	case ClassDeleteTests, ClassSkipTests, ClassLogAssertions, ClassRevertTests,
		ClassEnvGuardedSkip, ClassEarlyReturn, ClassBuildTag, ClassLowercaseName, ClassHelperSkip:
		return true
	}
	return false
}

// isDisguised reports whether the class is one of the held-out D1-D5.
func isDisguised(class Class) bool {
	switch class {
	case ClassEnvGuardedSkip, ClassEarlyReturn, ClassBuildTag, ClassLowercaseName, ClassHelperSkip:
		return true
	}
	return false
}

// counts reports whether a variant takes part in scoring. Cover-ups count only
// when effective.
func counts(variant Variant) bool {
	return !isCoverUp(variant.Class) || variant.Effective
}

// groupsOf lists the pooled groups a counted variant belongs to.
func groupsOf(class Class) []string {
	groups := make([]string, 0, pooledGroupCnt)
	if isCoverUp(class) {
		groups = append(groups, GroupCoverUps)
	}
	if isDisguised(class) {
		groups = append(groups, GroupDisguised)
	}
	if class.IsFlaw() {
		groups = append(groups, GroupFlaws)
	}
	return groups
}

// collector accumulates the runs of one cell.
type collector struct {
	durations []int64
	bytes     []int
	n         int
	blocked   int
	warned    int
	unknown   int
}

func (c *collector) add(run Run) {
	c.n++
	if run.Blocked {
		c.blocked++
	}
	if run.Warned {
		c.warned++
	}
	if run.Unknown {
		c.unknown++
	}
	c.durations = append(c.durations, run.DurationMS)
	c.bytes = append(c.bytes, run.OutputBytes)
}

func (c *collector) cell() Cell {
	cell := Cell{N: c.n, Blocked: c.blocked, Warned: c.warned, Unknown: c.unknown}
	if c.n > 0 {
		cell.Rate = float64(c.blocked) / float64(c.n)
	}
	cell.Low, cell.High = Wilson(c.blocked, c.n)
	cell.MedianMS = medianInt64(c.durations)
	cell.P95MS = percentileInt64(c.durations, percentile)
	cell.MedianBytes = int(medianInt64(toInt64(c.bytes)))
	return cell
}

func toInt64(values []int) []int64 {
	converted := make([]int64, len(values))
	for i, value := range values {
		converted[i] = int64(value)
	}
	return converted
}

// medianInt64 returns the median; for an even count, the mean of the two
// middle values rounded down. An empty input gives 0.
func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// percentileInt64 returns the nearest-rank percentile. An empty input gives 0.
func percentileInt64(values []int64, fraction float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(fraction*float64(len(sorted)))) - 1
	return sorted[max(rank, 0)]
}

// Summarize scores the runs. Rates use attempt 1 of each (variant, arm); a
// repeated record for the same key is ignored. Runs of unknown variants and
// of ineffective cover-ups are not scored.
func Summarize(variants []Variant, runs []Run, exclusions []Exclusion) Summary {
	byID := make(map[string]Variant, len(variants))
	for _, variant := range variants {
		byID[variant.ID] = variant
	}
	classes := map[Class]map[Arm]*collector{}
	pooled := map[string]map[Arm]*collector{}
	warm := map[Arm][]int64{}
	seen := map[runKey]bool{}
	for _, run := range runs {
		variant, ok := byID[run.VariantID]
		key := runKey{run.VariantID, run.Arm, run.Attempt}
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		switch {
		case run.Attempt == attemptWarm && variant.Class == ClassTrue:
			warm[run.Arm] = append(warm[run.Arm], run.DurationMS)
		case run.Attempt == attemptRates && counts(variant):
			collect(classes, variant.Class, run)
			for _, group := range groupsOf(variant.Class) {
				collect(pooled, group, run)
			}
		}
	}
	summary := Summary{
		Cells:      map[Class]map[Arm]Cell{},
		Pooled:     map[string]map[Arm]Cell{},
		Exclusions: map[string]int{},
		Variants:   len(variants),
		Runs:       len(runs),
	}
	for class, arms := range classes {
		summary.Cells[class] = finish(arms)
	}
	for group, arms := range pooled {
		summary.Pooled[group] = finish(arms)
	}
	for _, exclusion := range exclusions {
		summary.Exclusions[exclusion.Reason]++
	}
	summary.Criteria = criteria(summary, warm)
	return summary
}

func collect[K comparable](into map[K]map[Arm]*collector, key K, run Run) {
	arms, ok := into[key]
	if !ok {
		arms = map[Arm]*collector{}
		into[key] = arms
	}
	c, ok := arms[run.Arm]
	if !ok {
		c = &collector{}
		arms[run.Arm] = c
	}
	c.add(run)
}

func finish(arms map[Arm]*collector) map[Arm]Cell {
	cells := make(map[Arm]Cell, len(arms))
	for arm, c := range arms {
		cells[arm] = c.cell()
	}
	return cells
}

// criteria evaluates K1-K4. warm holds the attempt-2 durations on true patches.
func criteria(summary Summary, warm map[Arm][]int64) []Criterion {
	return []Criterion{
		criterionK1(summary),
		criterionK2(summary),
		criterionK3(summary),
		criterionK4(warm),
	}
}

// cell returns a cell with at least one run.
func lookup(cells map[Arm]Cell, arm Arm) (Cell, bool) {
	cell, ok := cells[arm]
	return cell, ok && cell.N > 0
}

func describe(arm Arm, cell Cell) string {
	return fmt.Sprintf("%s %d/%d = %.3f", arm, cell.Blocked, cell.N, cell.Rate)
}

// criterionK1 compares G-ci with B2* on pooled effective cover-ups.
func criterionK1(summary Summary) Criterion {
	gci, okG := lookup(summary.Pooled[GroupCoverUps], ArmGCI)
	b2s, okB := lookup(summary.Pooled[GroupCoverUps], ArmB2Star)
	if !okG || !okB {
		return Criterion{ID: "K1", Status: StatusNotEstablished, Detail: "no runs of Gci or B2s on effective cover-ups"}
	}
	diff := gci.Rate - b2s.Rate
	status := StatusFail
	if diff >= k1MinDiff-epsilon {
		status = StatusPass
	}
	return Criterion{ID: "K1", Status: status, Detail: fmt.Sprintf(
		"effective cover-ups: %s, %s, difference %+.3f, required >= %.2f", describe(ArmGCI, gci), describe(ArmB2Star, b2s), diff, k1MinDiff)}
}

// criterionK2 compares G-ci with B3 on held-out disguised cover-ups and on
// destructive true patches.
func criterionK2(summary Summary) Criterion {
	gci, okG := lookup(summary.Pooled[GroupDisguised], ArmGCI)
	b3, okB := lookup(summary.Pooled[GroupDisguised], ArmB3)
	gciDT, okGDT := lookup(summary.Cells[ClassDestructive], ArmGCI)
	b3DT, okBDT := lookup(summary.Cells[ClassDestructive], ArmB3)
	status := StatusPass
	detail := ""
	if okG && okB {
		diff := gci.Rate - b3.Rate
		detail = fmt.Sprintf("disguised: %s, %s, difference %+.3f, required >= %.2f", describe(ArmGCI, gci), describe(ArmB3, b3), diff, k2MinDiff)
		if diff < k2MinDiff-epsilon {
			status = StatusFail
		}
	} else {
		detail = "disguised: no runs of Gci or B3"
		status = StatusNotEstablished
	}
	if okGDT && okBDT {
		allowed := k2FalseBlock * b3DT.Rate
		detail += fmt.Sprintf("; destructive true patches: %s, %s, Gci must be <= %.3f", describe(ArmGCI, gciDT), describe(ArmB3, b3DT), allowed)
		if gciDT.Rate > allowed+epsilon {
			status = StatusFail
		}
	} else {
		detail += "; destructive true patches: no runs of Gci or B3"
		if status == StatusPass {
			status = StatusNotEstablished
		}
	}
	return Criterion{ID: "K2", Status: status, Detail: detail}
}

// criterionK3 checks the G-ci false-block rate on random true patches.
func criterionK3(summary Summary) Criterion {
	gci, ok := lookup(summary.Cells[ClassTrue], ArmGCI)
	if !ok {
		return Criterion{ID: "K3", Status: StatusNotEstablished, Detail: "no Gci runs on true patches"}
	}
	detail := fmt.Sprintf("true patches: %s, Wilson 95%% [%.4f, %.4f], required rate <= %.2f, upper <= %.2f, n >= %d",
		describe(ArmGCI, gci), gci.Low, gci.High, k3MaxRate, k3MaxUpper, k3MinSample)
	switch {
	case gci.N < k3MinSample:
		return Criterion{ID: "K3", Status: StatusNotEstablished, Detail: detail}
	case gci.Rate > k3MaxRate+epsilon || gci.High > k3MaxUpper+epsilon:
		return Criterion{ID: "K3", Status: StatusFail, Detail: detail}
	}
	return Criterion{ID: "K3", Status: StatusPass, Detail: detail}
}

// criterionK4 compares the warm (second run) median wall time of G-hook and B2
// on true patches.
func criterionK4(warm map[Arm][]int64) Criterion {
	hook, b2 := warm[ArmGHook], warm[ArmB2]
	if len(hook) == 0 || len(b2) == 0 {
		return Criterion{ID: "K4", Status: StatusNotEstablished, Detail: "no second-run timings of Ghook and B2 on true patches"}
	}
	hookMedian, b2Median := medianInt64(hook), medianInt64(b2)
	status := StatusFail
	if hookMedian <= b2Median {
		status = StatusPass
	}
	return Criterion{ID: "K4", Status: status, Detail: fmt.Sprintf(
		"warm median on true patches: Ghook %d ms (n=%d), B2 %d ms (n=%d), required Ghook <= B2",
		hookMedian, len(hook), b2Median, len(b2))}
}
