package verification

import (
	"fmt"

	"github.com/agentic-mcps/go/internal/parser"
)

func (e *Engine) raceFindings(report parser.RaceReportOutput) []Finding {
	findings := make([]Finding, 0, len(report.Conflicts))
	for _, conflict := range report.Conflicts {
		finding := Finding{
			Kind: "go.race", Severity: SeverityError, CheckID: "race",
			Message: fmt.Sprintf("data race between %s and %s", conflict.Current.Function, conflict.Previous.Function),
		}
		if conflict.Current.Location.File != "" {
			if file, err := e.workspace.Relative(conflict.Current.Location.File); err == nil {
				finding.Location = &Location{File: file, Line: conflict.Current.Location.Line, Col: conflict.Current.Location.Col}
			}
		}
		findings = append(findings, finding)
	}
	return findings
}
