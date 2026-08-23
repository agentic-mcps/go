package errors

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	tests := []string{"rule01"}
	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			analysistest.Run(t, testdata(), Analyzer, test)
		})
	}
}

func testdata() string { return analysistest.TestData() }
