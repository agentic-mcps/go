package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/gopls"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

func TestRunDoctorReportsWorkspaceSidecarAndRecovery(t *testing.T) {
	dependencies := doctorDependencies{
		inspectWorkspace: func(context.Context, string) (doctorWorkspace, error) {
			return doctorWorkspace{
				Root:       "/repo",
				Toolchain:  workspace.Toolchain{Path: "/toolchains/go", Version: "go1.27.0"},
				RequiredGo: "go1.25.0",
			}, nil
		},
		locateSidecar: func(context.Context) (gopls.Installation, error) {
			return gopls.Installation{Path: "/bin/agentic-go-gopls", Version: gopls.SupportedVersion, Bundled: true}, nil
		},
		inspectRecovery: func(context.Context, string, bool) (doctorRecovery, error) {
			return doctorRecovery{Status: recoveryRecovered, Message: "restored 1 file(s)"}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if exit := runDoctor([]string{"--format", "json", "--recover"}, &stdout, &stderr, dependencies); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != doctorStatusOK || report.Go.Version != "go1.27.0" || report.Gopls.Version != gopls.SupportedVersion {
		t.Fatalf("report = %#v", report)
	}
	if report.Recovery.Status != recoveryRecovered || len(report.Checks) != 3 {
		t.Fatalf("recovery/checks = %#v / %#v", report.Recovery, report.Checks)
	}
}

func TestRunDoctorReturnsStructuredUnhealthyReport(t *testing.T) {
	dependencies := doctorDependencies{
		inspectWorkspace: func(context.Context, string) (doctorWorkspace, error) {
			return doctorWorkspace{}, errors.New("Go 1.24 is unsupported")
		},
		locateSidecar: func(context.Context) (gopls.Installation, error) {
			return gopls.Installation{}, errors.New("pinned companion missing")
		},
		inspectRecovery: func(context.Context, string, bool) (doctorRecovery, error) {
			return doctorRecovery{}, errors.New("should not run")
		},
	}
	var stdout, stderr bytes.Buffer
	if exit := runDoctor([]string{"--format", "json"}, &stdout, &stderr, dependencies); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != doctorStatusError || len(report.Checks) != 3 {
		t.Fatalf("report = %#v", report)
	}
	if !strings.Contains(report.Checks[0].Message, "Go 1.24") || !strings.Contains(report.Checks[1].Message, "companion") {
		t.Fatalf("checks = %#v", report.Checks)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDoctorFailsClosedWhenRecoveryIsPending(t *testing.T) {
	dependencies := doctorDependencies{
		inspectWorkspace: func(context.Context, string) (doctorWorkspace, error) {
			return doctorWorkspace{Root: "/repo", Toolchain: workspace.Toolchain{Path: "/go", Version: "go1.27.0"}}, nil
		},
		locateSidecar: func(context.Context) (gopls.Installation, error) {
			return gopls.Installation{Path: "/gopls", Version: gopls.SupportedVersion}, nil
		},
		inspectRecovery: func(context.Context, string, bool) (doctorRecovery, error) {
			return doctorRecovery{Status: recoveryRequired, Message: "run doctor --recover"}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if exit := runDoctor([]string{"--format", "json"}, &stdout, &stderr, dependencies); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Recovery.Status != recoveryRequired || report.Checks[2].Status != doctorStatusError {
		t.Fatalf("report = %#v", report)
	}
}

func TestRunDoctorRejectsInvalidFormatBeforeInspection(t *testing.T) {
	called := false
	dependencies := doctorDependencies{
		inspectWorkspace: func(context.Context, string) (doctorWorkspace, error) {
			called = true
			return doctorWorkspace{}, nil
		},
		locateSidecar: func(context.Context) (gopls.Installation, error) {
			called = true
			return gopls.Installation{}, nil
		},
		inspectRecovery: func(context.Context, string, bool) (doctorRecovery, error) {
			called = true
			return doctorRecovery{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if exit := runDoctor([]string{"--format", "yaml"}, &stdout, &stderr, dependencies); exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if called || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--format") {
		t.Fatalf("called=%v stdout=%q stderr=%q", called, stdout.String(), stderr.String())
	}
}
