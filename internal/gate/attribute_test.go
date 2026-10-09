package gate

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	testjson "github.com/agentic-mcps/go/internal/parser"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestProbeClassify(t *testing.T) {
	event := func(action, test string) testjson.TestEvent {
		return testjson.TestEvent{Action: action, Package: "m/p", Test: test}
	}
	cases := []struct {
		name     string
		test     string
		stderr   string
		events   []testjson.TestEvent
		exitCode int
		want     probeOutcome
	}{
		{name: "named test passes", test: "TestX", events: []testjson.TestEvent{event("pass", "TestX"), event("pass", "")}, want: probePass},
		{name: "named test fails", test: "TestX", exitCode: 1, events: []testjson.TestEvent{event("fail", "TestX"), event("fail", "")}, want: probeFail},
		{name: "subtest failure fails the test", test: "TestX", exitCode: 1, events: []testjson.TestEvent{event("fail", "TestX/a"), event("fail", "")}, want: probeFail},
		{name: "near miss: similarly named test is ignored", test: "TestX", events: []testjson.TestEvent{event("fail", "TestXY"), event("pass", "")}, want: probeAbsent},
		{name: "absent test", test: "TestX", events: []testjson.TestEvent{event("pass", "")}, want: probeAbsent},
		{
			name: "build failure is broken", test: "TestX", exitCode: 1,
			events: []testjson.TestEvent{
				{Action: "build-output", ImportPath: "m/p", Output: "# m/p\n"},
				{Action: "build-output", ImportPath: "m/p", Output: "p.go:1:1: undefined: z\n"},
				{Action: "build-fail", ImportPath: "m/p"},
				{Action: "fail", Package: "m/p", FailedBuild: "m/p"},
			},
			want: probeBroken,
		},
		{name: "package failure around the test fails", test: "TestX", exitCode: 1, events: []testjson.TestEvent{event("pass", "TestX"), event("fail", "")}, want: probeFail},
		{name: "go command error is broken", test: "TestX", exitCode: 1, stderr: "go: updates to go.mod needed\n", want: probeBroken},
		{name: "package probe passes", events: []testjson.TestEvent{event("pass", "")}, want: probePass},
		{name: "package probe fails", exitCode: 1, events: []testjson.TestEvent{event("fail", "")}, want: probeFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := probeFacts{}
			for _, e := range tc.events {
				facts.observe(e, tc.test)
			}
			got := facts.classify(tc.test, tc.exitCode, tc.stderr)
			if got.outcome != tc.want {
				t.Fatalf("outcome = %d (%s), want %d", got.outcome, got.reason, tc.want)
			}
			if got.outcome == probeBroken && got.reason == "" {
				t.Fatal("broken outcome has no reason")
			}
		})
	}
}

func TestAttributeExpandTimeouts(t *testing.T) {
	timeout := "panic: test timed out after 1m0s\n\trunning tests:\n\t\tTestSlow (1m0s)\n\t\tTestSlow/sub (59s)\n\t\tTestHang (1m0s)\n\ngoroutine 1 [running]:\n"
	failures := []testFailure{
		{Package: "m/a", Output: timeout, Subtests: []string{}},
		{Package: "m/b", Output: "panic in TestMain", Subtests: []string{}},
		{Package: "m/c", Test: "TestNamed", Output: "panic: test timed out", Subtests: []string{}},
		// The timed-out test was also reported by name: attribute it once.
		{Package: "m/d", Test: "TestSlow", Output: "--- FAIL: TestSlow", Subtests: []string{}},
		{Package: "m/d", Output: timeout, Subtests: []string{}},
	}
	got := attributeExpandTimeouts(failures)
	names := make([]string, 0, len(got))
	for _, failure := range got {
		names = append(names, failure.Package+":"+failure.Test)
	}
	want := []string{"m/a:TestHang", "m/a:TestSlow", "m/b:", "m/c:TestNamed", "m/d:TestSlow", "m/d:TestHang"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("expanded = %v, want %v", names, want)
	}
}

func TestAttributeDetail(t *testing.T) {
	cases := []struct {
		name    string
		want    string
		failure testFailure
	}{
		{name: "output only", failure: testFailure{Output: "=== RUN TestX\nx_test.go:3: boom\n--- FAIL: TestX"}, want: "x_test.go:3: boom\n--- FAIL: TestX"},
		{name: "subtests first", failure: testFailure{Subtests: []string{"TestX/a", "TestX/b"}, Output: "boom"}, want: "failing subtests: TestX/a, TestX/b\nboom"},
		{name: "subtests without output", failure: testFailure{Subtests: []string{"TestX/a"}}, want: "failing subtests: TestX/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attributeDetail(tc.failure); got != tc.want {
				t.Fatalf("detail = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTestFlags(t *testing.T) {
	cases := []struct {
		want    []string
		request verification.Request
	}{
		{want: []string{}},
		{request: verification.Request{Short: true, Skip: "TestSlow", Race: true, TestCache: true}, want: []string{"-short", "-skip=TestSlow", "-race"}},
	}
	for _, tc := range cases {
		if got := testFlags(tc.request); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("testFlags(%+v) = %v, want %v", tc.request, got, tc.want)
		}
	}
}

func TestAttributeLocate(t *testing.T) {
	f := newBaseFixture(t)
	f.write("p/a_test.go", "package p\n\nimport \"testing\"\n\nfunc helper() {}\n\nfunc TestA(t *testing.T) {}\n")
	f.write("p/b_test.go", "package p\n\nimport \"testing\"\n\ntype s struct{}\n\nfunc (s) TestB(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n")
	cases := []struct {
		test     string
		wantFile string
		wantLine int
	}{
		{test: "TestA", wantFile: "p/a_test.go", wantLine: 7},
		{test: "TestB", wantFile: "p/b_test.go", wantLine: 9},
		{test: "TestMissing"},
	}
	for _, tc := range cases {
		file, line := attributeLocate(f.root, filepath.Join(f.root, "p"), tc.test)
		if file != tc.wantFile || line != tc.wantLine {
			t.Errorf("attributeLocate(%s) = %s:%d, want %s:%d", tc.test, file, line, tc.wantFile, tc.wantLine)
		}
	}
}

// TestAttributeStopsAtBudgetAndLimit checks that failures are never blocked
// or passed when the budget is gone: each is a warning and the run is
// incomplete, and failures beyond the limit are reported as not attributed.
func TestAttributeStopsAtBudgetAndLimit(t *testing.T) {
	f := newBaseFixture(t)
	f.write("go.mod", "module example.com/fixture\n\ngo 1.25\n")
	f.write("lib/lib.go", "package lib\n")
	f.commit("base")
	ws, err := workspace.Open(context.Background(), f.root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	attr := &attributor{
		materialize: func(context.Context, verification.Repository, string) (string, error) {
			t.Fatal("base must not be materialized after the budget ran out")
			return "", nil
		},
		runner: runner, root: ws.Root(), baseShort: "abc1234", flags: []string{},
		targets: map[string]verification.ExecutionTarget{"example.com/fixture/lib": {ID: "example.com/fixture/lib", Dir: filepath.Join(ws.Root(), "lib")}},
	}
	defer attr.close()
	failures := make([]testFailure, 0, attributeLimit+1)
	for i := range attributeLimit + 1 {
		failures = append(failures, testFailure{Package: "example.com/fixture/lib", Test: fmt.Sprintf("TestCase%02d", i), Subtests: []string{}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := attr.attribute(ctx, failures)
	if out.complete {
		t.Fatal("complete = true, want false")
	}
	if len(out.items) != attributeLimit+1 {
		t.Fatalf("got %d items, want %d", len(out.items), attributeLimit+1)
	}
	for i, item := range out.items {
		if item.Severity != SeverityWarn || item.Code != CodeTestFailed {
			t.Fatalf("item %d = %+v, want a test.failed warning", i, item)
		}
		want := "was not compared with base (time budget)"
		if i == attributeLimit {
			want = "was not attributed: limit of 10 failing tests reached"
		}
		if !strings.Contains(item.Message, want) {
			t.Fatalf("item %d message = %q, want it to contain %q", i, item.Message, want)
		}
	}
}
