package intelligence

import (
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/verification"
)

func TestProjectFocusAction(t *testing.T) {
	base := focusActionFixture
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
		{name: "legacy-like result", setup: func(result *FocusResult, report **verification.Report) {
			result.Verification.Applicable = false
			*report = &verification.Report{Result: verification.PolicyResult{Status: verification.ResultPass}}
		}, want: verificationNeeded},
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

func TestProjectFocusActionCauseSpecificGuidance(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*FocusResult, **verification.Report)
		wantKind   focusActionKind
		wantNext   string
		wantReason []string
	}{
		{
			name: "missing verification",
			setup: func(result *FocusResult, report **verification.Report) {
				result.Verification.Applicable = false
				*report = nil
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
			},
		},
		{
			name: "missing verification with focus byte budget omission",
			setup: func(result *FocusResult, report **verification.Report) {
				result.Verification.Applicable = false
				result.Context = &FocusContext{Truncated: true, EvidenceStates: []EvidenceState{{Facet: "related_tests", State: "examined_and_absent"}}}
				*report = nil
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy; make a fresh narrower selection or use a larger max_bytes on a new selection; replacement refresh inherits the prior budget and cannot fix the omission",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
				"focused context detail was omitted by the requested byte budget",
			},
		},
		{
			name: "missing verification with unavailable focus evidence",
			setup: func(result *FocusResult, report **verification.Report) {
				result.Verification.Applicable = false
				result.Context = &FocusContext{EvidenceStates: []EvidenceState{{Facet: "declaration_relationships", State: "unavailable"}}}
				*report = nil
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy; treat affected evidence as unavailable; retry recovery is not established",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
				"affected focus evidence is unavailable, unknown, or internally inconsistent",
			},
		},
		{
			name: "snapshot mismatch requests verification without reselection",
			setup: func(result *FocusResult, _ **verification.Report) {
				result.Verification.Applicable = false
				result.Verification.Reasons = []string{"workspace snapshot differs"}
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
			},
		},
		{
			name: "legacy report without applicability metadata",
			setup: func(result *FocusResult, report **verification.Report) {
				result.Verification.Applicable = false
				result.Verification.Reasons = []string{"stored report has no applicability metadata"}
				(*report).Result.Status = verification.ResultPass
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
			},
		},
		{
			name: "policy mismatch",
			setup: func(result *FocusResult, _ **verification.Report) {
				result.Verification.Applicable = false
				result.Verification.Reasons = []string{"requested verification check policy differs"}
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
			},
		},
		{
			name: "narrower verification scope",
			setup: func(result *FocusResult, _ **verification.Report) {
				result.Verification.Applicable = false
				result.Verification.Reasons = []string{"package scope differs"}
			},
			wantKind: verificationNeeded,
			wantNext: "request verification for the current snapshot and matching policy; a different package scope answers a different question and does not support the requested scope",
			wantReason: []string{
				"applicable verification evidence for the current snapshot and matching policy is unavailable",
				"a different package scope answers a different question and does not support the requested scope",
			},
		},
		{
			name: "focus byte budget omission",
			setup: func(result *FocusResult, _ **verification.Report) {
				result.Context = &FocusContext{Truncated: true, EvidenceStates: []EvidenceState{{Facet: "related_tests", State: "examined_and_absent"}}}
			},
			wantKind: evidenceUnavailable,
			wantNext: "make a fresh narrower selection or use a larger max_bytes on a new selection; replacement refresh inherits the prior budget and cannot fix the omission",
			wantReason: []string{
				"focused context detail was omitted by the requested byte budget",
			},
		},
		{
			name: "verification report detail truncation",
			setup: func(_ *FocusResult, report **verification.Report) {
				(*report).Change.FilesTruncated = true
			},
			wantKind: evidenceUnavailable,
			wantNext: "treat omitted verification report detail as unavailable; refreshing cannot recover the omitted detail",
			wantReason: []string{
				"verification report detail was truncated; refreshing cannot recover the omitted detail",
			},
		},
		{
			name: "pass with advisory finding",
			setup: func(_ *FocusResult, report **verification.Report) {
				(*report).Findings = []verification.Finding{{Location: &verification.Location{File: "pkg/file.go", Line: 3}}}
			},
			wantKind: findingInspectionNeeded,
			wantNext: "inspect the reported finding locations",
			wantReason: []string{
				"verification reported findings with usable locations",
			},
		},
		{
			name: "pass outcome contradicts blocking findings",
			setup: func(_ *FocusResult, report **verification.Report) {
				(*report).Result.BlockingFindings = 1
			},
			wantKind: evidenceUnavailable,
			wantNext: "treat affected evidence as unavailable; retry recovery is not established",
			wantReason: []string{
				"affected verification or focus evidence is unavailable, unknown, or internally inconsistent",
				"pass outcome contradicts blocking findings",
			},
		},
		{
			name: "findings outcome without retained findings",
			setup: func(_ *FocusResult, report **verification.Report) {
				(*report).Result.Status = verification.ResultFindings
			},
			wantKind: evidenceUnavailable,
			wantNext: "treat affected evidence as unavailable; retry recovery is not established",
			wantReason: []string{
				"affected verification or focus evidence is unavailable, unknown, or internally inconsistent",
				"findings outcome has no retained findings",
			},
		},
		{
			name: "mixed truncation and unavailable evidence",
			setup: func(result *FocusResult, report **verification.Report) {
				result.Context = &FocusContext{Truncated: true, EvidenceStates: []EvidenceState{{Facet: "budgeted_evidence", State: "gathered_but_omitted"}, {Facet: "declaration_relationships", State: "unavailable"}}}
				(*report).FindingsTruncated = true
			},
			wantKind: evidenceUnavailable,
			wantNext: "make a fresh narrower selection or use a larger max_bytes on a new selection; replacement refresh inherits the prior budget and cannot fix the omission; treat omitted verification report detail as unavailable; refreshing cannot recover the omitted detail; treat affected evidence as unavailable; retry recovery is not established",
			wantReason: []string{
				"focused context detail was omitted by the requested byte budget",
				"verification report detail was truncated; refreshing cannot recover the omitted detail",
				"affected verification or focus evidence is unavailable, unknown, or internally inconsistent",
			},
		},
		{
			name: "stale focus selection",
			setup: func(result *FocusResult, _ **verification.Report) {
				result.Refresh = &FocusRefresh{Status: "selection_required"}
				result.Context = &FocusContext{EvidenceStates: []EvidenceState{{Facet: "previous_declaration", State: "unavailable"}}}
			},
			wantKind: evidenceUnavailable,
			wantNext: "make a fresh selection against the current snapshot, then request verification for that snapshot and matching policy; treat affected evidence as unavailable; retry recovery is not established",
			wantReason: []string{
				"the previous focus selection could not be resolved and requires a fresh selection",
				"affected verification or focus evidence is unavailable, unknown, or internally inconsistent",
			},
		},
		{
			name: "passing checks do not complete the task",
			setup: func(result *FocusResult, _ **verification.Report) {
				result.Context = &FocusContext{EvidenceStates: []EvidenceState{{Facet: "related_tests", State: "examined_and_absent"}}}
			},
			wantKind: requestedChecksPassedWithLimits,
			wantNext: "review the requested checks and their stated limits",
			wantReason: []string{
				"requested checks passed; this does not establish task completion",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, report := focusActionFixture()
			tt.setup(result, &report)
			got := projectFocusAction(*result, report)
			if got.Kind != tt.wantKind || got.NextAction != tt.wantNext || strings.Join(got.Reasons, "\n") != strings.Join(tt.wantReason, "\n") {
				t.Fatalf("action = %#v, want kind %q, next action %q, reasons %q", got, tt.wantKind, tt.wantNext, strings.Join(tt.wantReason, "\n"))
			}
		})
	}
}

func focusActionFixture() (*FocusResult, *verification.Report) {
	result := &FocusResult{Verification: VerificationApplicability{Applicable: true}, Complete: true}
	report := &verification.Report{
		Plan:     []verification.Check{{ID: "tests", Kind: verification.CheckTests, Required: true}},
		Evidence: []verification.Evidence{{CheckID: "tests", Kind: verification.CheckTests, Status: verification.EvidencePassed, Tests: &verification.TestSummary{Packages: []verification.TestPackageSummary{}, Nonpassing: []verification.TestCaseSummary{}}}},
		Findings: []verification.Finding{}, Result: verification.PolicyResult{Status: verification.ResultPass},
	}
	return result, report
}
