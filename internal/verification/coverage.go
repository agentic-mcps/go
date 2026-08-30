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
		if filepath.Ext(file.Change.Path) != ".go" || strings.HasSuffix(file.Change.Path, "_test.go") || file.Change.Change == ChangeDeleted || len(file.Change.CurrentRanges) == 0 {
			continue
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
		if changed {
			return true
		}
	}
	return false
}

func (e *Engine) changedCoverage(analysis ChangeAnalysis, blocks []parser.CoverageBlock) (CoverageSummary, []Uncertainty, error) {
	changed := make(map[string][]LineRange)
	for _, file := range analysis.Files {
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
