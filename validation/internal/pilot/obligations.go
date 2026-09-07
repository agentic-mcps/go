package pilot

import (
	"fmt"
	"strings"
)

// ScoreObligations evaluates scenario obligations from recorded evidence only.
// Agent prose is evidence, never an assertion of completion by itself.
func ScoreObligations(s Scenario, r Run) []ObligationResult {
	var transcript strings.Builder
	for _, raw := range r.Transcript {
		transcript.Write(raw)
		transcript.WriteByte('\n')
	}
	text := strings.ToLower(transcript.String() + "\n" + r.Patch)
	results := make([]ObligationResult, 0, len(s.Obligations))
	for _, obligation := range s.Obligations {
		status, evidence := obligationEvidence(strings.ToLower(obligation), text, r)
		results = append(results, ObligationResult{Obligation: obligation, Status: status, Evidence: evidence})
	}
	return results
}

func obligationEvidence(obligation, text string, r Run) (string, string) {
	need := func(words ...string) bool {
		for _, w := range words {
			if !strings.Contains(text, w) {
				return false
			}
		}
		return true
	}
	switch {
	case strings.Contains(obligation, "declaration"):
		if need("retryonconflict") || need("normalization", "boundary") {
			return "satisfied", "transcript/patch identifies the declaration or normalization boundary"
		}
	case strings.Contains(obligation, "caller"):
		if need("caller") || strings.Contains(text, "call site") || strings.Contains(text, "calls") {
			return "satisfied", "transcript/patch identifies caller evidence"
		}
	case strings.Contains(obligation, "test"):
		if strings.Contains(text, "_test.go") || strings.Contains(text, "test") {
			return "satisfied", "transcript/patch identifies test evidence"
		}
	case strings.Contains(obligation, "recursive"):
		if need("recursive") || need("validation", "matching") {
			return "satisfied", "transcript/patch identifies recursive validation and matching"
		}
	case strings.Contains(obligation, "refresh"):
		if r.Condition == "focus" && r.FocusToolCalls >= 2 && (strings.Contains(text, "refresh") || strings.Contains(text, "previous_pack_id")) {
			return "satisfied", "recorded focus calls and refresh evidence"
		}
	case strings.Contains(obligation, "applicability"):
		if strings.Contains(text, "applicability") || strings.Contains(text, "verification") {
			return "satisfied", "recorded verification-applicability evidence"
		}
	case strings.Contains(obligation, "uncertainty"):
		if len(r.Uncertainty) > 0 || strings.Contains(text, "uncertain") {
			return "satisfied", "recorded uncertainty evidence"
		}
	case strings.Contains(obligation, "preserve"):
		if len(r.Uncertainty) > 0 || strings.Contains(text, "cannot claim") || strings.Contains(text, "uncertain") {
			return "satisfied", "recorded bounded-claim or uncertainty evidence"
		}
	}
	if r.Acceptance == "pass" && len(r.ScopeViolations) == 0 {
		return "uncertain", "qualification and scope evidence do not prove this decision obligation"
	}
	return "unsatisfied", fmt.Sprintf("acceptance=%s; scope_violations=%d", r.Acceptance, len(r.ScopeViolations))
}
