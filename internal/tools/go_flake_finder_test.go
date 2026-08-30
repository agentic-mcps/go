package tools

import (
	"context"
	"testing"

	"github.com/agentic-mcps/go/internal/parser"
)

func TestFlakeFinderReturnsOnlyMixedOutcomes(t *testing.T) {
	runtime := newTestRuntime(t)

	_, output, err := runtime.flakeFinder(context.Background(), nil, FlakeFinderInput{Package: ".", Runs: 4})
	if err != nil {
		t.Fatalf("flakeFinder() error = %v", err)
	}
	if len(output.Flaky) != 1 {
		t.Fatalf("flaky = %+v, want one result", output.Flaky)
	}
	result := output.Flaky[0]
	if result.Test != "TestFlaky" || result.Runs != 4 || result.Passes != 2 || result.Failures != 2 || result.FlakeRate != 0.5 {
		t.Fatalf("flake result = %+v", result)
	}
	if output.TotalTestsRun != 12 {
		t.Fatalf("TotalTestsRun = %d, want 12", output.TotalTestsRun)
	}
}

func TestFlakeCollectorDetectsPreTestPackageFailure(t *testing.T) {
	collector := newFlakeCollector()
	if err := collector.consume(parser.TestEvent{Action: "fail", Package: "example.com/broken"}); err != nil {
		t.Fatal(err)
	}
	if got := collector.executionFailure(); got != "example.com/broken" {
		t.Fatalf("executionFailure() = %q, want broken package", got)
	}
	if err := collector.consume(parser.TestEvent{Action: "fail", Package: "example.com/test-failed", Test: "TestFail"}); err != nil {
		t.Fatal(err)
	}
	if err := collector.consume(parser.TestEvent{Action: "fail", Package: "example.com/test-failed"}); err != nil {
		t.Fatal(err)
	}
	delete(collector.failedPackages, "example.com/broken")
	if got := collector.executionFailure(); got != "" {
		t.Fatalf("executionFailure() = %q for ordinary test failure", got)
	}
}

func TestNormalizeFlakeFinderInput(t *testing.T) {
	input := FlakeFinderInput{Package: ".", Runs: 201, TimeoutSeconds: 301}
	if err := normalizeFlakeFinderInput(&input); err != nil {
		t.Fatal(err)
	}
	if input.Runs != 200 || input.TimeoutSeconds != 300 {
		t.Fatalf("normalized input = %+v", input)
	}
}
