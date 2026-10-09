package gate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/verification"
	"github.com/agentic-mcps/go/internal/workspace"
)

// gateFixtureFiles is a module with a library and a consumer package, each
// with passing tests.
var gateFixtureFiles = map[string]string{
	"go.mod":     "module example.com/fixture\n\ngo 1.25\n",
	".gitignore": "*.marker\n",
	"lib/lib.go": `package lib

// Add returns a plus b.
func Add(a, b int) int { return a + b }

// Sign reports the sign of v.
func Sign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

// Greeting is the word consumers greet with.
func Greeting() string { return "hello" }
`,
	"lib/lib_test.go": `package lib

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2, 3) != 5")
	}
}

func TestSign(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-4, -1}, {0, 0}, {9, 1}} {
		if got := Sign(tc.in); got != tc.want {
			t.Fatalf("Sign(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
`,
	"app/app.go": `package app

import "example.com/fixture/lib"

// Welcome greets name.
func Welcome(name string) string { return lib.Greeting() + ", " + name }

// Total sums values.
func Total(values ...int) int {
	total := 0
	for _, value := range values {
		total = lib.Add(total, value)
	}
	return total
}
`,
	"app/app_test.go": `package app

import "testing"

func TestWelcome(t *testing.T) {
	if got := Welcome("world"); got != "hello, world" {
		t.Fatalf("Welcome() = %q", got)
	}
}

func TestTotal(t *testing.T) {
	if got := Total(1, 2, 3); got != 6 {
		t.Fatalf("Total() = %d", got)
	}
}
`,
}

// gateTruePatch adds a tested function to lib.
var gateTruePatch = map[string]string{
	"lib/max.go": `package lib

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
`,
	"lib/max_test.go": `package lib

import "testing"

func TestMax(t *testing.T) {
	if Max(1, 2) != 2 || Max(3, 2) != 3 {
		t.Fatal("Max is wrong")
	}
}
`,
}

// gateRepo commits the fixture plus extra base files, then applies the
// working-tree change (an empty content deletes the file).
func gateRepo(t *testing.T, baseExtra, change map[string]string) *baseFixture {
	t.Helper()
	f := newBaseFixture(t)
	for name, content := range gateFixtureFiles {
		f.write(name, content)
	}
	for name, content := range baseExtra {
		f.write(name, content)
	}
	f.commit("base")
	for name, content := range change {
		if content == "" {
			f.remove(name)
			continue
		}
		f.write(name, content)
	}
	return f
}

func gateNew(t *testing.T, root string, store *Store) *Gate {
	t.Helper()
	ctx := context.Background()
	ws, err := workspace.Open(ctx, root)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: 90 * time.Second, OutputLimit: 64 << 20})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	g, err := New(ws, runner, "test", store)
	if err != nil {
		t.Fatalf("new gate: %v", err)
	}
	g.getenv = noEnv
	return g
}

func gateRunFixture(t *testing.T, root string, options Options) Result {
	t.Helper()
	result, err := gateNew(t, root, nil).Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func gateFind(result Result, code string) (Item, bool) {
	for _, item := range result.Items {
		if item.Code == code {
			return item, true
		}
	}
	return Item{}, false
}

func gateWant(t *testing.T, result Result, verdict Verdict, code string, severity Severity) Item {
	t.Helper()
	if result.Verdict != verdict {
		t.Fatalf("verdict = %s, want %s\nitems: %+v\nnotes: %q", result.Verdict, verdict, result.Items, result.Notes)
	}
	if code == "" {
		return Item{}
	}
	item, ok := gateFind(result, code)
	if !ok {
		t.Fatalf("no %s item\nitems: %+v\nnotes: %q", code, result.Items, result.Notes)
	}
	if item.Severity != severity {
		t.Fatalf("%s severity = %s, want %s: %+v", code, item.Severity, severity, item)
	}
	return item
}

func TestGateRunScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs git and go")
	}
	cases := []struct {
		name     string
		base     map[string]string
		change   map[string]string
		check    func(t *testing.T, result Result)
		options  Options
		parallel bool
	}{
		{
			name: "true patch passes", change: gateTruePatch, parallel: true,
			check: func(t *testing.T, result Result) {
				gateWant(t, result, VerdictPass, "", "")
				for _, item := range result.Items {
					if item.Severity == SeverityBlock {
						t.Fatalf("unexpected block item %+v", item)
					}
				}
				if result.Stats.PackagesTested != 2 || result.Stats.PackagesAffected != 2 {
					t.Fatalf("stats = %+v, want 2 of 2 packages tested", result.Stats)
				}
				if result.Fingerprint == "" {
					t.Fatal("fingerprint is empty")
				}
			},
		},
		{
			name: "deleted test blocks", parallel: true,
			change: map[string]string{"lib/lib_test.go": `package lib

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("Add(2, 3) != 5")
	}
}
`},
			check: func(t *testing.T, result Result) {
				gateWant(t, result, VerdictBlock, CodeTestDeleted, SeverityBlock)
			},
		},
		{
			name: "consumer break blocks as consumer", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], `return "hello"`, `return "hi"`, 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestConsumerFailed, SeverityBlock)
				if item.File != "app/app_test.go" || item.Line != 5 {
					t.Fatalf("location = %s:%d, want app/app_test.go:5", item.File, item.Line)
				}
				for _, want := range []string{"TestWelcome", "consumer package example.com/fixture/app"} {
					if !strings.Contains(item.Message, want) {
						t.Fatalf("message %q lacks %q", item.Message, want)
					}
				}
				if !strings.Contains(item.Detail, `Welcome() = "hi, world"`) {
					t.Fatalf("detail lacks the failure: %q", item.Detail)
				}
			},
		},
		{
			name: "direct test broken by the change blocks", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], "return -1", "return 1", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestFailed, SeverityBlock)
				if item.Message != "TestSign fails after this change" || item.File != "lib/lib_test.go" || item.Line != 11 {
					t.Fatalf("item = %+v", item)
				}
				if !strings.Contains(item.Fix, "do not change or skip the test") || !strings.Contains(item.Detail, "Sign(-4) = 1, want -1") {
					t.Fatalf("item = %+v", item)
				}
				if _, consumer := gateFind(result, CodeTestConsumerFailed); consumer {
					t.Fatalf("direct failure reported as consumer: %+v", result.Items)
				}
			},
		},
		{
			name: "new failing test absent at base blocks", parallel: true,
			change: map[string]string{
				"lib/max.go":      gateTruePatch["lib/max.go"],
				"lib/max_test.go": strings.Replace(gateTruePatch["lib/max_test.go"], "Max(1, 2) != 2", "Max(1, 2) != 1", 1),
			},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestFailed, SeverityBlock)
				if item.Message != "TestMax fails after this change" {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "failing test in a package missing at base blocks", parallel: true,
			change: map[string]string{
				"extra/extra.go":      "package extra\n\n// Two returns two.\nfunc Two() int { return 3 }\n",
				"extra/extra_test.go": "package extra\n\nimport \"testing\"\n\nfunc TestTwo(t *testing.T) {\n\tif Two() != 2 {\n\t\tt.Fatal(\"Two() != 2\")\n\t}\n}\n",
			},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestFailed, SeverityBlock)
				if item.Message != "TestTwo fails after this change" {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "typo in a standard library import blocks", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], "package lib\n", "package lib\n\nimport \"fmtt\"\n\nvar _ = fmtt.Sprint\n", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeBuild, SeverityBlock)
				if item.File != "lib/lib.go" || item.Line != 3 || !strings.Contains(item.Message, "package fmtt is not in std") || strings.Contains(item.Message, "(/") {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "import of a missing local package blocks", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], "package lib\n", "package lib\n\nimport \"example.com/fixture/nope\"\n\nvar _ = nope.X\n", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeBuild, SeverityBlock)
				if item.File != "lib/lib.go" || item.Line != 3 || !strings.Contains(item.Message, "example.com/fixture/nope") {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "import cycle blocks", parallel: true,
			change: map[string]string{"lib/cycle.go": "package lib\n\nimport \"example.com/fixture/app\"\n\nvar _ = app.Total\n"},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeBuild, SeverityBlock)
				if !strings.Contains(item.Message, "import cycle not allowed") {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "library code exiting under a test blocks as a crash", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(strings.Replace(gateFixtureFiles["lib/lib.go"], "return -1", "os.Exit(1)\n\t\treturn -1", 1), "package lib\n", "package lib\n\nimport \"os\"\n", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestFailed, SeverityBlock)
				want := "tests in example.com/fixture/lib crash outside a named test after this change; last test started: TestSign"
				if item.Message != want || item.File != "lib/lib_test.go" || item.Line != 11 {
					t.Fatalf("item = %+v, want message %q at lib/lib_test.go:11", item, want)
				}
			},
		},
		{
			name: "base that does not build is not compared and never blocks", parallel: true,
			base: map[string]string{
				".gitignore":      "*.marker\ngen.go\n",
				"lib/gen.go":      "package lib\n\n// Generated is produced by a generator and not committed.\nconst Generated = 1\n",
				"lib/gen_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestGenerated(t *testing.T) {\n\tif Generated != 1 || Sign(-1) != -1 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
			},
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], "return -1", "return 1", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictUnknown, CodeTestFailed, SeverityWarn)
				if !strings.Contains(item.Message, "not compared with base: package does not build") {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "testdata change that breaks a test blocks", parallel: true,
			base: map[string]string{
				"lib/testdata/greeting.data": "hello\n",
				"lib/greet_test.go":          "package lib\n\nimport (\n\t\"os\"\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestGreetingFile(t *testing.T) {\n\tdata, err := os.ReadFile(\"testdata/greeting.data\")\n\tif err != nil || strings.TrimSpace(string(data)) != Greeting() {\n\t\tt.Fatalf(\"greeting file = %q\", data)\n\t}\n}\n",
			},
			change: map[string]string{"lib/testdata/greeting.data": "bye\n"},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestFailed, SeverityBlock)
				if item.Message != "TestGreetingFile fails after this change" {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "compile error in a test file blocks with compiler text", parallel: true,
			change: map[string]string{"lib/lib_test.go": gateFixtureFiles["lib/lib_test.go"] + "\nvar _ = undefinedHelper\n"},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeBuild, SeverityBlock)
				if item.File != "lib/lib_test.go" || !strings.Contains(item.Detail, "undefined: undefinedHelper") {
					t.Fatalf("item = %+v", item)
				}
			},
		},
		{
			name: "subtest failure is attributed to its top-level test", parallel: true,
			base: map[string]string{
				"lib/abs.go":      "package lib\n\n// Abs returns the absolute value of v.\nfunc Abs(v int) int {\n\tif v < 0 {\n\t\treturn -v\n\t}\n\treturn v\n}\n",
				"lib/abs_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestAbs(t *testing.T) {\n\tt.Run(\"neg\", func(t *testing.T) {\n\t\tif Abs(-2) != 2 {\n\t\t\tt.Fatal(\"Abs(-2) != 2\")\n\t\t}\n\t})\n\tt.Run(\"pos\", func(t *testing.T) {\n\t\tif Abs(3) != 3 {\n\t\t\tt.Fatal(\"Abs(3) != 3\")\n\t\t}\n\t})\n}\n",
			},
			change: map[string]string{"lib/abs.go": "package lib\n\n// Abs returns the absolute value of v.\nfunc Abs(v int) int {\n\treturn v\n}\n"},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeTestFailed, SeverityBlock)
				if item.Message != "TestAbs fails after this change" || !strings.HasPrefix(item.Detail, "failing subtests: TestAbs/neg\n") {
					t.Fatalf("item = %+v", item)
				}
				count := 0
				for _, other := range result.Items {
					if other.Severity == SeverityBlock && strings.HasPrefix(other.Code, "test.") {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("got %d test items, want 1: %+v", count, result.Items)
				}
			},
		},
		{
			name: "test failing at base too is a warning", parallel: true,
			base: map[string]string{"lib/broken_test.go": `package lib

import "testing"

func TestBroken(t *testing.T) { t.Fatal("broken before the change") }
`},
			change: gateTruePatch,
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictPass, CodeTestPreexisting, SeverityWarn)
				if !strings.Contains(item.Message, "TestBroken also fails at ") {
					t.Fatalf("message = %q", item.Message)
				}
			},
		},
		{
			name: "syntax error blocks with its line", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], "return a + b }", "return a + }", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeSyntax, SeverityBlock)
				if item.File != "lib/lib.go" || item.Line != 4 {
					t.Fatalf("location = %s:%d, want lib/lib.go:4", item.File, item.Line)
				}
				if len(result.Notes) != 1 || result.Notes[0] != "other checks skipped until the code parses" {
					t.Fatalf("notes = %q", result.Notes)
				}
			},
		},
		{
			name: "compile error blocks with compiler text", parallel: true,
			change: map[string]string{"lib/lib.go": strings.Replace(gateFixtureFiles["lib/lib.go"], "return a + b }", "return a + undefinedThing }", 1)},
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictBlock, CodeBuild, SeverityBlock)
				if item.File != "lib/lib.go" || item.Line != 4 || !strings.Contains(item.Detail, "undefined: undefinedThing") {
					t.Fatalf("build item = %+v", item)
				}
				for _, other := range result.Items {
					if strings.HasPrefix(other.Code, "test.") {
						t.Fatalf("build failure was also reported as a test failure: %+v", other)
					}
				}
			},
		},
		{
			name: "flaky test is a warning",
			base: map[string]string{"lib/flaky_test.go": `package lib

import (
	"os"
	"testing"
)

func TestFlaky(t *testing.T) {
	if _, err := os.Stat("flaky.marker"); err != nil {
		_ = os.WriteFile("flaky.marker", nil, 0o644)
		t.Fatal("fails on the first run only")
	}
}
`},
			change: gateTruePatch, parallel: true,
			check: func(t *testing.T, result Result) {
				item := gateWant(t, result, VerdictPass, CodeTestFlaky, SeverityWarn)
				if item.File != "lib/flaky_test.go" {
					t.Fatalf("flaky item = %+v", item)
				}
			},
		},
		{
			name:    "exhausted budget is unknown and keeps integrity items",
			change:  map[string]string{"lib/testdata/want.golden": "new\n", "lib/max.go": gateTruePatch["lib/max.go"]},
			base:    map[string]string{"lib/testdata/want.golden": "old\n"},
			options: Options{Budget: time.Millisecond}, parallel: true,
			check: func(t *testing.T, result Result) {
				gateWant(t, result, VerdictUnknown, CodeGoldenModified, SeverityWarn)
				if !strings.Contains(strings.Join(result.Notes, "\n"), "time budget 1ms ran out before tests finished") {
					t.Fatalf("notes = %q", result.Notes)
				}
			},
		},
		{
			name: "no Go changes pass fast", parallel: true,
			change: map[string]string{"README.md": "docs\n"},
			check: func(t *testing.T, result Result) {
				gateWant(t, result, VerdictPass, "", "")
				if result.Stats.PackagesTested != 0 || result.Stats.ChangedFiles != 1 || result.Stats.ChangedGoFiles != 0 {
					t.Fatalf("stats = %+v", result.Stats)
				}
			},
		},
		{
			name: "no changes at all pass with a note", parallel: true,
			check: func(t *testing.T, result Result) {
				gateWant(t, result, VerdictPass, "", "")
				if len(result.Notes) != 1 || result.Notes[0] != "no changes against main" {
					t.Fatalf("notes = %q", result.Notes)
				}
			},
		},
		{
			name: "config change without Go changes still blocks", parallel: true,
			change: map[string]string{".github/workflows/ci.yml": "on: push\n"},
			check: func(t *testing.T, result Result) {
				gateWant(t, result, VerdictBlock, CodeConfigModified, SeverityBlock)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.parallel {
				t.Parallel()
			}
			f := gateRepo(t, tc.base, tc.change)
			tc.check(t, gateRunFixture(t, f.root, tc.options))
		})
	}
}

func TestGateRunCachesDefiniteResults(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs git and go")
	}
	f := gateRepo(t, nil, gateTruePatch)
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := gateNew(t, f.root, store)
	first, err := g.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if first.Stats.Cached || first.Verdict != VerdictPass {
		t.Fatalf("first run = %+v", first)
	}
	second, err := g.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !second.Stats.Cached || second.Verdict != VerdictPass || second.Fingerprint != first.Fingerprint {
		t.Fatalf("second run = %+v, want a cached pass with the same fingerprint", second)
	}

	// Near miss: different options are a different cache entry.
	third, err := g.Run(context.Background(), Options{Skip: "TestNothing"})
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if third.Stats.Cached || third.Fingerprint == first.Fingerprint {
		t.Fatalf("third run = %+v, want a fresh run", third)
	}
}

func TestGateRunRejectsUnknownProfile(t *testing.T) {
	f := gateRepo(t, nil, nil)
	if _, err := gateNew(t, f.root, nil).Run(context.Background(), Options{Profile: "nightly"}); err == nil {
		t.Fatal("Run accepted an unknown profile")
	}
}

func TestGateRunReturnsCallerCancellation(t *testing.T) {
	f := gateRepo(t, nil, gateTruePatch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gateNew(t, f.root, nil).Run(ctx, Options{}); err == nil {
		t.Fatal("Run ignored caller cancellation")
	}
}

func TestGateTrim(t *testing.T) {
	packages := []verification.ExecutionTarget{
		{ID: "m/far", Distance: 3},
		{ID: "m/b", Distance: 1},
		{ID: "m/a", Distance: 1},
		{ID: "m/direct2", Distance: 0},
		{ID: "m/direct1", Distance: 0},
		{ID: "m/c", Distance: 2},
	}
	cases := []struct {
		name  string
		want  []string
		limit int
	}{
		{name: "everything fits", limit: 10, want: []string{"m/direct1", "m/direct2", "m/a", "m/b", "m/c", "m/far"}},
		{name: "closest consumers first", limit: 4, want: []string{"m/direct1", "m/direct2", "m/a", "m/b"}},
		{name: "direct packages are always kept", limit: 1, want: []string{"m/direct1", "m/direct2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := make([]string, 0)
			for _, target := range gateTrim(packages, tc.limit) {
				got = append(got, target.ID)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("gateTrim = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGateCacheUnsafe(t *testing.T) {
	cases := []struct {
		files map[string]string
		name  string
		want  bool
	}{
		{name: "plain module", files: map[string]string{"go.mod": "module m\n\ngo 1.25\n"}},
		{name: "remote replace is safe", files: map[string]string{"go.mod": "module m\n\ngo 1.25\n\nreplace example.com/a => example.com/b v1.0.0\n"}},
		{name: "relative replace", want: true, files: map[string]string{"go.mod": "module m\n\ngo 1.25\n\nreplace example.com/a => ../a\n"}},
		{name: "absolute replace", want: true, files: map[string]string{"go.mod": "module m\n\ngo 1.25\n\nreplace example.com/a => /src/a\n"}},
		{name: "go.work", want: true, files: map[string]string{"go.mod": "module m\n\ngo 1.25\n", "go.work": "go 1.25\n\nuse .\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBaseFixture(t)
			for name, content := range tc.files {
				f.write(name, content)
			}
			if got := gateCacheUnsafe(f.root); got != tc.want {
				t.Fatalf("gateCacheUnsafe = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGateDocsOnly(t *testing.T) {
	cases := []struct {
		paths []string
		want  bool
	}{
		{paths: []string{"README.md", "docs/a.png", "sub/docs/guide/x.yaml", "LICENSE", "NOTICE.txt", "notes.rst", "a.markdown", "CHANGES.TXT"}, want: true},
		{paths: []string{"README.md", "lib/testdata/x.golden"}},
		{paths: []string{"lib/a.go"}},
		{paths: []string{"go.mod"}},
		{paths: []string{"lib/asm_amd64.s"}},
		{paths: []string{"db/schema.sql"}},
		{paths: []string{"config.yaml"}},
		{paths: []string{"documentation/x.yaml"}},
	}
	for _, tc := range cases {
		if got := gateDocsOnly(tc.paths); got != tc.want {
			t.Errorf("gateDocsOnly(%v) = %v, want %v", tc.paths, got, tc.want)
		}
	}
}

func TestGateSaltIncludesBuildEnvironment(t *testing.T) {
	g := &Gate{ws: &workspace.Workspace{}, version: "v"}
	env := map[string]string{}
	g.getenv = func(name string) string { return env[name] }
	base := g.salt(Options{})
	for _, name := range gateSaltEnv {
		env = map[string]string{name: "x"}
		if g.salt(Options{}) == base {
			t.Errorf("salt ignores %s", name)
		}
	}
	env = map[string]string{"HOME": "/elsewhere"}
	if g.salt(Options{}) != base {
		t.Error("salt depends on an unrelated variable")
	}
}

func TestGateRunReusesCachedBaseTree(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs git and go")
	}
	f := gateRepo(t, map[string]string{"lib/broken_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n"}, gateTruePatch)
	storeDir := t.TempDir()
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	g := gateNew(t, f.root, store)
	trees := func() []string {
		entries, readErr := os.ReadDir(filepath.Join(storeDir, "base"))
		if readErr != nil {
			t.Fatalf("reading base cache: %v", readErr)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	first, err := g.Run(context.Background(), Options{NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	gateWant(t, first, VerdictPass, CodeTestPreexisting, SeverityWarn)
	names := trees()
	if len(names) != 1 {
		t.Fatalf("base cache = %v, want one tree", names)
	}
	sentinel := filepath.Join(storeDir, "base", names[0], "sentinel")
	if err = os.WriteFile(sentinel, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := g.Run(context.Background(), Options{NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	gateWant(t, second, VerdictPass, CodeTestPreexisting, SeverityWarn)
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("second run rebuilt the base tree: %v", err)
	}
	if got := trees(); len(got) != 1 {
		t.Fatalf("base cache = %v, want one tree", got)
	}
}

func TestGateRunWithoutLockNotesTheError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs git and go")
	}
	f := gateRepo(t, nil, gateTruePatch)
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	// Replace the store directory with a file so the lock cannot be opened.
	if err = os.RemoveAll(filepath.Join(dir, "store")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "store"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := gateNew(t, f.root, store).Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	gateWant(t, result, VerdictPass, "", "")
	if !strings.Contains(strings.Join(result.Notes, "\n"), "ran without the gate lock: opening gate lock") {
		t.Fatalf("notes = %q", result.Notes)
	}
}
