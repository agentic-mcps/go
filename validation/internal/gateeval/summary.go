package gateeval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// classOrder fixes the order of class tables.
var classOrder = []Class{
	ClassTrue, ClassDestructive, ClassMutant,
	ClassDeleteTests, ClassSkipTests, ClassLogAssertions, ClassRevertTests,
	ClassEnvGuardedSkip, ClassEarlyReturn, ClassBuildTag, ClassLowercaseName, ClassHelperSkip,
	ClassHeldout6, ClassHeldout7, ClassHeldout8, ClassHeldout9, ClassHeldout10,
	ClassStub, ClassConsumerBreak, ClassDroppedErr,
}

// armOrder fixes the order of table rows.
var armOrder = []Arm{ArmB0, ArmB1, ArmB2, ArmB2Star, ArmB3, ArmGCI, ArmGHook}

// classTitles describe each class in the tables.
var classTitles = map[Class]string{
	ClassTrue:           "true patches (blocked means false block)",
	ClassDestructive:    "destructive true patches (blocked means false block)",
	ClassMutant:         "real-bug mutants",
	ClassDeleteTests:    "cover-up: delete oracle tests (effective only)",
	ClassSkipTests:      "cover-up: skip oracle tests (effective only)",
	ClassLogAssertions:  "cover-up: log instead of assert (effective only)",
	ClassRevertTests:    "cover-up: revert test files (effective only)",
	ClassEnvGuardedSkip: "seen disguise: environment-guarded skip (effective only)",
	ClassEarlyReturn:    "seen disguise: early return (effective only)",
	ClassBuildTag:       "seen disguise: build tag (effective only)",
	ClassLowercaseName:  "seen disguise: lowercase test name (effective only)",
	ClassHelperSkip:     "seen disguise: helper skip (effective only)",
	ClassHeldout6:       "held-out disguise 6 (effective only)",
	ClassHeldout7:       "held-out disguise 7 (effective only)",
	ClassHeldout8:       "held-out disguise 8 (effective only)",
	ClassHeldout9:       "held-out disguise 9 (effective only)",
	ClassHeldout10:      "held-out disguise 10 (effective only)",
	ClassStub:           "standalone: stubbed function",
	ClassConsumerBreak:  "standalone: consumer break",
	ClassDroppedErr:     "standalone: dropped error check",
}

// groupTitles describe the pooled groups.
var groupTitles = map[string]string{
	GroupCoverUps: "pooled cover-ups (C1–C4, D1–D10), effective only",
	GroupSeen:     "pooled seen disguises (D1–D5), effective only",
	GroupHeldOut:  "pooled held-out disguises (D6–D10), effective only",
	GroupFlaws:    "pooled flaws (all flaw classes; cover-ups effective only)",
}

// groupOrder fixes the order of pooled tables.
var groupOrder = []string{GroupCoverUps, GroupSeen, GroupHeldOut, GroupFlaws}

// bytesHeader labels the output-size column, whose meaning differs by arm.
const bytesHeader = "median raw output bytes (B: go test -json + tools; G: --format json)"

// WriteSummary writes summary.json and summary.md into dir.
func WriteSummary(dir string, summary Summary) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding summary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing summary.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(renderMarkdown(summary)), 0o600); err != nil {
		return fmt.Errorf("writing summary.md: %w", err)
	}
	return nil
}

// renderMarkdown formats the summary: kill criteria, then one table per
// pooled group and class, then timeout blocks and exclusion counts.
func renderMarkdown(summary Summary) string {
	var out strings.Builder
	out.WriteString("# Gate evaluation summary\n\n")
	fmt.Fprintf(&out, "%d variants, %d runs. Rates use attempt 1 of each run; the interval is the Wilson 95%% interval. "+
		"Kill-criterion comparisons use only variants with a completed result for both arms. "+
		"Gate arms ran with --no-cache, so warm timings reflect OS and Go build caches only.\n\n", summary.Variants, summary.Runs)
	out.WriteString("## Kill criteria\n\n| criterion | status | detail |\n| --- | --- | --- |\n")
	for _, criterion := range summary.Criteria {
		fmt.Fprintf(&out, "| %s | %s | %s |\n", criterion.ID, criterion.Status, criterion.Detail)
	}
	out.WriteString("\n")
	for _, group := range groupOrder {
		writeTable(&out, groupTitles[group], summary.Pooled[group])
	}
	for _, class := range classOrder {
		writeTable(&out, fmt.Sprintf("%s: %s", class, classTitles[class]), summary.Cells[class])
	}
	writeTimeouts(&out, summary.TimeoutBlocks)
	out.WriteString("## Exclusions\n\n")
	if len(summary.Exclusions) == 0 {
		out.WriteString("None.\n")
		return out.String()
	}
	out.WriteString("| reason | count |\n| --- | --- |\n")
	reasons := make([]string, 0, len(summary.Exclusions))
	for reason := range summary.Exclusions {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		fmt.Fprintf(&out, "| %s | %d |\n", reason, summary.Exclusions[reason])
	}
	return out.String()
}

// writeTimeouts lists the go test timeouts that the B arms counted as blocks.
func writeTimeouts(out *strings.Builder, timeouts map[Arm]int) {
	out.WriteString("## go test timeouts counted as blocks\n\n")
	listed := false
	for _, arm := range armOrder {
		if count := timeouts[arm]; count > 0 {
			if !listed {
				out.WriteString("| arm | blocks from a go test timeout |\n| --- | --- |\n")
				listed = true
			}
			fmt.Fprintf(out, "| %s | %d |\n", arm, count)
		}
	}
	if !listed {
		out.WriteString("None.\n")
	}
	out.WriteString("\n")
}

// writeTable writes one table with a row per arm that has runs. Nothing is
// written for an empty cell set. The text-bytes column applies to G arms only.
func writeTable(out *strings.Builder, title string, cells map[Arm]Cell) {
	if len(cells) == 0 {
		return
	}
	fmt.Fprintf(out, "## %s\n\n", title)
	fmt.Fprintf(out, "| arm | n | blocked | rate | 95%% CI | warned | unknown | median ms | p95 ms | %s | median text bytes (G: gate text report, 2048 budget) |\n", bytesHeader)
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, arm := range armOrder {
		cell, ok := cells[arm]
		if !ok {
			continue
		}
		text := "-"
		if cell.MedianTextBytes > 0 {
			text = fmt.Sprint(cell.MedianTextBytes)
		}
		fmt.Fprintf(out, "| %s | %d | %d | %.3f | [%.3f, %.3f] | %d | %d | %d | %d | %d | %s |\n",
			arm, cell.N, cell.Blocked, cell.Rate, cell.Low, cell.High, cell.Warned, cell.Unknown, cell.MedianMS, cell.P95MS, cell.MedianBytes, text)
	}
	out.WriteString("\n")
}
