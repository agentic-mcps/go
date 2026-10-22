package verification

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestGoTestArgs(t *testing.T) {
	targets := []ExecutionTarget{{ID: "example.test/a"}, {ID: "example.test/b"}}
	tests := []struct {
		name     string
		want     []string
		wantNot  []string
		request  Request
		coverage bool
	}{
		{
			name:    "default disables the test cache",
			want:    []string{"test", "-json", "-count=1", "-timeout=60s", "example.test/a", "example.test/b"},
			wantNot: []string{"-short"},
		},
		{
			name:    "test cache omits count",
			request: Request{TestCache: true},
			want:    []string{"test", "-json", "-timeout=60s", "example.test/a", "example.test/b"},
		},
		{
			name:    "short and skip reach go test",
			request: Request{Short: true, Skip: "TestSlow|TestNetwork"},
			want:    []string{"test", "-json", "-count=1", "-timeout=60s", "-short", "-skip=TestSlow|TestNetwork", "example.test/a", "example.test/b"},
		},
		{
			name:     "coverage and race keep their flags",
			request:  Request{Race: true, TestCache: true},
			coverage: true,
			want:     []string{"test", "-json", "-timeout=60s", "-covermode=atomic", "-coverprofile=/run/coverage.out", "-coverpkg=example.test/a", "-race", "example.test/a", "example.test/b"},
		},
		{
			name:    "blank skip adds no flag",
			request: Request{Skip: ""},
			want:    []string{"test", "-json", "-count=1", "-timeout=60s", "example.test/a", "example.test/b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := goTestArgs(test.request, targets, []string{"example.test/a"}, "/run/coverage.out", test.coverage)
			if !slices.Equal(got, test.want) {
				t.Fatalf("args = %q, want %q", got, test.want)
			}
			for _, flag := range test.wantNot {
				if slices.Contains(got, flag) {
					t.Fatalf("args = %q, want no %s", got, flag)
				}
			}
		})
	}
}

func TestDirectExecutionTargets(t *testing.T) {
	targets := []ExecutionTarget{{ID: "a", Distance: 0}, {ID: "b", Distance: 1}, {ID: "c", Distance: 0}}
	tests := []struct {
		name       string
		want       []string
		directOnly bool
	}{
		{name: "full closure by default", want: []string{"a", "b", "c"}},
		{name: "direct only", directOnly: true, want: []string{"a", "c"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := executionTargetIDs(analyzerTargets(targets, test.directOnly))
			if !slices.Equal(got, test.want) {
				t.Fatalf("targets = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFailedAnalyzerOutcome(t *testing.T) {
	outcome := failedAnalyzerOutcome(errors.New("loading /work/space/pkg: broken"), "/work/space")
	if len(outcome.Evidence) != len(analyzerSpecs) || len(outcome.Uncertainties) != len(analyzerSpecs) || len(outcome.Findings) != 0 || outcome.Findings == nil {
		t.Fatalf("outcome = %#v, want one error evidence and uncertainty per analyzer", outcome)
	}
	for index, spec := range analyzerSpecs {
		evidence := outcome.Evidence[index]
		if evidence.CheckID != spec.checkID || evidence.Kind != spec.kind || evidence.Status != EvidenceError {
			t.Fatalf("evidence = %#v, want error evidence for %s", evidence, spec.checkID)
		}
		if strings.Contains(evidence.Error, "/work/space") || !strings.Contains(evidence.Error, "broken") {
			t.Fatalf("evidence error = %q, want portable cause", evidence.Error)
		}
		uncertainty := outcome.Uncertainties[index]
		if uncertainty.Code != "analysis_unavailable" || uncertainty.CheckID != spec.checkID || uncertainty.Locations == nil {
			t.Fatalf("uncertainty = %#v, want analysis_unavailable for %s", uncertainty, spec.checkID)
		}
	}
}
