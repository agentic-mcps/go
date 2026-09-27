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
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/sourceview"
	"github.com/agentic-mcps/go/internal/workspace"
)

func runSourceView(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agentic-go source-view", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspacePath := flags.String("workspace", ".", "Git repository root containing the Go workspace")
	branch := flags.String("branch", "", "local branch or full refs/heads or refs/remotes ref; defaults to main")
	outputPath := flags.String("output", "", "new visible worktree directory outside the source repository")
	includeDirty := flags.Bool("include-dirty", false, "overlay staged, unstaged, and regular untracked files when source HEAD exactly matches the selected branch")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*outputPath) == "" || (*format != "text" && *format != "json") {
		_, _ = fmt.Fprintln(stderr, "agentic-go source-view: --output is required; --format must be text or json")
		return 2
	}
	if invalidCLIArgument(*branch) || invalidCLIArgument(*workspacePath) {
		_, _ = fmt.Fprintln(stderr, "agentic-go source-view: branch and workspace must each be one local argument")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ws, err := workspace.Open(ctx, *workspacePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go source-view: validating workspace: %v\n", err)
		return 1
	}
	runner, err := execution.New(ws, execution.Config{MaxConcurrent: 2, Timeout: 5 * time.Minute})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go source-view: creating process runner: %v\n", err)
		return 1
	}
	result, err := sourceview.Create(ctx, runner, ws, sourceview.Request{
		Branch: *branch, OutputPath: *outputPath, IncludeDirty: *includeDirty,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go source-view: %v\n", err)
		return 1
	}
	if err := writeSourceViewResult(stdout, *format, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go source-view: writing result: %v\n", err)
		return 1
	}
	return 0
}

func writeSourceViewResult(writer io.Writer, format string, result sourceview.Result) error {
	switch format {
	case "json":
		return json.NewEncoder(writer).Encode(result)
	case "text":
		coverage := "complete Git tree checkout"
		if !result.CheckoutComplete {
			coverage = "partial Git tree checkout"
		}
		if _, err := fmt.Fprintf(writer, "Branch view: %s\nCommit: %s\nTree: %s\nPath: %s\nDirty overlay: %t\nCheckout: %s\n", result.Ref, result.Commit, result.Tree, result.ViewPath, result.OverlayIncluded, coverage); err != nil {
			return err
		}
		for _, limitation := range result.CheckoutLimitations {
			if _, err := fmt.Fprintf(writer, "Checkout limitation: %s\n", limitation); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}
