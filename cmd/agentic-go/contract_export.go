package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/intelligence"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

type contractExportDependencies struct {
	export func(context.Context, string, intelligence.ContractExportRequest) (intelligence.ContractExport, error)
}

func defaultContractExportDependencies() contractExportDependencies {
	return contractExportDependencies{export: func(ctx context.Context, workspacePath string, request intelligence.ContractExportRequest) (intelligence.ContractExport, error) {
		ws, err := workspace.Open(ctx, workspacePath)
		if err != nil {
			return intelligence.ContractExport{}, fmt.Errorf("workspace preflight failed: %w", err)
		}
		runner, err := execution.New(ws, execution.Config{MaxConcurrent: 4})
		if err != nil {
			return intelligence.ContractExport{}, fmt.Errorf("execution setup failed: %w", err)
		}
		store, err := intelligence.NewContractStore("")
		if err != nil {
			return intelligence.ContractExport{}, fmt.Errorf("contract store setup failed: %w", err)
		}
		return intelligence.ExportChangeContract(ctx, ws, runner, store, request)
	}}
}

func runContract(args []string, stdout, stderr io.Writer) int {
	return runContractWithDependencies(args, stdout, stderr, defaultContractExportDependencies())
}

func runContractWithDependencies(args []string, stdout, stderr io.Writer, dependencies contractExportDependencies) int {
	if len(args) == 0 || args[0] != "export" {
		_, _ = fmt.Fprintln(stderr, "agentic-go contract: subcommand export is required")
		return 2
	}
	flags := flag.NewFlagSet("agentic-go contract export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	destination := flags.String("output", "", "workspace-relative contract export path")
	contractID := flags.String("id", "", "optional contract ID; default current active contract")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "agentic-go contract export: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if strings.TrimSpace(*destination) == "" {
		_, _ = fmt.Fprintln(stderr, "agentic-go contract export: --output is required")
		return 2
	}
	if *format != "text" && *format != "json" {
		_, _ = fmt.Fprintf(stderr, "agentic-go contract export: invalid --format %q (want text or json)\n", *format)
		return 2
	}

	if dependencies.export == nil {
		_, _ = fmt.Fprintln(stderr, "agentic-go contract export: export dependency is unavailable")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exported, err := dependencies.export(ctx, *workspacePath, intelligence.ContractExportRequest{ContractID: *contractID, Destination: *destination})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go contract export: %v\n", err)
		return 1
	}
	if *format == "json" {
		if err := json.NewEncoder(stdout).Encode(exported); err != nil {
			_, _ = fmt.Fprintf(stderr, "agentic-go contract export: writing JSON result: %v\n", err)
			return 1
		}
		return 0
	}
	_, err = fmt.Fprintf(stdout, "Exported contract %s to %s (%s)\n", exported.ContractID, exported.Path, exported.Digest)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go contract export: writing text result: %v\n", err)
		return 1
	}
	return 0
}
