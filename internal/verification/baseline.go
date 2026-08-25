package verification

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/ashwingopalsamy/agentic-go/internal/finding"
)

type baselineComparison struct {
	Introduced    []Finding
	Uncertainties []Uncertainty
	Summary       AnalysisSummary
}

func compareAnalyzerFindings(checkID string, base, current []finding.Finding, files []SourceFile) baselineComparison {
	comparison := baselineComparison{
		Summary:    AnalysisSummary{Base: len(base), Current: len(current)},
		Introduced: make([]Finding, 0), Uncertainties: make([]Uncertainty, 0),
	}
	baseMapped := make([]Location, len(base))
	mappable := make([]bool, len(base))
	baseExact := make(map[string][]int)
	currentExact := make(map[string][]int)
	for index, item := range base {
		location, ok := mapBaseLocation(item.Location, files)
		if !ok {
			continue
		}
		baseMapped[index], mappable[index] = location, true
		key := exactFindingKey(item.Rule, mapMessageLocations(item.Message, files), location)
		baseExact[key] = append(baseExact[key], index)
	}
	for index, item := range current {
		location := findingLocation(item.Location)
		key := exactFindingKey(item.Rule, item.Message, location)
		currentExact[key] = append(currentExact[key], index)
	}
	baseMatched := make([]bool, len(base))
	baseUnknown := make([]bool, len(base))
	currentMatched := make([]bool, len(current))
	currentUnknown := make([]bool, len(current))
	for key, baseIndexes := range baseExact {
		currentIndexes := currentExact[key]
		if len(currentIndexes) == 0 {
			continue
		}
		if len(baseIndexes) == 1 && len(currentIndexes) == 1 {
			baseMatched[baseIndexes[0]] = true
			currentMatched[currentIndexes[0]] = true
			comparison.Summary.Existing++
			continue
		}
		for _, index := range baseIndexes {
			baseUnknown[index] = true
		}
		for _, index := range currentIndexes {
			currentUnknown[index] = true
		}
	}

	remainingBaseBySignature := make(map[string][]int)
	for index, item := range base {
		if baseMatched[index] || baseUnknown[index] {
			continue
		}
		signature := findingSignature(item.Rule, item.Message)
		remainingBaseBySignature[signature] = append(remainingBaseBySignature[signature], index)
	}
	for index, item := range current {
		if currentMatched[index] || currentUnknown[index] {
			continue
		}
		candidates := remainingBaseBySignature[findingSignature(item.Rule, item.Message)]
		if len(candidates) == 0 {
			comparison.Introduced = append(comparison.Introduced, portableAnalyzerFinding(checkID, item, BaselineIntroduced))
			comparison.Summary.Introduced++
			continue
		}
		currentUnknown[index] = true
		for _, candidate := range candidates {
			baseUnknown[candidate] = true
		}
	}
	for index := range current {
		if !currentUnknown[index] {
			continue
		}
		comparison.Summary.Unknown++
		location := findingLocation(current[index].Location)
		comparison.Uncertainties = append(comparison.Uncertainties, Uncertainty{
			Code: "baseline_unknown", CheckID: checkID,
			Message:   "analyzer finding could not be matched unambiguously to an unchanged base location",
			Locations: []Location{location},
		})
	}
	for index := range base {
		if !baseMatched[index] && !baseUnknown[index] {
			comparison.Summary.Resolved++
		}
	}
	sort.Slice(comparison.Introduced, func(i, j int) bool {
		return portableFindingKey(comparison.Introduced[i]) < portableFindingKey(comparison.Introduced[j])
	})
	return comparison
}

func mapBaseLocation(location finding.Location, files []SourceFile) (Location, bool) {
	basePath := filepath.ToSlash(location.File)
	for _, file := range files {
		previous := file.Change.PreviousPath
		if previous == "" {
			previous = file.Change.Path
		}
		if previous != basePath {
			continue
		}
		if file.Change.Change == ChangeDeleted {
			return Location{}, false
		}
		line, ok := mapUnchangedLine(location.Line, file.Edits)
		if !ok {
			return Location{}, false
		}
		return Location{File: file.Change.Path, Line: line, Col: location.Col}, true
	}
	return findingLocation(location), true
}

func mapUnchangedLine(line int, edits []LineEdit) (int, bool) {
	delta := 0
	for _, edit := range edits {
		if edit.BaseCount == 0 {
			if line > edit.BaseStart {
				delta += edit.CurrentCount
			}
			continue
		}
		end := edit.BaseStart + edit.BaseCount - 1
		if line >= edit.BaseStart && line <= end {
			return 0, false
		}
		if line > end {
			delta += edit.CurrentCount - edit.BaseCount
		}
	}
	return line + delta, true
}

func portableAnalyzerFinding(checkID string, item finding.Finding, baseline BaselineState) Finding {
	location := findingLocation(item.Location)
	return Finding{
		Kind: "go.analysis", Rule: item.Rule, Severity: Severity(item.Severity), Message: item.Message,
		Suggestion: item.Suggestion, Location: &location, CheckID: checkID, Baseline: baseline,
	}
}

func findingLocation(location finding.Location) Location {
	return Location{File: filepath.ToSlash(location.File), Line: location.Line, Col: location.Col}
}

func exactFindingKey(rule, message string, location Location) string {
	return fmt.Sprintf("%s\x00%s\x00%s:%09d:%09d", rule, message, location.File, location.Line, location.Col)
}

var sourcePositionPattern = regexp.MustCompile(`([A-Za-z0-9_./-]+\.go):([0-9]+)(?::([0-9]+))?`)

func mapMessageLocations(message string, files []SourceFile) string {
	return sourcePositionPattern.ReplaceAllStringFunc(message, func(value string) string {
		match := sourcePositionPattern.FindStringSubmatch(value)
		line, err := strconv.Atoi(match[2])
		if err != nil {
			return value
		}
		column := 0
		if match[3] != "" {
			column, err = strconv.Atoi(match[3])
			if err != nil {
				return value
			}
		}
		mapped, ok := mapBaseLocation(finding.Location{File: match[1], Line: line, Col: column}, files)
		if !ok {
			return value
		}
		if match[3] == "" {
			return fmt.Sprintf("%s:%d", mapped.File, mapped.Line)
		}
		return fmt.Sprintf("%s:%d:%d", mapped.File, mapped.Line, mapped.Col)
	})
}

func findingSignature(rule, message string) string {
	return rule + "\x00" + sourcePositionPattern.ReplaceAllString(message, "<location>")
}

func portableFindingKey(item Finding) string {
	if item.Location == nil {
		return item.Rule + "\x00" + item.Message
	}
	return exactFindingKey(item.Rule, item.Message, *item.Location)
}
