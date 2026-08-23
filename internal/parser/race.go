package parser

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

const unparsedRaceMarker = "UNPARSED: race detector block did not match the supported format"

// RaceAccess describes one access reported by the race detector.
//
//nolint:govet // Keep the stable race-report field order.
type RaceAccess struct {
	Kind        string           `json:"kind"`
	Address     string           `json:"address"`
	GoroutineID int              `json:"goroutine_id"`
	Function    string           `json:"function"`
	Location    finding.Location `json:"location"`
	State       string           `json:"state,omitempty"`
}

// RaceConflict contains both conflicting accesses and their creation stacks.
type RaceConflict struct {
	Current           RaceAccess   `json:"current"`
	Previous          RaceAccess   `json:"previous"`
	GoroutineCreation []RaceAccess `json:"goroutine_creation"`
}

// RaceReportOutput contains parsed race conflicts and block counts.
type RaceReportOutput struct {
	Conflicts      []RaceConflict `json:"conflicts"`
	RawBlocksFound int            `json:"raw_blocks_found"`
}

var (
	currentRaceRE  = regexp.MustCompile(`^(Write|Read) at (0x[0-9A-Fa-f]+) by goroutine ([0-9]+):$`)
	previousRaceRE = regexp.MustCompile(`^Previous (write|read) at (0x[0-9A-Fa-f]+) by goroutine ([0-9]+):$`)
	creationRaceRE = regexp.MustCompile(`^Goroutine ([0-9]+) \((running|finished)\) created at:$`)
	locationRaceRE = regexp.MustCompile(`^\s*(\S+\.go):([0-9]+)(?:\s+\+0x[0-9A-Fa-f]+)?\s*$`)
)

// Parse converts race detector text into one conflict per DATA RACE block.
func Parse(input string) RaceReportOutput {
	output := RaceReportOutput{Conflicts: make([]RaceConflict, 0)}
	for _, block := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "==================") {
		if !strings.Contains(block, "WARNING: DATA RACE") {
			continue
		}
		output.RawBlocksFound++
		conflict, complete := parseRaceBlock(block)
		if !complete {
			conflict.Current.Function = unparsedRaceMarker
		}
		output.Conflicts = append(output.Conflicts, conflict)
	}
	return output
}

func parseRaceBlock(block string) (RaceConflict, bool) {
	lines := strings.Split(block, "\n")
	result := RaceConflict{GoroutineCreation: make([]RaceAccess, 0, 2)}
	creations := make(map[int]RaceAccess)
	currentOK, previousOK := false, false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if m := currentRaceRE.FindStringSubmatch(line); m != nil {
			result.Current, currentOK = parseAccess(lines, i, m, strings.ToLower(m[1]))
		}
		if m := previousRaceRE.FindStringSubmatch(line); m != nil {
			result.Previous, previousOK = parseAccess(lines, i, m, m[1])
		}
		if m := creationRaceRE.FindStringSubmatch(line); m != nil {
			access, ok := parseCreation(lines, i, m)
			if ok {
				creations[access.GoroutineID] = access
			}
		}
	}
	if access, ok := creations[result.Current.GoroutineID]; currentOK && ok {
		result.GoroutineCreation = append(result.GoroutineCreation, access)
	}
	if access, ok := creations[result.Previous.GoroutineID]; previousOK && ok && access.GoroutineID != result.Current.GoroutineID {
		result.GoroutineCreation = append(result.GoroutineCreation, access)
	}
	return result, currentOK && previousOK && result.Current.Function != "" && result.Previous.Function != "" && result.Current.Location.File != "" && result.Previous.Location.File != ""
}

func parseAccess(lines []string, at int, m []string, kind string) (RaceAccess, bool) {
	id, _ := strconv.Atoi(m[3])
	a := RaceAccess{Kind: kind, Address: m[2], GoroutineID: id}
	fn, loc, ok := frameAfter(lines, at)
	a.Function, a.Location = fn, loc
	return a, ok
}

func parseCreation(lines []string, at int, m []string) (RaceAccess, bool) {
	id, _ := strconv.Atoi(m[1])
	a := RaceAccess{GoroutineID: id, State: m[2]}
	a.Function, a.Location, _ = frameAfter(lines, at)
	return a, a.Function != "" && a.Location.File != ""
}

func frameAfter(lines []string, at int) (string, finding.Location, bool) {
	i := at + 1
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return "", finding.Location{}, false
	}
	fn := strings.TrimSpace(lines[i])
	i++
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return fn, finding.Location{}, false
	}
	m := locationRaceRE.FindStringSubmatch(lines[i])
	if m == nil {
		return fn, finding.Location{}, false
	}
	line, _ := strconv.Atoi(m[2])
	return fn, finding.Location{File: m[1], Line: line}, true
}
