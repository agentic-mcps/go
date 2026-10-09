package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/gate"
)

// fakeCheckGate returns canned results and records how it was driven.
type fakeCheckGate struct {
	err     error
	results []gate.Result
	options []gate.Options
	panics  bool
}

func (f *fakeCheckGate) Run(_ context.Context, options gate.Options) (gate.Result, error) {
	if f.panics {
		panic("boom")
	}
	f.options = append(f.options, options)
	if f.err != nil {
		return gate.Result{}, f.err
	}
	index := min(len(f.options), len(f.results)) - 1
	if index < 0 {
		return gate.Result{}, errors.New("fake gate has no result")
	}
	return f.results[index], nil
}

// checkHarness builds fake dependencies and remembers how they were used.
type checkHarness struct {
	gate       *fakeCheckGate
	store      *gate.Store
	openErr    error
	env        map[string]string
	stdin      string
	head       string
	openedRoot string
	timeout    time.Duration
}

func newCheckHarness(t *testing.T, results ...gate.Result) *checkHarness {
	t.Helper()
	store, err := gate.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &checkHarness{gate: &fakeCheckGate{results: results}, store: store, env: map[string]string{}}
}

func (h *checkHarness) deps() checkDependencies {
	return checkDependencies{
		getenv: func(key string) string { return h.env[key] },
		stdin:  strings.NewReader(h.stdin),
		open: func(_ context.Context, root string, timeout time.Duration, _ io.Writer) (checkEnvironment, error) {
			h.openedRoot, h.timeout = root, timeout
			if h.openErr != nil {
				return checkEnvironment{}, h.openErr
			}
			return checkEnvironment{gate: h.gate, store: h.store, close: func() {}}, nil
		},
		openStore: func(string) (*gate.Store, error) {
			if h.store == nil {
				return nil, errors.New("no state directory")
			}
			return h.store, nil
		},
		startHead: func(context.Context, string) string { return h.head },
	}
}

func (h *checkHarness) run(args ...string) (exit int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	exit = runCheckWithDependencies(args, &out, &errOut, h.deps())
	return exit, out.String(), errOut.String()
}

func checkResult(verdict gate.Verdict, fingerprint string) gate.Result {
	result := gate.Result{
		SchemaVersion: gate.SchemaVersion, Verdict: verdict, Fingerprint: fingerprint,
		Base:  gate.Base{Ref: "main", Commit: "0123456789abcdef", Source: "default"},
		Items: []gate.Item{}, Notes: []string{},
	}
	switch verdict {
	case gate.VerdictBlock:
		result.Items = []gate.Item{{
			Severity: gate.SeverityBlock, Code: gate.CodeTestDeleted, File: "lib/lib_test.go", Line: 7,
			Message: "TestSub was deleted", Fix: "Restore TestSub.",
		}}
	case gate.VerdictUnknown:
		result.Notes = []string{"time budget ran out"}
	}
	return result
}

func goModuleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/m\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func hookPayload(event, cwd string, extra string) string {
	payload := `{"session_id":"s1","hook_event_name":"` + event + `","cwd":` + quoteJSON(cwd)
	if extra != "" {
		payload += "," + extra
	}
	return payload + "}"
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestCheckExitCodeMapping(t *testing.T) {
	tests := []struct {
		env        map[string]string
		name       string
		verdict    gate.Verdict
		wantStderr string
		args       []string
		wantExit   int
	}{
		{name: "pass local", verdict: gate.VerdictPass, wantExit: 0},
		{name: "block local", verdict: gate.VerdictBlock, wantExit: 1},
		{name: "unknown local never fails", verdict: gate.VerdictUnknown, wantExit: 0, wantStderr: "time budget ran out"},
		{name: "pass ci", verdict: gate.VerdictPass, args: []string{"--profile", "ci"}, wantExit: 0},
		{name: "block ci", verdict: gate.VerdictBlock, args: []string{"--profile", "ci"}, wantExit: 1},
		{name: "unknown ci fails closed", verdict: gate.VerdictUnknown, args: []string{"--profile", "ci"}, wantExit: 2},
		{name: "unknown via CI env fails closed", verdict: gate.VerdictUnknown, env: map[string]string{"CI": "true"}, wantExit: 2},
		{name: "unknown explicit local beats CI env", verdict: gate.VerdictUnknown, args: []string{"--profile", "local"}, env: map[string]string{"CI": "true"}, wantExit: 0},
		{name: "CI env other than true is not ci", verdict: gate.VerdictUnknown, env: map[string]string{"CI": "1"}, wantExit: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newCheckHarness(t, checkResult(test.verdict, "fp"))
			for key, value := range test.env {
				h.env[key] = value
			}
			exit, stdout, stderr := h.run(test.args...)
			if exit != test.wantExit {
				t.Fatalf("exit = %d, want %d (stderr %q)", exit, test.wantExit, stderr)
			}
			if stdout == "" {
				t.Fatal("stdout is empty, want a report")
			}
			if !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr, test.wantStderr)
			}
		})
	}
}

func TestCheckProfileAndOptionsReachTheGate(t *testing.T) {
	h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
	h.env["CI"] = "true"
	exit, _, stderr := h.run("--base", "origin/main", "--race", "--require-coverage", "--skip", "TestSlow",
		"--max-packages", "40", "--budget", "90s", "--no-cache", "--workspace", "/repo")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr %q", exit, stderr)
	}
	want := gate.Options{
		Base: "origin/main", Profile: gate.ProfileCI, Budget: 90 * time.Second, Race: true,
		RequireCoverage: true, Skip: "TestSlow", MaxPackages: 40, NoCache: true,
	}
	if got := h.gate.options[0]; got != want {
		t.Fatalf("options = %+v, want %+v", got, want)
	}
	if h.openedRoot != "/repo" {
		t.Fatalf("workspace = %q, want /repo", h.openedRoot)
	}
}

func TestCheckRunnerTimeoutCoversTheBudget(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: "local default", args: nil, want: 12 * time.Minute},
		{name: "ci default", args: []string{"--profile", "ci"}, want: 22 * time.Minute},
		{name: "explicit budget", args: []string{"--budget", "30m"}, want: 32 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
			if exit, _, stderr := h.run(test.args...); exit != 0 {
				t.Fatalf("exit = %d, stderr %q", exit, stderr)
			}
			if h.timeout != test.want {
				t.Fatalf("timeout = %s, want %s", h.timeout, test.want)
			}
		})
	}
	h := newCheckHarness(t)
	h.stdin = hookPayload("Stop", goModuleDir(t), "")
	h.gate.results = []gate.Result{checkResult(gate.VerdictPass, "fp")}
	h.run("--hook", "claude")
	if h.timeout != 4*time.Minute {
		t.Fatalf("hook timeout = %s, want 4m (120s budget + 2m)", h.timeout)
	}
}

func TestCheckOutputFormats(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp"))
		_, stdout, _ := h.run("--format", "text")
		if json.Valid([]byte(stdout)) || !strings.Contains(stdout, "TestSub was deleted") {
			t.Fatalf("text output = %q", stdout)
		}
		if len(stdout) > checkTextLimit {
			t.Fatalf("text is %d bytes, limit %d", len(stdout), checkTextLimit)
		}
	})
	t.Run("json", func(t *testing.T) {
		h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp"))
		_, stdout, _ := h.run("--format", "json")
		var decoded gate.Result
		if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
			t.Fatalf("json output %q: %v", stdout, err)
		}
		if decoded.SchemaVersion != gate.SchemaVersion || decoded.Verdict != gate.VerdictBlock || len(decoded.Items) != 1 {
			t.Fatalf("decoded = %+v", decoded)
		}
	})
}

func TestCheckHelpExitsZero(t *testing.T) {
	for _, flagName := range []string{"-h", "--help"} {
		h := newCheckHarness(t)
		exit, stdout, stderr := h.run(flagName)
		if exit != 0 || stdout != "" || !strings.Contains(stderr, "Usage of agentic-go check") {
			t.Fatalf("%s: exit=%d stdout=%q stderr=%q", flagName, exit, stdout, stderr)
		}
	}
}

func TestCheckUsageAndSetupErrors(t *testing.T) {
	tests := []struct {
		name string
		want string
		args []string
	}{
		{name: "bad format", args: []string{"--format", "yaml"}, want: "--format"},
		{name: "bad profile", args: []string{"--profile", "fast"}, want: "--profile"},
		{name: "bad hook", args: []string{"--hook", "gemini"}, want: "--hook"},
		{name: "bad max packages", args: []string{"--max-packages", "501"}, want: "--max-packages"},
		{name: "negative budget", args: []string{"--budget", "-1s"}, want: "--budget"},
		{name: "extra arguments", args: []string{"extra"}, want: "unexpected arguments"},
		{name: "option-like base", args: []string{"--base=--evil"}, want: "--base"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
			args := test.args
			wantExit := 2
			if test.name == "bad hook" {
				wantExit = 0 // a hook must never fail with exit 2
			}
			exit, stdout, stderr := h.run(args...)
			if exit != wantExit || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want exit %d and %q", exit, stdout, stderr, wantExit, test.want)
			}
			if len(h.gate.options) != 0 {
				t.Fatal("gate ran despite invalid arguments")
			}
		})
	}
	t.Run("workspace open failure", func(t *testing.T) {
		h := newCheckHarness(t)
		h.openErr = errors.New("not a Go workspace")
		exit, _, stderr := h.run()
		if exit != 2 || !strings.Contains(stderr, "not a Go workspace") {
			t.Fatalf("exit=%d stderr=%q", exit, stderr)
		}
	})
	t.Run("gate failure", func(t *testing.T) {
		h := newCheckHarness(t)
		h.gate.err = errors.New("no repository")
		exit, _, stderr := h.run()
		if exit != 2 || !strings.Contains(stderr, "no repository") {
			t.Fatalf("exit=%d stderr=%q", exit, stderr)
		}
	})
}

func TestHookClaudeStop(t *testing.T) {
	dir := goModuleDir(t)
	h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp1"))
	h.stdin = hookPayload("Stop", dir, "")
	exit, stdout, stderr := h.run("--hook", "claude")
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	var out gate.HookOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout %q: %v", stdout, err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "TestSub was deleted") {
		t.Fatalf("output = %+v", out)
	}
	if got := h.gate.options[0]; got.Profile != gate.ProfileHook {
		t.Fatalf("profile = %q, want hook", got.Profile)
	}

	// The same unresolved state on the next stop is allowed and disclosed.
	h.stdin = hookPayload("Stop", dir, `"stop_hook_active":true`)
	exit, stdout, _ = h.run("--hook", "claude")
	if exit != 0 {
		t.Fatalf("second exit = %d", exit)
	}
	out = gate.HookOutput{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("second stdout %q: %v", stdout, err)
	}
	if out.Decision != "" || !strings.Contains(out.SystemMessage, "stopped again without changes") {
		t.Fatalf("second output = %+v, want allow with systemMessage", out)
	}
	if strings.Contains(stdout, `"decision"`) || strings.Contains(stdout, `"reason"`) {
		t.Fatalf("empty fields must be omitted: %q", stdout)
	}
}

func TestHookClaudePrintsNothingWhenAllowed(t *testing.T) {
	dir := goModuleDir(t)
	h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
	h.stdin = hookPayload("SubagentStop", dir, "")
	exit, stdout, stderr := h.run("--hook", "claude")
	if exit != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want silent allow", exit, stdout, stderr)
	}
	if len(h.gate.options) != 1 {
		t.Fatalf("gate ran %d times, want 1 (SubagentStop is treated as Stop)", len(h.gate.options))
	}
}

func TestHookUnknownVerdictAllowsWithMessage(t *testing.T) {
	dir := goModuleDir(t)
	h := newCheckHarness(t, checkResult(gate.VerdictUnknown, "fp"))
	h.stdin = hookPayload("Stop", dir, "")
	exit, stdout, _ := h.run("--hook", "claude")
	var out gate.HookOutput
	if exit != 0 || json.Unmarshal([]byte(stdout), &out) != nil || out.Decision != "" || !strings.Contains(out.SystemMessage, "time budget ran out") {
		t.Fatalf("exit=%d stdout=%q", exit, stdout)
	}
}

func TestHookSessionStartRecordsHead(t *testing.T) {
	dir := goModuleDir(t)
	h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
	h.head = "abc123"
	h.stdin = hookPayload("SessionStart", dir, `"source":"startup"`)
	exit, stdout, stderr := h.run("--hook", "claude")
	if exit != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want silent", exit, stdout, stderr)
	}
	session, _ := h.store.LoadSession("s1")
	if session.StartHead != "abc123" {
		t.Fatalf("StartHead = %q, want abc123", session.StartHead)
	}
	if len(h.gate.options) != 0 || h.openedRoot != "" {
		t.Fatal("SessionStart must neither run nor build the gate")
	}

	// A later start event (resume) keeps the original head.
	h.head = "def456"
	h.stdin = hookPayload("SessionStart", dir, `"source":"resume"`)
	h.run("--hook", "claude")
	if session, _ = h.store.LoadSession("s1"); session.StartHead != "abc123" {
		t.Fatalf("StartHead = %q, want abc123 kept", session.StartHead)
	}

	// The Stop gate compares against the recorded head.
	h.stdin = hookPayload("Stop", dir, "")
	h.run("--hook", "claude")
	if got := h.gate.options[0].SessionBase; got != "abc123" {
		t.Fatalf("SessionBase = %q, want abc123", got)
	}
}

func TestHookSessionStartEmptyRepositoryLeavesHeadEmpty(t *testing.T) {
	dir := goModuleDir(t)
	h := newCheckHarness(t)
	h.stdin = hookPayload("SessionStart", dir, "")
	if exit, stdout, _ := h.run("--hook", "claude"); exit != 0 || stdout != "" {
		t.Fatalf("exit=%d stdout=%q", exit, stdout)
	}
	if session, _ := h.store.LoadSession("s1"); session.StartHead != "" {
		t.Fatalf("StartHead = %q, want empty", session.StartHead)
	}
}

func TestHookWalksUpFromCwdToTheGoRoot(t *testing.T) {
	dir := goModuleDir(t)
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
	h.stdin = hookPayload("Stop", nested, "")
	h.run("--hook", "claude")
	if h.openedRoot != dir {
		t.Fatalf("workspace = %q, want %q", h.openedRoot, dir)
	}
}

func TestHookIgnoresNonGoDirectoriesPlanModeAndOtherEvents(t *testing.T) {
	goDir := goModuleDir(t)
	tests := []struct {
		name    string
		payload string
	}{
		{name: "non-Go directory", payload: hookPayload("Stop", t.TempDir(), "")},
		{name: "plan mode stop", payload: hookPayload("Stop", goDir, `"permission_mode":"plan"`)},
		{name: "plan mode session start", payload: hookPayload("SessionStart", goDir, `"permission_mode":"plan"`)},
		{name: "unrelated event", payload: hookPayload("UserPromptSubmit", goDir, "")},
	}
	for _, kind := range []string{"claude", "codex"} {
		for _, test := range tests {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp"))
				h.stdin = test.payload
				exit, stdout, stderr := h.run("--hook", kind)
				if exit != 0 || stdout != "" || stderr != "" {
					t.Fatalf("exit=%d stdout=%q stderr=%q, want silent exit 0", exit, stdout, stderr)
				}
				if len(h.gate.options) != 0 || h.openedRoot != "" {
					t.Fatal("the gate must not be built or run")
				}
			})
		}
	}
}

func TestHookInternalErrorsNeverBlock(t *testing.T) {
	dir := goModuleDir(t)
	tests := []struct {
		setup func(*checkHarness)
		name  string
		want  string
	}{
		{name: "workspace open failure", setup: func(h *checkHarness) { h.openErr = errors.New("boom") }, want: "boom"},
		{name: "gate failure", setup: func(h *checkHarness) { h.gate.err = errors.New("no base") }, want: "no base"},
		{name: "gate panic", setup: func(h *checkHarness) { h.gate.panics = true }, want: "internal error"},
		{name: "bad stdin", setup: func(h *checkHarness) { h.stdin = "not json" }, want: "decoding hook input"},
		{name: "empty stdin", setup: func(h *checkHarness) { h.stdin = "" }, want: "input is empty"},
	}
	for _, test := range tests {
		t.Run("claude/"+test.name, func(t *testing.T) {
			h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp"))
			h.stdin = hookPayload("Stop", dir, "")
			test.setup(h)
			exit, stdout, _ := h.run("--hook", "claude")
			var out gate.HookOutput
			if exit != 0 || json.Unmarshal([]byte(stdout), &out) != nil {
				t.Fatalf("exit=%d stdout=%q", exit, stdout)
			}
			if out.Decision != "" || !strings.HasPrefix(out.SystemMessage, "agentic-go check could not run: ") || !strings.Contains(out.SystemMessage, test.want) {
				t.Fatalf("output = %+v, want systemMessage containing %q", out, test.want)
			}
		})
		t.Run("codex/"+test.name, func(t *testing.T) {
			h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp"))
			h.stdin = hookPayload("Stop", dir, "")
			test.setup(h)
			exit, stdout, stderr := h.run("--hook", "codex")
			if exit != 0 || stdout != "{}\n" || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
			}
		})
	}
}

func TestHookCodexOutput(t *testing.T) {
	dir := goModuleDir(t)
	t.Run("block emits exactly decision and reason", func(t *testing.T) {
		h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp1"))
		h.stdin = hookPayload("Stop", dir, "")
		exit, stdout, stderr := h.run("--hook", "codex")
		if exit != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q", exit, stderr)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(stdout), &fields); err != nil {
			t.Fatalf("stdout %q: %v", stdout, err)
		}
		if len(fields) != 2 || fields["decision"] != "block" || !strings.Contains(fields["reason"].(string), "TestSub was deleted") {
			t.Fatalf("fields = %v, want only decision and reason", fields)
		}
	})
	t.Run("allow emits an empty object", func(t *testing.T) {
		h := newCheckHarness(t, checkResult(gate.VerdictPass, "fp"))
		h.stdin = hookPayload("Stop", dir, "")
		exit, stdout, stderr := h.run("--hook", "codex")
		if exit != 0 || stdout != "{}\n" || stderr != "" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
		}
	})
	t.Run("disclosure goes to stderr not stdout", func(t *testing.T) {
		h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp1"))
		h.stdin = hookPayload("Stop", dir, "")
		h.run("--hook", "codex")
		h.stdin = hookPayload("Stop", dir, "")
		exit, stdout, stderr := h.run("--hook", "codex")
		if exit != 0 || stdout != "{}\n" || !strings.Contains(stderr, "stopped again without changes") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
		}
	})
}

func TestHookWithoutStateStillRuns(t *testing.T) {
	dir := goModuleDir(t)
	h := newCheckHarness(t, checkResult(gate.VerdictBlock, "fp1"))
	h.store = nil
	h.stdin = hookPayload("Stop", dir, "")
	exit, stdout, _ := h.run("--hook", "claude")
	if exit != 0 || !strings.Contains(stdout, `"decision":"block"`) {
		t.Fatalf("exit=%d stdout=%q", exit, stdout)
	}
}

func TestCheckEndToEndDeletedTestBlocks(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs git and go")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	repository := t.TempDir()
	cliGit(t, repository, "init", "-b", "main")
	cliGit(t, repository, "config", "user.email", "test@example.test")
	cliGit(t, repository, "config", "user.name", "Test")
	cliWrite(t, repository, "go.mod", "module example.test/e2e\n\ngo 1.25.0\n")
	cliWrite(t, repository, "lib.go", "package e2e\n\nfunc Add(a, b int) int { return a + b }\n\nfunc Sub(a, b int) int { return a - b }\n")
	both := "package e2e\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"Add\")\n\t}\n}\n\nfunc TestSub(t *testing.T) {\n\tif Sub(3, 2) != 1 {\n\t\tt.Fatal(\"Sub\")\n\t}\n}\n"
	cliWrite(t, repository, "lib_test.go", both)
	cliGit(t, repository, "add", ".")
	cliGit(t, repository, "commit", "-m", "base")
	only := strings.Split(both, "\nfunc TestSub")[0]
	cliWrite(t, repository, "lib_test.go", only)

	var stdout, stderr bytes.Buffer
	exit := runCheckWithDependencies([]string{"--workspace", repository, "--base", "HEAD", "--profile", "local"}, &stdout, &stderr, defaultCheckDependencies())
	if exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s\nstderr: %s", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "test.deleted") && !strings.Contains(stdout.String(), "TestSub") {
		t.Fatalf("report does not mention the deleted test:\n%s", stdout.String())
	}
}
