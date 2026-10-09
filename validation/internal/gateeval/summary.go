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
	ClassEnvGuardedSkip: "disguised: environment-guarded skip (effective only)",
	ClassEarlyReturn:    "disguised: early return (effective only)",
	ClassBuildTag:       "disguised: build tag (effective only)",
	ClassLowercaseName:  "disguised: lowercase test name (effective only)",
	ClassHelperSkip:     "disguised: helper skip (effective only)",
	ClassStub:           "standalone: stubbed function",
	ClassConsumerBreak:  "standalone: consumer break",
	ClassDroppedErr:     "standalone: dropped error check",
}

// groupTitles describe the pooled groups.
var groupTitles = map[string]string{
	GroupCoverUps:  "pooled cover-ups (effective C1-C4 and D1-D5)",
	GroupDisguised: "pooled disguised cover-ups (effective D1-D5)",
	GroupFlaws:     "pooled flaws (all flaw classes; cover-ups effective only)",
}

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
// pooled group and class, then exclusion counts.
func renderMarkdown(summary Summary) string {
	var out strings.Builder
	out.WriteString("# Gate evaluation summary\n\n")
	fmt.Fprintf(&out, "%d variants, %d runs. Rates use attempt 1 of each run; the interval is the Wilson 95%% interval.\n\n", summary.Variants, summary.Runs)
	out.WriteString("## Kill criteria\n\n| criterion | status | detail |\n| --- | --- | --- |\n")
	for _, criterion := range summary.Criteria {
		fmt.Fprintf(&out, "| %s | %s | %s |\n", criterion.ID, criterion.Status, criterion.Detail)
	}
	out.WriteString("\n")
	for _, group := range []string{GroupCoverUps, GroupDisguised, GroupFlaws} {
		writeTable(&out, groupTitles[group], summary.Pooled[group])
	}
	for _, class := range classOrder {
		writeTable(&out, fmt.Sprintf("%s: %s", class, classTitles[class]), summary.Cells[class])
	}
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

// writeTable writes one table with a row per arm that has runs. Nothing is
// written for an empty cell set.
func writeTable(out *strings.Builder, title string, cells map[Arm]Cell) {
	if len(cells) == 0 {
		return
	}
	fmt.Fprintf(out, "## %s\n\n", title)
	out.WriteString("| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median bytes |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, arm := range armOrder {
		cell, ok := cells[arm]
		if !ok {
			continue
		}
		fmt.Fprintf(out, "| %s | %d | %d | %.3f | [%.3f, %.3f] | %d | %d | %d | %d | %d |\n",
			arm, cell.N, cell.Blocked, cell.Rate, cell.Low, cell.High, cell.Warned, cell.Unknown, cell.MedianMS, cell.P95MS, cell.MedianBytes)
	}
	out.WriteString("\n")
}
