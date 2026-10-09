package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	initScopeProject = "project"
	initScopeLocal   = "local"
	initScopeUser    = "user"

	initSessionStartTimeout = 30
	initStopTimeout         = 180
	initNewFileMode         = 0o644
	initHookFileMode        = 0o755
	initDirMode             = 0o755
	initGitTimeout          = 10 * time.Second

	initCodexNote = "Codex asks you to review and trust new hooks: run /hooks in Codex."
)

// initPrePushScript is the .git/hooks/pre-push script agentic-go installs.
const initPrePushScript = `#!/bin/sh
# agentic-go pre-push hook: blocks the push when the change introduced problems.
if ! command -v agentic-go >/dev/null 2>&1; then
  echo "agentic-go not found on PATH; skipping check" >&2
  exit 0
fi
if git rev-parse '@{upstream}' >/dev/null 2>&1; then
  agentic-go check --profile local --base '@{upstream}' || exit 1
else
  agentic-go check --profile local || exit 1
fi
`

// initDependencies are the seams of runInit: tests replace them to avoid the
// real home directory and git.
type initDependencies struct {
	homeDir  func() (string, error)
	hooksDir func(ctx context.Context, workspace string) (string, error)
}

func defaultInitDependencies() initDependencies {
	return initDependencies{homeDir: os.UserHomeDir, hooksDir: gitHooksDir}
}

// gitHooksDir asks git where the repository keeps its hooks.
func gitHooksDir(ctx context.Context, workspace string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, initGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "hooks")
	command.Dir = workspace
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-path hooks: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	dir := strings.TrimSpace(string(output))
	if dir == "" {
		return "", errors.New("git reported no hooks directory")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(workspace, dir)
	}
	return dir, nil
}

// initConfig is the parsed command line of agentic-go init.
//
//nolint:govet // Keep fields grouped by meaning.
type initConfig struct {
	workspace string
	scope     string
	claude    bool
	codex     bool
	prePush   bool
	write     bool
}

func runInit(args []string, stdout, stderr io.Writer) int {
	return runInitWithDependencies(args, stdout, stderr, defaultInitDependencies())
}

func runInitWithDependencies(args []string, stdout, stderr io.Writer, deps initDependencies) int {
	config, ok := parseInitFlags(args, stderr)
	if !ok {
		return 2
	}
	var err error
	switch {
	case config.claude:
		err = initAgentConfig(config, "claude", ".claude", claudeSettingsName(config.scope), stdout, deps)
	case config.codex:
		err = initAgentConfig(config, "codex", ".codex", "hooks.json", stdout, deps)
		if err == nil {
			_, _ = fmt.Fprintln(stderr, initCodexNote)
		}
	default:
		err = initPrePush(config, stdout, deps)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go init: %v\n", err)
		return 1
	}
	return 0
}

func parseInitFlags(args []string, stderr io.Writer) (initConfig, bool) {
	flags := flag.NewFlagSet("agentic-go init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var config initConfig
	flags.BoolVar(&config.claude, "claude", false, "configure Claude Code Stop and SessionStart hooks")
	flags.BoolVar(&config.codex, "codex", false, "configure Codex Stop and SessionStart hooks")
	flags.BoolVar(&config.prePush, "git-pre-push", false, "install a git pre-push hook")
	flags.BoolVar(&config.write, "write", false, "write the configuration instead of printing it")
	flags.StringVar(&config.scope, "scope", initScopeProject, "where to write: project, local, or user")
	flags.StringVar(&config.workspace, "workspace", ".", "project directory")
	if err := flags.Parse(args); err != nil {
		return initConfig{}, false
	}
	if err := validateInitConfig(config, flags.Args()); err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go init: %v\n", err)
		return initConfig{}, false
	}
	return config, true
}

func validateInitConfig(config initConfig, extra []string) error {
	selected := 0
	for _, on := range []bool{config.claude, config.codex, config.prePush} {
		if on {
			selected++
		}
	}
	switch {
	case len(extra) != 0:
		return fmt.Errorf("unexpected arguments: %s", strings.Join(extra, " "))
	case selected != 1:
		return errors.New("choose exactly one of --claude, --codex, or --git-pre-push")
	case config.scope != initScopeProject && config.scope != initScopeLocal && config.scope != initScopeUser:
		return fmt.Errorf("invalid --scope %q (want project, local, or user)", config.scope)
	case config.codex && config.scope == initScopeLocal:
		return errors.New("--scope local is not available for --codex (want project or user)")
	}
	return nil
}

func claudeSettingsName(scope string) string {
	if scope == initScopeLocal {
		return "settings.local.json"
	}
	return "settings.json"
}

// initHookCommand returns the hook command line for an agent.
func initHookCommand(agent string) string {
	return "agentic-go check --hook " + agent
}

// initHookCommandEntry is one command hook; field order is the written order.
type initHookCommandEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// initHookGroup is one hook group with a single command hook.
type initHookGroup struct {
	Hooks []initHookCommandEntry `json:"hooks"`
}

func initHookEntry(agent string, timeout int) initHookGroup {
	return initHookGroup{Hooks: []initHookCommandEntry{{Type: "command", Command: initHookCommand(agent), Timeout: timeout}}}
}

// initHookEvents lists the events agentic-go registers and their timeouts.
func initHookEvents() []struct {
	name    string
	timeout int
} {
	return []struct {
		name    string
		timeout int
	}{{checkEventSessionStart, initSessionStartTimeout}, {checkEventStop, initStopTimeout}}
}

func initAgentConfig(config initConfig, agent, dirName, fileName string, stdout io.Writer, deps initDependencies) error {
	if !config.write {
		out, _, err := mergeHookSettings(nil, agent)
		if err != nil {
			return err
		}
		_, err = stdout.Write(out)
		return err
	}
	base := config.workspace
	if config.scope == initScopeUser {
		home, err := deps.homeDir()
		if err != nil {
			return fmt.Errorf("locating home directory: %w", err)
		}
		base = home
	}
	path := filepath.Join(base, dirName, fileName)
	changed, err := writeHookSettings(path, agent)
	if err != nil {
		return err
	}
	if changed {
		_, err = fmt.Fprintf(stdout, "agentic-go init: wrote %s\n", path)
	} else {
		_, err = fmt.Fprintf(stdout, "agentic-go init: %s already configured\n", path)
	}
	return err
}

// writeHookSettings merges the agentic-go hooks into the settings file at
// path, creating it when missing. It leaves the file untouched when the hooks
// are already present or the existing content is not valid JSON.
func writeHookSettings(path, agent string) (bool, error) {
	existing, err := os.ReadFile(path)
	mode := os.FileMode(initNewFileMode)
	switch {
	case err == nil:
		info, statErr := os.Stat(path)
		if statErr != nil {
			return false, fmt.Errorf("inspecting %s: %w", path, statErr)
		}
		mode = info.Mode().Perm()
	case errors.Is(err, os.ErrNotExist):
		existing = nil
	default:
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	merged, changed, err := mergeHookSettings(existing, agent)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), initDirMode); err != nil {
		return false, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, merged, mode); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// mergeHookSettings adds the agentic-go hook entries to existing settings
// JSON (nil or blank means an empty object). Every other key and every
// existing hook is preserved; an event that already runs agentic-go check is
// not changed. changed reports whether the output differs in content.
func mergeHookSettings(existing []byte, agent string) (merged []byte, changed bool, err error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(existing))
		decoder.UseNumber()
		if decodeErr := decoder.Decode(&root); decodeErr != nil {
			return nil, false, fmt.Errorf("existing file is not a valid JSON object: %w", decodeErr)
		}
		if decoder.More() {
			return nil, false, errors.New("existing file has trailing content after the JSON object")
		}
	}
	hooks, err := initChildObject(root, "hooks")
	if err != nil {
		return nil, false, err
	}
	for _, event := range initHookEvents() {
		entries, entriesErr := initEntries(hooks, event.name)
		if entriesErr != nil {
			return nil, false, entriesErr
		}
		if initHasGateHook(entries) {
			continue
		}
		hooks[event.name] = append(entries, initHookEntry(agent, event.timeout))
		changed = true
	}
	root["hooks"] = hooks
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(root); encodeErr != nil {
		return nil, false, fmt.Errorf("encoding settings: %w", encodeErr)
	}
	return out.Bytes(), changed, nil
}

// initChildObject returns root[key] as an object, creating it when absent.
func initChildObject(root map[string]any, key string) (map[string]any, error) {
	value, present := root[key]
	if !present || value == nil {
		return map[string]any{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q is not a JSON object", key)
	}
	return object, nil
}

// initEntries returns the hook groups registered for an event.
func initEntries(hooks map[string]any, event string) ([]any, error) {
	value, present := hooks[event]
	if !present || value == nil {
		return []any{}, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("hooks.%s is not a JSON array", event)
	}
	return entries, nil
}

// initHasGateHook reports whether any hook group already runs agentic-go check.
func initHasGateHook(entries []any) bool {
	for _, entry := range entries {
		group, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		commands, _ := group["hooks"].([]any)
		for _, item := range commands {
			hook, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if command, _ := hook["command"].(string); initIsGateCommand(command) {
				return true
			}
		}
	}
	return false
}

// initIsGateCommand matches "agentic-go check ..." with or without a path.
func initIsGateCommand(command string) bool {
	fields := strings.Fields(command)
	return len(fields) >= 2 && filepath.Base(fields[0]) == "agentic-go" && fields[1] == "check"
}

func initPrePush(config initConfig, stdout io.Writer, deps initDependencies) error {
	if !config.write {
		_, err := io.WriteString(stdout, initPrePushScript)
		return err
	}
	ctx := context.Background()
	dir, err := deps.hooksDir(ctx, config.workspace)
	if err != nil {
		return fmt.Errorf("locating git hooks directory: %w", err)
	}
	path := filepath.Join(dir, "pre-push")
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if string(existing) != initPrePushScript {
			return fmt.Errorf("%s already exists and differs; not overwriting it. To chain, add this to it: agentic-go check --profile local --base '@{upstream}' || exit 1", path)
		}
		if chmodErr := os.Chmod(path, initHookFileMode); chmodErr != nil {
			return fmt.Errorf("making %s executable: %w", path, chmodErr)
		}
		_, err = fmt.Fprintf(stdout, "agentic-go init: %s already installed\n", path)
		return err
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err = os.MkdirAll(dir, initDirMode); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err = os.WriteFile(path, []byte(initPrePushScript), initHookFileMode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err = os.Chmod(path, initHookFileMode); err != nil {
		return fmt.Errorf("making %s executable: %w", path, err)
	}
	_, err = fmt.Fprintf(stdout, "agentic-go init: wrote %s\n", path)
	return err
}
