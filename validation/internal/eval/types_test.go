package eval

import "testing"

func TestTaskValidationEnforcesScoringBoundary(t *testing.T) {
	task := Task{
		SchemaVersion: TaskSchema,
		ID:            "example-task",
		Repository: Repository{
			Name: "example", URL: "https://github.com/example/example",
			Base: "1111111111111111111111111111111111111111", Target: "2222222222222222222222222222222222222222",
		},
		Prompt:     "Correct one behavior without changing unrelated packages.",
		Scope:      Scope{Workspace: ".", Packages: []string{"."}, AllowedPaths: []string{"x.go", "x_test.go"}},
		Oracle:     Oracle{OverlayPaths: []string{"x_test.go"}},
		Acceptance: []Command{{Argv: []string{"go", "test", "."}, TimeoutSeconds: 30}},
		Invariants: []string{"The regression is fixed."}, Exercises: []string{"scope"},
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}

	unsafe := task
	unsafe.Oracle.OverlayPaths = []string{"../x_test.go"}
	if err := unsafe.Validate(); err == nil {
		t.Fatal("unsafe oracle path was accepted")
	}

	shell := task
	shell.Acceptance = []Command{{Argv: []string{"sh", "-c", "go test ./..."}, TimeoutSeconds: 30}}
	if err := shell.Validate(); err == nil {
		t.Fatal("shell acceptance command was accepted")
	}

	outside := task
	outside.Oracle.OverlayPaths = []string{"other_test.go"}
	if err := outside.Validate(); err == nil {
		t.Fatal("oracle outside the permitted scope was accepted")
	}
}
