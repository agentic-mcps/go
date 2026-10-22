package gateeval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// runGit runs git in dir with a fixed identity and returns trimmed stdout.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // Test fixture.
		t.Fatal(err)
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil { //nolint:gosec // Test fixture must be executable.
		t.Fatal(err)
	}
}

const (
	fixtureMod  = "module example.com/proj\n\ngo 1.21\n"
	fixtureCode = "package proj\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n"
	fixtureTest = "package proj\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"
	preexisting = "package proj\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) { t.Fatal(\"fails at base\") }\n"
)

// fixture is a work directory holding a project clone and its variants.
type fixture struct {
	workDir  string
	clone    string
	base     string
	variants []Variant
}

// newFixture creates clones/proj with a passing package at the base commit
// (plus a test that always fails when preexisting is set) and returns it.
func newFixture(t *testing.T, withPreexisting bool) *fixture {
	t.Helper()
	files := map[string]string{}
	if withPreexisting {
		files["old_test.go"] = preexisting
	}
	return newFixtureWith(t, files)
}

// newFixtureWith is newFixture with extra files committed at the base.
func newFixtureWith(t *testing.T, extra map[string]string) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	workDir := t.TempDir()
	clone := filepath.Join(workDir, "clones", "proj")
	if err := os.MkdirAll(clone, 0o750); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, "init", "-q", "-b", "main")
	writeFile(t, clone, "go.mod", fixtureMod)
	writeFile(t, clone, "a.go", fixtureCode)
	writeFile(t, clone, "a_test.go", fixtureTest)
	for name, content := range extra {
		writeFile(t, clone, name, content)
	}
	runGit(t, clone, "add", "-A")
	runGit(t, clone, "commit", "-q", "-m", "base")
	return &fixture{workDir: workDir, clone: clone, base: runGit(t, clone, "rev-parse", "HEAD")}
}

// addVariant commits files on a new branch off main and registers a variant.
func (f *fixture) addVariant(t *testing.T, id string, class Class, files map[string]string) {
	t.Helper()
	runGit(t, f.clone, "checkout", "-q", "-b", id, "main")
	if _, ok := files["a.go"]; !ok {
		files["a.go"] = fixtureCode + "\n// touched by " + id + "\n"
	}
	for name, content := range files {
		writeFile(t, f.clone, name, content)
	}
	runGit(t, f.clone, "add", "-A")
	runGit(t, f.clone, "commit", "-q", "-m", id)
	runGit(t, f.clone, "checkout", "-q", "main")
	f.variants = append(f.variants, Variant{ID: id, Project: "proj", Commit: id, Base: f.base, Class: class, Branch: id, Effective: true})
}

func readRuns(t *testing.T, path string) []Run {
	t.Helper()
	runs, err := ReadJSONL[Run](path)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func findRun(t *testing.T, runs []Run, variant string, arm Arm, attempt int) Run {
	t.Helper()
	for _, run := range runs {
		if run.VariantID == variant && run.Arm == arm && run.Attempt == attempt {
			return run
		}
	}
	t.Fatalf("no run for %s %s attempt %d in %+v", variant, arm, attempt, runs)
	return Run{}
}

func TestRunArmsAgentHabitAndGate(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.addVariant(t, "broken", ClassMutant, map[string]string{"a.go": "package proj\n\nfunc Add(a, b int) int { return a - b }\n"})
	f.addVariant(t, "flagged", ClassSkipTests, map[string]string{"BLOCK": "yes\n"})

	argsFile := filepath.Join(t.TempDir(), "gate-args")
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, `echo "$PWD $*" >> "`+argsFile+`"
if [ -f BLOCK ]; then
  echo '{"verdict":"block","items":[{"severity":"warn"},{"severity":"block"}]}'
  exit 1
fi
if [ -f UNKNOWN ]; then
  echo '{"verdict":"unknown","items":[]}'
  exit 0
fi
echo '{"verdict":"pass","items":[]}'
`)
	options := RunOptions{
		WorkDir:        f.workDir,
		Out:            filepath.Join(f.workDir, "runs.jsonl"),
		GateBin:        gate,
		B3Script:       filepath.Join(f.workDir, "unused-b3.sh"),
		Variants:       f.variants,
		Arms:           []Arm{ArmB0, ArmGCI, ArmGHook},
		Workers:        2,
		CommandTimeout: time.Minute,
	}
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	if len(runs) != 9 {
		t.Fatalf("recorded %d runs, want 9: %+v", len(runs), runs)
	}

	if run := findRun(t, runs, "good", ArmB0, 1); run.Blocked || run.Unknown || run.ExitCode != 0 || run.Attempt != 1 {
		t.Errorf("B0 on good patch = %+v, want pass", run)
	}
	broken := findRun(t, runs, "broken", ArmB0, 1)
	if !broken.Blocked || broken.ExitCode == 0 || !strings.Contains(broken.Reason, "FAIL") || len(broken.Reason) > reasonLimit {
		t.Errorf("B0 on broken mutant = %+v, want blocked with test output as reason", broken)
	}
	if broken.OutputBytes == 0 || broken.DurationMS < 0 {
		t.Errorf("B0 on broken mutant has no output accounting: %+v", broken)
	}
	if run := findRun(t, runs, "good", ArmGCI, 1); run.Blocked || run.Warned || run.Unknown {
		t.Errorf("Gci on good patch = %+v, want clean pass", run)
	}
	flagged := findRun(t, runs, "flagged", ArmGCI, 1)
	if !flagged.Blocked || !flagged.Warned || flagged.Unknown || flagged.ExitCode != 1 {
		t.Errorf("Gci on flagged = %+v, want blocked and warned, exit 1", flagged)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"check --profile ci --base " + f.base + " --format json --no-cache",
		"check --profile hook --base " + f.base + " --format json --no-cache",
	} {
		if !strings.Contains(string(args), want) {
			t.Errorf("gate was never invoked with %q; calls:\n%s", want, args)
		}
	}
	if !strings.Contains(string(args), filepath.Join(f.workDir, "work", "proj-")) {
		t.Errorf("gate did not run in a worker worktree; calls:\n%s", args)
	}

	// A second call finds every record and appends nothing.
	if err = RunArms(context.Background(), options); err != nil {
		t.Fatalf("second RunArms() error = %v", err)
	}
	if again := readRuns(t, options.Out); len(again) != len(runs) {
		t.Fatalf("second call appended %d runs", len(again)-len(runs))
	}

	// Dropping the last record makes exactly that run repeat.
	data, err := os.ReadFile(options.Out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if err := os.WriteFile(options.Out, []byte(strings.Join(lines[:len(lines)-1], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("resumed RunArms() error = %v", err)
	}
	if resumed := readRuns(t, options.Out); len(resumed) != len(runs) {
		t.Fatalf("resumed file has %d runs, want %d", len(resumed), len(runs))
	}
}

func TestRunArmsGateUnparseableAndUnknown(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "unknown", ClassTrue, map[string]string{"UNKNOWN": "yes\n"})
	f.addVariant(t, "garbage", ClassTrue, map[string]string{"GARBAGE": "yes\n"})
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, `if [ -f GARBAGE ]; then echo "panic: nope" >&2; exit 2; fi
echo '{"verdict":"unknown","items":[]}'
`)
	options := RunOptions{
		WorkDir: f.workDir, Out: filepath.Join(f.workDir, "runs.jsonl"), GateBin: gate,
		Variants: f.variants, Arms: []Arm{ArmGCI}, Workers: 1, CommandTimeout: time.Minute,
	}
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	if run := findRun(t, runs, "unknown", ArmGCI, 1); !run.Unknown || run.Blocked || run.Error != "" {
		t.Errorf("unknown verdict = %+v", run)
	}
	garbage := findRun(t, runs, "garbage", ArmGCI, 1)
	if !garbage.Unknown || garbage.Blocked || garbage.Error == "" || garbage.ExitCode != 2 {
		t.Errorf("unparseable output = %+v, want unknown with an error", garbage)
	}
}

func TestRunArmsTimeoutIsUnknown(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "slow", ClassTrue, map[string]string{})
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, "exec sleep 30\n")
	options := RunOptions{
		WorkDir: f.workDir, Out: filepath.Join(f.workDir, "runs.jsonl"), GateBin: gate,
		Variants: f.variants, Arms: []Arm{ArmGCI}, Workers: 1, CommandTimeout: 300 * time.Millisecond,
	}
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	run := findRun(t, readRuns(t, options.Out), "slow", ArmGCI, 1)
	if !run.Unknown || run.Blocked || !strings.Contains(run.Error, "timed out") {
		t.Errorf("timeout run = %+v, want unknown with timeout error", run)
	}
}

func TestRunArmsTimingRepeatsTruePatchesOnly(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.addVariant(t, "flaw", ClassStub, map[string]string{})
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, "echo '{\"verdict\":\"pass\",\"items\":[]}'\n")
	options := RunOptions{
		WorkDir: f.workDir, Out: filepath.Join(f.workDir, "runs.jsonl"), GateBin: gate,
		Variants: f.variants, Arms: []Arm{ArmGHook}, Workers: 1, CommandTimeout: time.Minute, Timing: true,
	}
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	var got []string
	for _, run := range runs {
		got = append(got, run.VariantID+"/"+string(run.Arm)+"/"+string(rune('0'+run.Attempt)))
	}
	slices.Sort(got)
	want := []string{"flaw/Ghook/1", "good/Ghook/1", "good/Ghook/2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
}

// conventionalFixture builds a project whose base already has a failing test
// and fake golangci-lint and grep scripts that react to marker files.
func conventionalFixture(t *testing.T) (*fixture, RunOptions) {
	t.Helper()
	f := newFixture(t, true)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.addVariant(t, "newfail", ClassMutant, map[string]string{"a.go": "package proj\n\nfunc Add(a, b int) int { return a - b }\n"})
	f.addVariant(t, "lintissue", ClassTrue, map[string]string{"LINT_ISSUES": "x\n"})
	f.addVariant(t, "linterror", ClassTrue, map[string]string{"LINT_ERROR": "x\n"})
	f.addVariant(t, "lintbroken", ClassTrue, map[string]string{"LINT_BROKEN": "x\n"})
	f.addVariant(t, "grep", ClassSkipTests, map[string]string{"GREP_HIT": "x\n"})
	f.addVariant(t, "grepbroken", ClassSkipTests, map[string]string{"GREP_ERROR": "x\n"})

	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "golangci-lint"), `case " $* " in *" --new-from-rev=`+f.base+` "*) ;; *) echo "missing base flag" >&2; exit 4;; esac
if [ -f LINT_ISSUES ]; then echo "x.go:1: issue"; exit 1; fi
if [ -f LINT_ERROR ]; then
  case " $* " in *" --no-config "*) exit 0;; esac
  echo "config error" >&2; exit 3
fi
if [ -f LINT_BROKEN ]; then echo "broken" >&2; exit 3; fi
exit 0
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	grep := filepath.Join(t.TempDir(), "b3-grep.sh")
	writeScript(t, grep, `[ "$1" = "`+f.base+`" ] || exit 2
if [ -f GREP_HIT ]; then echo "skip added"; exit 1; fi
if [ -f GREP_ERROR ]; then echo "oops" >&2; exit 2; fi
exit 0
`)
	return f, RunOptions{
		WorkDir: f.workDir, Out: filepath.Join(f.workDir, "runs.jsonl"), B3Script: grep, Variants: f.variants,
		Arms:    []Arm{ArmB1, ArmB2, ArmB2Star, ArmB3},
		Workers: 2, CommandTimeout: 2 * time.Minute,
	}
}

func TestRunArmsConventionalArms(t *testing.T) {
	f, options := conventionalFixture(t)
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	if len(runs) != len(f.variants)*4 {
		t.Fatalf("recorded %d runs, want %d", len(runs), len(f.variants)*4)
	}

	type want struct {
		variant string
		arm     Arm
		blocked bool
	}
	wants := []want{
		// The failing test at base blocks the plain arms and not the starred ones.
		{"good", ArmB1, true},
		{"good", ArmB2, true},
		{"good", ArmB2Star, false},
		{"good", ArmB3, false},
		{"newfail", ArmB2, true},
		{"newfail", ArmB2Star, true},
		{"newfail", ArmB3, true},
		{"lintissue", ArmB1, true},
		{"lintissue", ArmB2, true},
		{"lintissue", ArmB2Star, true},
		{"lintissue", ArmB3, true},
		{"linterror", ArmB2, true},
		{"linterror", ArmB2Star, false},
		{"linterror", ArmB3, false},
		{"lintbroken", ArmB2Star, false},
		{"lintbroken", ArmB3, false},
		{"grep", ArmB2Star, false},
		{"grep", ArmB3, true},
		{"grepbroken", ArmB2Star, false},
		{"grepbroken", ArmB3, false},
	}
	for _, w := range wants {
		run := findRun(t, runs, w.variant, w.arm, 1)
		if run.Blocked != w.blocked || run.Unknown {
			t.Errorf("%s on %s: blocked=%v unknown=%v error=%q reason=%q, want blocked=%v", w.arm, w.variant, run.Blocked, run.Unknown, run.Error, run.Reason, w.blocked)
		}
	}

	if reason := findRun(t, runs, "newfail", ArmB2Star, 1).Reason; !strings.Contains(reason, "new test failure: example.com/proj.TestAdd") || strings.Contains(reason, "TestOld") {
		t.Errorf("B2s reason on newfail = %q", reason)
	}
	if note := findRun(t, runs, "linterror", ArmB2Star, 1).Error; !strings.Contains(note, "--no-config --default=standard") {
		t.Errorf("fallback lint note = %q", note)
	}
	if note := findRun(t, runs, "lintbroken", ArmB2Star, 1).Error; !strings.Contains(note, "lint not run") {
		t.Errorf("lint-not-run note = %q", note)
	}
	if note := findRun(t, runs, "grepbroken", ArmB3, 1).Error; !strings.Contains(note, "exit 2") {
		t.Errorf("grep error note = %q", note)
	}
	if reason := findRun(t, runs, "grep", ArmB3, 1).Reason; !strings.Contains(reason, "skip added") {
		t.Errorf("B3 reason = %q", reason)
	}

	// Base failures are computed once and cached as JSON.
	cached, err := os.ReadFile(filepath.Join(f.workDir, "basefail", "proj-"+f.base+".json"))
	if err != nil {
		t.Fatalf("base failure cache: %v", err)
	}
	if !strings.Contains(string(cached), "example.com/proj.TestOld") || strings.Contains(string(cached), "TestAdd") {
		t.Errorf("base failure cache = %s", cached)
	}
}

func TestDirectPackages(t *testing.T) {
	f := newFixture(t, false)
	files := map[string]string{
		"a.go":               fixtureCode + "// changed\n",
		"sub/s.go":           "package sub\n",
		"testdata/t.go":      "package testdata\n",
		"_skip/z.go":         "package skip\n",
		"inner/go.mod":       "module example.com/inner\n\ngo 1.21\n",
		"inner/i.go":         "package inner\n",
		"docs/README.md":     "docs\n",
		"gone/g.go":          "package gone\n",
		"onlytest/x_test.go": "package onlytest\n",
	}
	for name, content := range files {
		writeFile(t, f.clone, name, content)
	}
	runGit(t, f.clone, "add", "-A")
	runGit(t, f.clone, "commit", "-q", "-m", "tree")
	tree := runGit(t, f.clone, "rev-parse", "HEAD")
	runGit(t, f.clone, "rm", "-q", "-r", "gone")
	writeFile(t, f.clone, "a.go", fixtureCode+"// changed again\n")
	writeFile(t, f.clone, "sub/s.go", "package sub\n\n// changed\n")
	writeFile(t, f.clone, "testdata/t.go", "package testdata\n\n// changed\n")
	writeFile(t, f.clone, "_skip/z.go", "package skip\n\n// changed\n")
	writeFile(t, f.clone, "inner/i.go", "package inner\n\n// changed\n")
	writeFile(t, f.clone, "docs/README.md", "docs changed\n")
	writeFile(t, f.clone, "onlytest/x_test.go", "package onlytest\n\n// changed\n")
	runGit(t, f.clone, "add", "-A")
	runGit(t, f.clone, "commit", "-q", "-m", "edit")

	got, err := directPackages(context.Background(), f.clone, tree)
	if err != nil {
		t.Fatalf("directPackages() error = %v", err)
	}
	want := []string{".", "./onlytest", "./sub"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directPackages() = %v, want %v (deleted, testdata, underscore, nested-module and non-Go directories excluded)", got, want)
	}
}

func TestNormalizeOptions(t *testing.T) {
	if _, err := normalizeOptions(RunOptions{}); err == nil {
		t.Error("empty options accepted")
	}
	if _, err := normalizeOptions(RunOptions{WorkDir: "w", Out: "o", Arms: []Arm{ArmGCI}}); err == nil {
		t.Error("gate arm without binary accepted")
	}
	if _, err := normalizeOptions(RunOptions{WorkDir: "w", Out: "o", Arms: []Arm{ArmB3}}); err == nil {
		t.Error("B3 without script accepted")
	}
	got, err := normalizeOptions(RunOptions{WorkDir: "w", Out: "o", GateBin: "bin/gate", B3Script: "b3.sh"})
	if err != nil {
		t.Fatalf("normalizeOptions() error = %v", err)
	}
	if got.Workers != 2 || got.CommandTimeout != 10*time.Minute || len(got.Arms) != 7 {
		t.Errorf("defaults = workers %d, timeout %s, arms %v", got.Workers, got.CommandTimeout, got.Arms)
	}
	if !filepath.IsAbs(got.WorkDir) || !filepath.IsAbs(got.GateBin) || !filepath.IsAbs(got.B3Script) {
		t.Errorf("paths not absolute: %+v", got)
	}
}
