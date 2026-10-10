package gate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/agentic-mcps/go/internal/verification"
)

const (
	// mapDetailLines bounds the failing output kept in an item's Detail.
	mapDetailLines = 12
	// mapBuildFix is the fix for a compile error.
	mapBuildFix = "Fix the compile error."
	// mapRaceFix is the fix for a data race.
	mapRaceFix = "Fix the data race; do not remove the test or the -race flag."
	// mapCoverageFix is the fix for uncovered changed lines.
	mapCoverageFix = "Add a test that executes these lines, or remove them if they are unused."
)

// testFailure is one failing top-level test, or a package that failed outside
// any named test when Test is empty, waiting for attribution.
type testFailure struct {
	Package  string
	Test     string
	Output   string
	Subtests []string
}

// mapped is what the engine's collection contributes to a gate result.
type mapped struct {
	items    []Item
	notes    []string
	failures []testFailure
	complete bool
}

// mapCollection converts an unfinalized verification report into gate items,
// notes and test failures that still need attribution. complete is false when
// required evidence could not be produced.
func mapCollection(report verification.Report, files []verification.SourceFile, requireCoverage bool) mapped {
	out := mapped{items: []Item{}, notes: []string{}, failures: []testFailure{}, complete: true}
	for _, finding := range report.Findings {
		if item, ok := mapFinding(finding); ok {
			out.items = append(out.items, item)
		}
	}
	for _, evidence := range report.Evidence {
		mapEvidence(&out, evidence, files, requireCoverage)
	}
	unknown := 0
	for _, uncertainty := range report.Uncertainties {
		switch uncertainty.Code {
		case "baseline_unknown":
			unknown++
		case "coverage_incomplete":
			out.notes = append(out.notes, uncertainty.Message)
			if requireCoverage {
				out.complete = false
			}
		}
	}
	if unknown > 0 {
		out.notes = append(out.notes, fmt.Sprintf("%d analyzer %s could not be compared with base", unknown, mapNoun(unknown, "finding", "findings")))
	}
	return out
}

// mapFinding maps build, race and introduced analyzer findings. Test failure
// findings are skipped because the test summary carries their identity.
func mapFinding(finding verification.Finding) (Item, bool) {
	switch {
	case finding.Kind == verification.BuildFailureKind:
		return mapBuildFinding(finding), true
	case finding.Kind == "go.race":
		item := Item{Severity: SeverityBlock, Code: CodeRace, Message: finding.Message, Fix: mapRaceFix}
		mapLocate(&item, finding.Location)
		return item, true
	case finding.Kind == "go.analysis" && finding.Baseline == verification.BaselineIntroduced:
		item := Item{Severity: SeverityWarn, Code: CodeAnalysisIntroduced, Message: finding.Message, Fix: finding.Suggestion}
		if finding.Severity == verification.SeverityError {
			item.Severity = SeverityBlock
		}
		if finding.Rule != "" {
			item.Message = finding.Rule + ": " + finding.Message
		}
		mapLocate(&item, finding.Location)
		return item, true
	default:
		return Item{}, false
	}
}

// mapBuildFinding turns "<pkg> failed to build: <compiler lines>" into a
// blocking item at the first compiler position.
func mapBuildFinding(finding verification.Finding) Item {
	pkg, compiler, _ := strings.Cut(finding.Message, " failed to build")
	compiler = strings.TrimSpace(strings.TrimPrefix(compiler, ":"))
	message := pkg + " does not build"
	if first, _, _ := strings.Cut(compiler, "\n"); first != "" {
		message += ": " + mapStripPosition(first)
	}
	item := Item{Severity: SeverityBlock, Code: CodeBuild, Message: message, Fix: mapBuildFix, Detail: compiler}
	mapLocate(&item, finding.Location)
	if item.File == "" {
		item.File, item.Line = mapCompilerPosition(compiler)
	}
	return item
}

// mapCompilerPositionPattern matches "file.go:line:" with an optional column;
// coverage instrumentation makes the compiler omit columns.
var mapCompilerPositionPattern = regexp.MustCompile(`^([^\s:]+\.go):([0-9]+):`)

// mapCompilerPosition returns the first workspace-relative compiler position.
func mapCompilerPosition(compiler string) (string, int) {
	for _, line := range strings.Split(compiler, "\n") {
		match := mapCompilerPositionPattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil || !filepath.IsLocal(filepath.FromSlash(match[1])) {
			continue
		}
		row, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		return filepath.ToSlash(match[1]), row
	}
	return "", 0
}

// mapStripPosition drops a leading "file.go:line:col: " from a compiler line.
func mapStripPosition(line string) string {
	parts := strings.SplitN(line, ": ", 2)
	if len(parts) == 2 && strings.Contains(parts[0], ".go:") {
		return parts[1]
	}
	return line
}

func mapLocate(item *Item, location *verification.Location) {
	if location == nil {
		return
	}
	item.File = location.File
	item.Line = location.Line
}

func mapEvidence(out *mapped, evidence verification.Evidence, files []verification.SourceFile, requireCoverage bool) {
	switch evidence.Kind {
	case verification.CheckTests:
		if evidence.Status == verification.EvidenceError {
			out.notes = append(out.notes, "tests could not run: "+mapEvidenceError(evidence))
			out.complete = false
			return
		}
		if evidence.Tests != nil {
			out.failures = append(out.failures, mapTestFailures(*evidence.Tests)...)
		}
	case verification.CheckRace:
		if evidence.Status == verification.EvidenceError {
			out.notes = append(out.notes, "race detection could not run: "+mapEvidenceError(evidence))
			out.complete = false
		}
	case verification.CheckCoverage:
		mapCoverage(out, evidence, files, requireCoverage)
	case verification.CheckConcurrency, verification.CheckErrors:
		// Analyzers are advisory evidence: a failure to run them is reported
		// but does not make the verdict unknown.
		if evidence.Status == verification.EvidenceError {
			out.notes = append(out.notes, evidence.Summary+": "+mapEvidenceError(evidence))
		}
	}
}

func mapEvidenceError(evidence verification.Evidence) string {
	text := evidence.Error
	if text == "" {
		text = evidence.Summary
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return first
}

// mapTestFailures groups failed named tests by package and top-level test and
// adds packages that failed outside any named test, including a crash after
// named tests failed, except packages that could not build: their build
// failure is reported on its own.
func mapTestFailures(summary verification.TestSummary) []testFailure {
	type key struct{ pkg, test string }
	byKey := make(map[key]*testFailure)
	order := make([]key, 0)
	for _, test := range summary.Nonpassing {
		if test.Status != "fail" {
			continue
		}
		top, _, nested := strings.Cut(test.Name, "/")
		k := key{test.Package, top}
		failure, seen := byKey[k]
		if !seen {
			failure = &testFailure{Package: test.Package, Test: top, Subtests: []string{}}
			byKey[k] = failure
			order = append(order, k)
		}
		if nested {
			failure.Subtests = append(failure.Subtests, test.Name)
		}
		if output := strings.TrimSpace(test.Output); output != "" {
			failure.Output = strings.TrimSpace(failure.Output + "\n" + output)
		}
	}
	failures := make([]testFailure, 0, len(order)+len(summary.Packages))
	for _, k := range order {
		failures = append(failures, *byKey[k])
	}
	for _, pkg := range summary.Packages {
		if pkg.Status != "FAIL" || mapBuildFailed(pkg.Output) || (pkg.Failed > 0 && !mapAborted(pkg.Output)) {
			continue
		}
		failures = append(failures, testFailure{Package: pkg.Package, Output: strings.TrimSpace(pkg.Output), Subtests: []string{}})
	}
	sort.SliceStable(failures, func(i, j int) bool {
		if failures[i].Package != failures[j].Package {
			return failures[i].Package < failures[j].Package
		}
		return failures[i].Test < failures[j].Test
	})
	return failures
}

// mapAborted reports package-level output showing the test binary crashing
// outside any named test, as when TestMain panics after m.Run or go test
// kills a test binary that timed out.
func mapAborted(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "panic: ") || strings.HasPrefix(line, "fatal error: ") {
			return true
		}
	}
	return false
}

// mapBuildFailed reports go test's marker for a package that never ran.
func mapBuildFailed(output string) bool {
	return strings.Contains(output, "[build failed]") || strings.Contains(output, "[setup failed]")
}

// mapCoverage reports uncovered changed lines per file. Unavailable coverage
// is an info item, never a 0% result; it makes the run incomplete, with a
// note, only when coverage is required. Partial coverage is a note.
func mapCoverage(out *mapped, evidence verification.Evidence, files []verification.SourceFile, requireCoverage bool) {
	if evidence.Status == verification.EvidenceSkipped {
		return
	}
	if evidence.Status == verification.EvidenceError || evidence.Coverage == nil {
		summary := evidence.Summary
		if !strings.HasPrefix(summary, "coverage unavailable") {
			summary = "coverage unavailable: " + summary
		}
		out.items = append(out.items, Item{Severity: SeverityInfo, Code: CodeCoverageUnavailable, Message: summary})
		if requireCoverage {
			out.notes = append(out.notes, summary)
			out.complete = false
		}
		return
	}
	severity := SeverityWarn
	if requireCoverage {
		severity = SeverityBlock
	}
	for _, file := range mapUncoveredLines(evidence.Coverage.Uncovered, files) {
		noun := "lines"
		if file.count == 1 {
			noun = "line"
		}
		out.items = append(out.items, Item{
			Severity: severity, Code: CodeCoverageUncovered, File: file.path, Line: file.first,
			Message: fmt.Sprintf("%d changed %s in %s %s not executed by any test", file.count, noun, file.path, mapVerb(file.count)),
			Fix:     mapCoverageFix,
		})
	}
}

func mapVerb(count int) string {
	return mapNoun(count, "is", "are")
}

func mapNoun(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

type uncoveredFile struct {
	path  string
	count int
	first int
}

// mapUncoveredLines counts the changed lines inside uncovered coverage blocks
// for each file. A block spans whole statements, so only lines that are both
// changed and uncovered are counted.
func mapUncoveredLines(ranges []verification.SourceRange, files []verification.SourceFile) []uncoveredFile {
	changed := make(map[string][]verification.LineRange, len(files))
	for _, file := range files {
		changed[file.Change.Path] = file.Change.CurrentRanges
	}
	lines := make(map[string]map[int]struct{})
	for _, block := range ranges {
		for line := block.StartLine; line <= block.EndLine; line++ {
			if !mapLineIn(line, changed[block.File]) {
				continue
			}
			if lines[block.File] == nil {
				lines[block.File] = make(map[int]struct{})
			}
			lines[block.File][line] = struct{}{}
		}
	}
	result := make([]uncoveredFile, 0, len(lines))
	for path, set := range lines {
		first := 0
		for line := range set {
			if first == 0 || line < first {
				first = line
			}
		}
		result = append(result, uncoveredFile{path: path, count: len(set), first: first})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].path < result[j].path })
	return result
}

func mapLineIn(line int, ranges []verification.LineRange) bool {
	for _, r := range ranges {
		if line >= r.Start && line <= r.End {
			return true
		}
	}
	return false
}

// mapDetail keeps the first meaningful lines of failing test output, dropping
// go test's "=== RUN" style progress lines.
func mapDetail(output string) string {
	kept := make([]string, 0, mapDetailLines)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "=== ") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t\r"))
		if len(kept) == mapDetailLines {
			break
		}
	}
	return strings.Join(kept, "\n")
}

// mapListFix is the fix for a package that go list cannot load.
const mapListFix = "Fix the import or package clause so the package loads."

var (
	// mapListPosition matches "path:line: msg" and "path:line:col: msg".
	mapListPosition = regexp.MustCompile(`^([^\s:]+):([0-9]+)(?::[0-9]+)?: (.+)$`)
	// mapListRootSuffix drops a trailing absolute path such as GOROOT.
	mapListRootSuffix = regexp.MustCompile(`\s+\(/[^)]*\)$`)
	// mapListMarkers are go list errors that only a code change causes.
	mapListMarkers = []string{
		"found packages", "import cycle not allowed", "no required module provides package",
		"cannot find module providing package", "is not in std",
	}
	// mapListNetwork marks environment failures that must never block.
	mapListNetwork = []string{"dial tcp", "i/o timeout", "no such host", "connection refused", "connection reset", "TLS handshake"}
)

// mapListErrors turns a change-discovery failure caused by go list rejecting
// the code (bad imports, import cycles, mixed package clauses) into blocking
// build items. Errors of any other shape, including network failures, return
// nothing so the caller reports an unknown verdict.
func mapListErrors(root, text string) []Item {
	dir, stderr := mapListOutput(root, text)
	if stderr == "" || mapContainsAny(stderr, mapListNetwork) {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	detail := strings.Join(lines[:min(len(lines), mapDetailLines)], "\n")
	items := make([]Item, 0)
	seen := make(map[string]bool)
	for _, line := range lines {
		match := mapListPosition.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		row, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		file := mapListFile(root, dir, match[1])
		key := file + ":" + match[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, Item{
			Severity: SeverityBlock, Code: CodeBuild, File: file, Line: row,
			Message: "package does not load: " + mapListRootSuffix.ReplaceAllString(match[3], ""),
			Fix:     mapListFix, Detail: detail,
		})
	}
	if len(items) > 0 {
		return items
	}
	for _, line := range lines {
		if mapContainsAny(line, mapListMarkers) {
			return []Item{{
				Severity: SeverityBlock, Code: CodeBuild,
				Message: "package does not load: " + strings.TrimSpace(mapListRootSuffix.ReplaceAllString(line, "")),
				Fix:     mapListFix, Detail: detail,
			}}
		}
	}
	return nil
}

// mapListOutput splits the analyzer's wrapped error into the directory go
// list ran in and its stderr.
func mapListOutput(root, text string) (string, string) {
	const exited = "go list exited "
	index := strings.Index(text, exited)
	if index < 0 {
		return root, ""
	}
	_, stderr, found := strings.Cut(text[index+len(exited):], ": ")
	if !found {
		return root, ""
	}
	dir := root
	if _, after, ok := strings.Cut(text[:index], "listing packages in "); ok {
		dir = strings.TrimSuffix(strings.TrimSpace(after), ":")
	}
	return dir, stderr
}

// mapListFile makes a go list path workspace-relative, or empty when it is
// outside the workspace.
func mapListFile(root, dir, name string) string {
	local := filepath.FromSlash(name)
	if !filepath.IsAbs(local) {
		local = filepath.Join(dir, local)
	}
	rel, err := filepath.Rel(root, local)
	if err != nil || !filepath.IsLocal(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func mapContainsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
