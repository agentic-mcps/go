package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func initTestDeps(home, hooks string) initDependencies {
	return initDependencies{
		homeDir:  func() (string, error) { return home, nil },
		hooksDir: func(context.Context, string) (string, error) { return hooks, nil },
	}
}

func runInitTest(t *testing.T, deps initDependencies, args ...string) (exit int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	exit = runInitWithDependencies(args, &out, &errOut, deps)
	return exit, out.String(), errOut.String()
}

func wantHookConfig(agent string) map[string]any {
	command := "agentic-go check --hook " + agent
	group := func(timeout float64) []any {
		return []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": timeout}}}}
	}
	return map[string]any{"hooks": map[string]any{"SessionStart": group(30), "Stop": group(180)}}
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("%s is not valid JSON: %v\n%s", path, err, data)
	}
	return decoded
}

func TestInitPrintsAgentConfig(t *testing.T) {
	tests := []struct {
		name       string
		agent      string
		wantStderr string
	}{
		{name: "claude", agent: "claude"},
		{name: "codex", agent: "codex", wantStderr: "Codex asks you to review and trust new hooks: run /hooks in Codex."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			exit, stdout, stderr := runInitTest(t, initTestDeps(dir, dir), "--"+test.agent, "--workspace", dir)
			if exit != 0 {
				t.Fatalf("exit = %d, stderr %q", exit, stderr)
			}
			var decoded map[string]any
			if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			if !reflect.DeepEqual(decoded, wantHookConfig(test.agent)) {
				t.Fatalf("config = %v", decoded)
			}
			if strings.TrimSpace(stderr) != test.wantStderr {
				t.Fatalf("stderr = %q, want %q", stderr, test.wantStderr)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("print mode wrote files: %v", entries)
			}
		})
	}
}

func TestInitWriteTargetsByScope(t *testing.T) {
	tests := []struct {
		name string
		want string
		args []string
	}{
		{name: "claude project", args: []string{"--claude"}, want: "project/.claude/settings.json"},
		{name: "claude local", args: []string{"--claude", "--scope", "local"}, want: "project/.claude/settings.local.json"},
		{name: "claude user", args: []string{"--claude", "--scope", "user"}, want: "home/.claude/settings.json"},
		{name: "codex project", args: []string{"--codex"}, want: "project/.codex/hooks.json"},
		{name: "codex user", args: []string{"--codex", "--scope", "user"}, want: "home/.codex/hooks.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
			for _, dir := range []string{project, home} {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string{"--write", "--workspace", project}, test.args...)
			exit, stdout, stderr := runInitTest(t, initTestDeps(home, ""), args...)
			if exit != 0 || !strings.Contains(stdout, filepath.FromSlash(test.want)) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
			}
			agent := "claude"
			if test.args[0] == "--codex" {
				agent = "codex"
			}
			if got := readJSONFile(t, filepath.Join(root, filepath.FromSlash(test.want))); !reflect.DeepEqual(got, wantHookConfig(agent)) {
				t.Fatalf("config = %v", got)
			}
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(test.want)))
			if err != nil || info.Mode().Perm()&0o600 != 0o600 {
				t.Fatalf("mode = %v, err %v", info.Mode(), err)
			}
		})
	}
}

const initExistingSettings = `{
  "model": "opus",
  "permissions": {"allow": ["Bash(go test:*)"], "defaultMode": "acceptEdits"},
  "limit": 12345678901234567890,
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "audit.sh"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "notify-done.sh", "timeout": 5}]}]
  }
}
`

func TestInitWriteMergesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(initExistingSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := initTestDeps(dir, "")
	if exit, _, stderr := runInitTest(t, deps, "--claude", "--write", "--workspace", dir); exit != 0 {
		t.Fatalf("exit = %d, stderr %q", exit, stderr)
	}
	merged := readJSONFile(t, path)
	if merged["model"] != "opus" || merged["limit"] == nil {
		t.Fatalf("other keys lost: %v", merged)
	}
	if !reflect.DeepEqual(merged["permissions"], map[string]any{"allow": []any{"Bash(go test:*)"}, "defaultMode": "acceptEdits"}) {
		t.Fatalf("permissions changed: %v", merged["permissions"])
	}
	hooks := merged["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("existing PreToolUse hook lost: %v", hooks)
	}
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 || !strings.Contains(commandsOf(stop[0]), "notify-done.sh") || !strings.Contains(commandsOf(stop[1]), "agentic-go check --hook claude") {
		t.Fatalf("Stop entries = %v, want the existing hook then ours", stop)
	}
	if got := len(hooks["SessionStart"].([]any)); got != 1 {
		t.Fatalf("SessionStart entries = %d, want 1", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "12345678901234567890") {
		t.Fatalf("large number was rewritten:\n%s", raw)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want the existing 0600 preserved", info.Mode().Perm())
	}

	exit, stdout, _ := runInitTest(t, deps, "--claude", "--write", "--workspace", dir)
	if exit != 0 || !strings.Contains(stdout, "already configured") {
		t.Fatalf("second write: exit=%d stdout=%q", exit, stdout)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("second write changed the file:\n%s\n---\n%s", raw, again)
	}
}

func commandsOf(group any) string {
	var commands []string
	for _, hook := range group.(map[string]any)["hooks"].([]any) {
		commands = append(commands, hook.(map[string]any)["command"].(string))
	}
	return strings.Join(commands, " ")
}

func TestInitWriteRecognizesExistingGateHooks(t *testing.T) {
	tests := []struct {
		name    string
		command string
		added   bool
	}{
		{name: "same command", command: "agentic-go check --hook claude", added: false},
		{name: "different flags", command: "agentic-go check --hook claude --profile hook", added: false},
		{name: "absolute path", command: "/usr/local/bin/agentic-go check --hook claude", added: false},
		{name: "other agentic-go subcommand", command: "agentic-go verify --base HEAD", added: true},
		{name: "unrelated tool named check", command: "other-tool check", added: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existing := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":` + quoteJSON(test.command) + `}]}]}}`
			merged, changed, err := mergeHookSettings([]byte(existing), "claude")
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(merged, &decoded); err != nil {
				t.Fatal(err)
			}
			stop := decoded["hooks"].(map[string]any)["Stop"].([]any)
			if test.added != (len(stop) == 2) {
				t.Fatalf("Stop entries = %d, added want %v", len(stop), test.added)
			}
			// SessionStart is always newly added, so the file always changes.
			if !changed {
				t.Fatal("SessionStart should have been added")
			}
		})
	}
}

func TestInitWriteRejectsInvalidExistingSettings(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "invalid JSON", content: `{"hooks": `},
		{name: "not an object", content: `["hooks"]`},
		{name: "hooks not an object", content: `{"hooks": []}`},
		{name: "event not an array", content: `{"hooks": {"Stop": {}}}`},
		{name: "trailing content", content: `{} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".claude", "settings.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			exit, stdout, stderr := runInitTest(t, initTestDeps(dir, ""), "--claude", "--write", "--workspace", dir)
			if exit == 0 || stdout != "" || !strings.Contains(stderr, "settings.json") {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want a failure naming the file", exit, stdout, stderr)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != test.content {
				t.Fatalf("file was modified: %q err %v", after, err)
			}
		})
	}
}

func TestInitBlankSettingsFileIsTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if exit, _, stderr := runInitTest(t, initTestDeps(dir, ""), "--claude", "--write", "--workspace", dir); exit != 0 {
		t.Fatalf("exit = %d, stderr %q", exit, stderr)
	}
	if got := readJSONFile(t, path); !reflect.DeepEqual(got, wantHookConfig("claude")) {
		t.Fatalf("config = %v", got)
	}
}

func TestInitUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		want string
		args []string
	}{
		{name: "nothing selected", args: nil, want: "choose exactly one"},
		{name: "two targets", args: []string{"--claude", "--codex"}, want: "choose exactly one"},
		{name: "bad scope", args: []string{"--claude", "--scope", "global"}, want: "--scope"},
		{name: "codex local scope", args: []string{"--codex", "--scope", "local"}, want: "--scope local"},
		{name: "extra arguments", args: []string{"--claude", "extra"}, want: "unexpected arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			exit, stdout, stderr := runInitTest(t, initTestDeps(dir, dir), test.args...)
			if exit != 2 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
			}
		})
	}
}

func TestInitPrePush(t *testing.T) {
	t.Run("print", func(t *testing.T) {
		exit, stdout, _ := runInitTest(t, initTestDeps("", ""), "--git-pre-push")
		if exit != 0 || stdout != initPrePushScript {
			t.Fatalf("exit=%d stdout=%q", exit, stdout)
		}
		for _, want := range []string{
			"#!/bin/sh",
			"agentic-go check --profile local --base '@{upstream}'",
			"git rev-parse '@{upstream}' >/dev/null 2>&1",
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("script lacks %q:\n%s", want, stdout)
			}
		}
	})
	t.Run("script is valid sh", func(t *testing.T) {
		sh, err := exec.LookPath("sh")
		if err != nil {
			t.Skip("sh not available")
		}
		if output, err := exec.Command(sh, "-n", "-c", initPrePushScript).CombinedOutput(); err != nil {
			t.Fatalf("sh -n: %v\n%s", err, output)
		}
	})
	t.Run("script blocks only on exit 1", func(t *testing.T) {
		sh, err := exec.LookPath("sh")
		if err != nil {
			t.Skip("sh not available")
		}
		work := t.TempDir()
		script := filepath.Join(work, "pre-push")
		if err := os.WriteFile(script, []byte(initPrePushScript), 0o755); err != nil {
			t.Fatal(err)
		}
		stubs := t.TempDir()
		stub := "#!/bin/sh\nexit \"$STUB_EXIT\"\n"
		if err := os.WriteFile(filepath.Join(stubs, "agentic-go"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
		tests := []struct {
			name       string
			path       string
			stubExit   string
			wantStderr string
			wantExit   int
		}{
			{name: "pass", path: stubs + ":" + os.Getenv("PATH"), stubExit: "0", wantExit: 0},
			{name: "block", path: stubs + ":" + os.Getenv("PATH"), stubExit: "1", wantExit: 1},
			{name: "tool failure", path: stubs + ":" + os.Getenv("PATH"), stubExit: "2", wantExit: 0, wantStderr: "agentic-go check could not run (exit 2); not blocking push"},
			{name: "tool crash", path: stubs + ":" + os.Getenv("PATH"), stubExit: "137", wantExit: 0, wantStderr: "could not run (exit 137)"},
			{name: "tool missing", path: t.TempDir(), stubExit: "1", wantExit: 0, wantStderr: "not found"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				command := exec.Command(sh, script)
				command.Dir = work
				command.Env = []string{"PATH=" + test.path, "STUB_EXIT=" + test.stubExit}
				var stderr bytes.Buffer
				command.Stderr = &stderr
				exit := 0
				if err := command.Run(); err != nil {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) {
						t.Fatal(err)
					}
					exit = exitErr.ExitCode()
				}
				if exit != test.wantExit || !strings.Contains(stderr.String(), test.wantStderr) {
					t.Fatalf("exit=%d stderr=%q, want exit %d and %q", exit, stderr.String(), test.wantExit, test.wantStderr)
				}
			})
		}
	})
	t.Run("write installs once", func(t *testing.T) {
		hooks := filepath.Join(t.TempDir(), "hooks")
		deps := initTestDeps("", hooks)
		if exit, _, stderr := runInitTest(t, deps, "--git-pre-push", "--write"); exit != 0 {
			t.Fatalf("exit = %d, stderr %q", exit, stderr)
		}
		path := filepath.Join(hooks, "pre-push")
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %v, err %v, want 0755", info, err)
		}
		if content, _ := os.ReadFile(path); string(content) != initPrePushScript {
			t.Fatalf("content = %q", content)
		}
		exit, stdout, _ := runInitTest(t, deps, "--git-pre-push", "--write")
		if exit != 0 || !strings.Contains(stdout, "already installed") {
			t.Fatalf("second write: exit=%d stdout=%q", exit, stdout)
		}
	})
	t.Run("write refuses to overwrite a different hook", func(t *testing.T) {
		hooks := t.TempDir()
		path := filepath.Join(hooks, "pre-push")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nmake lint\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		exit, _, stderr := runInitTest(t, initTestDeps("", hooks), "--git-pre-push", "--write")
		if exit == 0 || !strings.Contains(stderr, "not overwriting") || !strings.Contains(stderr, "agentic-go check --profile local") || !strings.Contains(stderr, `[ "$?" -ne 1 ] || exit 1`) {
			t.Fatalf("exit=%d stderr=%q, want a refusal that explains chaining", exit, stderr)
		}
		if content, _ := os.ReadFile(path); string(content) != "#!/bin/sh\nmake lint\n" {
			t.Fatalf("existing hook was modified: %q", content)
		}
	})
}

func TestGitHooksDirResolvesInsideARepository(t *testing.T) {
	repository := t.TempDir()
	cliGit(t, repository, "init", "-b", "main")
	dir, err := gitHooksDir(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	resolvedWant, _ := filepath.EvalSymlinks(filepath.Join(repository, ".git", "hooks"))
	resolvedGot, _ := filepath.EvalSymlinks(dir)
	if resolvedGot != resolvedWant {
		t.Fatalf("hooks dir = %q, want %q", dir, resolvedWant)
	}
}

func writeInitSettings(t *testing.T, content string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestInitWritePreservesUserFileLayout(t *testing.T) {
	const existing = `{
  "zeta": 1,
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify.sh"}]}
    ]
  },
  "alpha": {"b": 2, "a": 1}
}
`
	const want = `{
  "zeta": 1,
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify.sh"}]},
      {
        "hooks": [
          {
            "type": "command",
            "command": "agentic-go check --hook claude",
            "timeout": 180
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "agentic-go check --hook claude",
            "timeout": 30
          }
        ]
      }
    ]
  },
  "alpha": {"b": 2, "a": 1}
}
`
	dir, path := writeInitSettings(t, existing)
	deps := initTestDeps(dir, "")
	if exit, _, stderr := runInitTest(t, deps, "--claude", "--write", "--workspace", dir); exit != 0 {
		t.Fatalf("exit = %d, stderr %q", exit, stderr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("merged file differs:\n%s\nwant:\n%s", got, want)
	}
	runInitTest(t, deps, "--claude", "--write", "--workspace", dir)
	if again, _ := os.ReadFile(path); !bytes.Equal(got, again) {
		t.Fatal("second write changed the file")
	}
}

func TestInitWriteKeepsCompactAndAbsentHooksLayouts(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		prefix   string
	}{
		{name: "compact with empty event", existing: `{"zeta":1,"hooks":{"Stop":[]}}`, prefix: `{"zeta":1,"hooks":{"Stop":[{"hooks":[{"type":"command","command":"agentic-go check --hook claude","timeout":180}]}],"SessionStart":[`},
		{name: "compact without hooks", existing: `{"zeta":1,"alpha":[1,2]}`, prefix: `{"zeta":1,"alpha":[1,2],"hooks":{"SessionStart":[`},
		{name: "empty object", existing: "{}\n", prefix: "{\n  \"hooks\": {"},
		{name: "null hooks", existing: `{"hooks":null,"a":1}`, prefix: `{"hooks":{"SessionStart":[`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, path := writeInitSettings(t, test.existing)
			deps := initTestDeps(dir, "")
			if exit, _, stderr := runInitTest(t, deps, "--claude", "--write", "--workspace", dir); exit != 0 {
				t.Fatalf("exit = %d, stderr %q", exit, stderr)
			}
			got, _ := os.ReadFile(path)
			if !strings.HasPrefix(string(got), test.prefix) {
				t.Fatalf("file = %s\nwant prefix %s", got, test.prefix)
			}
			merged := readJSONFile(t, path)
			want := wantHookConfig("claude")["hooks"]
			if !reflect.DeepEqual(merged["hooks"], want) {
				t.Fatalf("hooks = %v", merged["hooks"])
			}
			runInitTest(t, deps, "--claude", "--write", "--workspace", dir)
			if again, _ := os.ReadFile(path); !bytes.Equal(got, again) {
				t.Fatal("second write changed the file")
			}
		})
	}
}

func TestInitWriteKeepsSymlinkedSettings(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "claude-settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"model": "opus"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if exit, _, stderr := runInitTest(t, initTestDeps(dir, ""), "--claude", "--write", "--workspace", dir); exit != 0 {
		t.Fatalf("exit = %d, stderr %q", exit, stderr)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("settings.json is no longer a symlink: %v %v", info, err)
	}
	if got := readJSONFile(t, target); got["model"] != "opus" || got["hooks"] == nil {
		t.Fatalf("symlink target = %v", got)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %v, want 0600", info.Mode().Perm())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "dotfiles", "*.tmp*")); len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}
