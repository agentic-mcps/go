package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/gate"
	"github.com/agentic-mcps/go/internal/workspace"
)

const (
	// checkOutputLimit bounds the output of one gate subprocess.
	checkOutputLimit = 64 << 20
	// checkTimeoutSlack is added to the gate budget to form the runner timeout,
	// so the budget, not the runner deadline, ends a slow run.
	checkTimeoutSlack = 2 * time.Minute
	// checkTextLimit is the byte budget of the text report.
	checkTextLimit = 2048
	// checkMaxBlocks is how many times a hook may block one agent session.
	checkMaxBlocks = 3
	// checkMaxPackagesLimit is the largest accepted --max-packages value.
	checkMaxPackagesLimit = 500
	// checkSessionStartTimeout bounds the command SessionStart runs.
	checkSessionStartTimeout = 10 * time.Second

	checkHookClaude = "claude"
	checkHookCodex  = "codex"

	checkEventSessionStart = "SessionStart"
	checkEventStop         = "Stop"
	checkEventSubagentStop = "SubagentStop"
	checkPlanMode          = "plan"
)

// checkProfileBudgets duplicates the profile budgets in internal/gate/gate.go
// (the profiles table) so the runner timeout can be sized before the gate
// exists. Keep them equal; TestCheckRunnerTimeoutCoversTheBudget pins the
// values this command assumes.
var checkProfileBudgets = map[gate.Profile]time.Duration{
	gate.ProfileHook:  120 * time.Second,
	gate.ProfileLocal: 10 * time.Minute,
	gate.ProfileCI:    20 * time.Minute,
}

// checkRunner is the part of the gate the check command drives.
type checkRunner interface {
	Run(context.Context, gate.Options) (gate.Result, error)
}

// checkEnvironment is everything one check run needs. store is nil when the
// state directory is unavailable.
type checkEnvironment struct {
	gate  checkRunner
	store *gate.Store
	close func()
}

// checkDependencies are the seams of runCheck: tests replace them to avoid the
// real workspace, Go toolchain, and state directory.
type checkDependencies struct {
	open          func(ctx context.Context, root string, timeout time.Duration, stderr io.Writer) (checkEnvironment, error)
	openStore     func(root string) (*gate.Store, error)
	startHead     func(ctx context.Context, root string) string
	configDigests func(ctx context.Context, root string) map[string]string
	getenv        func(string) string
	stdin         io.Reader
}

func defaultCheckDependencies() checkDependencies {
	return checkDependencies{
		open: openCheckEnvironment, openStore: openSessionStore, startHead: gitStartHead,
		configDigests: gitConfigDigests, getenv: os.Getenv, stdin: os.Stdin,
	}
}

// openSessionStore opens the state store the gate itself would use for root.
// The gate keys its store by the symlink-resolved workspace root, so the
// session recorded at SessionStart must be keyed the same way.
func openSessionStore(root string) (*gate.Store, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace path: %w", err)
	}
	return gate.OpenStore(resolved)
}

// gitStartHead returns the current commit of the repository at root, or an
// empty string when there is none (an empty repository, or no git).
func gitStartHead(ctx context.Context, root string) string {
	ctx, cancel := context.WithTimeout(ctx, checkSessionStartTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// gitConfigDigests records the gate-configuration files of the workspace at
// root, or nil when they cannot be listed (then every configuration edit is
// reported, as without a session record).
func gitConfigDigests(ctx context.Context, root string) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, checkSessionStartTimeout)
	defer cancel()
	git := func(ctx context.Context, args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = root
		return command.Output()
	}
	digests, err := gate.ConfigDigests(ctx, git, root)
	if err != nil {
		return nil
	}
	return digests
}

// openCheckEnvironment builds the real workspace, runner, store, and gate. A
// state directory failure is not fatal: the gate runs without caching.
func openCheckEnvironment(ctx context.Context, root string, timeout time.Duration, stderr io.Writer) (checkEnvironment, error) {
	ws, err := workspace.Open(ctx, root)
	if err != nil {
		return checkEnvironment{}, fmt.Errorf("workspace preflight failed: %w", err)
	}
	runner, err := execution.New(ws, execution.Config{OutputLimit: checkOutputLimit, Timeout: timeout})
	if err != nil {
		return checkEnvironment{}, fmt.Errorf("execution setup failed: %w", err)
	}
	store, err := gate.OpenStore(ws.Root())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go check: running without cache or session state: %v\n", err)
		store = nil
	}
	g, err := gate.New(ws, runner, version, store)
	if err != nil {
		return checkEnvironment{}, fmt.Errorf("gate setup failed: %w", err)
	}
	return checkEnvironment{gate: g, store: store, close: func() {}}, nil
}

// checkConfig is the parsed command line of agentic-go check.
//
//nolint:govet // Keep fields grouped by meaning.
type checkConfig struct {
	workspace       string
	base            string
	profile         string
	format          string
	hook            string
	skip            string
	budget          time.Duration
	maxPackages     int
	race            bool
	requireCoverage bool
	noCache         bool
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	return runCheckWithDependencies(args, stdout, stderr, defaultCheckDependencies())
}

func runCheckWithDependencies(args []string, stdout, stderr io.Writer, deps checkDependencies) int {
	config, exit, ok := parseCheckFlags(args, stderr)
	if !ok {
		return exit
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if config.hook != "" {
		return runCheckHook(ctx, config, stdout, stderr, deps)
	}
	return runCheckCLI(ctx, config, stdout, stderr, deps)
}

// checkWantsHook reports whether the arguments ask for hook mode, even when
// they are otherwise malformed.
func checkWantsHook(args []string) bool {
	for _, arg := range args {
		name := strings.TrimLeft(arg, "-")
		if arg != name && (name == "hook" || strings.HasPrefix(name, "hook=")) {
			return true
		}
	}
	return false
}

// parseCheckFlags parses the command line. When ok is false the caller returns
// exit: 0 for --help, 2 for usage errors, and 0 for a hook invocation, because
// a misconfigured hook must not block an agent (exit 2 would).
func parseCheckFlags(args []string, stderr io.Writer) (config checkConfig, exit int, ok bool) {
	flags := flag.NewFlagSet("agentic-go check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&config.workspace, "workspace", ".", "Go workspace root")
	flags.StringVar(&config.base, "base", "", "base ref to compare against; empty detects it")
	flags.StringVar(&config.profile, "profile", "", "profile: local, hook, or ci (default local, ci when CI=true)")
	flags.StringVar(&config.format, "format", "text", "report format: text or json")
	flags.StringVar(&config.hook, "hook", "", "run as an agent hook: claude or codex")
	flags.BoolVar(&config.race, "race", false, "run affected packages with the race detector")
	flags.BoolVar(&config.requireCoverage, "require-coverage", false, "block on uncovered changed lines")
	flags.StringVar(&config.skip, "skip", "", "go test -skip pattern")
	flags.IntVar(&config.maxPackages, "max-packages", 0, "maximum tested package closure; 0 selects the profile default")
	flags.DurationVar(&config.budget, "budget", 0, "time budget for the whole run; 0 selects the profile default")
	flags.BoolVar(&config.noCache, "no-cache", false, "ignore cached results")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return checkConfig{}, 0, false
		}
		return checkConfig{}, checkUsageExit(args), false
	}
	if err := validateCheckConfig(config, flags.Args()); err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go check: %v\n", err)
		return checkConfig{}, checkUsageExit(args), false
	}
	return config, 0, true
}

func checkUsageExit(args []string) int {
	if checkWantsHook(args) {
		return 0
	}
	return 2
}

func validateCheckConfig(config checkConfig, extra []string) error {
	switch {
	case len(extra) != 0:
		return fmt.Errorf("unexpected arguments: %s", strings.Join(extra, " "))
	case config.format != "text" && config.format != "json":
		return fmt.Errorf("invalid --format %q (want text or json)", config.format)
	case config.hook != "" && config.hook != checkHookClaude && config.hook != checkHookCodex:
		return fmt.Errorf("invalid --hook %q (want claude or codex)", config.hook)
	case config.profile != "" && !validCheckProfile(gate.Profile(config.profile)):
		return fmt.Errorf("invalid --profile %q (want local, hook, or ci)", config.profile)
	case config.maxPackages < 0 || config.maxPackages > checkMaxPackagesLimit:
		return fmt.Errorf("--max-packages must be between 0 and %d", checkMaxPackagesLimit)
	case config.budget < 0:
		return errors.New("--budget must not be negative")
	case invalidCLIArgument(config.base) || invalidCLIArgument(config.skip):
		return errors.New("--base and --skip must each be one local argument")
	}
	return nil
}

func validCheckProfile(profile gate.Profile) bool {
	_, ok := checkProfileBudgets[profile]
	return ok
}

// resolveCheckProfile picks the explicit profile, else ci when CI=true.
func resolveCheckProfile(config checkConfig, getenv func(string) string) gate.Profile {
	if config.profile != "" {
		return gate.Profile(config.profile)
	}
	if getenv != nil && getenv("CI") == "true" {
		return gate.ProfileCI
	}
	return gate.ProfileLocal
}

func (c checkConfig) options(profile gate.Profile) gate.Options {
	return gate.Options{
		Base: c.base, Profile: profile, Budget: c.budget, Race: c.race,
		RequireCoverage: c.requireCoverage, Skip: c.skip, MaxPackages: c.maxPackages, NoCache: c.noCache,
	}
}

// timeout sizes the runner deadline so it never ends a run before the budget.
func (c checkConfig) timeout(profile gate.Profile) time.Duration {
	budget := c.budget
	if budget <= 0 {
		budget = checkProfileBudgets[profile]
	}
	return budget + checkTimeoutSlack
}

func runCheckCLI(ctx context.Context, config checkConfig, stdout, stderr io.Writer, deps checkDependencies) int {
	profile := resolveCheckProfile(config, deps.getenv)
	env, err := deps.open(ctx, config.workspace, config.timeout(profile), stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go check: %v\n", err)
		return 2
	}
	if env.close != nil {
		defer env.close()
	}
	result, err := env.gate.Run(ctx, config.options(profile))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go check: %v\n", err)
		return 2
	}
	if config.format == "json" {
		err = gate.WriteJSON(stdout, result)
	} else {
		err = gate.WriteText(stdout, result, checkTextLimit)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go check: %v\n", err)
		return 2
	}
	return checkExitCode(result, profile, stderr)
}

// checkExitCode maps the verdict to the process exit status. Unknown fails
// only the ci profile; a developer's local run is never failed by missing
// evidence.
func checkExitCode(result gate.Result, profile gate.Profile, stderr io.Writer) int {
	switch result.Verdict {
	case gate.VerdictPass:
		return 0
	case gate.VerdictBlock:
		return 1
	default:
		if profile == gate.ProfileCI {
			return 2
		}
		note := "evidence was incomplete"
		if len(result.Notes) > 0 {
			note = result.Notes[0]
		}
		_, _ = fmt.Fprintf(stderr, "agentic-go check: verdict unknown (not failing a %s run): %s\n", profile, note)
		return 0
	}
}

// runCheckHook serves an agent hook. It always returns 0: the gate failing
// must never block an agent, and exit code 2 would.
func runCheckHook(ctx context.Context, config checkConfig, stdout, stderr io.Writer, deps checkDependencies) int {
	reporter := hookReporter{kind: config.hook, stdout: stdout, stderr: stderr}
	failed := false
	defer func() {
		if recovered := recover(); recovered != nil && !failed {
			reporter.fail(fmt.Errorf("internal error: %v", recovered))
		}
	}()
	event, err := gate.ParseHookInput(deps.stdin)
	if err != nil {
		failed = true
		reporter.fail(err)
		return 0
	}
	if event.PermissionMode == checkPlanMode {
		return 0
	}
	start := event.Cwd
	if start == "" {
		start = config.workspace
	}
	root, found := findGoRoot(start)
	if !found {
		return 0
	}
	switch event.Name {
	case checkEventSessionStart:
		if err := recordSessionStart(ctx, root, event, deps); err != nil {
			_, _ = fmt.Fprintf(stderr, "agentic-go check: recording session start: %v\n", err)
		}
	case checkEventStop, checkEventSubagentStop:
		out, err := decideHookStop(ctx, config, root, event, stderr, deps)
		if err != nil {
			failed = true
			reporter.fail(err)
			return 0
		}
		reporter.write(out)
	}
	return 0
}

// findGoRoot walks up from start to the nearest directory holding go.mod or
// go.work.
func findGoRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		for _, name := range []string{"go.mod", "go.work"} {
			if info, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil && !info.IsDir() {
				return dir, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// recordSessionStart remembers the commit an agent session started from and
// the gate-configuration files at that moment, so earlier human commits and
// configuration edits are not blamed on the agent. It runs the VCS commands
// directly in root and never builds the workspace, so it stays fast.
func recordSessionStart(ctx context.Context, root string, event gate.HookEvent, deps checkDependencies) error {
	store, err := deps.openStore(root)
	if err != nil {
		return err
	}
	session, err := store.LoadSession(event.SessionID)
	if err != nil {
		return err
	}
	if session.StartHead != "" {
		return nil
	}
	session.StartHead = deps.startHead(ctx, root)
	if session.ConfigDigests == nil {
		session.ConfigDigests = deps.configDigests(ctx, root)
	}
	if session.StartHead == "" && session.ConfigDigests == nil {
		return nil
	}
	return store.SaveSession(session)
}

// decideHookStop runs the gate for a Stop event and applies the stop policy.
func decideHookStop(ctx context.Context, config checkConfig, root string, event gate.HookEvent, stderr io.Writer, deps checkDependencies) (gate.HookOutput, error) {
	env, err := deps.open(ctx, root, config.timeout(gate.ProfileHook), stderr)
	if err != nil {
		return gate.HookOutput{}, err
	}
	if env.close != nil {
		defer env.close()
	}
	session := gate.Session{ID: event.SessionID}
	if env.store != nil {
		if loaded, loadErr := env.store.LoadSession(event.SessionID); loadErr == nil {
			session = loaded
		}
	}
	options := config.options(gate.ProfileHook)
	options.SessionBase = session.StartHead
	options.SessionConfigDigests = session.ConfigDigests
	result, err := env.gate.Run(ctx, options)
	if err != nil {
		return gate.HookOutput{}, err
	}
	out, session := gate.DecideStop(result, session, checkMaxBlocks)
	if env.store != nil {
		if saveErr := env.store.SaveSession(session); saveErr != nil {
			_, _ = fmt.Fprintf(stderr, "agentic-go check: saving session: %v\n", saveErr)
		}
	}
	return out, nil
}

// hookReporter prints hook output in the form each agent accepts.
type hookReporter struct {
	stdout io.Writer
	stderr io.Writer
	kind   string
}

// codexStopOutput is the only shape Codex accepts for a blocking Stop: extra
// fields are rejected.
type codexStopOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (r hookReporter) write(out gate.HookOutput) {
	if r.kind == checkHookCodex {
		r.writeCodex(out)
		return
	}
	if out == (gate.HookOutput{}) {
		return
	}
	r.encode(out)
}

func (r hookReporter) writeCodex(out gate.HookOutput) {
	if out.SystemMessage != "" {
		_, _ = fmt.Fprintln(r.stderr, out.SystemMessage)
	}
	if out.Decision == "block" {
		r.encode(codexStopOutput{Decision: out.Decision, Reason: out.Reason})
		return
	}
	_, _ = fmt.Fprintln(r.stdout, "{}")
}

// fail reports that the gate itself could not run, without blocking the agent.
func (r hookReporter) fail(err error) {
	message := "agentic-go check could not run: " + err.Error()
	if r.kind == checkHookCodex {
		_, _ = fmt.Fprintln(r.stderr, message)
		_, _ = fmt.Fprintln(r.stdout, "{}")
		return
	}
	r.encode(gate.HookOutput{SystemMessage: message})
}

func (r hookReporter) encode(value any) {
	encoder := json.NewEncoder(r.stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintf(r.stderr, "agentic-go check: writing hook output: %v\n", err)
	}
}
