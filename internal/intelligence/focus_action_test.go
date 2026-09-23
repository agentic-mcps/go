package intelligence

import (
	"testing"

	"github.com/agentic-mcps/go/internal/verification"
)

func TestProjectFocusAction(t *testing.T) {
	base := func() (*FocusResult, *verification.Report) {
		result := &FocusResult{Verification: VerificationApplicability{Applicable: true}, Complete: true}
		report := &verification.Report{
			Plan:     []verification.Check{{ID: "tests", Kind: verification.CheckTests, Required: true}},
			Evidence: []verification.Evidence{{CheckID: "tests", Kind: verification.CheckTests, Status: verification.EvidencePassed, Tests: &verification.TestSummary{Packages: []verification.TestPackageSummary{}, Nonpassing: []verification.TestCaseSummary{}}}},
			Findings: []verification.Finding{}, Result: verification.PolicyResult{Status: verification.ResultPass},
		}
		return result, report
	}
	validContext := func(result *FocusResult) {
		result.Context = &FocusContext{EvidenceStates: []EvidenceState{{Facet: "related_tests", State: "examined_and_absent"}}, TypedEvidence: &TypedEvidence{Complete: true}}
	}

	tests := []struct {
		name  string
		setup func(*FocusResult, **verification.Report)
		want  focusActionKind
	}{
		{name: "no report", setup: func(_ *FocusResult, report **verification.Report) { *report = nil }, want: verificationNeeded},
		{name: "non-applicable report", setup: func(result *FocusResult, _ **verification.Report) { result.Verification.Applicable = false }, want: verificationNeeded},
		{name: "legacy-like result", setup: func(_ *FocusResult, report **verification.Report) {
			*report = &verification.Report{Result: verification.PolicyResult{Status: verification.ResultPass}}
		}, want: evidenceUnavailable},
		{name: "incomplete report", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Result.Status = verification.ResultIncomplete
		}, want: evidenceUnavailable},
		{name: "incomplete focus", setup: func(result *FocusResult, _ **verification.Report) { result.Complete = false }, want: evidenceUnavailable},
		{name: "uncertain focus", setup: func(result *FocusResult, _ **verification.Report) {
			result.Uncertainties = []verification.Uncertainty{{Code: "change.incomplete"}}
		}, want: evidenceUnavailable},
		{name: "truncated changed files", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Change.FilesTruncated = true
		}, want: evidenceUnavailable},
		{name: "truncated current changed files", setup: func(result *FocusResult, _ **verification.Report) {
			result.Change.FilesTruncated = true
		}, want: evidenceUnavailable},
		{name: "truncated declarations", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Change.DeclarationsTruncated = true
		}, want: evidenceUnavailable},
		{name: "truncated current declarations", setup: func(result *FocusResult, _ **verification.Report) {
			result.Change.DeclarationsTruncated = true
		}, want: evidenceUnavailable},
		{name: "truncated impact", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Impact.PackagesTruncated = true
		}, want: evidenceUnavailable},
		{name: "truncated current impact", setup: func(result *FocusResult, _ **verification.Report) {
			result.Impact.PackagesTruncated = true
		}, want: evidenceUnavailable},
		{name: "missing required evidence", setup: func(_ *FocusResult, report **verification.Report) { (*report).Evidence = nil }, want: evidenceUnavailable},
		{name: "duplicate required evidence", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Evidence = append((*report).Evidence, (*report).Evidence[0])
		}, want: evidenceUnavailable},
		{name: "missing optional evidence", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Plan = append((*report).Plan, verification.Check{ID: "race", Kind: verification.CheckRace})
		}, want: evidenceUnavailable},
		{name: "truncated check targets", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Plan[0].TargetsTruncated = true
		}, want: evidenceUnavailable},
		{name: "skipped optional evidence", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Plan = append((*report).Plan, verification.Check{ID: "race", Kind: verification.CheckRace})
			(*report).Evidence = append((*report).Evidence, verification.Evidence{CheckID: "race", Kind: verification.CheckRace, Status: verification.EvidenceSkipped})
		}, want: evidenceUnavailable},
		{name: "skipped evidence", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Evidence[0].Status = verification.EvidenceSkipped
		}, want: evidenceUnavailable},
		{name: "errored evidence", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Evidence[0].Status = verification.EvidenceError
		}, want: evidenceUnavailable},
		{name: "truncated evidence", setup: func(_ *FocusResult, report **verification.Report) { (*report).FindingsTruncated = true }, want: evidenceUnavailable},
		{name: "unknown analysis", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Evidence[0].Kind = verification.CheckErrors
			(*report).Evidence[0].Analysis = &verification.AnalysisSummary{Unknown: 1}
		}, want: evidenceUnavailable},
		{name: "mismatched evidence kind", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Evidence[0].Kind = verification.CheckErrors
			(*report).Evidence[0].Analysis = &verification.AnalysisSummary{}
		}, want: evidenceUnavailable},
		{name: "valid findings", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Findings = []verification.Finding{{Location: &verification.Location{File: "pkg/file.go", Line: 3}}}
			(*report).Result.Status = verification.ResultFindings
		}, want: findingInspectionNeeded},
		{name: "invalid finding locations", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Findings = []verification.Finding{{Location: &verification.Location{File: "../outside.go", Line: 3}}}
			(*report).Result.Status = verification.ResultFindings
		}, want: evidenceUnavailable},
		{name: "workspace directory finding location", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Findings = []verification.Finding{{Location: &verification.Location{File: ".", Line: 3}}}
			(*report).Result.Status = verification.ResultFindings
		}, want: evidenceUnavailable},
		{name: "negative column finding location", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Findings = []verification.Finding{{Location: &verification.Location{File: "pkg/file.go", Line: 3, Col: -1}}}
			(*report).Result.Status = verification.ResultFindings
		}, want: evidenceUnavailable},
		{name: "mixed finding locations", setup: func(_ *FocusResult, report **verification.Report) {
			(*report).Findings = []verification.Finding{
				{Location: &verification.Location{File: "pkg/file.go", Line: 3}},
				{Location: &verification.Location{File: "../outside.go", Line: 4}},
			}
			(*report).Result.Status = verification.ResultFindings
		}, want: evidenceUnavailable},
		{name: "unknown context state", setup: func(result *FocusResult, _ **verification.Report) {
			validContext(result)
			result.Context.EvidenceStates[0].State = "unknown"
		}, want: evidenceUnavailable},
		{name: "pass", setup: func(result *FocusResult, _ **verification.Report) { validContext(result) }, want: requestedChecksPassedWithLimits},
		{name: "valid typed and context evidence", setup: func(result *FocusResult, _ **verification.Report) {
			validContext(result)
			result.Context.TypedEvidence.Relationships = []GoRelationship{}
		}, want: requestedChecksPassedWithLimits},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, report := base()
			tt.setup(result, &report)
			got := projectFocusAction(*result, report)
			if got.Kind != tt.want {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.want)
			}
			if got.NextAction == "" || got.Reasons == nil || len(got.Reasons) == 0 {
				t.Fatalf("action lacks concise guidance: %#v", got)
			}
		})
	}
}
