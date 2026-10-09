// Package parser contains bounded decoders for tool output formats.
package parser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const maxTestJSONEvent = 8 << 20

// TestEvent is one event emitted by go test -json. Build events
// ("build-output" and "build-fail", Go 1.24+) carry ImportPath instead of
// Package; a package "fail" event caused by a build failure names the failed
// build's ImportPath in FailedBuild.
//
//nolint:govet // JSON field order mirrors the upstream go test event shape.
type TestEvent struct {
	Time        time.Time `json:"Time"`
	Action      string    `json:"Action"`
	Package     string    `json:"Package"`
	ImportPath  string    `json:"ImportPath,omitempty"`
	Test        string    `json:"Test,omitempty"`
	Elapsed     float64   `json:"Elapsed,omitempty"`
	Output      string    `json:"Output,omitempty"`
	FailedBuild string    `json:"FailedBuild,omitempty"`
}

// Stats describes decoding outcomes. Malformed is the number of complete input
// lines that were not valid JSON test events and were skipped.
type Stats struct {
	Valid     int
	Malformed int
}

// DecodeTestJSON decodes newline-delimited go test -json output in bounded
// memory. Each valid event is delivered immediately, in input order. Complete
// malformed lines are skipped so incidental stdout cannot hide later events.
func DecodeTestJSON(r io.Reader, callback func(TestEvent) error) (Stats, error) {
	if r == nil {
		return Stats{}, errors.New("testjson: nil reader")
	}
	if callback == nil {
		return Stats{}, errors.New("testjson: nil callback")
	}

	scanner := bufio.NewScanner(r)
	// Scanner's default token limit is smaller than the protocol's event cap.
	scanner.Buffer(make([]byte, 64*1024), maxTestJSONEvent)
	stats := Stats{}
	for scanner.Scan() {
		var event TestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			stats.Malformed++
			continue
		}
		if !validTestEvent(event) {
			stats.Malformed++
			continue
		}
		stats.Valid++
		if err := callback(event); err != nil {
			return stats, fmt.Errorf("testjson: callback: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("testjson: scan: %w", err)
	}
	if stats.Valid == 0 {
		return stats, errors.New("testjson: no valid events")
	}
	return stats, nil
}

func validTestEvent(event TestEvent) bool {
	if validBuildAction(event.Action) {
		return event.ImportPath != ""
	}
	return event.Package != "" && validTestAction(event.Action)
}

func validBuildAction(action string) bool {
	return action == "build-output" || action == "build-fail"
}

func validTestAction(action string) bool {
	switch action {
	case "start", "run", "pause", "cont", "pass", "bench", "fail", "output", "skip":
		return true
	default:
		return false
	}
}
