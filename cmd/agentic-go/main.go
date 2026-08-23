package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("agentic-go", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	logLevel := flags.String("log-level", "info", "lifecycle log level: debug or info")
	showVersion := flags.Bool("version", false, "print version and exit")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "agentic-go: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if *showVersion {
		fmt.Fprintf(os.Stdout, "agentic-go %s\n", version)
		return 0
	}

	level, err := parseLogLevel(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentic-go: %v\n", err)
		return 2
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := workspace.Open(ctx, *workspacePath); err != nil {
		logger.Error("workspace preflight failed", "error", err)
		return 1
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "agentic-go", Version: version},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}},
	)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("server stopped", "error", err)
		return 1
	}
	return 0
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	default:
		return 0, fmt.Errorf("invalid --log-level %q (want debug or info)", value)
	}
}
