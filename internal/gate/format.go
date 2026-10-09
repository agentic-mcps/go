package gate

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	// formatDefaultLimit is the byte budget used when a caller passes no limit.
	formatDefaultLimit = 2048
	// formatDetailItems is how many blocking items show detail lines in WriteText.
	formatDetailItems = 2
	// formatDetailMaxLines caps the detail lines shown for one item.
	formatDetailMaxLines = 6
	// formatDetailLineBytes caps the length of one detail line.
	formatDetailLineBytes = 160
	// formatReasonDetailBytes caps the detail block inside Reason.
	formatReasonDetailBytes = 800
	// formatShortLen is how many commit characters identify the base in headers.
	formatShortLen = 7
	// formatReasonClosing is the final sentence of Reason, which must always appear.
	formatReasonClosing = "Fix these, or if a change is intentional say why in your final message; stopping again with no changes will be allowed and reported to the user."
)

// WriteText writes the compact human/agent report, never exceeding limit bytes (<=0 means 2048).
func WriteText(w io.Writer, r Result, limit int) error {
	if limit <= 0 {
		limit = formatDefaultLimit
	}
	header := formatHeader(r)
	blocks := formatBlocks(r.Items)
	total := len(header)
	for _, block := range blocks {
		total += len(block)
	}
	// Reserve room for the "+N more" trailer only when some item will be hidden.
	reserve := 0
	if total > limit && len(blocks) > 0 {
		reserve = len(formatMore(len(blocks)))
	}
	if len(header)+reserve > limit {
		return fmt.Errorf("check report limit %d is too small for its header", limit)
	}

	var b strings.Builder
	b.WriteString(header)
	shown := 0
	for _, block := range blocks {
		if b.Len()+len(block)+reserve > limit {
			break
		}
		b.WriteString(block)
		shown++
	}
	if hidden := len(blocks) - shown; hidden > 0 {
		b.WriteString(formatMore(hidden))
	}
	for _, note := range r.Notes {
		line := "note: " + formatOneLine(note) + "\n"
		if b.Len()+len(line) <= limit {
			b.WriteString(line)
		}
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write check report: %w", err)
	}
	return nil
}

// WriteJSON writes r as one line of JSON with non-nil empty Items and Notes.
func WriteJSON(w io.Writer, r Result) error {
	if r.Items == nil {
		r.Items = []Item{}
	}
	if r.Notes == nil {
		r.Notes = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("write check result json: %w", err)
	}
	return nil
}

// Reason renders the stop-hook block reason: blocking items only, bounded by limit (<=0 means 2048).
// It returns an empty string when no item blocks.
func Reason(r Result, limit int) string {
	if limit <= 0 {
		limit = formatDefaultLimit
	}
	blocking := formatBlockingItems(r.Items)
	if len(blocking) == 0 {
		return ""
	}
	header := fmt.Sprintf("agentic-go check blocked this stop: %d %s introduced by your change.\n",
		len(blocking), formatPlural(len(blocking), "problem", "problems"))

	// The header, closing sentence and trailer are fixed; the first blocking item's
	// detail is reserved next, and items are dropped from the end to make room.
	fixed := len(header) + len(formatReasonClosing) + len(formatMore(len(blocking)))
	detailAt := formatFirstDetail(blocking)
	detail := ""
	if detailAt >= 0 {
		detail = formatDetailLines(blocking[detailAt].Detail, min(formatReasonDetailBytes, limit-fixed))
	}

	entries := make([]string, len(blocking))
	total := len(header) + len(detail) + len(formatReasonClosing)
	for i, it := range blocking {
		entries[i] = formatItemLine(it, false) + formatFixLine(it)
		total += len(entries[i])
	}
	// The trailer is reserved only when items will be hidden.
	reserve := 0
	if total > limit {
		reserve = len(formatMore(len(blocking)))
	}

	var b strings.Builder
	b.WriteString(header)
	shown := 0
	for _, entry := range entries {
		if b.Len()+len(entry)+reserve+len(detail)+len(formatReasonClosing) > limit {
			break
		}
		b.WriteString(entry)
		shown++
	}
	if hidden := len(blocking) - shown; hidden > 0 {
		b.WriteString(formatMore(hidden))
	}
	// The detail is written only when its item is shown, so it never appears
	// without the item it describes.
	if detailAt >= 0 && detailAt < shown {
		b.WriteString(detail)
	}
	b.WriteString(formatReasonClosing)
	return formatTruncate(b.String(), limit)
}

// formatHeader renders the one-line report header, ending in a newline.
func formatHeader(r Result) string {
	base := fmt.Sprintf("base %s @ %s", r.Base.Ref, formatShortCommit(r.Base.Commit))
	if r.Verdict == VerdictPass && len(r.Items) == 0 {
		return fmt.Sprintf("agentic-go check: PASS (%s, %d packages tested, %s)\n",
			base, r.Stats.PackagesTested, formatDuration(r.Stats.DurationMS))
	}
	blocking, warnings := 0, 0
	for _, it := range r.Items {
		switch it.Severity {
		case SeverityBlock:
			blocking++
		case SeverityWarn:
			warnings++
		}
	}
	var parts []string
	if blocking > 0 {
		parts = append(parts, fmt.Sprintf("%d blocking", blocking))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", warnings, formatPlural(warnings, "warning", "warnings")))
	}
	verdict := strings.ToUpper(string(r.Verdict))
	if len(parts) == 0 {
		return fmt.Sprintf("agentic-go check: %s (%s)\n", verdict, base)
	}
	return fmt.Sprintf("agentic-go check: %s — %s (%s)\n", verdict, strings.Join(parts, ", "), base)
}

// formatBlocks renders one block per item: entry, fix and, for the first
// formatDetailItems blocking items with detail, the detail lines.
func formatBlocks(items []Item) []string {
	blocks := make([]string, 0, len(items))
	detailLeft := formatDetailItems
	for _, it := range items {
		detail := ""
		if detailLeft > 0 && it.Severity == SeverityBlock && strings.TrimSpace(it.Detail) != "" {
			detail = formatDetailLines(it.Detail, math.MaxInt)
			detailLeft--
		}
		blocks = append(blocks, formatItemLine(it, true)+formatFixLine(it)+detail)
	}
	return blocks
}

// formatItemLine renders the "- " entry line. tag adds the "[severity]" prefix
// used by the text report; the reason omits it because every item blocks.
func formatItemLine(it Item, tag bool) string {
	var b strings.Builder
	b.WriteString("- ")
	if tag {
		b.WriteString("[" + string(it.Severity) + "] ")
	}
	if loc := formatLocation(it); loc != "" {
		b.WriteString(loc + " — ")
	}
	b.WriteString(formatOneLine(it.Message) + "\n")
	return b.String()
}

// formatFixLine renders the indented fix line, or nothing when the item has no fix.
func formatFixLine(it Item) string {
	fix := formatOneLine(it.Fix)
	if fix == "" {
		return ""
	}
	return "  fix: " + fix + "\n"
}

// formatDetailLines renders up to formatDetailMaxLines non-blank detail lines
// with a "  | " prefix, cutting each line to formatDetailLineBytes. Lines stop
// before the output would exceed budget bytes.
func formatDetailLines(detail string, budget int) string {
	var b strings.Builder
	lines := 0
	for _, raw := range strings.Split(detail, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lines == formatDetailMaxLines {
			break
		}
		row := "  | " + formatTruncate(line, formatDetailLineBytes) + "\n"
		if b.Len()+len(row) > budget {
			break
		}
		b.WriteString(row)
		lines++
	}
	return b.String()
}

// formatBlockingItems returns the items whose severity blocks, in input order.
func formatBlockingItems(items []Item) []Item {
	var blocking []Item
	for _, it := range items {
		if it.Severity == SeverityBlock {
			blocking = append(blocking, it)
		}
	}
	return blocking
}

// formatFirstDetail returns the index of the first item with detail, or -1.
func formatFirstDetail(items []Item) int {
	for i, it := range items {
		if strings.TrimSpace(it.Detail) != "" {
			return i
		}
	}
	return -1
}

// formatMore renders the trailer that reports how many items were not shown.
func formatMore(n int) string {
	return fmt.Sprintf("+%d more (run with --format json for all)\n", n)
}

// formatLocation renders "file:line", "file" when the line is zero, or "" when there is no file.
func formatLocation(it Item) string {
	switch {
	case it.File == "":
		return ""
	case it.Line == 0:
		return it.File
	default:
		return fmt.Sprintf("%s:%d", it.File, it.Line)
	}
}

// formatShortCommit returns the first formatShortLen characters of a commit.
func formatShortCommit(commit string) string {
	if len(commit) > formatShortLen {
		return commit[:formatShortLen]
	}
	return commit
}

// formatDuration renders milliseconds as seconds with one decimal, such as "1.2s".
func formatDuration(ms int64) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// formatPlural picks the singular or plural noun for n.
func formatPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// formatOneLine collapses whitespace so a message can never break the line layout.
func formatOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// formatTruncate cuts s to at most limit bytes without splitting a UTF-8 rune.
func formatTruncate(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
