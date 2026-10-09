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
	"sort"
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
# Only a real finding (exit 1) blocks; if agentic-go itself fails, the push goes on.
if ! command -v agentic-go >/dev/null 2>&1; then
  echo "agentic-go not found on PATH; skipping check" >&2
  exit 0
fi
if git rev-parse '@{upstream}' >/dev/null 2>&1; then
  agentic-go check --profile local --base '@{upstream}'
else
  agentic-go check --profile local
fi
status=$?
if [ "$status" -eq 1 ]; then
  exit 1
fi
if [ "$status" -ne 0 ]; then
  echo "agentic-go check could not run (exit $status); not blocking push" >&2
fi
exit 0
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
	if err := writeFileAtomic(path, merged, mode); err != nil {
		return false, err
	}
	return true, nil
}

// writeFileAtomic replaces path with data through a temporary file in the same
// directory, so a crash never leaves a half-written settings file. A symlink
// is followed: the file it points to is replaced and the link is kept.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, initDirMode); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tempPath := temp.Name()
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	chmodErr := os.Chmod(tempPath, mode)
	if err := errors.Join(writeErr, closeErr, chmodErr); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// initHooksValue is the "hooks" object agentic-go adds; field order is the
// written order. Events that already run the gate are left out.
type initHooksValue struct {
	SessionStart []initHookGroup `json:"SessionStart,omitempty"`
	Stop         []initHookGroup `json:"Stop,omitempty"`
}

// initSettings is a complete new settings document.
type initSettings struct {
	Hooks initHooksValue `json:"hooks"`
}

func initFreshHooks(agent string) initHooksValue {
	return initHooksValue{
		SessionStart: []initHookGroup{initHookEntry(agent, initSessionStartTimeout)},
		Stop:         []initHookGroup{initHookEntry(agent, initStopTimeout)},
	}
}

// mergeHookSettings adds the agentic-go hook entries to existing settings
// JSON (nil or blank means a new file). It edits the document in place: only
// the bytes of our additions inside "hooks" are inserted, so key order,
// formatting, and every other value stay exactly as the user wrote them. An
// event that already runs agentic-go check is not changed. changed reports
// whether the output differs from the input.
func mergeHookSettings(existing []byte, agent string) (merged []byte, changed bool, err error) {
	const space = " \t\r\n"
	start := len(existing) - len(bytes.TrimLeft(existing, space))
	end := len(bytes.TrimRight(existing, space))
	if start >= end {
		data, marshalErr := json.MarshalIndent(initSettings{Hooks: initFreshHooks(agent)}, "", "  ")
		if marshalErr != nil {
			return nil, false, fmt.Errorf("encoding settings: %w", marshalErr)
		}
		return append(data, '\n'), true, nil
	}
	document := existing[start:end]
	if !json.Valid(document) {
		return nil, false, errors.New("existing file is not valid JSON")
	}
	if document[0] != '{' {
		return nil, false, errors.New("existing file is not a JSON object")
	}
	top := jsonSpan{start, end}
	members, err := scanJSONMembers(existing, top)
	if err != nil {
		return nil, false, err
	}
	layout := detectJSONLayout(existing, top, members)
	edits, err := layout.hookEdits(existing, top, members, agent)
	if err != nil {
		return nil, false, err
	}
	if len(edits) == 0 {
		return existing, false, nil
	}
	merged = applyJSONEdits(existing, edits)
	if !json.Valid(merged) {
		return nil, false, errors.New("internal error: merged settings are not valid JSON")
	}
	return merged, true, nil
}

// jsonSpan is the byte range [start, end) of one JSON value in a document.
type jsonSpan struct{ start, end int }

// jsonMember is one object member (key set) or array element (key empty).
type jsonMember struct {
	key   string
	value jsonSpan
}

// jsonEdit replaces remove bytes at offset at with text.
type jsonEdit struct {
	text   string
	at     int
	remove int
}

// jsonItem is one value to add to a container; key is used for objects only.
type jsonItem struct {
	value any
	key   string
}

// scanJSONMembers lists the members or elements of the object or array at
// span in a document that is known to be valid JSON.
func scanJSONMembers(data []byte, span jsonSpan) ([]jsonMember, error) {
	decoder := json.NewDecoder(bytes.NewReader(data[span.start:span.end]))
	opening, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("scanning JSON: %w", err)
	}
	isObject := opening == json.Delim('{')
	var members []jsonMember
	for decoder.More() {
		key := ""
		if isObject {
			token, tokenErr := decoder.Token()
			if tokenErr != nil {
				return nil, fmt.Errorf("scanning JSON: %w", tokenErr)
			}
			key, _ = token.(string)
		}
		var raw json.RawMessage
		if decodeErr := decoder.Decode(&raw); decodeErr != nil {
			return nil, fmt.Errorf("scanning JSON: %w", decodeErr)
		}
		end := span.start + int(decoder.InputOffset())
		members = append(members, jsonMember{key: key, value: jsonSpan{end - len(raw), end}})
	}
	return members, nil
}

// lastJSONMember returns the final member with key, which is the one JSON
// parsers honor when a key repeats.
func lastJSONMember(members []jsonMember, key string) (jsonMember, bool) {
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].key == key {
			return members[i], true
		}
	}
	return jsonMember{}, false
}

// jsonLayout describes how the document is formatted so additions match it.
type jsonLayout struct {
	unit      string
	multiline bool
}

func detectJSONLayout(data []byte, top jsonSpan, members []jsonMember) jsonLayout {
	layout := jsonLayout{unit: "  "}
	if len(members) == 0 {
		// An empty document gets the conventional multi-line style.
		layout.multiline = true
		return layout
	}
	layout.multiline = bytes.IndexByte(data[top.start:members[0].value.start], '\n') >= 0
	if indent := jsonLineIndent(data, members[0].value.start); layout.multiline && indent != "" {
		layout.unit = indent
	}
	return layout
}

// jsonLineIndent returns the leading whitespace of the line holding pos.
func jsonLineIndent(data []byte, pos int) string {
	lineStart := bytes.LastIndexByte(data[:pos], '\n') + 1
	end := lineStart
	for end < pos && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[lineStart:end])
}

// hookEdits computes the edits that add the hooks to the top-level object.
func (l jsonLayout) hookEdits(data []byte, top jsonSpan, members []jsonMember, agent string) ([]jsonEdit, error) {
	hooks, found := lastJSONMember(members, "hooks")
	if !found {
		item := jsonItem{key: "hooks", value: initFreshHooks(agent)}
		return []jsonEdit{l.insert(data, top, members, []jsonItem{item}, true)}, nil
	}
	raw := data[hooks.value.start:hooks.value.end]
	switch raw[0] {
	case 'n':
		indent := jsonLineIndent(data, hooks.value.start)
		text, err := l.render(initFreshHooks(agent), indent)
		if err != nil {
			return nil, err
		}
		return []jsonEdit{{at: hooks.value.start, remove: len(raw), text: text}}, nil
	case '{':
		return l.eventEdits(data, hooks.value, agent)
	default:
		return nil, errors.New(`"hooks" is not a JSON object`)
	}
}

// eventEdits computes the edits for the SessionStart and Stop events inside
// the hooks object at span.
func (l jsonLayout) eventEdits(data []byte, span jsonSpan, agent string) ([]jsonEdit, error) {
	members, err := scanJSONMembers(data, span)
	if err != nil {
		return nil, err
	}
	var edits []jsonEdit
	var missing []jsonItem
	for _, event := range initHookEvents() {
		group := initHookEntry(agent, event.timeout)
		member, found := lastJSONMember(members, event.name)
		if !found {
			missing = append(missing, jsonItem{key: event.name, value: []initHookGroup{group}})
			continue
		}
		edit, ok, editErr := l.eventEdit(data, member, event.name, group)
		if editErr != nil {
			return nil, editErr
		}
		if ok {
			edits = append(edits, edit)
		}
	}
	if len(missing) > 0 {
		edits = append(edits, l.insert(data, span, members, missing, true))
	}
	return edits, nil
}

// eventEdit adds group to one event array unless the event already runs the
// gate. ok is false when nothing needs to change.
func (l jsonLayout) eventEdit(data []byte, member jsonMember, name string, group initHookGroup) (edit jsonEdit, ok bool, err error) {
	raw := data[member.value.start:member.value.end]
	switch raw[0] {
	case 'n':
		text, renderErr := l.render([]initHookGroup{group}, jsonLineIndent(data, member.value.start))
		return jsonEdit{at: member.value.start, remove: len(raw), text: text}, true, renderErr
	case '[':
		var entries []any
		if err := json.Unmarshal(raw, &entries); err != nil {
			return jsonEdit{}, false, fmt.Errorf("hooks.%s: %w", name, err)
		}
		if initHasGateHook(entries) {
			return jsonEdit{}, false, nil
		}
		elements, scanErr := scanJSONMembers(data, member.value)
		if scanErr != nil {
			return jsonEdit{}, false, scanErr
		}
		return l.insert(data, member.value, elements, []jsonItem{{value: group}}, false), true, nil
	default:
		return jsonEdit{}, false, fmt.Errorf("hooks.%s is not a JSON array", name)
	}
}

// render encodes value in the document's style; indent is the indentation of
// the line the value starts on.
func (l jsonLayout) render(value any, indent string) (string, error) {
	var data []byte
	var err error
	if l.multiline {
		data, err = json.MarshalIndent(value, indent, l.unit)
	} else {
		data, err = json.Marshal(value)
	}
	if err != nil {
		return "", fmt.Errorf("encoding settings: %w", err)
	}
	return string(data), nil
}

// insert adds items to the container (object or array) at span, in the style
// the container already uses. The items are known to marshal, so encoding
// errors cannot occur.
func (l jsonLayout) insert(data []byte, span jsonSpan, members []jsonMember, items []jsonItem, isObject bool) jsonEdit {
	closing := jsonLineIndent(data, span.start)
	multiline, child := l.multiline, closing+l.unit
	if len(members) > 0 {
		last := members[len(members)-1].value
		multiline = bytes.IndexByte(data[span.start:members[0].value.start], '\n') >= 0
		if multiline {
			child = jsonLineIndent(data, last.start)
		}
		return jsonEdit{at: last.end, text: l.itemsText(items, isObject, multiline, child, true)}
	}
	text := l.itemsText(items, isObject, multiline, child, false)
	if multiline {
		text = "\n" + child + text + "\n" + closing
	}
	return jsonEdit{at: span.start + 1, remove: span.end - span.start - 2, text: text}
}

// itemsText renders items separated by commas; leading adds a comma before the
// first item so it can follow an existing member.
func (l jsonLayout) itemsText(items []jsonItem, isObject, multiline bool, child string, leading bool) string {
	var out strings.Builder
	for i, item := range items {
		if i > 0 || leading {
			out.WriteString(",")
			if multiline {
				out.WriteString("\n" + child)
			}
		}
		if isObject {
			key, _ := json.Marshal(item.key)
			out.Write(key)
			out.WriteString(":")
			if multiline {
				out.WriteString(" ")
			}
		}
		var value []byte
		if multiline {
			value, _ = json.MarshalIndent(item.value, child, l.unit)
		} else {
			value, _ = json.Marshal(item.value)
		}
		out.Write(value)
	}
	return out.String()
}

// applyJSONEdits applies non-overlapping edits to data.
func applyJSONEdits(data []byte, edits []jsonEdit) []byte {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	out := append([]byte(nil), data...)
	for _, edit := range edits {
		tail := append([]byte(edit.text), out[edit.at+edit.remove:]...)
		out = append(out[:edit.at], tail...)
	}
	return out
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
			return fmt.Errorf("%s already exists and differs; not overwriting it. To chain, add this to it: agentic-go check --profile local --base '@{upstream}'; [ \"$?\" -ne 1 ] || exit 1", path)
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
