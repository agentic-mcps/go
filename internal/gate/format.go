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
	// formatTextMaxBytes caps one message, fix or note so one verbose item cannot
	// crowd out the rest of the report.
	formatTextMaxBytes = 240
	// formatDetailMaxLines caps the detail lines WriteText shows for one item.
	formatDetailMaxLines = 4
	// formatDetailLineBytes caps the length of one detail line.
	formatDetailLineBytes = 120
	// formatReasonDetailBytes caps the detail block inside Reason.
	formatReasonDetailBytes = 800
	// formatShortLen is how many commit characters identify the base in headers.
	formatShortLen = 7
	// formatReasonClosing is the final sentence of Reason, which must always appear.
	formatReasonClosing = "Fix these, or if a change is intentional say why in your final message; stopping again with no changes will be allowed and reported to the user."
)

// WriteText writes the compact human/agent report, never exceeding limit bytes (<=0 means 2048).
//
// Priority under the limit: the header, then every entry line and fix line
// (blocking items first), then detail for the first shown blocking item that has
// one, then notes. For UNKNOWN, the first note is reserved up front so the reason
// for the verdict is never dropped.
func WriteText(w io.Writer, r Result, limit int) error {
	if limit <= 0 {
		limit = formatDefaultLimit
	}
	header := formatHeader(r)
	notes := r.Notes
	reserved := ""
	if r.Verdict == VerdictUnknown && len(notes) > 0 {
		reserved = formatNoteLine(notes[0])
		notes = notes[1:]
	}

	entries := make([]string, len(r.Items))
	total := 0
	for i, it := range r.Items {
		entries[i] = formatEntry(it, true)
		total += len(entries[i])
	}
	fixed := len(header) + len(reserved)
	// The trailer is reserved only when some entry will be hidden. Its reserved
	// length bounds the real trailer, whose counts are never larger.
	trailerReserve := 0
	if fixed+total > limit {
		trailerReserve = len(formatMore(len(r.Items), formatCountBlocking(r.Items)))
	}
	if fixed+trailerReserve > limit {
		return fmt.Errorf("check report limit %d is too small for its header", limit)
	}

	shown, used := formatChooseEntries(r.Items, entries, limit-fixed-trailerReserve)
	hidden, hiddenBlocking := formatCountHidden(r.Items, shown)
	trailer := ""
	if hidden > 0 {
		trailer = formatMore(hidden, hiddenBlocking)
	}
	room := limit - fixed - used - len(trailer)
	detailAt := formatFirstShownDetail(r.Items, shown)
	detail := ""
	if detailAt >= 0 {
		detail = formatDetailRows(r.Items[detailAt].Detail, formatDetailMaxLines, room)
	}
	room -= len(detail)

	var b strings.Builder
	b.WriteString(header)
	for i := range r.Items {
		if !shown[i] {
			continue
		}
		b.WriteString(entries[i])
		if i == detailAt {
			b.WriteString(detail)
		}
	}
	b.WriteString(trailer)
	b.WriteString(reserved)
	for _, note := range notes {
		line := formatNoteLine(note)
		if len(line) <= room {
			b.WriteString(line)
			room -= len(line)
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
//
// The header and closing sentence are fixed. Entries are placed next, then the
// detail of the first shown item that has one, directly under that item.
// Items are dropped from the end to make room; no space is reserved for detail
// of an item that is not shown.
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

	entries := make([]string, len(blocking))
	total := 0
	for i, it := range blocking {
		entries[i] = formatEntry(it, false)
		total += len(entries[i])
	}
	fixed := len(header) + len(formatReasonClosing)
	trailerReserve := 0
	if fixed+total > limit {
		trailerReserve = len(formatMore(len(blocking), 0))
	}

	shown, used := formatChooseEntries(blocking, entries, limit-fixed-trailerReserve)
	hidden, _ := formatCountHidden(blocking, shown)
	trailer := ""
	if hidden > 0 {
		trailer = formatMore(hidden, 0)
	}
	room := limit - fixed - used - len(trailer)
	detailAt := formatFirstShownDetail(blocking, shown)
	detail := ""
	if detailAt >= 0 {
		detail = formatDetailRows(blocking[detailAt].Detail, math.MaxInt, min(formatReasonDetailBytes, room))
	}

	var b strings.Builder
	b.WriteString(header)
	for i := range blocking {
		if !shown[i] {
			continue
		}
		b.WriteString(entries[i])
		if i == detailAt {
			b.WriteString(detail)
		}
	}
	b.WriteString(trailer)
	b.WriteString(formatReasonClosing)
	return formatTruncate(b.String(), limit)
}

// formatHeader renders the one-line report header, ending in a newline.
func formatHeader(r Result) string {
	ref := formatOneLine(r.Base.Ref)
	base := fmt.Sprintf("base %s @ %s", ref, formatShortCommit(formatOneLine(r.Base.Commit)))
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

// formatEntry renders an item's entry line followed by its fix line, if any.
func formatEntry(it Item, tag bool) string {
	return formatItemLine(it, tag) + formatFixLine(it)
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
	b.WriteString(formatCut(formatOneLine(it.Message), formatTextMaxBytes) + "\n")
	return b.String()
}

// formatFixLine renders the indented fix line, or nothing when the item has no fix.
func formatFixLine(it Item) string {
	fix := formatCut(formatOneLine(it.Fix), formatTextMaxBytes)
	if fix == "" {
		return ""
	}
	return "  fix: " + fix + "\n"
}

// formatNoteLine renders one note as a single "note:" line.
func formatNoteLine(note string) string {
	return "note: " + formatCut(formatOneLine(note), formatTextMaxBytes) + "\n"
}

// formatChooseEntries picks the entries that fit in budget bytes. Blocking items
// are considered before the rest, each group in input order, and the first entry
// that does not fit stops the selection. It returns the shown flags and the bytes
// they use.
func formatChooseEntries(items []Item, entries []string, budget int) ([]bool, int) {
	order := make([]int, 0, len(items))
	for i, it := range items {
		if it.Severity == SeverityBlock {
			order = append(order, i)
		}
	}
	for i, it := range items {
		if it.Severity != SeverityBlock {
			order = append(order, i)
		}
	}
	shown := make([]bool, len(items))
	used := 0
	for _, i := range order {
		if used+len(entries[i]) > budget {
			break
		}
		used += len(entries[i])
		shown[i] = true
	}
	return shown, used
}

// formatCountHidden counts the items that are not shown, and how many of them block.
func formatCountHidden(items []Item, shown []bool) (int, int) {
	hidden, blocking := 0, 0
	for i, it := range items {
		if shown[i] {
			continue
		}
		hidden++
		if it.Severity == SeverityBlock {
			blocking++
		}
	}
	return hidden, blocking
}

// formatCountBlocking counts the items whose severity blocks.
func formatCountBlocking(items []Item) int {
	n := 0
	for _, it := range items {
		if it.Severity == SeverityBlock {
			n++
		}
	}
	return n
}

// formatFirstShownDetail returns the index of the first shown blocking item with
// detail, or -1.
func formatFirstShownDetail(items []Item, shown []bool) int {
	for i, it := range items {
		if shown[i] && it.Severity == SeverityBlock && strings.TrimSpace(it.Detail) != "" {
			return i
		}
	}
	return -1
}

// formatDetailRows renders up to maxLines non-blank detail lines with a "  | "
// prefix, each cut to formatDetailLineBytes. Whole lines are added only while the
// block stays within budget bytes, so a line is never partial.
func formatDetailRows(detail string, maxLines, budget int) string {
	var b strings.Builder
	lines := 0
	for _, raw := range strings.Split(detail, "\n") {
		if lines == maxLines {
			break
		}
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		row := "  | " + formatCut(line, formatDetailLineBytes) + "\n"
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

// formatMore renders the trailer for hidden items. The blocking count is named
// only when some hidden item blocks.
func formatMore(hidden, blocking int) string {
	if blocking > 0 {
		return fmt.Sprintf("+%d more (%d blocking) (run with --format json for all)\n", hidden, blocking)
	}
	return fmt.Sprintf("+%d more (run with --format json for all)\n", hidden)
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

// formatOneLine collapses whitespace so text can never break the line layout.
func formatOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// formatCut shortens s to at most limit bytes. When it cuts, the result ends with
// "…" and still fits in limit; it never splits a UTF-8 rune.
func formatCut(s string, limit int) string {
	const ellipsis = "…"
	if len(s) <= limit {
		return s
	}
	if limit < len(ellipsis) {
		return formatTruncate(s, limit)
	}
	return formatTruncate(s, limit-len(ellipsis)) + ellipsis
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
