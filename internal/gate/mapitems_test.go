package gate

import (
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/verification"
)

func TestMapCollectionFindings(t *testing.T) {
	location := &verification.Location{File: "lib/lib.go", Line: 7, Col: 2}
	cases := []struct {
		name    string
		finding verification.Finding
		want    []Item
	}{
		{
			name: "build failure blocks at the compiler position with compiler text",
			finding: verification.Finding{
				Kind: verification.BuildFailureKind, Severity: verification.SeverityError, Location: location,
				Message: "example.com/m/lib failed to build: lib/lib.go:7:2: undefined: x\nlib/lib.go:8:2: undefined: y",
			},
			want: []Item{{
				Severity: SeverityBlock, Code: CodeBuild, File: "lib/lib.go", Line: 7,
				Message: "example.com/m/lib does not build: undefined: x", Fix: mapBuildFix,
				Detail: "lib/lib.go:7:2: undefined: x\nlib/lib.go:8:2: undefined: y",
			}},
		},
		{
			name: "build failure without a column still gets a location",
			finding: verification.Finding{
				Kind: verification.BuildFailureKind, Severity: verification.SeverityError,
				Message: "example.com/m/lib failed to build: lib/lib.go:4: undefined: x",
			},
			want: []Item{{
				Severity: SeverityBlock, Code: CodeBuild, File: "lib/lib.go", Line: 4,
				Message: "example.com/m/lib does not build: undefined: x", Fix: mapBuildFix,
				Detail: "lib/lib.go:4: undefined: x",
			}},
		},
		{
			name: "build failure outside the workspace has no location",
			finding: verification.Finding{
				Kind: verification.BuildFailureKind, Severity: verification.SeverityError,
				Message: "example.com/m/lib failed to build: ../other/x.go:4: undefined: x",
			},
			want: []Item{{
				Severity: SeverityBlock, Code: CodeBuild,
				Message: "example.com/m/lib does not build: undefined: x", Fix: mapBuildFix,
				Detail: "../other/x.go:4: undefined: x",
			}},
		},
		{
			name:    "race blocks",
			finding: verification.Finding{Kind: "go.race", Severity: verification.SeverityError, Location: location, Message: "data race between a and b"},
			want:    []Item{{Severity: SeverityBlock, Code: CodeRace, File: "lib/lib.go", Line: 7, Message: "data race between a and b", Fix: mapRaceFix}},
		},
		{
			name: "introduced error analyzer finding blocks",
			finding: verification.Finding{
				Kind: "go.analysis", Rule: "errors.ignored", Severity: verification.SeverityError, Location: location,
				Message: "error is ignored", Suggestion: "Handle the error.", Baseline: verification.BaselineIntroduced,
			},
			want: []Item{{Severity: SeverityBlock, Code: CodeAnalysisIntroduced, File: "lib/lib.go", Line: 7, Message: "errors.ignored: error is ignored", Fix: "Handle the error."}},
		},
		{
			name: "introduced warning analyzer finding warns",
			finding: verification.Finding{
				Kind: "go.analysis", Rule: "r", Severity: verification.SeverityWarning, Location: location,
				Message: "m", Baseline: verification.BaselineIntroduced,
			},
			want: []Item{{Severity: SeverityWarn, Code: CodeAnalysisIntroduced, File: "lib/lib.go", Line: 7, Message: "r: m"}},
		},
		{
			name: "existing analyzer finding is ignored",
			finding: verification.Finding{
				Kind: "go.analysis", Rule: "r", Severity: verification.SeverityError, Location: location,
				Message: "m", Baseline: verification.BaselineExisting,
			},
			want: []Item{},
		},
		{
			name:    "test failure findings come from the test summary instead",
			finding: verification.Finding{Kind: "test.failure", Severity: verification.SeverityError, Message: "TestX failed in p"},
			want:    []Item{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := verification.Report{Findings: []verification.Finding{tc.finding}}
			got := mapCollection(report, nil, false)
			if !reflect.DeepEqual(got.items, tc.want) {
				t.Fatalf("items = %#v, want %#v", got.items, tc.want)
			}
			if !got.complete {
				t.Fatal("complete = false, want true")
			}
		})
	}
}

func TestMapTestFailures(t *testing.T) {
	summary := verification.TestSummary{
		Packages: []verification.TestPackageSummary{
			{Package: "m/a", Status: "FAIL", Failed: 3},
			{Package: "m/b", Status: "FAIL", Failed: 0, Output: "panic in TestMain\nFAIL\tm/b\t0.01s\n"},
			{Package: "m/c", Status: "FAIL", Failed: 0, Output: "c.go:1:1: undefined: z\nFAIL\tm/c [build failed]\n"},
			{Package: "m/d", Status: "FAIL", Failed: 0, Output: "FAIL\tm/d [setup failed]\n"},
			{Package: "m/e", Status: "ok"},
			{Package: "m/f", Status: "FAIL", Failed: 1, Output: "panic: TestMain cleanup failed\n"},
			// Near miss: a named failure whose package output only says FAIL.
			{Package: "m/g", Status: "FAIL", Failed: 1, Output: "FAIL\tm/g\t0.01s\n"},
		},
		Nonpassing: []verification.TestCaseSummary{
			{Package: "m/a", Name: "TestOne", Status: "fail", Output: "=== RUN   TestOne\n--- FAIL: TestOne (0.00s)\n"},
			{Package: "m/a", Name: "TestOne/case_1", Status: "fail", Output: "    one_test.go:9: got 1, want 2\n"},
			{Package: "m/a", Name: "TestOne/case_2", Status: "fail"},
			{Package: "m/a", Name: "TestSkipped", Status: "skip"},
			{Package: "m/a", Name: "TestTwo", Status: "fail", Output: "two_test.go:3: boom\n"},
			{Package: "m/f", Name: "TestF", Status: "fail", Output: "f_test.go:1: f\n"},
			{Package: "m/g", Name: "TestG", Status: "fail", Output: "g_test.go:1: g\n"},
		},
	}
	got := mapTestFailures(summary)
	want := []testFailure{
		{Package: "m/a", Test: "TestOne", Subtests: []string{"TestOne/case_1", "TestOne/case_2"}, Output: "=== RUN   TestOne\n--- FAIL: TestOne (0.00s)\none_test.go:9: got 1, want 2"},
		{Package: "m/a", Test: "TestTwo", Subtests: []string{}, Output: "two_test.go:3: boom"},
		{Package: "m/b", Output: "panic in TestMain\nFAIL\tm/b\t0.01s", Subtests: []string{}},
		{Package: "m/f", Output: "panic: TestMain cleanup failed", Subtests: []string{}},
		{Package: "m/f", Test: "TestF", Subtests: []string{}, Output: "f_test.go:1: f"},
		{Package: "m/g", Test: "TestG", Subtests: []string{}, Output: "g_test.go:1: g"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failures =\n%#v\nwant\n%#v", got, want)
	}
}

func TestMapCoverage(t *testing.T) {
	files := []verification.SourceFile{
		{Change: verification.ChangedFile{Path: "lib/lib.go", CurrentRanges: []verification.LineRange{{Start: 10, End: 12}, {Start: 20, End: 20}}}},
		{Change: verification.ChangedFile{Path: "lib/other.go", CurrentRanges: []verification.LineRange{{Start: 5, End: 5}}}},
	}
	covered := verification.Evidence{
		Kind: verification.CheckCoverage, Status: verification.EvidencePassed,
		Coverage: &verification.CoverageSummary{Uncovered: []verification.SourceRange{
			// Lines 8-11 are uncovered but only 10 and 11 changed.
			{File: "lib/lib.go", StartLine: 8, EndLine: 11, Statements: 2},
			{File: "lib/lib.go", StartLine: 20, EndLine: 22, Statements: 1},
			{File: "lib/other.go", StartLine: 5, EndLine: 5, Statements: 1},
			// Near miss: an uncovered block that touches no changed line.
			{File: "lib/other.go", StartLine: 30, EndLine: 31, Statements: 1},
		}},
	}
	//nolint:govet // Keep each case readable in input, then expectation order.
	cases := []struct {
		name         string
		evidence     verification.Evidence
		uncertainty  []verification.Uncertainty
		require      bool
		wantItems    []Item
		wantNotes    []string
		wantComplete bool
	}{
		{
			name: "uncovered changed lines warn once per file", evidence: covered, wantComplete: true,
			wantItems: []Item{
				{Severity: SeverityWarn, Code: CodeCoverageUncovered, File: "lib/lib.go", Line: 10, Message: "3 changed lines in lib/lib.go are not executed by any test", Fix: mapCoverageFix},
				{Severity: SeverityWarn, Code: CodeCoverageUncovered, File: "lib/other.go", Line: 5, Message: "1 changed line in lib/other.go is not executed by any test", Fix: mapCoverageFix},
			},
			wantNotes: []string{},
		},
		{
			name: "required coverage blocks", evidence: covered, require: true, wantComplete: true,
			wantItems: []Item{
				{Severity: SeverityBlock, Code: CodeCoverageUncovered, File: "lib/lib.go", Line: 10, Message: "3 changed lines in lib/lib.go are not executed by any test", Fix: mapCoverageFix},
				{Severity: SeverityBlock, Code: CodeCoverageUncovered, File: "lib/other.go", Line: 5, Message: "1 changed line in lib/other.go is not executed by any test", Fix: mapCoverageFix},
			},
			wantNotes: []string{},
		},
		{
			name: "unavailable coverage is an info item, never 0%",
			evidence: verification.Evidence{
				Kind: verification.CheckCoverage, Status: verification.EvidenceError,
				Summary: "coverage unavailable: tests in m/lib failed",
			},
			wantItems: []Item{
				{Severity: SeverityInfo, Code: CodeCoverageUnavailable, Message: "coverage unavailable: tests in m/lib failed"},
			},
			wantNotes: []string{}, wantComplete: true,
		},
		{
			name: "unavailable required coverage is incomplete", require: true,
			evidence: verification.Evidence{
				Kind: verification.CheckCoverage, Status: verification.EvidenceError, Summary: "changed coverage could not be calculated",
			},
			wantItems: []Item{
				{Severity: SeverityInfo, Code: CodeCoverageUnavailable, Message: "coverage unavailable: changed coverage could not be calculated"},
			},
			wantNotes: []string{"coverage unavailable: changed coverage could not be calculated"},
		},
		{
			name:     "partial coverage adds a note",
			evidence: verification.Evidence{Kind: verification.CheckCoverage, Status: verification.EvidencePassed, Coverage: &verification.CoverageSummary{}},
			uncertainty: []verification.Uncertainty{
				{Code: "coverage_incomplete", Message: "tests in m/x failed before writing coverage"},
				{Code: "baseline_unknown", Message: "a"},
				{Code: "baseline_unknown", Message: "b"},
				{Code: "cgo", Message: "ignored"},
			},
			wantItems:    []Item{},
			wantNotes:    []string{"tests in m/x failed before writing coverage", "2 analyzer findings could not be compared with base"},
			wantComplete: true,
		},
		{
			name:         "skipped coverage says nothing",
			evidence:     verification.Evidence{Kind: verification.CheckCoverage, Status: verification.EvidenceSkipped, Summary: "no added or modified executable Go statements"},
			wantItems:    []Item{},
			wantNotes:    []string{},
			wantComplete: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := verification.Report{Evidence: []verification.Evidence{tc.evidence}, Uncertainties: tc.uncertainty}
			got := mapCollection(report, files, tc.require)
			if !reflect.DeepEqual(got.items, tc.wantItems) {
				t.Fatalf("items = %#v, want %#v", got.items, tc.wantItems)
			}
			if !reflect.DeepEqual(got.notes, tc.wantNotes) {
				t.Fatalf("notes = %#v, want %#v", got.notes, tc.wantNotes)
			}
			if got.complete != tc.wantComplete {
				t.Fatalf("complete = %v, want %v", got.complete, tc.wantComplete)
			}
		})
	}
}

func TestMapEvidenceErrors(t *testing.T) {
	cases := []struct {
		name         string
		evidence     verification.Evidence
		wantNote     string
		wantComplete bool
	}{
		{
			name:     "tests that could not run are incomplete",
			evidence: verification.Evidence{Kind: verification.CheckTests, Status: verification.EvidenceError, Summary: "check could not produce trustworthy evidence", Error: "running affected package tests: boom\nmore"},
			wantNote: "tests could not run: running affected package tests: boom",
		},
		{
			name:     "race that could not run is incomplete",
			evidence: verification.Evidence{Kind: verification.CheckRace, Status: verification.EvidenceError, Summary: "check could not produce trustworthy evidence"},
			wantNote: "race detection could not run: check could not produce trustworthy evidence",
		},
		{
			name:         "analyzer that could not run is only a note",
			evidence:     verification.Evidence{Kind: verification.CheckErrors, Status: verification.EvidenceError, Summary: "error analyzer could not run", Error: "load failed"},
			wantNote:     "error analyzer could not run: load failed",
			wantComplete: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapCollection(verification.Report{Evidence: []verification.Evidence{tc.evidence}}, nil, false)
			if len(got.notes) != 1 || got.notes[0] != tc.wantNote {
				t.Fatalf("notes = %q, want [%q]", got.notes, tc.wantNote)
			}
			if got.complete != tc.wantComplete {
				t.Fatalf("complete = %v, want %v", got.complete, tc.wantComplete)
			}
		})
	}
}

func TestMapDetail(t *testing.T) {
	var b strings.Builder
	b.WriteString("=== RUN   TestX\n=== PAUSE TestX\n")
	for i := range 20 {
		b.WriteString("    x_test.go:" + strings.Repeat("1", i+1) + ": line\n")
	}
	got := strings.Split(mapDetail(b.String()), "\n")
	if len(got) != mapDetailLines {
		t.Fatalf("detail has %d lines, want %d: %q", len(got), mapDetailLines, got)
	}
	if strings.HasPrefix(strings.TrimSpace(got[0]), "===") {
		t.Fatalf("detail kept a progress line: %q", got[0])
	}
}
