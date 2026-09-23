package intelligence

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/agentic-mcps/go/internal/verification"
)

type focusActionKind string

const (
	verificationNeeded              focusActionKind = "verification_needed"
	findingInspectionNeeded         focusActionKind = "finding_inspection_needed"
	evidenceUnavailable             focusActionKind = "evidence_unavailable"
	requestedChecksPassedWithLimits focusActionKind = "requested_checks_passed_with_limits"
)

type focusAction struct {
	Kind       focusActionKind
	NextAction string
	Reasons    []string
}

func applyFocusAction(result *FocusResult, report *verification.Report) {
	if result == nil {
		return
	}
	action := projectFocusAction(*result, report)
	result.Verification.NextAction = action.NextAction
	result.Verification.Reasons = appendFocusActionReasons(result.Verification.Reasons, action.Reasons...)
}

func appendFocusActionReasons(existing []string, additions ...string) []string {
	result := append([]string{}, existing...)
	for _, addition := range additions {
		found := false
		for _, current := range result {
			if current == addition {
				found = true
				break
			}
		}
		if !found {
			result = append(result, addition)
		}
	}
	return result
}

func projectFocusAction(result FocusResult, report *verification.Report) focusAction {
	if !result.Verification.Applicable || report == nil {
		return focusAction{Kind: verificationNeeded, NextAction: "request verification for the current snapshot and policy", Reasons: []string{"applicable verification evidence is unavailable"}}
	}
	if incompleteFocusEvidence(result, report) {
		return focusAction{Kind: evidenceUnavailable, NextAction: "complete or refresh the requested evidence", Reasons: []string{"verification evidence is incomplete or unavailable"}}
	}
	if report.Result.Status == verification.ResultFindings {
		if len(report.Findings) == 0 {
			return focusAction{Kind: evidenceUnavailable, NextAction: "complete or refresh the requested evidence", Reasons: []string{"findings outcome has no retained findings"}}
		}
		for _, finding := range report.Findings {
			if finding.Location == nil || !validFocusLocation(*finding.Location) {
				return focusAction{Kind: evidenceUnavailable, NextAction: "refresh finding evidence with usable locations", Reasons: []string{"findings lack valid workspace-relative locations"}}
			}
		}
		return focusAction{Kind: findingInspectionNeeded, NextAction: "inspect the reported finding locations", Reasons: []string{"verification reported findings with usable locations"}}
	}
	if len(report.Findings) > 0 || report.Result.BlockingFindings > 0 {
		return focusAction{Kind: evidenceUnavailable, NextAction: "complete or refresh the requested evidence", Reasons: []string{"report findings do not match its finalized outcome"}}
	}
	if report.Result.Status == verification.ResultPass {
		return focusAction{Kind: requestedChecksPassedWithLimits, NextAction: "review the requested checks and their stated limits", Reasons: []string{"requested checks passed; this does not establish task completion"}}
	}
	return focusAction{Kind: evidenceUnavailable, NextAction: "complete or refresh the requested evidence", Reasons: []string{"report does not establish a complete finding or pass outcome"}}
}

func incompleteFocusEvidence(result FocusResult, report *verification.Report) bool {
	if !result.Complete || len(result.Uncertainties) > 0 ||
		result.Change.FilesTruncated || result.Change.DeclarationsTruncated || result.Impact.PackagesTruncated ||
		report.Result.Status == verification.ResultIncomplete || report.FindingsTruncated ||
		report.Change.FilesTruncated || report.Change.DeclarationsTruncated || report.Impact.PackagesTruncated {
		return true
	}
	planned := map[string]verification.Check{}
	for _, check := range report.Plan {
		if check.ID == "" || check.TargetsTruncated {
			return true
		}
		if _, exists := planned[check.ID]; exists {
			return true
		}
		planned[check.ID] = check
	}
	if len(planned) == 0 {
		return true
	}
	seen := map[string]int{}
	for _, evidence := range report.Evidence {
		check, exists := planned[evidence.CheckID]
		if evidence.CheckID == "" || !exists || evidence.Kind != check.Kind {
			return true
		}
		seen[evidence.CheckID]++
		if evidence.Status != verification.EvidencePassed && evidence.Status != verification.EvidenceFailed && evidence.Status != verification.EvidenceSkipped && evidence.Status != verification.EvidenceError {
			return true
		}
		if evidence.Status == verification.EvidenceSkipped || evidence.Status == verification.EvidenceError {
			return true
		}
		if report.Result.Status == verification.ResultPass && evidence.Status == verification.EvidenceFailed {
			return true
		}
		if evidence.Analysis != nil && evidence.Analysis.Unknown > 0 {
			return true
		}
		if evidenceIncompleteDetails(evidence) {
			return true
		}
	}
	for id := range planned {
		if seen[id] != 1 {
			return true
		}
	}
	for _, count := range seen {
		if count != 1 {
			return true
		}
	}
	if report.Result.Status != verification.ResultPass && report.Result.Status != verification.ResultFindings {
		return true
	}
	if result.Context != nil {
		context := result.Context
		if context.Truncated || len(context.EvidenceStates) == 0 || (context.TypedEvidence != nil && (!context.TypedEvidence.Complete || context.TypedEvidence.Truncated)) {
			return true
		}
		for _, state := range context.EvidenceStates {
			switch state.State {
			case "examined_and_absent", "unexamined":
			case "unavailable", "gathered_but_omitted", "":
				return true
			default:
				return true
			}
		}
		for _, uncertainty := range context.Uncertainties {
			if strings.Contains(uncertainty.Code, "unavailable") || strings.Contains(uncertainty.Code, "omitted") || strings.Contains(uncertainty.Code, "partial") || strings.Contains(uncertainty.Code, "truncated") {
				return true
			}
		}
	}
	for _, uncertainty := range report.Uncertainties {
		if uncertainty.LocationsTruncated || strings.Contains(uncertainty.Code, "unknown") || strings.Contains(uncertainty.Code, "unavailable") {
			return true
		}
	}
	return false
}

func evidenceIncompleteDetails(evidence verification.Evidence) bool {
	if evidence.Tests != nil && (evidence.Tests.PackagesTruncated || evidence.Tests.NonpassingTruncated) ||
		evidence.Coverage != nil && evidence.Coverage.UncoveredTruncated ||
		evidence.Diagnostics != nil && evidence.Diagnostics.Truncated ||
		evidence.Contract != nil && evidence.Contract.ViolationsTruncated {
		return true
	}
	switch evidence.Kind {
	case verification.CheckTests:
		return evidence.Tests == nil
	case verification.CheckCoverage:
		return evidence.Coverage == nil
	case verification.CheckRace:
		return evidence.Race == nil
	case verification.CheckConcurrency, verification.CheckErrors:
		return evidence.Analysis == nil || evidence.Analysis.Unknown > 0
	case verification.CheckDiagnostics:
		return evidence.Diagnostics == nil
	case verification.CheckContract:
		return evidence.Contract == nil
	default:
		return true
	}
}

func validFocusLocation(location verification.Location) bool {
	file := strings.ReplaceAll(filepath.ToSlash(location.File), `\`, "/")
	clean := path.Clean(file)
	return file != "" && !strings.HasSuffix(file, "/") && !filepath.IsAbs(location.File) && !strings.HasPrefix(file, "/") && location.Line > 0 && location.Col >= 0 && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
