package verification

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentic-mcps/go/internal/parser"
)

func (e *Engine) hasChangedExecutableStatements(analysis ChangeAnalysis) bool {
	for _, file := range analysis.Files {
		if hasChangedStatements(file) {
			return true
		}
	}
	return false
}

// hasChangedStatements reports whether an added or modified non-test Go file
// has an executable statement intersecting its changed lines. Unparseable
// source is treated as changed so coverage stays required.
func hasChangedStatements(file SourceFile) bool {
	if filepath.Ext(file.Change.Path) != ".go" || strings.HasSuffix(file.Change.Path, "_test.go") || file.Change.Change == ChangeDeleted || len(file.Change.CurrentRanges) == 0 {
		return false
	}
	set := token.NewFileSet()
	parsed, err := goparser.ParseFile(set, file.Change.Path, file.CurrentContent, 0)
	if err != nil {
		return true
	}
	changed := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		statement, ok := node.(ast.Stmt)
		if !ok {
			return true
		}
		switch statement.(type) {
		case *ast.BadStmt, *ast.BlockStmt, *ast.EmptyStmt:
			return true
		}
		start := set.Position(statement.Pos()).Line
		end := set.Position(statement.End()).Line
		for _, current := range file.Change.CurrentRanges {
			if start <= current.End && end >= current.Start {
				changed = true
				return false
			}
		}
		return true
	})
	return changed
}

// coverageEvidence turns the coverage profile into changed-statement evidence.
// A package whose own test binary aborted before writing coverage (panic,
// build failure, timeout) has no trustworthy blocks: since Go 1.22 other test
// binaries still emit zero-count blocks for every -coverpkg package. Changed
// files owned by such a package are excluded; when nothing measurable remains,
// coverage is unavailable, never a 0% pass.
func (e *Engine) coverageEvidence(analysis ChangeAnalysis, run affectedRun) (Evidence, []Uncertainty) {
	excluded, lost := e.lostCoverageFiles(analysis, run.facts.coverageLost)
	coverage, uncertainties, coverageErr := e.changedCoverage(analysis, run.profile, excluded)
	if run.coverageErr != nil {
		coverageErr = run.coverageErr
	}
	evidence := Evidence{CheckID: "coverage", Kind: CheckCoverage, DurationMS: run.result.Duration.Milliseconds()}
	if len(lost) > 0 && (coverageErr != nil || coverage.TotalStatements == 0) {
		evidence.Status = EvidenceError
		evidence.Summary = fmt.Sprintf("coverage unavailable: tests in %s failed", strings.Join(lost, ", "))
		evidence.Error = "tests failed before writing coverage for the changed statements"
		return evidence, uncertainties
	}
	if coverageErr != nil {
		evidence.Status = EvidenceError
		evidence.Summary = "changed coverage could not be calculated"
		evidence.Error = portableCheckError(coverageErr, e.workspace.Root())
		return evidence, uncertainties
	}
	evidence.Status = EvidencePassed
	evidence.Summary = fmt.Sprintf("%.1f%% of changed statements covered", coverage.Percent)
	evidence.Coverage = &coverage
	for _, pkg := range lost {
		uncertainties = append(uncertainties, Uncertainty{
			Code: "coverage_incomplete", CheckID: "coverage", Locations: make([]Location, 0),
			Message: fmt.Sprintf("tests in %s failed before writing coverage; its changed statements are excluded from changed coverage", pkg),
		})
	}
	return evidence, uncertainties
}

// lostCoverageFiles returns the changed files whose owning package's test
// binary wrote no coverage, and those packages in sorted order.
func (e *Engine) lostCoverageFiles(analysis ChangeAnalysis, lost map[string]struct{}) (map[string]struct{}, []string) {
	files := make(map[string]struct{})
	packages := make(map[string]struct{})
	for _, file := range analysis.Files {
		if !hasChangedStatements(file) {
			continue
		}
		owner := e.owningPackage(analysis.Packages, file.Change.Path)
		if _, failed := lost[owner]; owner == "" || !failed {
			continue
		}
		files[file.Change.Path] = struct{}{}
		packages[owner] = struct{}{}
	}
	ordered := make([]string, 0, len(packages))
	for pkg := range packages {
		ordered = append(ordered, pkg)
	}
	sort.Strings(ordered)
	return files, ordered
}

func (e *Engine) owningPackage(targets []ExecutionTarget, path string) string {
	directory := filepath.Dir(filepath.Join(e.workspace.Root(), filepath.FromSlash(path)))
	for _, target := range targets {
		if target.Distance == 0 && filepath.Clean(target.Dir) == directory {
			return target.ID
		}
	}
	return ""
}

func (e *Engine) changedCoverage(analysis ChangeAnalysis, blocks []parser.CoverageBlock, excluded map[string]struct{}) (CoverageSummary, []Uncertainty, error) {
	changed := make(map[string][]LineRange)
	for _, file := range analysis.Files {
		if _, skip := excluded[file.Change.Path]; skip {
			continue
		}
		if filepath.Ext(file.Change.Path) == ".go" && file.Change.Change != ChangeDeleted && len(file.Change.CurrentRanges) > 0 {
			changed[file.Change.Path] = file.Change.CurrentRanges
		}
	}
	type normalizedBlock struct {
		file       string
		startLine  int
		startCol   int
		endLine    int
		endCol     int
		statements uint64
		covered    bool
	}
	unique := make(map[string]normalizedBlock)
	uncertainties := make([]Uncertainty, 0)
	for _, block := range blocks {
		if block.Statements == 0 {
			continue
		}
		file, err := e.coverageFile(analysis.Packages, block.File)
		if err != nil {
			uncertainties = append(uncertainties, Uncertainty{
				Code: "coverage_path_unmapped", Message: portableCheckError(err, e.workspace.Root()), Locations: make([]Location, 0),
			})
			continue
		}
		ranges, exists := changed[file]
		if !exists || !coverageIntersects(block, ranges) {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d:%d:%d", file, block.StartLine, block.StartCol, block.EndLine, block.EndCol)
		item, duplicate := unique[key]
		if !duplicate {
			item = normalizedBlock{
				file: file, startLine: block.StartLine, startCol: block.StartCol,
				endLine: block.EndLine, endCol: block.EndCol, statements: block.Statements,
			}
		} else if item.statements != block.Statements {
			return CoverageSummary{}, nil, fmt.Errorf("coverage block %s has inconsistent statement counts", key)
		}
		item.covered = item.covered || block.Count > 0
		unique[key] = item
	}
	ordered := make([]normalizedBlock, 0, len(unique))
	for _, block := range unique {
		ordered = append(ordered, block)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := fmt.Sprintf("%s:%09d:%09d", ordered[i].file, ordered[i].startLine, ordered[i].startCol)
		right := fmt.Sprintf("%s:%09d:%09d", ordered[j].file, ordered[j].startLine, ordered[j].startCol)
		return left < right
	})
	result := CoverageSummary{Uncovered: make([]SourceRange, 0)}
	for _, block := range ordered {
		if block.statements > uint64(maxPlatformInt()) || result.TotalStatements > maxPlatformInt()-int(block.statements) {
			return CoverageSummary{}, nil, fmt.Errorf("changed coverage statement total exceeds platform integer range")
		}
		statements := int(block.statements)
		result.TotalStatements += statements
		if block.covered {
			result.CoveredStatements += statements
			continue
		}
		result.Uncovered = append(result.Uncovered, SourceRange{
			File: block.file, StartLine: block.startLine, StartCol: block.startCol,
			EndLine: block.endLine, EndCol: block.endCol, Statements: statements,
		})
	}
	if result.TotalStatements > 0 {
		result.Percent = 100 * float64(result.CoveredStatements) / float64(result.TotalStatements)
	} else if len(changed) > 0 {
		uncertainties = append(uncertainties, Uncertainty{
			Code:      "changed_coverage_unmapped",
			Message:   "no executable coverage blocks intersected the added or modified Go source ranges",
			Locations: make([]Location, 0),
		})
	}
	return result, uncertainties, nil
}

func (e *Engine) coverageFile(targets []ExecutionTarget, file string) (string, error) {
	if filepath.IsAbs(file) {
		if relative, err := e.workspace.Relative(file); err == nil {
			return relative, nil
		}
		return "", fmt.Errorf("coverage file could not be mapped into the configured workspace")
	}
	if relative, err := e.workspace.Relative(filepath.FromSlash(file)); err == nil {
		return relative, nil
	}
	for _, target := range targets {
		prefix := strings.TrimSuffix(target.ID, "/") + "/"
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		candidate := filepath.Join(target.Dir, filepath.FromSlash(strings.TrimPrefix(file, prefix)))
		if relative, err := e.workspace.Relative(candidate); err == nil {
			return relative, nil
		}
	}
	return "", fmt.Errorf("coverage file could not be mapped into the configured workspace")
}

func coverageIntersects(block parser.CoverageBlock, ranges []LineRange) bool {
	for _, changed := range ranges {
		if block.StartLine <= changed.End && block.EndLine >= changed.Start {
			return true
		}
	}
	return false
}

func maxPlatformInt() int { return int(^uint(0) >> 1) }
