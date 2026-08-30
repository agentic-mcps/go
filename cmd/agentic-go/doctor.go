package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/gopls"
	"github.com/agentic-mcps/go/internal/intelligence"
	"github.com/agentic-mcps/go/internal/workspace"
)

const (
	doctorStatusOK    = "ok"
	doctorStatusError = "error"
	recoveryClean     = "clean"
	recoveryRequired  = "recovery_required"
	recoveryRecovered = "recovered"
)

type doctorWorkspace struct {
	Root       string
	Toolchain  workspace.Toolchain
	RequiredGo string
}

type doctorDependencies struct {
	inspectWorkspace func(context.Context, string) (doctorWorkspace, error)
	locateSidecar    func(context.Context) (gopls.Installation, error)
	inspectRecovery  func(context.Context, string, bool) (doctorRecovery, error)
}

type doctorReport struct {
	Status    string             `json:"status"`
	Version   string             `json:"version"`
	Workspace doctorWorkspaceOut `json:"workspace"`
	Go        doctorGo           `json:"go"`
	Gopls     doctorGopls        `json:"gopls"`
	Recovery  doctorRecovery     `json:"recovery"`
	Checks    []doctorCheck      `json:"checks"`
}

type doctorWorkspaceOut struct {
	Root string `json:"root"`
}

type doctorGo struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Required string `json:"required"`
}

type doctorGopls struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Bundled bool   `json:"bundled"`
}

type doctorRecovery struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func defaultDoctorDependencies() doctorDependencies {
	return doctorDependencies{
		inspectWorkspace: func(ctx context.Context, path string) (doctorWorkspace, error) {
			ws, err := workspace.Open(ctx, path)
			if err != nil {
				return doctorWorkspace{}, err
			}
			return doctorWorkspace{Root: ws.Root(), Toolchain: ws.Toolchain(), RequiredGo: ws.RequiredGo()}, nil
		},
		locateSidecar: func(ctx context.Context) (gopls.Installation, error) {
			host, err := os.Executable()
			if err != nil {
				return gopls.Installation{}, fmt.Errorf("locating agentic-go executable: %w", err)
			}
			return gopls.Locate(ctx, host, os.Getenv("AGENTIC_GO_GOPLS"))
		},
		inspectRecovery: func(ctx context.Context, path string, recoverState bool) (doctorRecovery, error) {
			ws, err := workspace.Open(ctx, path)
			if err != nil {
				return doctorRecovery{}, err
			}
			runner, err := execution.New(ws, execution.Config{})
			if err != nil {
				return doctorRecovery{}, err
			}
			store, err := intelligence.NewRefactorStore("")
			if err != nil {
				return doctorRecovery{}, err
			}
			result, err := intelligence.RecoverGuardedRefactor(ctx, ws, runner, store, recoverState)
			if err != nil {
				return doctorRecovery{}, err
			}
			switch result.Status {
			case intelligence.RefactorRecoveryRequired:
				return doctorRecovery{Status: recoveryRequired, Message: "an interrupted guarded refactor requires agentic-go doctor --recover"}, nil
			case intelligence.RefactorRecoveryRecovered:
				return doctorRecovery{Status: recoveryRecovered, Message: fmt.Sprintf("restored %d file(s) from the interrupted guarded refactor", result.RecoveredFiles)}, nil
			default:
				return doctorRecovery{Status: recoveryClean, Message: "no interrupted refactor requires recovery"}, nil
			}
		},
	}
}

func runDoctor(args []string, stdout, stderr io.Writer, dependencies doctorDependencies) int {
	flags := flag.NewFlagSet("agentic-go doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	format := flags.String("format", "text", "report format: text or json")
	recoverState := flags.Bool("recover", false, "recover an interrupted guarded refactor")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "agentic-go doctor: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if *format != "text" && *format != "json" {
		_, _ = fmt.Fprintf(stderr, "agentic-go doctor: invalid --format %q (want text or json)\n", *format)
		return 2
	}

	report := doctorReport{
		Status:  doctorStatusOK,
		Version: version,
		Checks:  make([]doctorCheck, 0, 3),
		Recovery: doctorRecovery{
			Status:  recoveryClean,
			Message: "no interrupted refactor requires recovery",
		},
	}
	ctx := context.Background()
	inspected, workspaceErr := dependencies.inspectWorkspace(ctx, *workspacePath)
	if workspaceErr != nil {
		report.Status = doctorStatusError
		report.Checks = append(report.Checks, doctorCheck{Name: "go.workspace", Status: doctorStatusError, Message: workspaceErr.Error()})
	} else {
		report.Workspace.Root = inspected.Root
		report.Go = doctorGo{Path: inspected.Toolchain.Path, Version: inspected.Toolchain.Version, Required: inspected.RequiredGo}
		report.Checks = append(report.Checks, doctorCheck{Name: "go.workspace", Status: doctorStatusOK, Message: "workspace and inherited Go toolchain are compatible"})
	}

	installation, sidecarErr := dependencies.locateSidecar(ctx)
	if sidecarErr != nil {
		report.Status = doctorStatusError
		report.Checks = append(report.Checks, doctorCheck{Name: "gopls.sidecar", Status: doctorStatusError, Message: sidecarErr.Error()})
	} else {
		report.Gopls = doctorGopls{Path: installation.Path, Version: installation.Version, Bundled: installation.Bundled}
		report.Checks = append(report.Checks, doctorCheck{Name: "gopls.sidecar", Status: doctorStatusOK, Message: "pinned semantic sidecar is available"})
	}

	if workspaceErr != nil {
		report.Recovery = doctorRecovery{Status: doctorStatusError, Message: "workspace inspection failed before recovery state could be identified"}
		report.Checks = append(report.Checks, doctorCheck{Name: "refactor.recovery", Status: doctorStatusError, Message: report.Recovery.Message})
	} else {
		recovery, recoveryErr := dependencies.inspectRecovery(ctx, *workspacePath, *recoverState)
		if recoveryErr != nil {
			report.Status = doctorStatusError
			report.Recovery = doctorRecovery{Status: doctorStatusError, Message: recoveryErr.Error()}
			report.Checks = append(report.Checks, doctorCheck{Name: "refactor.recovery", Status: doctorStatusError, Message: recoveryErr.Error()})
		} else {
			report.Recovery = recovery
			checkStatus := doctorStatusOK
			if recovery.Status == recoveryRequired {
				report.Status = doctorStatusError
				checkStatus = doctorStatusError
			}
			report.Checks = append(report.Checks, doctorCheck{Name: "refactor.recovery", Status: checkStatus, Message: recovery.Message})
		}
	}

	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "agentic-go doctor: writing JSON report: %v\n", err)
			return 2
		}
	} else if err := renderDoctorText(stdout, report); err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go doctor: writing text report: %v\n", err)
		return 2
	}
	if report.Status == doctorStatusError {
		return 1
	}
	return 0
}

func renderDoctorText(writer io.Writer, report doctorReport) error {
	if _, err := fmt.Fprintf(writer, "agentic-go doctor\n\nStatus: %s\n", report.Status); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(writer, "- %s: %s - %s\n", check.Name, check.Status, check.Message); err != nil {
			return err
		}
	}
	return nil
}
