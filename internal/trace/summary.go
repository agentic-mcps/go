package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	maxSummaryFileBytes = 16 << 20
	maxSummaryRecords   = 10_000
	maxSummaryLineBytes = 64 << 10
)

// ToolSummary is a bounded aggregate for one tool in the current trace run.
type ToolSummary struct {
	Tool          string `json:"tool"`
	Calls         int    `json:"calls"`
	ErrorCount    int    `json:"error_count"`
	P50DurationMS int64  `json:"p50_duration_ms"`
	P99DurationMS int64  `json:"p99_duration_ms"`
}

// Summary describes a bounded recent window from the current server run.
type Summary struct {
	Enabled           bool          `json:"enabled"`
	RecordsConsidered int           `json:"records_considered"`
	Truncated         bool          `json:"truncated"`
	Tools             []ToolSummary `json:"tools"`
}

type summaryRecord struct {
	tool       string
	durationMS int64
	error      bool
}

// Summary returns aggregate trace data without exposing arguments or raw records.
func (t *Tracer) Summary() (Summary, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil || t.closed {
		return Summary{Enabled: false, Tools: []ToolSummary{}}, nil
	}
	if err := t.file.Sync(); err != nil {
		return Summary{}, fmt.Errorf("syncing trace before summary: %w", err)
	}
	file, err := os.Open(t.file.Name())
	if err != nil {
		return Summary{}, fmt.Errorf("opening trace summary source: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Summary{}, fmt.Errorf("inspecting trace summary source: %w", err)
	}
	truncated := info.Size() > maxSummaryFileBytes
	if truncated {
		if _, err := file.Seek(info.Size()-maxSummaryFileBytes, io.SeekStart); err != nil {
			return Summary{}, fmt.Errorf("seeking trace summary window: %w", err)
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxSummaryLineBytes)
	if truncated {
		// The bounded tail may begin in the middle of one JSON line.
		if scanner.Scan() {
			// Discard the partial record.
		}
	}
	records := make([]summaryRecord, 0, maxSummaryRecords)
	next := 0
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return Summary{}, fmt.Errorf("decoding trace summary record: %w", err)
		}
		item := summaryRecord{tool: record.Tool, durationMS: record.DurationMS, error: record.Error}
		if len(records) < maxSummaryRecords {
			records = append(records, item)
			continue
		}
		truncated = true
		records[next] = item
		next = (next + 1) % len(records)
	}
	if err := scanner.Err(); err != nil {
		return Summary{}, fmt.Errorf("reading trace summary source: %w", err)
	}
	if next != 0 {
		ordered := make([]summaryRecord, 0, len(records))
		ordered = append(ordered, records[next:]...)
		ordered = append(ordered, records[:next]...)
		records = ordered
	}

	type aggregate struct {
		calls     int
		errors    int
		durations []int64
	}
	byTool := make(map[string]*aggregate)
	for _, record := range records {
		agg := byTool[record.tool]
		if agg == nil {
			agg = &aggregate{}
			byTool[record.tool] = agg
		}
		agg.calls++
		if record.error {
			agg.errors++
		}
		agg.durations = append(agg.durations, record.durationMS)
	}
	tools := make([]ToolSummary, 0, len(byTool))
	for tool, agg := range byTool {
		sort.Slice(agg.durations, func(i, j int) bool { return agg.durations[i] < agg.durations[j] })
		tools = append(tools, ToolSummary{
			Tool: tool, Calls: agg.calls, ErrorCount: agg.errors,
			P50DurationMS: percentile(agg.durations, 50),
			P99DurationMS: percentile(agg.durations, 99),
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Tool < tools[j].Tool })
	return Summary{Enabled: true, RecordsConsidered: len(records), Truncated: truncated, Tools: tools}, nil
}

func percentile(sorted []int64, value int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted)*value+99)/100 - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
