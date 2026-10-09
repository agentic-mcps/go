package gateeval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const passJSON = "echo '{\"verdict\":\"pass\",\"items\":[]}'\n"

// prependPath puts dir first on PATH for the rest of the test.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakeLint installs a golangci-lint stand-in running body in the worktree.
func fakeLint(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "golangci-lint"), body)
	prependPath(t, bin)
}

// goWrapper installs a go that logs its subcommand to the returned file, then
// runs body (which should exec the real go, available as $REAL_GO).
func goWrapper(t *testing.T, body string) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not available")
	}
	log := filepath.Join(t.TempDir(), "go-calls")
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "go"), "REAL_GO='"+goBin+"'\necho \"$1\" >> '"+log+"'\n"+body)
	prependPath(t, bin)
	return log
}

func baseOptions(f *fixture, arms ...Arm) RunOptions {
	return RunOptions{
		WorkDir: f.workDir, Out: filepath.Join(f.workDir, "runs.jsonl"), Variants: f.variants,
		Arms: arms, Workers: 1, CommandTimeout: 2 * time.Minute,
	}
}

func TestRunArmsResetsTreeBeforeEveryArm(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	fakeLint(t, "echo '// dirt' >> go.mod\necho x > junk.txt\necho '// more' >> a.go\nexit 0\n")
	statusFile := filepath.Join(t.TempDir(), "status")
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, "git status --porcelain > '"+statusFile+"'\n"+passJSON)
	options := baseOptions(f, ArmB1, ArmGCI)
	options.GateBin = gate
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	status, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(status)) != "" {
		t.Fatalf("the gate saw a dirty tree left by the previous arm:\n%s", status)
	}
	runs := readRuns(t, options.Out)
	if run := findRun(t, runs, "good", ArmGCI, 1); run.Error != "" || run.Blocked {
		t.Errorf("Gci run = %+v, want clean pass without notes", run)
	}
	if run := findRun(t, runs, "good", ArmB1, 1); run.Blocked || run.Unknown {
		t.Errorf("B1 run = %+v, want pass", run)
	}
}

func TestEvaluateRecordsDirtyTreeAndHarnessFailures(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, passJSON)
	statusCalls := 0
	ac := &armContext{
		variant: Variant{ID: "v", Base: "b"}, dir: t.TempDir(), gateBin: gate, timeout: time.Minute,
		reset: func(context.Context) error { return nil },
		status: func(context.Context) (string, error) {
			statusCalls++
			return " M go.mod\n?? junk.txt\n", nil
		},
	}
	run, err := ac.evaluate(context.Background(), ArmGCI, 1)
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if !strings.Contains(run.Error, "dirty tree before arm") || !strings.Contains(run.Error, "M go.mod") {
		t.Errorf("Run.Error = %q, want the dirty tree recorded", run.Error)
	}
	if run.Blocked || run.Unknown {
		t.Errorf("dirt must be a note, not a verdict: %+v", run)
	}
	if statusCalls != 1 {
		t.Errorf("status called %d times for one gate arm, want 1", statusCalls)
	}

	ac.status = func(context.Context) (string, error) { return "", errors.New("git exploded") }
	if _, err = ac.evaluate(context.Background(), ArmGCI, 1); err == nil {
		t.Error("status failure was swallowed")
	}
	ac.reset = func(context.Context) error { return errors.New("checkout failed") }
	_, err = ac.evaluate(context.Background(), ArmGCI, 1)
	var infra *infraError
	if !errors.As(err, &infra) {
		t.Errorf("reset failure error = %v, want an infraError", err)
	}
}

func TestRunArmsHarnessFailureIsNotRecorded(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.variants = append(f.variants, Variant{ID: "ghost", Project: "proj", Base: f.base, Class: ClassTrue, Branch: "no-such-branch"})
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, passJSON)
	options := baseOptions(f, ArmGCI)
	options.GateBin = gate
	for attempt := range 2 {
		err := RunArms(context.Background(), options)
		if err == nil || !strings.Contains(err.Error(), "ghost") {
			t.Fatalf("attempt %d: RunArms() error = %v, want a failure naming the ghost variant", attempt, err)
		}
		runs := readRuns(t, options.Out)
		if len(runs) != 1 || runs[0].VariantID != "good" {
			t.Fatalf("attempt %d: recorded %+v, want only the good variant (resume must retry ghost)", attempt, runs)
		}
	}
}

func TestRunArmsTimingNeedsOneWorker(t *testing.T) {
	base := RunOptions{WorkDir: "w", Out: "o", GateBin: "g", Timing: true, Arms: []Arm{ArmGCI}}
	for _, workers := range []int{2, 5} {
		options := base
		options.Workers = workers
		if _, err := normalizeOptions(options); err == nil || !strings.Contains(err.Error(), "one worker") {
			t.Errorf("Workers=%d with Timing: error = %v, want a one-worker message", workers, err)
		}
	}
	for _, workers := range []int{0, 1} {
		options := base
		options.Workers = workers
		got, err := normalizeOptions(options)
		if err != nil || got.Workers != 1 {
			t.Errorf("Workers=%d with Timing: workers = %d, error = %v, want 1", workers, got.Workers, err)
		}
	}
	options := base
	options.Workers = 2
	if err := RunArms(context.Background(), options); err == nil {
		t.Error("RunArms accepted concurrent timing")
	}
}

func TestRunArmsDedupesVariantIDs(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.variants = append(f.variants, f.variants[0])
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, passJSON)
	options := baseOptions(f, ArmGCI)
	options.GateBin = gate
	options.Workers = 2
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	if runs := readRuns(t, options.Out); len(runs) != 1 {
		t.Fatalf("recorded %d runs for a duplicated variant, want 1", len(runs))
	}
}

func TestRunArmsMemoDoesNotLeakAcrossVariants(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.addVariant(t, "broken", ClassMutant, map[string]string{"a.go": "package proj\n\nfunc Add(a, b int) int { return a - b }\n"})
	f.addVariant(t, "good2", ClassTrue, map[string]string{})
	options := baseOptions(f, ArmB0)
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	want := map[string]bool{"good": false, "broken": true, "good2": false}
	for id, blocked := range want {
		if run := findRun(t, runs, id, ArmB0, 1); run.Blocked != blocked || run.Unknown {
			t.Errorf("B0 on %s = %+v, want blocked=%v (an earlier variant's result leaked)", id, run, blocked)
		}
	}
}

func TestRunArmsB0StopsAtFirstFailure(t *testing.T) {
	log := goWrapper(t, "exec \"$REAL_GO\" \"$@\"\n")
	f := newFixture(t, false)
	f.addVariant(t, "nobuild", ClassStub, map[string]string{"a.go": "package proj\n\nfunc Add(a, b int) int { return missing }\n"})
	if err := RunArms(context.Background(), baseOptions(f, ArmB0)); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	run := findRun(t, readRuns(t, filepath.Join(f.workDir, "runs.jsonl")), "nobuild", ArmB0, 1)
	if !run.Blocked || !strings.Contains(run.Reason, "missing") {
		t.Fatalf("B0 = %+v, want blocked with the build error", run)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range strings.Fields(string(calls)) {
		if call == "vet" || call == "test" {
			t.Fatalf("B0 ran go %s after go build failed; calls: %s", call, calls)
		}
	}
}

func TestRunArmsGoTestTimeoutBlocksBArms(t *testing.T) {
	goWrapper(t, "if [ \"$1\" = test ] && [ -f SLOW_TEST ]; then exec sleep 120; fi\nexec \"$REAL_GO\" \"$@\"\n")
	fakeLint(t, "exit 0\n")
	f := newFixture(t, false)
	f.addVariant(t, "slow", ClassTrue, map[string]string{"SLOW_TEST": "x\n"})
	options := baseOptions(f, ArmB0, ArmB2, ArmB2Star)
	options.CommandTimeout = 5 * time.Second
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	for _, arm := range []Arm{ArmB0, ArmB2, ArmB2Star} {
		run := findRun(t, runs, "slow", arm, 1)
		if !run.Blocked || run.Unknown || !strings.HasPrefix(run.Reason, timeoutReasonPrefix) {
			t.Errorf("%s = %+v, want a block caused by the go test timeout", arm, run)
		}
	}
	summary := Summarize(f.variants, runs, nil)
	if summary.TimeoutBlocks[ArmB0] != 1 || summary.TimeoutBlocks[ArmB2] != 1 || summary.TimeoutBlocks[ArmB2Star] != 1 {
		t.Errorf("timeout blocks = %v, want one per B arm", summary.TimeoutBlocks)
	}
}

func TestRunArmsLintErrorNeverDowngradesABlock(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "golangci-lint"), []byte("#!/nonexistent/interpreter\n"), 0o700); err != nil { //nolint:gosec // Test fixture must be executable.
		t.Fatal(err)
	}
	prependPath(t, bin)
	f := newFixture(t, true)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.addVariant(t, "newfail", ClassMutant, map[string]string{"a.go": "package proj\n\nfunc Add(a, b int) int { return a - b }\n"})
	options := baseOptions(f, ArmB2, ArmB2Star)
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	for _, arm := range []Arm{ArmB2, ArmB2Star} {
		run := findRun(t, runs, "newfail", arm, 1)
		if !run.Blocked || run.Unknown || !strings.Contains(run.Error, "golangci-lint") {
			t.Errorf("%s on newfail = %+v, want blocked, with the lint failure only a note", arm, run)
		}
	}
	// With nothing else blocking, the missing lint verdict does make B2* unknown.
	if run := findRun(t, runs, "good", ArmB2Star, 1); !run.Unknown || run.Blocked {
		t.Errorf("B2s on good = %+v, want unknown", run)
	}
}

const unreachableCode = "package %s\n\n// Un has code vet reports but go test's vet subset does not.\nfunc Un() int {\n\treturn 1\n\treturn 2\n}\n"

func TestRunArmsB2StarIgnoresVetFailuresAlsoAtBase(t *testing.T) {
	fakeLint(t, "exit 0\n")
	f := newFixtureWith(t, map[string]string{"v.go": strings.Replace(unreachableCode, "%s", "proj", 1)})
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	f.addVariant(t, "newvet", ClassStub, map[string]string{"sub/s.go": strings.Replace(unreachableCode, "%s", "sub", 1)})
	options := baseOptions(f, ArmB2, ArmB2Star)
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	if run := findRun(t, runs, "good", ArmB2, 1); !run.Blocked {
		t.Errorf("B2 on good = %+v, want blocked by the vet failure that exists at base", run)
	}
	if run := findRun(t, runs, "good", ArmB2Star, 1); run.Blocked || run.Unknown {
		t.Errorf("B2s on good = %+v, want the base vet failure ignored", run)
	}
	newvet := findRun(t, runs, "newvet", ArmB2Star, 1)
	if !newvet.Blocked || !strings.HasPrefix(newvet.Reason, "go vet failed: example.com/proj/sub") {
		t.Errorf("B2s on newvet = %+v, want blocked by the new package's vet failure", newvet)
	}
	cached, err := os.ReadFile(filepath.Join(f.workDir, "basefail", "proj-"+f.base+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cached), `"vet_packages":["example.com/proj"]`) {
		t.Errorf("base failure cache = %s, want the vet package", cached)
	}
}

func TestRunArmsB3GrepSeesTheResetTree(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "gate", "b3-grep.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skip("validation/gate/b3-grep.sh not present")
	}
	// The lint step leaves a line the grep would flag if it saw the dirty tree.
	fakeLint(t, "echo '// t.Skip(1)' >> a.go\nexit 0\n")
	f := newFixture(t, false)
	f.addVariant(t, "good", ClassTrue, map[string]string{})
	skipped := strings.Replace(fixtureTest, "func TestAdd(t *testing.T) {\n", "func TestAdd(t *testing.T) {\n\tt.Skip(\"flaky\")\n", 1)
	f.addVariant(t, "skipadd", ClassSkipTests, map[string]string{"a_test.go": skipped})
	options := baseOptions(f, ArmB3)
	options.B3Script = script
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	if run := findRun(t, runs, "good", ArmB3, 1); run.Blocked || run.Unknown || run.Error != "" {
		t.Errorf("B3 on good = %+v, want a pass: the grep must not see lint's changes", run)
	}
	if run := findRun(t, runs, "skipadd", ArmB3, 1); !run.Blocked || !strings.Contains(run.Reason, "added-skip") {
		t.Errorf("B3 on skipadd = %+v, want blocked by the real grep script", run)
	}
}

func TestRunArmsRecordsGateTextBytes(t *testing.T) {
	f := newFixture(t, false)
	f.addVariant(t, "flagged", ClassSkipTests, map[string]string{})
	gate := filepath.Join(t.TempDir(), "agentic-go")
	writeScript(t, gate, "cat <<'EOF'\n"+`{"schema_version":"agentic.check/v1","verdict":"block","base":{"ref":"main","commit":"abcdef1234567","source":"flag"},`+
		`"items":[{"severity":"block","code":"test.skip_added","file":"a_test.go","line":3,"message":"Skip added.","fix":"Remove it."}],"notes":[],"stats":{}}`+"\nEOF\n")
	options := baseOptions(f, ArmGCI, ArmB0)
	options.GateBin = gate
	if err := RunArms(context.Background(), options); err != nil {
		t.Fatalf("RunArms() error = %v", err)
	}
	runs := readRuns(t, options.Out)
	run := findRun(t, runs, "flagged", ArmGCI, 1)
	if run.TextBytes <= 0 || run.TextBytes > gateTextLimit || run.OutputBytes <= run.TextBytes/2 {
		t.Errorf("Gci bytes = raw %d, text %d; want a text report within the %d byte budget", run.OutputBytes, run.TextBytes, gateTextLimit)
	}
	if b0 := findRun(t, runs, "flagged", ArmB0, 1); b0.TextBytes != 0 {
		t.Errorf("B0 text bytes = %d, want 0", b0.TextBytes)
	}
}
