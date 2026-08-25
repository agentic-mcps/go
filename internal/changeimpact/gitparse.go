package changeimpact

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// RawChange is one record from git diff --raw -z --find-renames.
type RawChange struct {
	OldMode string
	NewMode string
	OldOID  string
	NewOID  string
	Status  byte
	Score   int
	Path    string
	OldPath string
}

// Hunk is one unified-diff hunk. Counts are retained even when zero.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
}

// ParseRawChanges parses NUL-delimited raw diff records. The input may have
// either no trailing NUL or one trailing NUL, but never empty records.
func ParseRawChanges(data []byte) ([]RawChange, error) {
	if len(data) == 0 {
		return []RawChange{}, nil
	}
	parts := bytes.Split(data, []byte{0})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	changes := make([]RawChange, 0)
	for i := 0; i < len(parts); i++ {
		fields := strings.Fields(string(parts[i]))
		if len(fields) != 5 || fields[0][0] != ':' {
			return nil, fmt.Errorf("raw record %d: malformed header", i)
		}
		statusField := fields[4]
		if len(statusField) == 0 {
			return nil, fmt.Errorf("raw record %d: empty status", i)
		}
		status := statusField[0]
		if !strings.ContainsRune("AMDTRCU", rune(status)) {
			return nil, fmt.Errorf("raw record %d: unsupported status %q", i, status)
		}
		score := 0
		if len(statusField) > 1 {
			parsed, err := strconv.Atoi(statusField[1:])
			if err != nil || parsed < 0 || parsed > 100 {
				return nil, fmt.Errorf("raw record %d: invalid score %q", i, statusField[1:])
			}
			score = parsed
		}
		change := RawChange{OldMode: fields[0][1:], NewMode: fields[1], OldOID: fields[2], NewOID: fields[3], Status: status, Score: score}
		if status == 'R' || status == 'C' {
			if i+2 >= len(parts) || len(parts[i+1]) == 0 || len(parts[i+2]) == 0 {
				return nil, fmt.Errorf("raw record %d: missing pair of paths", i)
			}
			change.OldPath = string(parts[i+1])
			change.Path = string(parts[i+2])
			i += 2
		} else {
			if i+1 >= len(parts) || len(parts[i+1]) == 0 {
				return nil, fmt.Errorf("raw record %d: missing path", i)
			}
			change.Path = string(parts[i+1])
			i++
		}
		changes = append(changes, change)
	}
	return changes, nil
}

var hunkPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

// ParseHunks parses unified-diff hunk headers. It ignores file headers and
// rejects malformed hunk-looking lines rather than silently losing ranges.
func ParseHunks(data []byte) ([]Hunk, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	hunks := make([]Hunk, 0)
	for lineNo, line := range lines {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		match := hunkPattern.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("hunk line %d: malformed header", lineNo+1)
		}
		oldStart, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("hunk line %d: invalid old start", lineNo+1)
		}
		newStart, err := strconv.Atoi(match[3])
		if err != nil {
			return nil, fmt.Errorf("hunk line %d: invalid new start", lineNo+1)
		}
		oldCount, err := hunkCount(match[2])
		if err != nil {
			return nil, fmt.Errorf("hunk line %d: %w", lineNo+1, err)
		}
		newCount, err := hunkCount(match[4])
		if err != nil {
			return nil, fmt.Errorf("hunk line %d: %w", lineNo+1, err)
		}
		hunks = append(hunks, Hunk{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount})
	}
	return hunks, nil
}

func hunkCount(value string) (int, error) {
	if value == "" {
		return 1, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("invalid count %q", value)
	}
	return count, nil
}
