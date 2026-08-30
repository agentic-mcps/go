package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentic-mcps/go/internal/finding"
	"github.com/agentic-mcps/go/internal/parser"
)

func TestRaceReportReturnsStructuredConflict(t *testing.T) {
	runtime := newTestRuntime(t)

	_, output, err := runtime.raceReport(context.Background(), nil, RaceReportInput{Package: "."})
	if err != nil {
		t.Fatalf("raceReport() error = %v", err)
	}
	if output.RawBlocksFound == 0 || len(output.Conflicts) == 0 {
		t.Fatalf("race report = %+v, want at least one conflict", output)
	}
	conflict := output.Conflicts[0]
	if conflict.Current.GoroutineID == 0 || conflict.Previous.GoroutineID == 0 {
		t.Fatalf("goroutine IDs were not parsed: %+v", conflict)
	}
	if conflict.Current.Address == "" || conflict.Current.Address != conflict.Previous.Address {
		t.Fatalf("conflicting addresses = %q/%q", conflict.Current.Address, conflict.Previous.Address)
	}
	if conflict.Current.Location.File != "race_test.go" || conflict.Previous.Location.File != "race_test.go" {
		t.Fatalf("race locations = %+v/%+v", conflict.Current.Location, conflict.Previous.Location)
	}
	if len(conflict.GoroutineCreation) == 0 {
		t.Fatalf("goroutine creation sites were not parsed: %+v", conflict)
	}
	for _, creation := range conflict.GoroutineCreation {
		if creation.Location.File == "" || filepath.IsAbs(creation.Location.File) {
			t.Fatalf("creation location is not workspace-relative: %+v", creation.Location)
		}
	}
}

func TestRaceLocationsClearExternalPaths(t *testing.T) {
	runtime := newTestRuntime(t)
	outside := filepath.Join(t.TempDir(), "external.go")
	if err := os.WriteFile(outside, []byte("package external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(runtime.workspace.Root(), "race_test.go")
	output := RaceReportOutput{Conflicts: []parser.RaceConflict{{
		Current:  parser.RaceAccess{Location: finding.Location{File: inside, Line: 10}},
		Previous: parser.RaceAccess{Location: finding.Location{File: outside, Line: 20}},
		GoroutineCreation: []parser.RaceAccess{
			{Location: finding.Location{File: outside, Line: 30}},
		},
	}}}

	runtime.makeRaceLocationsRelative(&output)
	conflict := output.Conflicts[0]
	if conflict.Current.Location.File != "race_test.go" {
		t.Fatalf("current location = %+v", conflict.Current.Location)
	}
	if conflict.Previous.Location.File != "" || conflict.Previous.Location.Line != 0 {
		t.Fatalf("external previous location was retained: %+v", conflict.Previous.Location)
	}
	if len(conflict.GoroutineCreation) != 0 {
		t.Fatalf("external creation locations were retained: %+v", conflict.GoroutineCreation)
	}
}

func TestNormalizeRaceReportInput(t *testing.T) {
	input := RaceReportInput{Package: ".", TimeoutSeconds: 301}
	if err := normalizeRaceReportInput(&input); err != nil {
		t.Fatal(err)
	}
	if input.TimeoutSeconds != 300 {
		t.Fatalf("TimeoutSeconds = %d, want clamped 300", input.TimeoutSeconds)
	}
}

func TestRaceReportHonorsCancellation(t *testing.T) {
	runtime := newTestRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := runtime.raceReport(ctx, nil, RaceReportInput{Package: "."})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("raceReport() error = %v, want context cancellation", err)
	}
}
