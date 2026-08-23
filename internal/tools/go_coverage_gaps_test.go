package tools

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCoverageGapsReturnsUncoveredBranch(t *testing.T) {
	runtime := newTestRuntime(t)

	_, output, err := runtime.coverageGaps(context.Background(), nil, CoverageGapsInput{Package: "."})
	if err != nil {
		t.Fatalf("coverageGaps() error = %v", err)
	}
	if output.OverallPercent >= 100 || len(output.Files) == 0 {
		t.Fatalf("coverage output = %+v, want incomplete coverage", output)
	}
	var found bool
	for _, file := range output.Files {
		if filepath.IsAbs(file.File) {
			t.Fatalf("coverage file is absolute: %q", file.File)
		}
		if file.Gaps == nil {
			t.Fatalf("gaps for %q are nil", file.File)
		}
		for _, gap := range file.Gaps {
			if gap.File != file.File || filepath.IsAbs(gap.File) {
				t.Fatalf("gap path = %q, file path = %q", gap.File, file.File)
			}
			// Coverage block boundaries changed in Go 1.27. Assert that the
			// uncovered range contains the unexecuted return on line 5.
			if gap.File == "panic.go" && gap.StartLine <= 5 && gap.EndLine >= 5 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("coverage output did not contain the uncovered panic.go return: %+v", output)
	}
}

func TestCoverageFileRejectsExternalPath(t *testing.T) {
	runtime := newTestRuntime(t)
	if _, err := runtime.coverageFile(packageSelection{}, filepath.Join(t.TempDir(), "outside.go")); err == nil {
		t.Fatal("coverageFile() accepted an external path")
	}
}
