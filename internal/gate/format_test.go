package gate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const wantReasonClosing = "Fix these, or if a change is intentional say why in your final message; stopping again with no changes will be allowed and reported to the user."

// formatMoreLine matches the trailer. The blocking count is present only when
// some hidden item blocks.
var formatMoreLine = regexp.MustCompile(`^\+(\d+) more (?:\((\d+) blocking\) )?\(run with --format json for all\)$`)

func formatBase() Base {
	return Base{Ref: "main", Commit: "abcdef1234567890", Source: "auto"}
}

// formatBlockResult has two blocking items (one with fix and detail, one
// without) and a warning that the reason must leave out.
func formatBlockResult() Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Verdict:       VerdictBlock,
		Base:          formatBase(),
		Items: []Item{
			{
				Severity: SeverityBlock,
				Code:     CodeTestDeleted,
				File:     "internal/x/y.go",
				Line:     42,
				Message:  "TestFoo was deleted",
				Fix:      "Restore TestFoo or fix the code it covered.",
				Detail:   "=== RUN TestFoo\n--- FAIL: TestFoo",
			},
			{
				Severity: SeverityBlock,
				Code:     CodeBuild,
				File:     "internal/x/z.go",
				Message:  "build failed",
			},
			{
				Severity: SeverityWarn,
				Code:     CodeCoverageUncovered,
				File:     "internal/x/w.go",
				Line:     3,
				Message:  "uncovered line",
			},
		},
		Notes: []string{},
	}
}

// formatManyItems returns n blocking items whose messages are 200 bytes and
// whose details are 20 lines of 200 bytes each.
func formatManyItems(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{
			Severity: SeverityBlock,
			Code:     CodeTestFailed,
			File:     fmt.Sprintf("internal/pkg%02d/file.go", i),
			Line:     i + 1,
			Message:  fmt.Sprintf("%02d %s", i, strings.Repeat("m", 197)),
			Fix:      "Fix it.",
			Detail:   strings.Repeat(strings.Repeat("d", 199)+"\n", 20),
		}
	}
	return items
}

// writeTextString renders r with WriteText and fails the test on error.
func writeTextString(t *testing.T, r Result, limit int) string {
	t.Helper()
	var b bytes.Buffer
	if err := WriteText(&b, r, limit); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	return b.String()
}

// countLinesWithPrefix counts the lines of out that start with prefix.
func countLinesWithPrefix(out, prefix string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func TestWriteTextExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		want   string
		result Result
	}{
		{
			name: "pass with no items",
			want: "agentic-go check: PASS (base main @ abcdef1, 3 packages tested, 1.2s)\n",
			result: Result{
				Verdict: VerdictPass,
				Base:    formatBase(),
				Items:   []Item{},
				Notes:   []string{},
				Stats:   Stats{PackagesTested: 3, DurationMS: 1234},
			},
		},
		{
			name: "block with fix and detail, then an item without fix",
			want: "agentic-go check: BLOCK — 2 blocking (base main @ abcdef1)\n" +
				"- [block] internal/x/y.go:42 — TestFoo was deleted\n" +
				"  fix: Restore TestFoo or fix the code it covered.\n" +
				"  | === RUN TestFoo\n" +
				"  | --- FAIL: TestFoo\n" +
				"- [block] internal/x/z.go — build failed\n",
			result: Result{
				Verdict: VerdictBlock,
				Base:    formatBase(),
				Items:   formatBlockResult().Items[:2],
				Notes:   []string{},
			},
		},
		{
			name: "item without a file has no location",
			want: "agentic-go check: BLOCK — 1 blocking (base main @ abcdef1)\n" +
				"- [block] go test failed to start\n",
			result: Result{
				Verdict: VerdictBlock,
				Base:    formatBase(),
				Items:   []Item{{Severity: SeverityBlock, Code: CodeBuild, Line: 7, Message: "go test failed to start"}},
			},
		},
		{
			name: "unknown with a note",
			want: "agentic-go check: UNKNOWN (base main @ abcdef1)\n" +
				"note: coverage unavailable: gopls not found\n",
			result: Result{
				Verdict: VerdictUnknown,
				Base:    formatBase(),
				Items:   []Item{},
				Notes:   []string{"coverage unavailable: gopls not found"},
			},
		},
		{
			name: "singular blocking and warning",
			want: "agentic-go check: BLOCK — 1 blocking, 1 warning (base main @ abcdef1)\n" +
				"- [block] a.go:1 — boom\n" +
				"- [warn] b.go:2 — careful\n",
			result: Result{
				Verdict: VerdictBlock,
				Base:    formatBase(),
				Items: []Item{
					{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 1, Message: "boom"},
					{Severity: SeverityWarn, Code: CodeTestFlaky, File: "b.go", Line: 2, Message: "careful"},
				},
			},
		},
		{
			name: "plural warnings without blocking",
			want: "agentic-go check: PASS — 2 warnings (base main @ abcdef1)\n" +
				"- [warn] a.go:1 — one\n" +
				"- [warn] a.go:2 — two\n",
			result: Result{
				Verdict: VerdictPass,
				Base:    formatBase(),
				Items: []Item{
					{Severity: SeverityWarn, Code: CodeTestFlaky, File: "a.go", Line: 1, Message: "one"},
					{Severity: SeverityWarn, Code: CodeTestFlaky, File: "a.go", Line: 2, Message: "two"},
				},
			},
		},
		{
			name: "detail is capped at four lines and goes under the first item that has it",
			want: "agentic-go check: BLOCK — 3 blocking (base main @ abcdef1)\n" +
				"- [block] a.go:1 — one\n" +
				"  | l1\n  | l2\n  | l3\n  | l4\n" +
				"- [block] a.go:2 — two\n" +
				"- [block] a.go:3 — three\n",
			result: Result{
				Verdict: VerdictBlock,
				Base:    formatBase(),
				Items: []Item{
					{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 1, Message: "one", Detail: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8"},
					{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 2, Message: "two", Detail: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8"},
					{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 3, Message: "three", Detail: "l1"},
				},
			},
		},
		{
			name: "blank detail lines are skipped and long lines end in an ellipsis on a rune boundary",
			want: "agentic-go check: BLOCK — 1 blocking (base main @ abcdef1)\n" +
				"- [block] a.go:1 — one\n" +
				"  | first\n  | second\n  | " + strings.Repeat("漢", 39) + "…\n",
			result: Result{
				Verdict: VerdictBlock,
				Base:    formatBase(),
				Items: []Item{
					{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 1, Message: "one", Detail: "first\n\n   \nsecond\n" + strings.Repeat("漢", 60)},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := writeTextString(t, tc.result, 0); got != tc.want {
				t.Fatalf("WriteText() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestWriteTextLimitHoldsForManyItems(t *testing.T) {
	t.Parallel()
	const limit = 2048
	result := Result{
		Verdict: VerdictBlock,
		Base:    formatBase(),
		Items:   formatManyItems(50),
		Notes:   []string{strings.Repeat("n", 1000), strings.Repeat("o", 1000)},
	}
	out := writeTextString(t, result, limit)
	if len(out) > limit {
		t.Fatalf("WriteText() wrote %d bytes, limit %d", len(out), limit)
	}
	if !utf8.ValidString(out) {
		t.Fatal("WriteText() output is not valid UTF-8")
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("WriteText() output does not end with a newline: %q", out[len(out)-20:])
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	last := lines[len(lines)-1]
	m := formatMoreLine.FindStringSubmatch(last)
	if m == nil {
		t.Fatalf("last line = %q, want +N more line", last)
	}
	hidden, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse hidden count %q: %v", m[1], err)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "note: ") {
			t.Fatalf("note written although it cannot fit: %.40q", line)
		}
	}
	shown := countLinesWithPrefix(out, "- [block] ")
	if shown+hidden != 50 {
		t.Fatalf("shown %d + hidden %d != 50 items", shown, hidden)
	}
	if shown == 0 {
		t.Fatal("expected at least one item to fit in 2048 bytes")
	}
}

func TestWriteTextDefaultLimitIs2048(t *testing.T) {
	t.Parallel()
	out := writeTextString(t, Result{Verdict: VerdictBlock, Base: formatBase(), Items: formatManyItems(50)}, 0)
	if len(out) > 2048 {
		t.Fatalf("WriteText(limit 0) wrote %d bytes, want <= 2048", len(out))
	}
}

func TestWriteTextRejectsLimitBelowHeader(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	err := WriteText(&b, formatBlockResult(), 10)
	if err == nil {
		t.Fatal("WriteText() with a limit below the header returned nil error")
	}
	if b.Len() != 0 {
		t.Fatalf("WriteText() wrote %d bytes before failing", b.Len())
	}
}

func TestWriteTextCapsLongMessageAndKeepsLocationAndFix(t *testing.T) {
	t.Parallel()
	msg := strings.Repeat("ш", 1500) // 3000 bytes of two-byte runes
	out := writeTextString(t, Result{
		Verdict: VerdictBlock,
		Base:    formatBase(),
		Items:   []Item{{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 7, Message: msg, Fix: "Fix it."}},
	}, 0)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header, entry and fix:\n%s", len(lines), out)
	}
	const prefix = "- [block] a.go:7 — "
	if !strings.HasPrefix(lines[1], prefix) {
		t.Fatalf("entry = %q, want prefix %q", lines[1], prefix)
	}
	cut := strings.TrimPrefix(lines[1], prefix)
	if len(cut) > 240 || !strings.HasSuffix(cut, "…") || !strings.HasPrefix(cut, "ш") || !utf8.ValidString(cut) {
		t.Fatalf("message = %d bytes %q, want a rune-safe cut of at most 240 bytes ending in an ellipsis", len(cut), cut)
	}
	if lines[2] != "  fix: Fix it." {
		t.Fatalf("fix line = %q, want the fix", lines[2])
	}
}

func TestWriteTextShowsAllEntriesBeforeDetail(t *testing.T) {
	t.Parallel()
	verbose := strings.Repeat(strings.Repeat("d", 199)+"\n", 20)
	items := []Item{
		{Severity: SeverityBlock, Code: CodeBuild, File: "v1.go", Line: 1, Message: strings.Repeat("v", 3000), Fix: "Fix it.", Detail: verbose},
		{Severity: SeverityBlock, Code: CodeBuild, File: "v2.go", Line: 1, Message: strings.Repeat("v", 3000), Fix: "Fix it.", Detail: verbose},
	}
	for i := range 5 {
		items = append(items, Item{Severity: SeverityBlock, Code: CodeBuild, File: fmt.Sprintf("s%d.go", i), Line: 1, Message: "small"})
	}
	out := writeTextString(t, Result{Verdict: VerdictBlock, Base: formatBase(), Items: items}, 2048)
	if got := countLinesWithPrefix(out, "- [block] "); got != 7 {
		t.Fatalf("entry lines = %d, want all 7\n%s", got, out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	at := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "- [block] v1.go:1 — ") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("first verbose entry missing:\n%s", out)
	}
	if got := countLinesWithPrefix(out, "  | "); got != 4 {
		t.Fatalf("detail rows = %d, want 4 under the first verbose item\n%s", got, out)
	}
	if !strings.HasPrefix(lines[at+2], "  | ") || !strings.HasPrefix(lines[at+6], "- [block] v2.go:1 — ") {
		t.Fatalf("detail is not directly under the first verbose item:\n%s", out)
	}
}

func TestWriteTextBlockingEntriesOutrankWarnings(t *testing.T) {
	t.Parallel()
	items := make([]Item, 0, 50)
	for i := range 40 {
		items = append(items, Item{Severity: SeverityWarn, Code: CodeTestFlaky, File: fmt.Sprintf("warn%02d.go", i), Line: 1, Message: strings.Repeat("w", 100)})
	}
	for i := range 10 {
		items = append(items, Item{Severity: SeverityBlock, Code: CodeBuild, File: fmt.Sprintf("block%02d.go", i), Line: 1, Message: strings.Repeat("b", 100)})
	}
	out := writeTextString(t, Result{Verdict: VerdictBlock, Base: formatBase(), Items: items}, 2048)
	if got := countLinesWithPrefix(out, "- [block] "); got != 10 {
		t.Fatalf("blocking entries = %d, want all 10 shown before any warning\n%s", got, out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	m := formatMoreLine.FindStringSubmatch(lines[len(lines)-1])
	if m == nil || m[2] != "" {
		t.Fatalf("trailer = %q, want the plain form since no hidden item blocks", lines[len(lines)-1])
	}
}

func TestWriteTextTrailerCountsHiddenBlockingItems(t *testing.T) {
	t.Parallel()
	out := writeTextString(t, Result{Verdict: VerdictBlock, Base: formatBase(), Items: formatManyItems(50)}, 2048)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	m := formatMoreLine.FindStringSubmatch(lines[len(lines)-1])
	if m == nil || m[2] == "" {
		t.Fatalf("trailer = %q, want the form that counts hidden blocking items", lines[len(lines)-1])
	}
	if m[1] != m[2] {
		t.Fatalf("trailer hidden = %s, blocking = %s, want equal for all-blocking items", m[1], m[2])
	}
}

func TestWriteTextUnknownKeepsFirstNote(t *testing.T) {
	t.Parallel()
	items := make([]Item, 50)
	for i := range items {
		items[i] = Item{
			Severity: SeverityWarn,
			Code:     CodeTestFlaky,
			File:     fmt.Sprintf("internal/pkg%02d/file.go", i),
			Line:     i + 1,
			Message:  fmt.Sprintf("%02d %s", i, strings.Repeat("m", 197)),
		}
	}
	out := writeTextString(t, Result{
		Verdict: VerdictUnknown,
		Base:    formatBase(),
		Items:   items,
		Notes:   []string{strings.Repeat("n", 3000), "second note"},
	}, 2048)
	if len(out) > 2048 {
		t.Fatalf("WriteText() wrote %d bytes, want <= 2048", len(out))
	}
	var noteLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "note: ") {
			noteLine = line
			break
		}
	}
	if noteLine == "" {
		t.Fatalf("UNKNOWN report dropped its first note:\n%.200s", out)
	}
	if len(noteLine) > len("note: ")+240 || !strings.HasSuffix(noteLine, "…") {
		t.Fatalf("note line = %d bytes %q, want the note cut to 240 bytes with an ellipsis", len(noteLine), noteLine)
	}
}

func TestHeaderSanitizesBaseRef(t *testing.T) {
	t.Parallel()
	out := writeTextString(t, Result{
		Verdict: VerdictUnknown,
		Base:    Base{Ref: "feat/x\nsneaky", Commit: "abcdef1234"},
	}, 0)
	want := "agentic-go check: UNKNOWN (base feat/x sneaky @ abcdef1)\n"
	if out != want {
		t.Fatalf("WriteText() = %q, want %q", out, want)
	}
}

func TestReasonExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		want   string
		result Result
	}{
		{
			name: "two blocking items with detail under the first",
			want: "agentic-go check blocked this stop: 2 problems introduced by your change.\n" +
				"- internal/x/y.go:42 — TestFoo was deleted\n" +
				"  fix: Restore TestFoo or fix the code it covered.\n" +
				"  | === RUN TestFoo\n" +
				"  | --- FAIL: TestFoo\n" +
				"- internal/x/z.go — build failed\n" +
				wantReasonClosing,
			result: formatBlockResult(),
		},
		{
			name: "singular problem",
			want: "agentic-go check blocked this stop: 1 problem introduced by your change.\n" +
				"- a.go:1 — boom\n" +
				wantReasonClosing,
			result: Result{
				Verdict: VerdictBlock,
				Items:   []Item{{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 1, Message: "boom"}},
			},
		},
		{
			name: "no blocking items gives an empty reason",
			want: "",
			result: Result{
				Verdict: VerdictPass,
				Items:   []Item{{Severity: SeverityWarn, Code: CodeTestFlaky, File: "a.go", Line: 1, Message: "careful"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Reason(tc.result, 0); got != tc.want {
				t.Fatalf("Reason() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestReasonLimitHoldsForManyItems(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{0, 2048, 300} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			t.Parallel()
			want := limit
			if want <= 0 {
				want = 2048
			}
			got := Reason(Result{Verdict: VerdictBlock, Items: formatManyItems(50)}, limit)
			if len(got) > want {
				t.Fatalf("Reason() wrote %d bytes, limit %d", len(got), want)
			}
			if !strings.HasSuffix(got, wantReasonClosing) {
				t.Fatalf("Reason() does not end with the closing sentence: %.80q", got)
			}
			if !utf8.ValidString(got) {
				t.Fatal("Reason() output is not valid UTF-8")
			}
			hidden, detailBytes := 0, 0
			for _, line := range strings.Split(got, "\n") {
				if m := formatMoreLine.FindStringSubmatch(line); m != nil {
					n, err := strconv.Atoi(m[1])
					if err != nil {
						t.Fatalf("parse hidden count %q: %v", m[1], err)
					}
					hidden = n
				}
				if strings.HasPrefix(line, "  | ") {
					detailBytes += len(line) + 1
				}
			}
			entries := countLinesWithPrefix(got, "- ")
			if entries+hidden != 50 {
				t.Fatalf("entries %d + hidden %d != 50 blocking items", entries, hidden)
			}
			if detailBytes > 800 {
				t.Fatalf("detail used %d bytes, want <= 800", detailBytes)
			}
		})
	}
}

func TestReasonPlacesDetailUnderItsItem(t *testing.T) {
	t.Parallel()
	got := Reason(Result{
		Verdict: VerdictBlock,
		Items: []Item{
			{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 1, Message: "first"},
			{Severity: SeverityBlock, Code: CodeBuild, File: "b.go", Line: 2, Message: "second", Fix: "Fix it.", Detail: "line one\nline two"},
			{Severity: SeverityBlock, Code: CodeBuild, File: "c.go", Line: 3, Message: "third"},
		},
	}, 0)
	want := "agentic-go check blocked this stop: 3 problems introduced by your change.\n" +
		"- a.go:1 — first\n" +
		"- b.go:2 — second\n" +
		"  fix: Fix it.\n" +
		"  | line one\n" +
		"  | line two\n" +
		"- c.go:3 — third\n" +
		wantReasonClosing
	if got != want {
		t.Fatalf("Reason() =\n%s\nwant\n%s", got, want)
	}
}

func TestReasonDetailIsCappedAt800Bytes(t *testing.T) {
	t.Parallel()
	detail := strings.Repeat(strings.Repeat("q", 150)+"\n", 10)
	got := Reason(Result{
		Verdict: VerdictBlock,
		Items:   []Item{{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 1, Message: "boom", Detail: detail}},
	}, 2048)
	rows, bytes := 0, 0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "  | ") {
			rows++
			bytes += len(line) + 1
			if !strings.HasSuffix(line, "…") {
				t.Fatalf("detail row %q is not cut with an ellipsis", line)
			}
		}
	}
	// Each row is 4 prefix bytes + 120 content bytes + newline = 125 bytes. Six
	// rows use 750 bytes and a seventh would pass the 800-byte cap.
	if rows != 6 || bytes > 800 {
		t.Fatalf("detail rows = %d using %d bytes, want 6 rows within 800 bytes", rows, bytes)
	}
}

func TestReasonDoesNotReserveDetailForHiddenItem(t *testing.T) {
	t.Parallel()
	plain := formatManyItems(50)
	for i := range plain {
		plain[i].Detail = ""
	}
	// Item 11 is the only one with detail, and it is not among the shown items.
	late := formatManyItems(50)
	for i := range late {
		if i != 10 {
			late[i].Detail = ""
		}
	}
	want := countLinesWithPrefix(Reason(Result{Verdict: VerdictBlock, Items: plain}, 2048), "- ")
	got := Reason(Result{Verdict: VerdictBlock, Items: late}, 2048)
	if n := countLinesWithPrefix(got, "- "); n != want {
		t.Fatalf("shown entries = %d with a hidden detail item, want %d as without detail", n, want)
	}
	if strings.Contains(got, "  | ") {
		t.Fatalf("detail shown for an item that is not shown:\n%.300s", got)
	}
}

func TestReasonCapsLongMessageAndKeepsLocationAndFix(t *testing.T) {
	t.Parallel()
	msg := strings.Repeat("ш", 1500)
	got := Reason(Result{
		Verdict: VerdictBlock,
		Items:   []Item{{Severity: SeverityBlock, Code: CodeBuild, File: "a.go", Line: 7, Message: msg, Fix: "Fix it."}},
	}, 0)
	lines := strings.Split(got, "\n")
	const prefix = "- a.go:7 — "
	if len(lines) < 3 || !strings.HasPrefix(lines[1], prefix) {
		t.Fatalf("Reason() entry missing location:\n%s", got)
	}
	cut := strings.TrimPrefix(lines[1], prefix)
	if len(cut) > 240 || !strings.HasSuffix(cut, "…") || !utf8.ValidString(cut) {
		t.Fatalf("message = %d bytes %q, want a rune-safe cut of at most 240 bytes ending in an ellipsis", len(cut), cut)
	}
	if lines[2] != "  fix: Fix it." {
		t.Fatalf("fix line = %q, want the fix", lines[2])
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		want   string
		result Result
	}{
		{
			name: "nil items and notes encode as empty arrays",
			want: `{"schema_version":"agentic.check/v1","verdict":"pass","base":{"ref":"main","commit":"abc","source":"auto"},"items":[],"notes":[],"stats":{"changed_files":0,"changed_go_files":0,"packages_tested":0,"packages_affected":0,"duration_ms":0,"cached":false}}` + "\n",
			result: Result{
				SchemaVersion: SchemaVersion,
				Verdict:       VerdictPass,
				Base:          Base{Ref: "main", Commit: "abc", Source: "auto"},
			},
		},
		{
			name: "html is not escaped",
			want: `{"schema_version":"agentic.check/v1","verdict":"block","base":{"ref":"","commit":"","source":""},"items":[{"severity":"block","code":"go.build","message":"use a < b && c > d"}],"notes":[],"stats":{"changed_files":0,"changed_go_files":0,"packages_tested":0,"packages_affected":0,"duration_ms":0,"cached":false}}` + "\n",
			result: Result{
				SchemaVersion: SchemaVersion,
				Verdict:       VerdictBlock,
				Items:         []Item{{Severity: SeverityBlock, Code: CodeBuild, Message: "use a < b && c > d"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b bytes.Buffer
			if err := WriteJSON(&b, tc.result); err != nil {
				t.Fatalf("WriteJSON() error = %v", err)
			}
			if got := b.String(); got != tc.want {
				t.Fatalf("WriteJSON() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestWriteJSONIsOneValidLine(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := WriteJSON(&b, Result{Verdict: VerdictBlock, Items: formatManyItems(3)}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if n := strings.Count(b.String(), "\n"); n != 1 {
		t.Fatalf("WriteJSON() wrote %d newlines, want 1", n)
	}
	var decoded Result
	if err := json.Unmarshal(b.Bytes(), &decoded); err != nil {
		t.Fatalf("WriteJSON() output is not valid JSON: %v", err)
	}
	if len(decoded.Items) != 3 {
		t.Fatalf("decoded %d items, want 3", len(decoded.Items))
	}
}

func TestFormatHelpers(t *testing.T) {
	t.Parallel()
	if got := formatLocation(Item{File: "", Line: 9}); got != "" {
		t.Errorf("formatLocation(no file) = %q, want empty", got)
	}
	if got := formatLocation(Item{File: "a.go"}); got != "a.go" {
		t.Errorf("formatLocation(no line) = %q, want a.go", got)
	}
	if got := formatLocation(Item{File: "a.go", Line: 42}); got != "a.go:42" {
		t.Errorf("formatLocation() = %q, want a.go:42", got)
	}
	if got := formatShortCommit("abc"); got != "abc" {
		t.Errorf("formatShortCommit(short) = %q, want abc", got)
	}
	if got := formatTruncate("漢字", 4); got != "漢" {
		t.Errorf("formatTruncate(漢字, 4) = %q, want 漢", got)
	}
	if got := formatTruncate("abc", 5); got != "abc" {
		t.Errorf("formatTruncate(abc, 5) = %q, want abc", got)
	}
	cuts := []struct {
		in, want string
		limit    int
	}{
		{"abc", "abc", 5},
		{"abcdef", "ab…", 5},
		{"漢字漢字", "…", 5},
	}
	for _, tc := range cuts {
		if got := formatCut(tc.in, tc.limit); got != tc.want {
			t.Errorf("formatCut(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
		}
	}
}
