package audit

import (
	"context"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/analysis/astutil"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

func Run(ctx context.Context, ws, pattern string, analyzers []*analysis.Analyzer) (result finding.AuditResult, err error) {
	started := time.Now()
	defer func() {
		if r := recover(); r != nil {
			result = finding.AuditResult{}
			err = fmt.Errorf("analyzing %s: panic in analyzer predicate: %v", pattern, r)
		}
	}()

	cfg := &packages.Config{
		Context:    ctx,
		Dir:        ws,
		Env:        closedWorldEnv(os.Environ()),
		BuildFlags: []string{"-mod=readonly"},
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedTypesSizes | packages.NeedTypesInfo | packages.NeedSyntax,
		Tests: true,
	}
	pkgs, loadErr := packages.Load(cfg, pattern)
	if loadErr != nil {
		return finding.AuditResult{}, fmt.Errorf("loading packages for %s: %w", pattern, loadErr)
	}
	if err := packageErrors(pkgs); err != nil {
		return finding.AuditResult{}, fmt.Errorf("loading packages for %s: %w", pattern, err)
	}
	pkgs = dedupeTestVariants(pkgs)
	if err := ctx.Err(); err != nil {
		return finding.AuditResult{}, fmt.Errorf("running analysis for %s: %w", pattern, err)
	}

	graph, analyzeErr := checker.Analyze(analyzers, pkgs, &checker.Options{Sequential: true})
	if analyzeErr != nil {
		return finding.AuditResult{}, fmt.Errorf("running analysis for %s: %w", pattern, analyzeErr)
	}
	if err := ctx.Err(); err != nil {
		return finding.AuditResult{}, fmt.Errorf("running analysis for %s: %w", pattern, err)
	}
	result = collect(graph, ws, pkgs)
	if err := ctx.Err(); err != nil {
		return finding.AuditResult{}, fmt.Errorf("collecting analysis for %s: %w", pattern, err)
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result, nil
}

func closedWorldEnv(env []string) []string {
	overrides := []string{"GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local"}
	filtered := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		keep := true
		for _, override := range overrides {
			key := strings.SplitN(override, "=", 2)[0] + "="
			if strings.HasPrefix(entry, key) {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, overrides...)
}

func dedupeTestVariants(pkgs []*packages.Package) []*packages.Package {
	filtered := make([]*packages.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		if pkg == nil || (pkg.Name == "main" && strings.HasSuffix(pkg.ID, ".test")) {
			continue
		}
		filtered = append(filtered, pkg)
	}
	return filtered
}

func packageErrors(pkgs []*packages.Package) error {
	for _, pkg := range pkgs {
		if len(pkg.Errors) != 0 {
			return fmt.Errorf("%s: %s", pkg.PkgPath, pkg.Errors[0].Msg)
		}
	}
	return nil
}

func collect(graph *checker.Graph, ws string, pkgs []*packages.Package) finding.AuditResult {
	result := finding.AuditResult{Findings: []finding.Finding{}, CountsBySeverity: map[finding.Severity]int{}}
	seen := make(map[string]struct{})
	for _, action := range graph.Roots {
		if action == nil || !action.IsRoot || action.Package == nil {
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			location, ok := diagnosticLocation(action.Package, ws, diagnostic.Pos)
			if !ok {
				continue
			}
			if isAugmentedTestVariant(action.Package) && !strings.HasSuffix(location.File, "_test.go") {
				continue
			}
			key := fmt.Sprintf("%s\x00%s\x00%s:%d:%d", diagnostic.Category, diagnostic.Message, location.File, location.Line, location.Col)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			severity := astutil.RuleSeverity(diagnostic.Category)
			suggestion := ""
			if len(diagnostic.SuggestedFixes) > 0 {
				suggestion = workspaceRelativeText(diagnostic.SuggestedFixes[0].Message, ws)
			}
			result.Findings = append(result.Findings, finding.Finding{
				Rule: diagnostic.Category, RuleName: astutil.RuleName(diagnostic.Category), Severity: severity,
				Location: location, Message: workspaceRelativeText(diagnostic.Message, ws), Suggestion: suggestion,
			})
		}
	}
	sort.Slice(result.Findings, func(i, j int) bool {
		a, b := result.Findings[i], result.Findings[j]
		if a.Location.File != b.Location.File {
			return a.Location.File < b.Location.File
		}
		if a.Location.Line != b.Location.Line {
			return a.Location.Line < b.Location.Line
		}
		if a.Location.Col != b.Location.Col {
			return a.Location.Col < b.Location.Col
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
	result.Total = len(result.Findings)
	for _, item := range result.Findings {
		result.CountsBySeverity[item.Severity]++
	}
	result.FilesScanned = astutil.FilesScanned(pkgs)
	return result
}

func workspaceRelativeText(value, ws string) string {
	root, err := filepath.Abs(ws)
	if err != nil {
		return value
	}
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		roots = append(roots, resolved)
	}
	for _, candidate := range roots {
		value = strings.ReplaceAll(value, candidate+string(filepath.Separator), "")
	}
	return value
}

func isAugmentedTestVariant(pkg *packages.Package) bool {
	return pkg != nil && strings.Contains(pkg.ID, " [") && strings.HasSuffix(pkg.ID, ".test]")
}

func diagnosticLocation(pkg *packages.Package, ws string, pos token.Pos) (finding.Location, bool) {
	p := pkg.Fset.PositionFor(pos, false)
	if p.Filename == "" {
		return finding.Location{}, false
	}
	abs, err := filepath.Abs(p.Filename)
	if err != nil {
		return finding.Location{}, false
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return finding.Location{}, false
	}
	root, err := filepath.Abs(ws)
	if err != nil {
		return finding.Location{}, false
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return finding.Location{}, false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return finding.Location{}, false
	}
	return finding.Location{File: filepath.ToSlash(rel), Line: p.Line, Col: p.Column}, true
}
