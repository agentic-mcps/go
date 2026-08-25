package verification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	concurrencyanalysis "github.com/ashwingopalsamy/agentic-go/internal/analysis/concurrency"
	erroranalysis "github.com/ashwingopalsamy/agentic-go/internal/analysis/errors"
	"github.com/ashwingopalsamy/agentic-go/internal/audit"
	"github.com/ashwingopalsamy/agentic-go/internal/finding"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
	"golang.org/x/tools/go/analysis"
)

type analyzerSpec struct {
	analyzer *analysis.Analyzer
	checkID  string
	kind     CheckKind
	label    string
}

var analyzerSpecs = []analyzerSpec{
	{checkID: "concurrency", kind: CheckConcurrency, label: "concurrency", analyzer: concurrencyanalysis.Analyzer},
	{checkID: "errors", kind: CheckErrors, label: "error", analyzer: erroranalysis.Analyzer},
}

func (e *Engine) runAnalyzerChecks(ctx context.Context, change ChangeAnalysis) (executionOutcome, error) {
	if len(change.Packages) == 0 {
		evidence := make([]Evidence, 0, len(analyzerSpecs))
		for _, spec := range analyzerSpecs {
			evidence = append(evidence, Evidence{
				CheckID: spec.checkID, Kind: spec.kind, Status: EvidenceSkipped,
				Summary: "no active affected packages to analyze",
			})
		}
		return executionOutcome{Evidence: evidence, Findings: []Finding{}, Uncertainties: []Uncertainty{}}, nil
	}
	targets := executionTargetIDs(change.Packages)
	var runDir string
	var baseWorkspace *workspace.Workspace
	var baseTargets []string
	var baselineSetupErr error
	runDir, baselineSetupErr = createVerificationRunDir("baseline")
	if baselineSetupErr == nil {
		defer func() { _ = os.RemoveAll(runDir) }()
		var baseRoot string
		baseRoot, baselineSetupErr = e.changeAnalyzer.MaterializeBase(ctx, change.Repository, runDir)
		if baselineSetupErr == nil {
			baseWorkspace, baselineSetupErr = workspace.Open(ctx, baseRoot)
			if baselineSetupErr != nil {
				baselineSetupErr = fmt.Errorf("opening materialized merge-base: %w", baselineSetupErr)
			}
		}
		if baselineSetupErr == nil {
			baseTargets, baselineSetupErr = e.basePackagePatterns(baseWorkspace.Root(), change)
		}
	}
	if errors.Is(baselineSetupErr, context.Canceled) || errors.Is(baselineSetupErr, context.DeadlineExceeded) {
		return executionOutcome{}, baselineSetupErr
	}
	outcome := executionOutcome{Evidence: make([]Evidence, 0, len(analyzerSpecs)), Findings: []Finding{}, Uncertainties: []Uncertainty{}}
	for _, spec := range analyzerSpecs {
		current, currentErr := e.runAudit(ctx, e.workspace.Root(), targets, spec.analyzer)
		if currentErr != nil && (errors.Is(currentErr, context.Canceled) || errors.Is(currentErr, context.DeadlineExceeded)) {
			return executionOutcome{}, currentErr
		}
		base := finding.AuditResult{}
		baseErr := baselineSetupErr
		if baseErr == nil {
			base, baseErr = e.runAudit(ctx, baseWorkspace.Root(), baseTargets, spec.analyzer)
		}
		if baseErr != nil && (errors.Is(baseErr, context.Canceled) || errors.Is(baseErr, context.DeadlineExceeded)) {
			return executionOutcome{}, baseErr
		}
		if currentErr != nil || baseErr != nil {
			combined := errors.Join(currentErr, baseErr)
			summary := AnalysisSummary{Base: len(base.Findings), Current: len(current.Findings)}
			if currentErr == nil && baseErr != nil {
				summary.Unknown = len(current.Findings)
			}
			roots := []string{e.workspace.Root(), runDir}
			if baseWorkspace != nil {
				roots = append(roots, baseWorkspace.Root())
			}
			if cache, cacheErr := os.UserCacheDir(); cacheErr == nil {
				roots = append(roots, cache)
			}
			outcome.Evidence = append(outcome.Evidence, Evidence{
				CheckID: spec.checkID, Kind: spec.kind, Status: EvidenceError,
				Summary: spec.label + " analyzer baseline could not be compared",
				Error:   portableCheckError(combined, roots...), Analysis: &summary,
			})
			outcome.Uncertainties = append(outcome.Uncertainties, unavailableAnalyzerUncertainties(spec, current.Findings, currentErr, baseErr, roots...)...)
			continue
		}
		comparison := compareAnalyzerFindings(spec.checkID, base.Findings, current.Findings, change.Files)
		outcome.Evidence = append(outcome.Evidence, Evidence{
			CheckID: spec.checkID, Kind: spec.kind, Status: EvidencePassed,
			DurationMS: current.DurationMS + base.DurationMS,
			Summary:    fmt.Sprintf("%d introduced, %d existing, %d resolved, %d unknown", comparison.Summary.Introduced, comparison.Summary.Existing, comparison.Summary.Resolved, comparison.Summary.Unknown),
			Analysis:   &comparison.Summary,
		})
		outcome.Findings = append(outcome.Findings, comparison.Introduced...)
		outcome.Uncertainties = append(outcome.Uncertainties, comparison.Uncertainties...)
	}
	sort.Slice(outcome.Evidence, func(i, j int) bool { return outcome.Evidence[i].CheckID < outcome.Evidence[j].CheckID })
	return outcome, nil
}

func unavailableAnalyzerUncertainties(spec analyzerSpec, current []finding.Finding, currentErr, baseErr error, roots ...string) []Uncertainty {
	result := make([]Uncertainty, 0, len(current)+2)
	if currentErr != nil {
		result = append(result, Uncertainty{
			Code: "analysis_unavailable", CheckID: spec.checkID,
			Message:   spec.label + " analyzer could not inspect the current snapshot",
			Locations: make([]Location, 0),
		})
	}
	if baseErr == nil {
		return result
	}
	result = append(result, Uncertainty{
		Code: "baseline_unavailable", CheckID: spec.checkID,
		Message:   spec.label + " analyzer could not establish a merge-base comparison; current diagnostics are not classified as introduced",
		Locations: make([]Location, 0),
	})
	if currentErr != nil {
		return result
	}
	for _, item := range current {
		result = append(result, Uncertainty{
			Code: "baseline_unknown", CheckID: spec.checkID,
			Message:   fmt.Sprintf("%s: %s", item.Rule, portableReportText(item.Message, roots...)),
			Locations: []Location{findingLocation(item.Location)},
		})
	}
	return result
}

func (e *Engine) runAudit(ctx context.Context, root string, targets []string, analyzer *analysis.Analyzer) (finding.AuditResult, error) {
	if len(targets) == 0 {
		return finding.AuditResult{Findings: []finding.Finding{}, CountsBySeverity: map[finding.Severity]int{}}, nil
	}
	release, err := e.runner.Permit(ctx)
	if err != nil {
		return finding.AuditResult{}, err
	}
	defer release()
	return audit.RunPatterns(ctx, root, targets, []*analysis.Analyzer{analyzer})
}

func (e *Engine) basePackagePatterns(baseRoot string, change ChangeAnalysis) ([]string, error) {
	patterns := make([]string, 0, len(change.Packages))
	seen := make(map[string]struct{})
	for _, target := range change.Packages {
		relative, err := e.workspace.Relative(target.Dir)
		if err != nil {
			return nil, fmt.Errorf("mapping current package %q: %w", target.ID, err)
		}
		baseDirectory := filepath.Join(baseRoot, filepath.FromSlash(relative))
		file, found, err := firstGoFile(baseDirectory)
		if err != nil {
			return nil, err
		}
		if !found {
			for _, source := range change.Files {
				if source.Change.PreviousPath == "" || filepath.ToSlash(filepath.Dir(filepath.FromSlash(source.Change.Path))) != relative {
					continue
				}
				previousDirectory := filepath.Join(baseRoot, filepath.Dir(filepath.FromSlash(source.Change.PreviousPath)))
				file, found, err = firstGoFile(previousDirectory)
				if err != nil {
					return nil, err
				}
				if found {
					break
				}
			}
		}
		if !found {
			continue
		}
		pattern := "file=" + file
		if _, duplicate := seen[pattern]; duplicate {
			continue
		}
		seen[pattern] = struct{}{}
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns, nil
}

func firstGoFile(directory string) (string, bool, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading materialized base package: %w", err)
	}
	fallback := ""
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return path, true, nil
		}
		if fallback == "" {
			fallback = path
		}
	}
	return fallback, fallback != "", nil
}

func portableCheckError(err error, roots ...string) string {
	if err == nil {
		return ""
	}
	return boundedText(portableReportText(err.Error(), roots...), 1024)
}

func portableReportText(value string, roots ...string) string {
	for _, root := range roots {
		if root == "" {
			continue
		}
		clean := filepath.Clean(root)
		value = strings.ReplaceAll(value, clean+string(os.PathSeparator), "")
		value = strings.ReplaceAll(value, clean, ".")
	}
	return value
}
