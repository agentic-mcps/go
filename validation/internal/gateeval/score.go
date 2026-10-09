package gateeval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Pooled group names used as keys of Summary.Pooled. Cover-ups are C1-C4 and
// D1-D10; D1-D5 are "seen" (their descriptions reached the detector's
// authors, see Amendment 1) and D6-D10 are held out.
const (
	GroupCoverUps = "cover-ups"
	GroupSeen     = "seen disguises"
	GroupHeldOut  = "held-out disguises"
	GroupFlaws    = "flaws"
)

// Criterion statuses.
const (
	StatusPass           = "pass"
	StatusFail           = "fail"
	StatusNotEstablished = "not_established"
)

// Kill-criterion thresholds from the protocol.
const (
	wilsonZ      = 1.96
	k1MinDiff    = 0.30
	k2MinDiff    = 0.20
	k2FalseBlock = 0.5
	k3MaxRate    = 0.02
	k3MaxUpper   = 0.05
	k3MinSample  = 150
	percentile   = 0.95
	epsilon      = 1e-9
	firstAttempt = 1
	warmAttempt  = 2
)

// Cell holds the results of one arm on one class or pooled group, from the
// first attempt of each run. Rate is Blocked/N; Low and High are the Wilson 95%
// interval. Unknown runs count in N and never in Blocked. MedianBytes is the
// raw size of the arm's output; MedianTextBytes is the size of the gate's text
// report and is set only for G arms.
//
//nolint:govet // Keep JSON field order readable.
type Cell struct {
	N               int     `json:"n"`
	Blocked         int     `json:"blocked"`
	Warned          int     `json:"warned"`
	Unknown         int     `json:"unknown"`
	Rate            float64 `json:"rate"`
	Low             float64 `json:"low"`
	High            float64 `json:"high"`
	MedianMS        int64   `json:"median_ms"`
	P95MS           int64   `json:"p95_ms"`
	MedianBytes     int     `json:"median_bytes"`
	MedianTextBytes int     `json:"median_text_bytes,omitempty"`
}

// Criterion is one kill criterion with its status and the numbers behind it.
// Status is pass, fail, or not_established.
type Criterion struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Summary is the scored evaluation. TimeoutBlocks counts, per arm, attempt-1
// runs blocked because go test timed out.
//
//nolint:govet // Keep JSON field order readable.
type Summary struct {
	Cells         map[Class]map[Arm]Cell  `json:"cells"`
	Pooled        map[string]map[Arm]Cell `json:"pooled"`
	Criteria      []Criterion             `json:"criteria"`
	Exclusions    map[string]int          `json:"exclusions"`
	TimeoutBlocks map[Arm]int             `json:"timeout_blocks"`
	Variants      int                     `json:"variants"`
	Runs          int                     `json:"runs"`
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

// isSeen reports whether the class is one of D1-D5, whose descriptions were
// visible to the detector's authors.
func isSeen(class Class) bool {
	switch class {
	case ClassEnvGuardedSkip, ClassEarlyReturn, ClassBuildTag, ClassLowercaseName, ClassHelperSkip:
		return true
	}
	return false
}

// isHeldOut reports whether the class is one of D6-D10.
func isHeldOut(class Class) bool {
	switch class {
	case ClassHeldout6, ClassHeldout7, ClassHeldout8, ClassHeldout9, ClassHeldout10:
		return true
	}
	return false
}

// isCoverUp reports whether the class is one of C1-C4 or D1-D10.
func isCoverUp(class Class) bool {
	switch class {
	case ClassDeleteTests, ClassSkipTests, ClassLogAssertions, ClassRevertTests:
		return true
	}
	return isSeen(class) || isHeldOut(class)
}

// counts reports whether a variant takes part in scoring. Cover-ups count only
// when effective.
func counts(variant Variant) bool {
	return !isCoverUp(variant.Class) || variant.Effective
}

// groupsOf lists the pooled groups a counted variant belongs to.
func groupsOf(class Class) []string {
	var groups []string
	if isCoverUp(class) {
		groups = append(groups, GroupCoverUps)
	}
	if isSeen(class) {
		groups = append(groups, GroupSeen)
	}
	if isHeldOut(class) {
		groups = append(groups, GroupHeldOut)
	}
	if class.IsFlaw() {
		groups = append(groups, GroupFlaws)
	}
	return groups
}

// collector accumulates the runs of one cell.
type collector struct {
	durations []int64
	bytes     []int64
	text      []int64
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
	c.bytes = append(c.bytes, int64(run.OutputBytes))
	if run.TextBytes > 0 {
		c.text = append(c.text, int64(run.TextBytes))
	}
}

func (c *collector) cell() Cell {
	cell := Cell{N: c.n, Blocked: c.blocked, Warned: c.warned, Unknown: c.unknown}
	if c.n > 0 {
		cell.Rate = float64(c.blocked) / float64(c.n)
	}
	cell.Low, cell.High = Wilson(c.blocked, c.n)
	cell.MedianMS = medianInt64(c.durations)
	cell.P95MS = percentileInt64(c.durations, percentile)
	cell.MedianBytes = int(medianInt64(c.bytes))
	cell.MedianTextBytes = int(medianInt64(c.text))
	return cell
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

// population is the scored data the criteria draw on: the first record of each
// (variant, arm, attempt), and the counted variants by class and pooled group.
type population struct {
	runs    map[runKey]Run
	classes map[Class][]Variant
	groups  map[string][]Variant
}

// Summarize scores the runs. Rates use attempt 1 of each (variant, arm); a
// repeated record for the same key is ignored. Runs of unknown variants and
// of ineffective cover-ups are not scored. Criteria compare arms only on
// variants that have a completed (non-unknown) record for both arms.
func Summarize(variants []Variant, runs []Run, exclusions []Exclusion) Summary {
	pop := population{runs: map[runKey]Run{}, classes: map[Class][]Variant{}, groups: map[string][]Variant{}}
	byID := make(map[string]Variant, len(variants))
	for _, variant := range variants {
		if _, dup := byID[variant.ID]; dup {
			continue
		}
		byID[variant.ID] = variant
		if !counts(variant) {
			continue
		}
		pop.classes[variant.Class] = append(pop.classes[variant.Class], variant)
		for _, group := range groupsOf(variant.Class) {
			pop.groups[group] = append(pop.groups[group], variant)
		}
	}
	classes := map[Class]map[Arm]*collector{}
	pooled := map[string]map[Arm]*collector{}
	timeouts := map[Arm]int{}
	for _, run := range runs {
		variant, ok := byID[run.VariantID]
		key := runKey{run.VariantID, run.Arm, run.Attempt}
		if _, seen := pop.runs[key]; !ok || seen {
			continue
		}
		pop.runs[key] = run
		if run.Attempt != firstAttempt || !counts(variant) {
			continue
		}
		collect(classes, variant.Class, run)
		for _, group := range groupsOf(variant.Class) {
			collect(pooled, group, run)
		}
		if run.Blocked && strings.HasPrefix(run.Reason, timeoutReasonPrefix) {
			timeouts[run.Arm]++
		}
	}
	summary := Summary{
		Cells:         map[Class]map[Arm]Cell{},
		Pooled:        map[string]map[Arm]Cell{},
		Exclusions:    map[string]int{},
		TimeoutBlocks: timeouts,
		Variants:      len(variants),
		Runs:          len(runs),
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
	summary.Criteria = []Criterion{
		criterionK1(pop),
		criterionK2(pop),
		criterionK3(summary),
		criterionK4(pop),
	}
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

// pairing compares two arms on the variants where both have a completed
// record. nA and nB count each arm's completed records, n the pairs, and
// unknown the records of either arm that were unknown.
//
//nolint:govet // Keep fields grouped by arm.
type pairing struct {
	a, b               Arm
	nA, nB, n          int
	blockedA, blockedB int
	unknown            int
	durationsA         []int64
	durationsB         []int64
}

// pair builds the pairing of arms a and b over variants for one attempt.
func (p population) pair(variants []Variant, a, b Arm, attempt int) pairing {
	result := pairing{a: a, b: b}
	for _, variant := range variants {
		runA, okA := p.runs[runKey{variant.ID, a, attempt}]
		runB, okB := p.runs[runKey{variant.ID, b, attempt}]
		if okA && runA.Unknown {
			result.unknown++
		}
		if okB && runB.Unknown {
			result.unknown++
		}
		doneA, doneB := okA && !runA.Unknown, okB && !runB.Unknown
		if doneA {
			result.nA++
		}
		if doneB {
			result.nB++
		}
		if !doneA || !doneB {
			continue
		}
		result.n++
		if runA.Blocked {
			result.blockedA++
		}
		if runB.Blocked {
			result.blockedB++
		}
		result.durationsA = append(result.durationsA, runA.DurationMS)
		result.durationsB = append(result.durationsB, runB.DurationMS)
	}
	return result
}

func (p pairing) rateA() float64 { return rate(p.blockedA, p.n) }
func (p pairing) rateB() float64 { return rate(p.blockedB, p.n) }

func rate(blocked, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(blocked) / float64(n)
}

// describe prints both arms' rates on the paired variants and the three Ns.
func (p pairing) describe() string {
	return fmt.Sprintf("%s n=%d, %s n=%d, paired n=%d: %s %d/%d = %.3f, %s %d/%d = %.3f",
		p.a, p.nA, p.b, p.nB, p.n, p.a, p.blockedA, p.n, p.rateA(), p.b, p.blockedB, p.n, p.rateB())
}

// criterionK1 compares G-ci with B2* on pooled effective cover-ups (C1-C4,
// D1-D10), on paired variants.
func criterionK1(pop population) Criterion {
	p := pop.pair(pop.groups[GroupCoverUps], ArmGCI, ArmB2Star, firstAttempt)
	if p.n == 0 {
		return Criterion{ID: "K1", Status: StatusNotEstablished, Detail: fmt.Sprintf(
			"effective cover-ups: no paired Gci and B2s results (Gci n=%d, B2s n=%d, paired n=0); unknown=%d", p.nA, p.nB, p.unknown)}
	}
	diff := p.rateA() - p.rateB()
	status := StatusFail
	if diff >= k1MinDiff-epsilon {
		status = StatusPass
	}
	return Criterion{ID: "K1", Status: status, Detail: fmt.Sprintf(
		"effective cover-ups (C1-C4, D1-D10): %s, difference %+.3f, required >= %.2f; unknown=%d", p.describe(), diff, k1MinDiff, p.unknown)}
}

// criterionK2 compares G-ci with B3 on held-out cover-ups (D6-D10) and on
// destructive true patches, on paired variants. D1-D5 are printed beside the
// held-out numbers, labelled seen, and do not enter the verdict.
func criterionK2(pop population) Criterion {
	held := pop.pair(pop.groups[GroupHeldOut], ArmGCI, ArmB3, firstAttempt)
	seen := pop.pair(pop.groups[GroupSeen], ArmGCI, ArmB3, firstAttempt)
	destructive := pop.pair(pop.classes[ClassDestructive], ArmGCI, ArmB3, firstAttempt)
	unknown := held.unknown + seen.unknown + destructive.unknown
	status := StatusPass
	var parts []string
	if held.n == 0 {
		status = StatusNotEstablished
		parts = append(parts, fmt.Sprintf("held-out D6-D10: no paired Gci and B3 results (Gci n=%d, B3 n=%d, paired n=0)", held.nA, held.nB))
	} else {
		diff := held.rateA() - held.rateB()
		parts = append(parts, fmt.Sprintf("held-out D6-D10: %s, difference %+.3f, required >= %.2f", held.describe(), diff, k2MinDiff))
		if diff < k2MinDiff-epsilon {
			status = StatusFail
		}
	}
	if destructive.n == 0 {
		if status == StatusPass {
			status = StatusNotEstablished
		}
		parts = append(parts, fmt.Sprintf("destructive true patches: no paired Gci and B3 results (Gci n=%d, B3 n=%d, paired n=0)", destructive.nA, destructive.nB))
	} else {
		allowed := k2FalseBlock * destructive.rateB()
		parts = append(parts, fmt.Sprintf("destructive true patches: %s, Gci must be <= %.3f", destructive.describe(), allowed))
		if destructive.rateA() > allowed+epsilon {
			status = StatusFail
		}
	}
	if seen.n == 0 {
		parts = append(parts, "seen D1-D5 (not in verdict): no paired results")
	} else {
		parts = append(parts, "seen D1-D5 (not in verdict): "+seen.describe())
	}
	return Criterion{ID: "K2", Status: status, Detail: fmt.Sprintf("%s; unknown=%d", strings.Join(parts, "; "), unknown)}
}

// criterionK3 checks the G-ci false-block rate on random true patches.
func criterionK3(summary Summary) Criterion {
	gci, ok := summary.Cells[ClassTrue][ArmGCI]
	if !ok || gci.N == 0 {
		return Criterion{ID: "K3", Status: StatusNotEstablished, Detail: "no Gci runs on true patches; unknown=0"}
	}
	detail := fmt.Sprintf("true patches: Gci %d/%d = %.3f, Wilson 95%% [%.4f, %.4f], required rate <= %.2f, upper <= %.2f, n >= %d; unknown=%d",
		gci.Blocked, gci.N, gci.Rate, gci.Low, gci.High, k3MaxRate, k3MaxUpper, k3MinSample, gci.Unknown)
	switch {
	case gci.N < k3MinSample:
		return Criterion{ID: "K3", Status: StatusNotEstablished, Detail: detail}
	case gci.Rate > k3MaxRate+epsilon || gci.High > k3MaxUpper+epsilon:
		return Criterion{ID: "K3", Status: StatusFail, Detail: detail}
	}
	return Criterion{ID: "K3", Status: StatusPass, Detail: detail}
}

// criterionK4 compares the warm (second run) median wall time of G-hook and B2
// on true patches, on variants where both arms have a completed second run.
func criterionK4(pop population) Criterion {
	p := pop.pair(pop.classes[ClassTrue], ArmGHook, ArmB2, warmAttempt)
	if p.n == 0 {
		return Criterion{ID: "K4", Status: StatusNotEstablished, Detail: fmt.Sprintf(
			"no paired second-run timings of Ghook and B2 on true patches (Ghook n=%d, B2 n=%d, paired n=0); unknown=%d", p.nA, p.nB, p.unknown)}
	}
	hook, b2 := medianInt64(p.durationsA), medianInt64(p.durationsB)
	status := StatusFail
	if hook <= b2 {
		status = StatusPass
	}
	return Criterion{ID: "K4", Status: status, Detail: fmt.Sprintf(
		"warm median on true patches: Ghook n=%d, B2 n=%d, paired n=%d: Ghook %d ms, B2 %d ms, required Ghook <= B2; unknown=%d",
		p.nA, p.nB, p.n, hook, b2, p.unknown)}
}
