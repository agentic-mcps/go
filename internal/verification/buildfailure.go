package verification

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// BuildFailureKind is the stable finding kind for a package whose code or
// tests did not compile, including go vet failures reported by go test.
const BuildFailureKind = "build.failure"

const (
	maxCompilerErrorLines = 20
	maxCompilerErrorBytes = 4096
)

var compilerPositionPattern = regexp.MustCompile(`^(\S+\.go):([0-9]+):([0-9]+): `)

func (e *Engine) buildFailureFinding(build buildFailure) Finding {
	excerpt := compilerErrorExcerpt(build.output)
	message := fmt.Sprintf("%s failed to build", build.importPath)
	if excerpt != "" {
		message += ": " + excerpt
	}
	return Finding{
		Kind: BuildFailureKind, Severity: SeverityError, CheckID: "tests",
		Message: message, Location: e.compilerLocation(excerpt),
	}
}

// compilerErrorExcerpt keeps the positioned "file:line:col: message" lines of
// compiler output, bounded by line count and bytes. Output without positions
// (for example a go command setup error) keeps its non-header lines instead.
func compilerErrorExcerpt(output string) string {
	positioned := make([]string, 0)
	other := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if compilerPositionPattern.MatchString(line) {
			positioned = append(positioned, line)
			continue
		}
		other = append(other, line)
	}
	kept := positioned
	if len(kept) == 0 {
		kept = other
	}
	var excerpt strings.Builder
	for index, line := range kept {
		if index == maxCompilerErrorLines {
			break
		}
		if excerpt.Len() > 0 && excerpt.Len()+1+len(line) > maxCompilerErrorBytes {
			break
		}
		if excerpt.Len() > 0 {
			excerpt.WriteByte('\n')
		}
		excerpt.WriteString(line)
	}
	return boundedText(excerpt.String(), maxCompilerErrorBytes)
}

// compilerLocation maps the first compiler position that names an existing
// workspace file. go test reports paths relative to the workspace root.
func (e *Engine) compilerLocation(excerpt string) *Location {
	for _, line := range strings.Split(excerpt, "\n") {
		match := compilerPositionPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		file, err := e.workspace.Relative(filepath.FromSlash(match[1]))
		if err != nil {
			continue
		}
		row, rowErr := strconv.Atoi(match[2])
		col, colErr := strconv.Atoi(match[3])
		if rowErr != nil || colErr != nil {
			continue
		}
		return &Location{File: file, Line: row, Col: col}
	}
	return nil
}
