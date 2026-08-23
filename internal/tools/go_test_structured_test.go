package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/parser"
	"github.com/ashwingopalsamy/agentic-go/internal/trace"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

func TestCollectorMapsPackageTerminalStatus(t *testing.T) {
	for _, test := range []struct {
		action string
		want   string
	}{
		{action: "pass", want: "ok"},
		{action: "skip", want: "skip"},
		{action: "fail", want: "FAIL"},
	} {
		t.Run(test.action, func(t *testing.T) {
			collector := newTestCollector(false)
			if err := collector.consume(parser.TestEvent{Action: test.action, Package: "example.com/package"}); err != nil {
				t.Fatal(err)
			}

			result := collector.result(0)
			if got := result.Packages["example.com/package"].Status; got != test.want {
				t.Fatalf("package status = %q, want %q", got, test.want)
			}
			if result.Passed != 0 || result.Failed != 0 || result.Skipped != 0 {
				t.Fatalf("test counts = %d/%d/%d, want 0/0/0", result.Passed, result.Failed, result.Skipped)
			}
		})
	}
}

func TestTestStructuredReturnsFailedRunsAsData(t *testing.T) {
	runtime := newTestRuntime(t)

	_, output, err := runtime.testStructured(context.Background(), nil, TestStructuredInput{Package: "./..."})
	if err != nil {
		t.Fatalf("testStructured() error = %v", err)
	}
	if output.Passed != 2 || output.Failed != 1 || output.Skipped != 1 {
		t.Fatalf("test counts = %d/%d/%d, want 2/1/1", output.Passed, output.Failed, output.Skipped)
	}
	if len(output.Tests) != 4 {
		t.Fatalf("len(Tests) = %d, want 4", len(output.Tests))
	}
	if output.Packages["example.com/testingfixture/failing"].Status != "FAIL" {
		t.Fatalf("package summary = %+v, want FAIL", output.Packages["example.com/testingfixture/failing"])
	}
	if output.Packages["example.com/testingfixture/notests"].Status != "skip" {
		t.Fatalf("no-test package summary = %+v, want skip", output.Packages["example.com/testingfixture/notests"])
	}

	failed := findTest(t, output.Tests, "TestFail")
	if !strings.Contains(failed.Output, "intentional failure") {
		t.Fatalf("failed output = %q", failed.Output)
	}
	passed := findTest(t, output.Tests, "TestPass")
	if passed.Output != "" {
		t.Fatalf("passing output was retained without verbose: %q", passed.Output)
	}
}

func TestTestStructuredVerboseKeepsPassingOutput(t *testing.T) {
	runtime := newTestRuntime(t)

	_, output, err := runtime.testStructured(context.Background(), nil, TestStructuredInput{Package: "./...", Verbose: true})
	if err != nil {
		t.Fatalf("testStructured() error = %v", err)
	}
	if got := findTest(t, output.Tests, "TestPass").Output; !strings.Contains(got, "passing output") {
		t.Fatalf("passing output = %q, want verbose log", got)
	}
}

func TestResolvePackageRejectsExternalPackage(t *testing.T) {
	runtime := newTestRuntime(t)

	if _, err := runtime.resolvePackage(context.Background(), "fmt"); err == nil {
		t.Fatal("resolvePackage() accepted a package outside the workspace")
	}
}

func TestNormalizeTestStructuredInput(t *testing.T) {
	input := TestStructuredInput{Package: "./...", TimeoutSeconds: 301}
	if err := normalizeTestStructuredInput(&input); err != nil {
		t.Fatal(err)
	}
	if input.TimeoutSeconds != 300 {
		t.Fatalf("TimeoutSeconds = %d, want clamped 300", input.TimeoutSeconds)
	}
}

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "fixtures", "testing"))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTIC_GO_TRACE", "false")
	tracer, err := trace.Init()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := tracer.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	runtime, err := NewRuntime(ws, runner, tracer)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func findTest(t *testing.T, tests []TestCase, name string) TestCase {
	t.Helper()
	for _, test := range tests {
		if test.Name == name {
			return test
		}
	}
	t.Fatalf("test %q not found in %+v", name, tests)
	return TestCase{}
}
