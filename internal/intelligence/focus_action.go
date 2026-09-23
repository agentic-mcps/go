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

const (
	verifyCurrentSnapshotAction   = "request verification for the current snapshot and matching policy"
	freshSelectionAndVerifyAction = "make a fresh selection against the current snapshot, then request verification for that snapshot and matching policy"
	focusBudgetAction             = "make a fresh narrower selection or use a larger max_bytes on a new selection; replacement refresh inherits the prior budget and cannot fix the omission"
	reportTruncationAction        = "treat omitted verification report detail as unavailable; refreshing cannot recover the omitted detail"
	unavailableEvidenceAction     = "treat affected evidence as unavailable; retry recovery is not established"
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
		return verificationNeededAction(result)
	}
	if focusSelectionRequired(result) || incompleteFocusEvidence(result, report) {
		return unavailableFocusAction(result, report)
	}
	if report.Result.Status == verification.ResultPass && report.Result.BlockingFindings > 0 {
		return unavailableFocusAction(result, report, "pass outcome contradicts blocking findings")
	}
	if len(report.Findings) > 0 {
		for _, finding := range report.Findings {
			if finding.Location == nil || !validFocusLocation(*finding.Location) {
				return unavailableFocusAction(result, report, "findings lack valid workspace-relative locations")
			}
		}
		return focusAction{Kind: findingInspectionNeeded, NextAction: "inspect the reported finding locations", Reasons: []string{"verification reported findings with usable locations"}}
	}
	if report.Result.Status == verification.ResultFindings {
		return unavailableFocusAction(result, report, "findings outcome has no retained findings")
	}
	if report.Result.Status == verification.ResultPass {
		return focusAction{Kind: requestedChecksPassedWithLimits, NextAction: "review the requested checks and their stated limits", Reasons: []string{"requested checks passed; this does not establish task completion"}}
	}
	return unavailableFocusAction(result, report, "report does not establish a complete finding or pass outcome")
}

func verificationNeededAction(result FocusResult) focusAction {
	parts := []string{verifyCurrentSnapshotAction}
	reasons := []string{"applicable verification evidence for the current snapshot and matching policy is unavailable"}
	if focusSelectionRequired(result) {
		parts[0] = freshSelectionAndVerifyAction
		reasons = append(reasons, "the previous focus selection could not be resolved and requires a fresh selection")
	}
	if focusScopeMismatch(result) {
		parts[0] += "; a different package scope answers a different question and does not support the requested scope"
		reasons = append(reasons, "a different package scope answers a different question and does not support the requested scope")
	}
	if focusContextBudgetOmitted(result.Context) {
		parts = append(parts, focusBudgetAction)
		reasons = append(reasons, "focused context detail was omitted by the requested byte budget")
	}
	if unavailableFocusContext(result) {
		parts = append(parts, unavailableEvidenceAction)
		reasons = append(reasons, "affected focus evidence is unavailable, unknown, or internally inconsistent")
	}
	return focusAction{Kind: verificationNeeded, NextAction: strings.Join(parts, "; "), Reasons: reasons}
}

func unavailableFocusAction(result FocusResult, report *verification.Report, cause ...string) focusAction {
	parts := []string{}
	reasons := []string{}
	if focusSelectionRequired(result) {
		parts = append(parts, freshSelectionAndVerifyAction)
		reasons = append(reasons, "the previous focus selection could not be resolved and requires a fresh selection")
	}
	if focusContextBudgetOmitted(result.Context) {
		parts = append(parts, focusBudgetAction)
		reasons = append(reasons, "focused context detail was omitted by the requested byte budget")
	}
	if reportDetailsTruncated(report) {
		parts = append(parts, reportTruncationAction)
		reasons = append(reasons, "verification report detail was truncated; refreshing cannot recover the omitted detail")
	}
	if unavailableFocusEvidence(result, report) {
		parts = append(parts, unavailableEvidenceAction)
		reasons = append(reasons, "affected verification or focus evidence is unavailable, unknown, or internally inconsistent")
	}
	reasons = append(reasons, cause...)
	if len(parts) == 0 {
		parts = append(parts, unavailableEvidenceAction)
		reasons = append(reasons, "verification evidence is incomplete or unavailable")
	}
	return focusAction{Kind: evidenceUnavailable, NextAction: strings.Join(parts, "; "), Reasons: reasons}
}

func focusSelectionRequired(result FocusResult) bool {
	return result.Refresh != nil && result.Refresh.Status == "selection_required"
}

func focusScopeMismatch(result FocusResult) bool {
	for _, reason := range result.Verification.Reasons {
		if reason == "package scope differs" {
			return true
		}
	}
	return false
}

func focusContextBudgetOmitted(context *FocusContext) bool {
	return context != nil && (context.Truncated || context.Symbol != nil && context.Symbol.Truncated)
}

func reportDetailsTruncated(report *verification.Report) bool {
	if report == nil {
		return false
	}
	if report.FindingsTruncated || report.Change.FilesTruncated || report.Change.DeclarationsTruncated || report.Impact.PackagesTruncated {
		return true
	}
	for _, file := range report.Change.Files {
		if file.BaseRangesTruncated || file.CurrentRangesTruncated {
			return true
		}
	}
	for _, check := range report.Plan {
		if check.TargetsTruncated {
			return true
		}
	}
	for _, evidence := range report.Evidence {
		if evidenceDetailsTruncated(evidence) {
			return true
		}
	}
	for _, risk := range report.Risks {
		if risk.LocationsTruncated {
			return true
		}
	}
	for _, uncertainty := range report.Uncertainties {
		if uncertainty.LocationsTruncated {
			return true
		}
	}
	return false
}

func evidenceDetailsTruncated(evidence verification.Evidence) bool {
	if evidence.Tests != nil && (evidence.Tests.PackagesTruncated || evidence.Tests.NonpassingTruncated) ||
		evidence.Coverage != nil && evidence.Coverage.UncoveredTruncated ||
		evidence.Diagnostics != nil && evidence.Diagnostics.Truncated ||
		evidence.Contract != nil && evidence.Contract.ViolationsTruncated {
		return true
	}
	if evidence.Contract != nil {
		for _, violation := range evidence.Contract.Violations {
			if violation.LocationsTruncated {
				return true
			}
		}
	}
	return false
}

func unavailableFocusEvidence(result FocusResult, report *verification.Report) bool {
	if unavailableFocusContext(result) {
		return true
	}
	if report == nil {
		return false
	}
	if report.Result.Status == verification.ResultIncomplete {
		return true
	}
	if report.Result.Status != verification.ResultPass && report.Result.Status != verification.ResultFindings {
		return true
	}
	planned := map[string]verification.Check{}
	for _, check := range report.Plan {
		if check.ID == "" {
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
		if evidence.Status == verification.EvidenceSkipped || evidence.Status == verification.EvidenceError ||
			evidence.Status != verification.EvidencePassed && evidence.Status != verification.EvidenceFailed ||
			evidence.Analysis != nil && evidence.Analysis.Unknown > 0 || evidencePayloadMissing(evidence) {
			return true
		}
		if report.Result.Status == verification.ResultPass && evidence.Status == verification.EvidenceFailed {
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
	for _, finding := range report.Findings {
		if finding.Location == nil || !validFocusLocation(*finding.Location) {
			return true
		}
	}
	if report.Result.Status == verification.ResultFindings {
		if len(report.Findings) == 0 {
			return true
		}
	} else if report.Result.Status == verification.ResultPass && report.Result.BlockingFindings > 0 {
		return true
	}
	for _, uncertainty := range report.Uncertainties {
		if strings.Contains(uncertainty.Code, "unknown") || strings.Contains(uncertainty.Code, "unavailable") {
			return true
		}
	}
	return false
}

func unavailableFocusContext(result FocusResult) bool {
	if !result.Complete || len(result.Uncertainties) > 0 || result.Change.FilesTruncated || result.Change.DeclarationsTruncated || result.Impact.PackagesTruncated {
		return true
	}
	if result.Context == nil {
		return false
	}
	context := result.Context
	if len(context.EvidenceStates) == 0 || context.TypedEvidence != nil && (!context.TypedEvidence.Complete || context.TypedEvidence.Truncated) {
		return true
	}
	for _, state := range context.EvidenceStates {
		switch state.State {
		case "examined_and_absent", "unexamined":
		case "gathered_but_omitted":
			if state.Facet != "budgeted_evidence" {
				return true
			}
		case "unavailable", "":
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
	return false
}

func evidencePayloadMissing(evidence verification.Evidence) bool {
	switch evidence.Kind {
	case verification.CheckTests:
		return evidence.Tests == nil
	case verification.CheckCoverage:
		return evidence.Coverage == nil
	case verification.CheckRace:
		return evidence.Race == nil
	case verification.CheckConcurrency, verification.CheckErrors:
		return evidence.Analysis == nil
	case verification.CheckDiagnostics:
		return evidence.Diagnostics == nil
	case verification.CheckContract:
		return evidence.Contract == nil
	default:
		return true
	}
}

func incompleteFocusEvidence(result FocusResult, report *verification.Report) bool {
	if !result.Complete || len(result.Uncertainties) > 0 ||
		result.Change.FilesTruncated || result.Change.DeclarationsTruncated || result.Impact.PackagesTruncated ||
		report.Result.Status == verification.ResultIncomplete || reportDetailsTruncated(report) {
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
		if context.Truncated || context.Symbol != nil && context.Symbol.Truncated || len(context.EvidenceStates) == 0 || (context.TypedEvidence != nil && (!context.TypedEvidence.Complete || context.TypedEvidence.Truncated)) {
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
