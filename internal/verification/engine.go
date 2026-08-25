package verification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

const (
	defaultVerificationPackage = "./..."
	defaultVerificationLimit   = 200
	maximumVerificationLimit   = 500
)

// Request configures one complete verification run independently of its CLI
// or MCP adapter.
type Request struct {
	Base               string
	Package            string
	Race               bool
	FailOn             FailOn
	MinChangedCoverage *float64
	MaxPackages        int
}

// Engine assembles one portable report from injected change discovery and the
// shared contained execution infrastructure.
type Engine struct {
	workspace       *workspace.Workspace
	runner          *execution.Runner
	changeAnalyzer  ChangeAnalyzer
	providerVersion string
}

// NewEngine constructs the deep verification module.
func NewEngine(ws *workspace.Workspace, runner *execution.Runner, analyzer ChangeAnalyzer, providerVersion string) (*Engine, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	if analyzer == nil {
		return nil, fmt.Errorf("change analyzer is nil")
	}
	if strings.TrimSpace(providerVersion) == "" {
		return nil, fmt.Errorf("provider version is empty")
	}
	return &Engine{workspace: ws, runner: runner, changeAnalyzer: analyzer, providerVersion: providerVersion}, nil
}

// Verify discovers impact, executes the selected evidence once, and evaluates
// the report policy. Caller cancellation and the shared deadline remain request
// errors; ordinary check failures are represented in the report.
func (e *Engine) Verify(ctx context.Context, request Request) (Report, error) {
	if err := normalizeRequest(&request); err != nil {
		return Report{}, err
	}
	callCtx, cancel := e.runner.Deadline(ctx)
	defer cancel()

	analysis, err := e.changeAnalyzer.Analyze(callCtx, ChangeOptions{
		Base: request.Base, Package: request.Package, MaxPackages: request.MaxPackages,
	})
	if err != nil {
		return Report{}, err
	}
	report := NewReport(e.providerVersion, analysis.Repository)
	report.Change = analysis.Change
	report.Impact = analysis.Impact
	report.Risks = append(report.Risks, analysis.Risks...)
	report.Uncertainties = append(report.Uncertainties, analysis.Uncertainties...)
	targets := executionTargetIDs(analysis.Packages)
	direct := directTargetIDs(analysis.Packages)
	report.Plan = verificationPlan(targets, request.Race)

	if analysis.Complete {
		outcome, runErr := e.runAffectedChecks(callCtx, analysis, request, direct)
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return Report{}, runErr
			}
			roots := []string{e.workspace.Root()}
			if cache, cacheErr := os.UserCacheDir(); cacheErr == nil {
				roots = append(roots, cache)
			}
			report.Evidence = append(report.Evidence, failedExecutionEvidence(executionChecks(targets, request.Race), runErr, roots...)...)
		} else {
			report.Evidence = append(report.Evidence, outcome.Evidence...)
			report.Findings = append(report.Findings, outcome.Findings...)
			report.Uncertainties = append(report.Uncertainties, outcome.Uncertainties...)
		}
		analyzerOutcome, analyzerErr := e.runAnalyzerChecks(callCtx, analysis)
		if analyzerErr != nil {
			if errors.Is(analyzerErr, context.Canceled) || errors.Is(analyzerErr, context.DeadlineExceeded) {
				return Report{}, analyzerErr
			}
			return Report{}, fmt.Errorf("running analyzer comparison: %w", analyzerErr)
		}
		report.Evidence = append(report.Evidence, analyzerOutcome.Evidence...)
		report.Findings = append(report.Findings, analyzerOutcome.Findings...)
		report.Uncertainties = append(report.Uncertainties, analyzerOutcome.Uncertainties...)
	}
	if request.MinChangedCoverage != nil {
		applyCoveragePolicy(&report, *request.MinChangedCoverage)
	}
	if err := report.Finalize(Policy{FailOn: request.FailOn, MinChangedCoverage: request.MinChangedCoverage}); err != nil {
		return Report{}, err
	}
	return report, nil
}

func normalizeRequest(request *Request) error {
	if strings.TrimSpace(request.Base) == "" {
		return fmt.Errorf("base is required")
	}
	if request.Package == "" {
		request.Package = defaultVerificationPackage
	}
	if request.FailOn == "" {
		request.FailOn = FailOnError
	}
	if request.MaxPackages == 0 {
		request.MaxPackages = defaultVerificationLimit
	}
	if request.MaxPackages < 1 || request.MaxPackages > maximumVerificationLimit {
		return fmt.Errorf("max packages must be between 1 and %d", maximumVerificationLimit)
	}
	policy := Policy{FailOn: request.FailOn, MinChangedCoverage: request.MinChangedCoverage}
	return policy.normalize()
}

func verificationPlan(targets []string, race bool) []Check {
	checks := executionChecks(targets, race)
	checks = append(checks,
		Check{ID: "concurrency", Kind: CheckConcurrency, Required: true, Targets: append([]string(nil), targets...), Reason: "compare calibrated concurrency findings with the merge-base"},
		Check{ID: "errors", Kind: CheckErrors, Required: true, Targets: append([]string(nil), targets...), Reason: "compare calibrated error-handling findings with the merge-base"},
	)
	return checks
}

func executionChecks(targets []string, race bool) []Check {
	checks := []Check{
		{ID: "tests", Kind: CheckTests, Required: true, Targets: append([]string(nil), targets...), Reason: "run package tests for the complete affected closure"},
		{ID: "coverage", Kind: CheckCoverage, Required: true, Targets: append([]string(nil), targets...), Reason: "measure executed statements intersecting changed source"},
	}
	if race {
		checks = append(checks, Check{ID: "race", Kind: CheckRace, Required: true, Targets: append([]string(nil), targets...), Reason: "race detection was explicitly requested"})
	}
	return checks
}

func executionTargetIDs(targets []ExecutionTarget) []string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	sort.Strings(ids)
	return ids
}

func directTargetIDs(targets []ExecutionTarget) []string {
	ids := make([]string, 0)
	for _, target := range targets {
		if target.Distance == 0 {
			ids = append(ids, target.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func failedExecutionEvidence(plan []Check, err error, roots ...string) []Evidence {
	result := make([]Evidence, 0, len(plan))
	for _, check := range plan {
		result = append(result, Evidence{
			CheckID: check.ID, Kind: check.Kind, Status: EvidenceError,
			Summary: "check could not produce trustworthy evidence", Error: portableCheckError(err, roots...),
		})
	}
	return result
}

func applyCoveragePolicy(report *Report, minimum float64) {
	for _, evidence := range report.Evidence {
		if evidence.Kind != CheckCoverage || evidence.Status == EvidenceError || evidence.Coverage == nil || evidence.Coverage.TotalStatements == 0 {
			continue
		}
		if evidence.Coverage.Percent >= minimum {
			return
		}
		report.Findings = append(report.Findings, Finding{
			Kind: "coverage.policy", Severity: SeverityError, CheckID: evidence.CheckID,
			Message: fmt.Sprintf("changed statement coverage %.1f%% is below the configured %.1f%% minimum", evidence.Coverage.Percent, minimum),
		})
		return
	}
}
