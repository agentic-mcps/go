package retrievalstudy

import (
	"testing"

	"github.com/agentic-mcps/go/internal/intelligence/retrieval"
)

func TestCandidatePoolAuditSeparatesCoverageFromRanking(t *testing.T) {
	gold := []GoldSpan{
		{Type: "documentation", Path: "docs/contracts.md", StartLine: 10, EndLine: 12},
		{Type: "test", Path: "focus_test.go", StartLine: 20, EndLine: 22},
	}
	source := archivedSource{
		coverage:           Coverage{SourceArchiveComplete: true},
		textCandidateIndex: TextCandidateIndexCoverage{Status: "complete"},
	}
	result := retrieval.Result{
		Candidates:     []retrieval.Candidate{{Path: "docs/contracts.md", Line: 11, Kind: "text.line"}},
		CandidateCount: 1, Complete: true,
	}
	audit := candidatePoolAudit(result, gold, source, true)
	if !audit.Complete || audit.HitGoldSpans != 1 || audit.GoldSpans != 2 || audit.Recall != 0.5 {
		t.Fatalf("candidate pool audit = %+v, want complete 1/2 coverage", audit)
	}
	if audit.CandidateCount != 1 || audit.CandidatesObserved != 1 {
		t.Fatalf("candidate pool counts = (%d, %d), want (1, 1)", audit.CandidateCount, audit.CandidatesObserved)
	}
}

func TestCandidatePoolAuditMarksCappedResultsPartial(t *testing.T) {
	source := archivedSource{coverage: Coverage{SourceArchiveComplete: true}}
	result := retrieval.Result{
		Candidates:     []retrieval.Candidate{{Path: "docs/contracts.md", Line: 11}},
		CandidateCount: candidatePoolAuditLimit + 1, Complete: true, Truncated: true,
	}
	audit := candidatePoolAudit(result, []GoldSpan{{Type: "documentation", Path: "docs/contracts.md", StartLine: 10, EndLine: 12}}, source, false)
	if audit.Complete || audit.Status != "partial" || audit.IncompleteReason == "" {
		t.Fatalf("capped audit = %+v, want explicit partial status", audit)
	}
}
