// Package changeimpact discovers the final local repository snapshot and the
// conservative Go package closure affected by it.
package changeimpact

import (
	"context"
	"fmt"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

const (
	defaultPackagePattern = "./..."
	defaultMaxPackages    = 200
	maximumMaxPackages    = 500
)

// Options is the verification engine's change-discovery request.
type Options = verification.ChangeOptions

// File is one source file passed from discovery into verification.
type File = verification.SourceFile

// Package is one executable unit passed from discovery into verification.
type Package = verification.ExecutionTarget

// Analysis is the complete discovery result consumed by verification.
type Analysis = verification.ChangeAnalysis

// Analyzer owns Git and Go discovery within one configured workspace.
type Analyzer struct {
	workspace *workspace.Workspace
	runner    *execution.Runner
}

// New constructs an analyzer from the shared containment and execution
// infrastructure.
func New(ws *workspace.Workspace, runner *execution.Runner) (*Analyzer, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	return &Analyzer{workspace: ws, runner: runner}, nil
}

// Analyze discovers one coherent final-worktree snapshot and its package
// impact. Protocol adapters should treat returned errors as request failures.
func (a *Analyzer) Analyze(ctx context.Context, options Options) (Analysis, error) {
	if err := normalizeOptions(&options); err != nil {
		return Analysis{}, err
	}
	callCtx, cancel := a.runner.Deadline(ctx)
	defer cancel()

	analysis, err := a.snapshot(callCtx, options)
	if err != nil {
		return Analysis{}, err
	}
	analysis, err = a.computeImpact(callCtx, analysis, options)
	if err != nil {
		return Analysis{}, err
	}
	risks, uncertainties, err := assessRisk(analysis)
	if err != nil {
		return Analysis{}, err
	}
	analysis.Risks = risks
	analysis.Uncertainties = append(analysis.Uncertainties, uncertainties...)
	return analysis, nil
}

func normalizeOptions(options *Options) error {
	if options.Base == "" {
		return fmt.Errorf("base is required")
	}
	if options.Package == "" {
		options.Package = defaultPackagePattern
	}
	if options.MaxPackages == 0 {
		options.MaxPackages = defaultMaxPackages
	}
	if options.MaxPackages < 1 || options.MaxPackages > maximumMaxPackages {
		return fmt.Errorf("max packages must be between 1 and %d", maximumMaxPackages)
	}
	return nil
}
