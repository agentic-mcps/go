package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/agentic-mcps/go/internal/changeimpact"
	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/gopls"
	"github.com/agentic-mcps/go/internal/intelligence"
	"github.com/agentic-mcps/go/internal/tools"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "1.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "verify":
			return runVerify(args[1:], os.Stdout, os.Stderr)
		case "context":
			return runContext(args[1:], os.Stdout, os.Stderr)
		case "doctor":
			return runDoctor(args[1:], os.Stdout, os.Stderr, defaultDoctorDependencies())
		case "mcp-config":
			return runMCPConfig(args[1:], os.Stdout, os.Stderr, defaultMCPConfigDependencies())
		case "contract":
			return runContract(args[1:], os.Stdout, os.Stderr)
		}
	}
	return runMCP(args)
}

func runMCP(args []string) int {
	flags := flag.NewFlagSet("agentic-go", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	logLevel := flags.String("log-level", "info", "lifecycle log level: debug or info")
	maxConcurrent := flags.Int("max-concurrent-loads", 4, "maximum concurrent Go subprocesses")
	maxToolSeconds := flags.Int("max-tool-seconds", 300, "maximum duration of a tool subprocess in seconds")
	showVersion := flags.Bool("version", false, "print version and exit")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "agentic-go: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if *showVersion {
		if _, writeErr := fmt.Fprintf(os.Stdout, "agentic-go %s\n", version); writeErr != nil {
			return 1
		}
		return 0
	}

	level, err := parseLogLevel(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentic-go: %v\n", err)
		return 2
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	if *maxConcurrent < 1 {
		logger.Error("invalid process limit", "max_concurrent_loads", *maxConcurrent)
		return 2
	}
	if *maxToolSeconds < 1 || *maxToolSeconds > 300 {
		logger.Error("invalid tool deadline", "max_tool_seconds", *maxToolSeconds, "allowed", "1..300")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ws, err := workspace.Open(ctx, *workspacePath)
	if err != nil {
		logger.Error("workspace preflight failed", "error", err)
		return 1
	}
	runner, err := execution.New(ws, execution.Config{
		MaxConcurrent: *maxConcurrent,
		Timeout:       time.Duration(*maxToolSeconds) * time.Second,
	})
	if err != nil {
		logger.Error("execution setup failed", "error", err)
		return 1
	}
	installation, err := gopls.Locate(ctx, "", "")
	if err != nil {
		logger.Error("semantic sidecar preflight failed", "error", err)
		return 1
	}
	semanticManager, err := gopls.NewManager(ctx, gopls.Config{Command: installation.Path, Args: []string{"serve"}, Workspace: ws.Root(), ClientVersion: version})
	if err != nil {
		logger.Error("semantic sidecar setup failed", "error", err)
		return 1
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if closeErr := semanticManager.Close(closeCtx); closeErr != nil {
			logger.Error("semantic sidecar shutdown failed", "error", closeErr)
		}
	}()
	impact, err := changeimpact.New(ws, runner)
	if err != nil {
		logger.Error("change analysis setup failed", "error", err)
		return 1
	}
	engine, err := verification.NewEngine(ws, runner, impact, version)
	if err != nil {
		logger.Error("verification setup failed", "error", err)
		return 1
	}
	intelligenceService, err := intelligence.NewCore(ws, runner, semanticManager, impact, engine)
	if err != nil {
		logger.Error("intelligence setup failed", "error", err)
		return 1
	}
	tracer, err := trace.Init()
	if err != nil {
		logger.Error("trace setup failed", "error", err)
		return 1
	}
	defer func() {
		closeErr := tracer.Close()
		if closeErr != nil {
			logger.Error("trace shutdown failed", "error", closeErr)
		}
	}()
	runtime, err := tools.NewRuntimeWithIntelligence(ws, runner, tracer, version, intelligenceService)
	if err != nil {
		logger.Error("tool runtime setup failed", "error", err)
		return 1
	}

	server := tools.NewProductionServer(&mcp.Implementation{Name: "agentic-go", Version: version})
	tools.RegisterProduction(server, runtime)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("server stopped", "error", err)
		return 1
	}
	return 0
}

type optionalPercentage struct {
	set   bool
	value float64
}

type verificationService interface {
	Verify(context.Context, verification.Request) (verification.Report, error)
}

type contextService interface {
	Focus(context.Context, intelligence.FocusRequest) (intelligence.FocusResult, error)
}

type verificationServiceFactory func(
	context.Context,
	*workspace.Workspace,
	*execution.Runner,
	*changeimpact.Analyzer,
) (verificationService, func(), error)

func (p *optionalPercentage) String() string {
	if p == nil || !p.set {
		return ""
	}
	return strconv.FormatFloat(p.value, 'f', -1, 64)
}

func (p *optionalPercentage) Set(value string) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return fmt.Errorf("must be a finite number")
	}
	p.value, p.set = parsed, true
	return nil
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	return runVerifyWithFactory(args, stdout, stderr, newUnifiedVerificationService)
}

func runContext(args []string, stdout, stderr io.Writer) int {
	return runContextWithFactory(args, stdout, stderr, func(ctx context.Context, ws *workspace.Workspace, runner *execution.Runner, impact *changeimpact.Analyzer) (contextService, func(), error) {
		service, closeService, err := newUnifiedVerificationService(ctx, ws, runner, impact)
		if err != nil {
			return nil, closeService, err
		}
		focused, ok := service.(contextService)
		if !ok {
			closeService()
			return nil, func() {}, errors.New("verification service does not support context focus")
		}
		return focused, closeService, nil
	})
}

type contextServiceFactory func(context.Context, *workspace.Workspace, *execution.Runner, *changeimpact.Analyzer) (contextService, func(), error)

func runContextWithFactory(args []string, stdout, stderr io.Writer, factory contextServiceFactory) int {
	flags := flag.NewFlagSet("agentic-go context", flag.ContinueOnError)
	flags.SetOutput(stderr)
	base := flags.String("base", "", "required local commit or ref")
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	packagePattern := flags.String("package", "./...", "Go package scope")
	format := flags.String("format", "text", "context format: text or json")
	race := flags.Bool("race", false, "require race evidence for applicability")
	failOn := flags.String("fail-on", "error", "verification severity policy")
	maxPackages := flags.Int("max-packages", 200, "maximum affected package closure")
	query := flags.String("query", "", "one focus selector: query, symbol-ref, file+line+column, or focus-file/package")
	symbolRef := flags.String("symbol-ref", "", "one current snapshot-bound focus selector; stale refs are rejected")
	file := flags.String("file", "", "source-position selector; use with line and column")
	line := flags.Int("line", 0, "source-position line; use with file and column")
	column := flags.Int("column", 0, "source-position one-based UTF-8 byte column; use with file and line")
	maxBytes := flags.Int("max-bytes", intelligence.DefaultBriefBytes, "focused evidence budget")
	previousPackID := flags.String("previous-pack-id", "", "full refresh selector; use with base only after editing")
	focusFile := flags.String("focus-file", "", "one active Go file focus selector")
	focusPackage := flags.String("focus-package", "", "one active Go package focus selector")
	var minimumCoverage optionalPercentage
	flags.Var(&minimumCoverage, "min-changed-coverage", "verification coverage policy from 0 through 100")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*base) == "" || (*format != "text" && *format != "json") {
		_, _ = fmt.Fprintln(stderr, "agentic-go context: --base is required; --format must be text or json")
		return 2
	}
	if invalidCLIArgument(*base) || invalidCLIArgument(*packagePattern) {
		_, _ = fmt.Fprintln(stderr, "agentic-go context: base and package must each be one local argument")
		return 2
	}
	threshold := verification.FailOn(*failOn)
	switch threshold {
	case verification.FailOnError, verification.FailOnWarning, verification.FailOnInfo, verification.FailOnNone:
	default:
		_, _ = fmt.Fprintln(stderr, "agentic-go context: --fail-on must be error, warning, info, or none")
		return 2
	}
	if *maxPackages < 1 || *maxPackages > 500 || (minimumCoverage.set && (minimumCoverage.value < 0 || minimumCoverage.value > 100)) {
		_, _ = fmt.Fprintln(stderr, "agentic-go context: invalid verification policy")
		return 2
	}
	request := intelligence.FocusRequest{Base: *base, Scope: *packagePattern, FailOn: verification.FailOn(*failOn), MaxPackages: *maxPackages, Race: *race, Query: *query, SymbolRef: intelligence.SymbolRef(*symbolRef), MaxBytes: *maxBytes, PreviousPackID: *previousPackID, FocusFile: *focusFile, FocusPackage: *focusPackage}
	if *file != "" || *line != 0 || *column != 0 {
		request.Position = &intelligence.SourcePosition{File: *file, Line: *line, Column: *column}
	}
	if minimumCoverage.set {
		request.MinChangedCoverage = &minimumCoverage.value
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ws, err := workspace.Open(ctx, *workspacePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go context: workspace preflight failed: %v\n", err)
		return 2
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go context: execution setup failed: %v\n", err)
		return 2
	}
	impact, err := changeimpact.New(ws, runner)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go context: change analysis setup failed: %v\n", err)
		return 2
	}
	service, closeService, err := factory(ctx, ws, runner, impact)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go context: setup failed: %v\n", err)
		return 2
	}
	defer closeService()
	result, err := service.Focus(ctx, request)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go context: %v\n", err)
		return 2
	}
	if err := writeContextResult(stdout, *format, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go context: writing text: %v\n", err)
		return 2
	}
	return 0
}

func writeContextResult(stdout io.Writer, format string, result intelligence.FocusResult) error {
	if format == "json" {
		return json.NewEncoder(stdout).Encode(result)
	}
	if _, err := fmt.Fprintf(stdout, "%s\nAnalysis complete: %t\nVerification present: %t\nNext action: %s\n", intelligence.FocusSummary(result), result.Complete, result.Verification.Present, result.Verification.NextAction); err != nil {
		return err
	}
	for _, reason := range result.Verification.Reasons {
		if _, err := fmt.Fprintf(stdout, "Applicability: %s\n", reason); err != nil {
			return err
		}
	}
	return nil
}

func invalidCLIArgument(value string) bool {
	return strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") || strings.ContainsRune(value, 0)
}

func runVerifyWithFactory(args []string, stdout, stderr io.Writer, factory verificationServiceFactory) int {
	flags := flag.NewFlagSet("agentic-go verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	base := flags.String("base", "", "required local commit or ref")
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	packagePattern := flags.String("package", "./...", "Go package scope")
	format := flags.String("format", "text", "report format: text or json")
	race := flags.Bool("race", false, "include race detection")
	failOn := flags.String("fail-on", "error", "blocking analyzer severity: error, warning, info, or none")
	maxPackages := flags.Int("max-packages", 200, "maximum affected package closure")
	var minimumCoverage optionalPercentage
	flags.Var(&minimumCoverage, "min-changed-coverage", "optional changed-statement coverage minimum from 0 through 100")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if strings.TrimSpace(*base) == "" {
		_, _ = fmt.Fprintln(stderr, "agentic-go verify: --base is required")
		return 2
	}
	if *format != "text" && *format != "json" {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: invalid --format %q (want text or json)\n", *format)
		return 2
	}
	threshold := verification.FailOn(*failOn)
	switch threshold {
	case verification.FailOnError, verification.FailOnWarning, verification.FailOnInfo, verification.FailOnNone:
	default:
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: invalid --fail-on %q (want error, warning, info, or none)\n", *failOn)
		return 2
	}
	if minimumCoverage.set && (minimumCoverage.value < 0 || minimumCoverage.value > 100) {
		_, _ = fmt.Fprintln(stderr, "agentic-go verify: --min-changed-coverage must be between 0 and 100")
		return 2
	}
	if *maxPackages < 1 || *maxPackages > 500 {
		_, _ = fmt.Fprintln(stderr, "agentic-go verify: --max-packages must be between 1 and 500")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ws, err := workspace.Open(ctx, *workspacePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: workspace preflight failed: %v\n", err)
		return 2
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: execution setup failed: %v\n", err)
		return 2
	}
	impact, err := changeimpact.New(ws, runner)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: change analysis setup failed: %v\n", err)
		return 2
	}
	service, closeService, err := factory(ctx, ws, runner, impact)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: verification setup failed: %v\n", err)
		return 2
	}
	defer closeService()
	request := verification.Request{
		Base: *base, Package: *packagePattern, Race: *race, FailOn: threshold, MaxPackages: *maxPackages,
	}
	if minimumCoverage.set {
		request.MinChangedCoverage = &minimumCoverage.value
	}
	report, err := service.Verify(ctx, request)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: %v\n", err)
		return 2
	}
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "agentic-go verify: writing JSON report: %v\n", err)
			return 2
		}
	} else if err := renderTextReport(stdout, report); err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go verify: writing text report: %v\n", err)
		return 2
	}
	return report.Result.ExitCode
}

func newUnifiedVerificationService(
	ctx context.Context,
	ws *workspace.Workspace,
	runner *execution.Runner,
	impact *changeimpact.Analyzer,
) (verificationService, func(), error) {
	engine, err := verification.NewEngine(ws, runner, impact, version)
	if err != nil {
		return nil, func() {}, err
	}
	installation, err := gopls.Locate(ctx, "", "")
	if err != nil {
		return nil, func() {}, fmt.Errorf("semantic sidecar preflight failed: %w", err)
	}
	manager, err := gopls.NewManager(ctx, gopls.Config{Command: installation.Path, Args: []string{"serve"}, Workspace: ws.Root(), ClientVersion: version})
	if err != nil {
		return nil, func() {}, fmt.Errorf("semantic sidecar setup failed: %w", err)
	}
	service, err := intelligence.NewCore(ws, runner, manager, impact, engine)
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Close(closeCtx)
		return nil, func() {}, err
	}
	closeService := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Close(closeCtx)
	}
	return service, closeService, nil
}

func renderTextReport(writer io.Writer, report verification.Report) error {
	write := func(format string, values ...any) error {
		_, err := fmt.Fprintf(writer, format, values...)
		return err
	}
	if err := write("agentic-go verification\n\nStatus: %s\n", report.Result.Status); err != nil {
		return err
	}
	if err := write("Base: %s (%s)\nMerge-base: %s\nSnapshot: %s\n", report.Repository.RequestedBase, shortCommit(report.Repository.BaseCommit), shortCommit(report.Repository.MergeBaseCommit), report.Repository.SnapshotID); err != nil {
		return err
	}
	files := boundedCount(report.Change.FilesTotal, len(report.Change.Files), report.Change.FilesTruncated)
	declarations := boundedCount(report.Change.DeclarationsTotal, len(report.Change.Declarations), report.Change.DeclarationsTruncated)
	packages := boundedCount(report.Impact.PackagesTotal, len(report.Impact.Packages), report.Impact.PackagesTruncated)
	if err := write("Change: %s files, %s declarations\nImpact: %s packages\n\nEvidence:\n", files, declarations, packages); err != nil {
		return err
	}
	for _, evidence := range report.Evidence {
		if err := write("- %s: %s — %s\n", evidence.Kind, evidence.Status, evidence.Summary); err != nil {
			return err
		}
	}
	if len(report.Findings) > 0 {
		heading := "\nFindings:\n"
		if report.FindingsTruncated {
			heading = fmt.Sprintf("\nFindings (%d total; %d shown):\n", report.FindingsTotal, len(report.Findings))
		}
		if err := write("%s", heading); err != nil {
			return err
		}
		for _, finding := range report.Findings {
			location := ""
			if finding.Location != nil {
				location = fmt.Sprintf("%s:%d: ", finding.Location.File, finding.Location.Line)
			}
			if err := write("- %s%s: %s\n", location, finding.Severity, finding.Message); err != nil {
				return err
			}
		}
	}
	if len(report.Risks) > 0 {
		if err := write("\nReview guidance:\n"); err != nil {
			return err
		}
		for _, risk := range report.Risks {
			if err := write("- %s: %s\n", risk.Code, risk.Guidance); err != nil {
				return err
			}
		}
	}
	if len(report.Uncertainties) > 0 {
		if err := write("\nUncertainty:\n"); err != nil {
			return err
		}
		for _, uncertainty := range report.Uncertainties {
			if err := write("- %s: %s\n", uncertainty.Code, uncertainty.Message); err != nil {
				return err
			}
		}
	}
	return write("\n%s. A passing report does not prove the change safe.\n", report.Result.Summary)
}

func boundedCount(total, visible int, truncated bool) string {
	if total < visible {
		total = visible
	}
	if truncated {
		return fmt.Sprintf("%d (%d shown)", total, visible)
	}
	return fmt.Sprintf("%d", total)
}

func shortCommit(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
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
