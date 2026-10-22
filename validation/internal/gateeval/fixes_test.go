package gateeval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEvalEnvDropsCI(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("CIRCLE", "kept")
	for _, entry := range evalEnv() {
		if entry == "CI" || strings.HasPrefix(entry, "CI=") {
			t.Fatalf("evalEnv kept %q", entry)
		}
	}
	if !contains(evalEnv(), "CIRCLE=kept") || !contains(evalEnv(), "GOTOOLCHAIN=local") || !contains(evalEnv(), "GOFLAGS=-mod=mod") {
		t.Error("evalEnv must keep other variables and set the Go settings")
	}
	result := runStep(context.Background(), t.TempDir(), time.Minute, "sh", "-c", `printf '%s' "${CI-unset}"`)
	if result.err != nil || string(result.stdout) != "unset" {
		t.Errorf("subprocess saw CI = %q (%v)", result.stdout, result.err)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestRunStepTimeoutIsDistinct(t *testing.T) {
	result := runStep(context.Background(), t.TempDir(), 50*time.Millisecond, "sleep", "5")
	if !errors.Is(result.err, errStepTimeout) {
		t.Errorf("err = %v, want a timeout", result.err)
	}
	if result := runStep(context.Background(), t.TempDir(), time.Minute, "sh", "-c", "exit 3"); result.err != nil || result.exit != 3 {
		t.Errorf("exit status must not be an error: %+v", result)
	}
}

func TestFinishEditKeepsNonGofmtFileIntact(t *testing.T) {
	const src = "package p\n\nimport (\"strings\"\n\t\"testing\")\n\nfunc   TestA(t *testing.T) {\n\tif strings.ToUpper(\"a\")!=\"A\" { t.Error(\"x\") }\n}\n\nfunc TestB(t *testing.T)   {  }\n"
	got, err := coverUpSource(ClassDeleteTests, "/tmp/x_test.go", []byte(src), []string{"TestA"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "func TestB(t *testing.T)   {  }\n") {
		t.Errorf("untouched code must keep its formatting:\n%s", text)
	}
	if strings.Contains(text, "strings") || strings.Contains(text, "TestA") || !strings.Contains(text, "\"testing\"") {
		t.Errorf("edit and import adjustment expected:\n%s", text)
	}
	if !strings.HasPrefix(text, "package p\n\nimport") {
		t.Errorf("the rest of the header must be unchanged:\n%s", text)
	}
	// The same edit on a gofmt-clean file is formatted as a whole.
	clean := "package p\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestA(t *testing.T) {\n\t_ = strings.ToUpper\n}\n\nfunc TestB(t *testing.T) {}\n"
	got, err = coverUpSource(ClassDeleteTests, "/tmp/x_test.go", []byte(clean), []string{"TestA"})
	if want := "package p\n\nimport (\n\t\"testing\"\n)\n\nfunc TestB(t *testing.T) {}\n"; err != nil || string(got) != want {
		t.Errorf("clean file result: %q %v", got, err)
	}
}

func TestSwapImportBlockRefusesWithoutBlock(t *testing.T) {
	raw := []byte("package p\n\nfunc F() { fmt.Println() }\n")
	formatted := []byte("package p\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n")
	if _, err := swapImportBlock(raw, formatted); !errors.Is(err, errFormattingNoise) {
		t.Errorf("err = %v, want formatting noise", err)
	}
	same, err := swapImportBlock(formatted, formatted)
	if err != nil || !reflect.DeepEqual(same, formatted) {
		t.Errorf("identical import sets must leave the file alone: %q %v", same, err)
	}
}

func TestCommitPathsIsReproducible(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	runGit(t, base, "init", "-q", "-b", "main")
	writeFile(t, base, "a.go", "package p\n")
	runGit(t, base, "add", "-A")
	runGit(t, base, "-c", "user.name=x", "-c", "user.email=x@x", "commit", "-q", "-m", "base")
	var shas []string
	for n := range 2 {
		dir := filepath.Join(t.TempDir(), "clone")
		runGit(t, filepath.Dir(dir), "clone", "-q", base, dir)
		writeFile(t, dir, "a.go", "package p // changed\n")
		sha, err := commitPaths(context.Background(), dir, []string{"a.go"}, "gateeval M")
		if err != nil {
			t.Fatal(err)
		}
		shas = append(shas, sha)
		if n == 0 {
			time.Sleep(1100 * time.Millisecond) // wall-clock seconds must not matter
		}
	}
	if shas[0] != shas[1] {
		t.Errorf("same edit gave %s and %s, want identical commits", shas[0], shas[1])
	}
}

func TestEmptyDirectListsNeverRunTheRootPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{"p.go": "package p\n\nfunc F() int { return 1 }\n"})
	cs := &commitState{generator: &generator{options: GenerateOptions{TestTimeout: time.Minute}}, tree: dir}
	if _, err := cs.directPasses(context.Background()); !errors.Is(err, errNoDirect) {
		t.Errorf("directPasses error = %v", err)
	}
	search := &flawSearch{sources: map[string][]byte{}, tree: dir, timeout: time.Minute}
	if _, err := search.firstCompiling(context.Background(), nil); !errors.Is(err, errNoDirect) {
		t.Errorf("firstCompiling error = %v", err)
	}
	if _, err := newMutantSearch(context.Background(), dir, "a", "b", "example.com/proj", nil, time.Minute, 5); !errors.Is(err, errNoDirect) {
		t.Errorf("newMutantSearch error = %v", err)
	}
	m := &mutantSearch{tree: dir, cands: []candidate{{File: "p.go"}}, sources: map[string][]byte{"p.go": []byte("package p\n")}, results: map[int]mutantResult{}}
	if _, err := m.try(context.Background(), 0); !errors.Is(err, errNoDirect) {
		t.Errorf("try error = %v", err)
	}
}

func TestScanTestStreamReportsFailedPackages(t *testing.T) {
	stream := `{"Action":"fail","Package":"x/a","Test":"TestA"}
{"Action":"fail","Package":"x/a"}
{"Action":"fail","Package":"x/b"}
{"Action":"build-fail","ImportPath":"x/c [x/c.test]"}
{"Action":"pass","Package":"x/d"}
`
	got := scanTestStream([]byte(stream)).FailedPackages
	if want := []string{"x/a", "x/b", "x/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("failed packages = %v, want %v", got, want)
	}
}

func TestConfirmRejectsAMutantThatDoesNotFailAgain(t *testing.T) {
	f := newGenFixture(t)
	tree := f.worktree(t, f.commits["c1"])
	search, err := newMutantSearch(context.Background(), tree, f.commits["c0"], f.commits["c1"], "example.com/proj", []string{"."}, time.Minute, 20)
	if err != nil {
		t.Fatal(err)
	}
	oracle := []TestRef{{Package: "example.com/proj", File: "calc_test.go", Name: "TestClamp"}}
	killing, _, err := search.findKilling(context.Background())
	if err != nil || killing < 0 {
		t.Fatalf("findKilling = %d, %v", killing, err)
	}
	if ok, err := search.confirm(context.Background(), killing, oracle); err != nil || !ok {
		t.Errorf("a real kill must confirm: %v %v", ok, err)
	}
	// A candidate that does not break TestClamp (the Double change) must not confirm.
	for i, c := range search.cands {
		if c.Op == opZeroReturn && strings.Contains(string(search.sources[c.File][c.Edits[0].Start:c.Edits[0].End]), "n * 2") {
			if ok, err := search.confirm(context.Background(), i, oracle); err != nil || ok {
				t.Errorf("a mutant that leaves TestClamp passing must not confirm: %v %v", ok, err)
			}
			return
		}
	}
	t.Fatal("no Double candidate found")
}

func TestOpCounts(t *testing.T) {
	m := &mutantSearch{cands: []candidate{{Op: 1}, {Op: 3}, {Op: 3}, {Op: 5}}}
	if got, want := m.opCounts(), "op1=1, op2=0, op3=2, op4=0, op5=1"; got != want {
		t.Errorf("opCounts = %q, want %q", got, want)
	}
}

// miniRepo builds a history of file sets (each commit's files are written over
// the previous) and returns the repo directory and the commit hashes.
func miniRepo(t *testing.T, history ...map[string]string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	var shas []string
	for n, files := range history {
		for name, content := range files {
			writeFile(t, dir, name, content)
		}
		shas = append(shas, commitAll(t, dir, "c"+string(rune('0'+n))))
	}
	return dir, shas
}

func selectMini(t *testing.T, dir, pinned string, timeout time.Duration) []Exclusion {
	t.Helper()
	work := t.TempDir()
	writeFile(t, work, "corpus.csv", "project,url,pinned\nmini,"+dir+","+pinned+"\n")
	options := SelectOptions{
		CorpusCSV: filepath.Join(work, "corpus.csv"), WorkDir: filepath.Join(work, "w"), Out: filepath.Join(work, "sel.jsonl"),
		ExclusionsOut: filepath.Join(work, "excl.jsonl"), MaxScan: 10, TestTimeout: timeout,
	}
	if err := Select(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	exclusions, err := ReadJSONL[Exclusion](options.ExclusionsOut)
	if err != nil {
		t.Fatal(err)
	}
	var commitReasons []Exclusion
	for _, e := range exclusions {
		if e.Commit != "" {
			commitReasons = append(commitReasons, e)
		}
	}
	return commitReasons
}

const (
	miniCode   = "package p\n\n// F returns n.\nfunc F(n int) int { return n }\n"
	miniCodeV2 = "package p\n\n// F returns n.\nfunc F(n int) int {\n\treturn n\n}\n"
	miniTest   = "package p\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {\n\tif F(1) != 1 {\n\t\tt.Fatal(\"f\")\n\t}\n}\n"
)

func TestExpensiveFilterReasons(t *testing.T) {
	t.Run("module download failed", func(t *testing.T) {
		t.Setenv("GOPROXY", "off")
		mod := "module example.com/p\n\ngo 1.21\n\nrequire example.invalid/dep v1.0.0\n"
		dir, shas := miniRepo(t,
			map[string]string{"go.mod": mod, "p.go": miniCode, "p_test.go": miniTest},
			map[string]string{"p.go": miniCodeV2, "p_test.go": miniTest + "\n// more\n"},
		)
		got := selectMini(t, dir, shas[1], time.Minute)
		if len(got) != 1 || got[0].Reason != "module download failed" || got[0].Commit != shas[1] {
			t.Errorf("exclusions = %+v", got)
		}
	})
	t.Run("timed out at base", func(t *testing.T) {
		dir, shas := miniRepo(t,
			map[string]string{"go.mod": fixtureMod, "p.go": miniCode, "p_test.go": miniTest},
			map[string]string{"p.go": miniCodeV2, "p_test.go": miniTest + "\n// more\n"},
		)
		got := selectMini(t, dir, shas[1], time.Millisecond)
		if len(got) != 1 || got[0].Reason != "timed out at base" {
			t.Errorf("exclusions = %+v", got)
		}
	})
	t.Run("build fails at commit", func(t *testing.T) {
		dir, shas := miniRepo(t,
			map[string]string{"go.mod": fixtureMod, "p.go": miniCode, "p_test.go": miniTest},
			map[string]string{"p.go": miniCodeV2 + "\nfunc Broken() int { return undefined }\n", "p_test.go": miniTest + "\n// more\n"},
		)
		got := selectMini(t, dir, shas[1], time.Minute)
		if len(got) != 1 || got[0].Reason != "build fails at commit" {
			t.Errorf("exclusions = %+v", got)
		}
	})
	t.Run("flaky at commit", func(t *testing.T) {
		mark := filepath.Join(t.TempDir(), "mark")
		t.Setenv("FLAKY_MARK", mark)
		flaky := "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestFlaky(t *testing.T) {\n\tif _, err := os.Stat(os.Getenv(\"FLAKY_MARK\")); err == nil {\n\t\tt.Fatal(\"second run fails\")\n\t}\n\tif err := os.WriteFile(os.Getenv(\"FLAKY_MARK\"), nil, 0o600); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n"
		dir, shas := miniRepo(t,
			map[string]string{"go.mod": fixtureMod, "p.go": miniCode, "p_test.go": miniTest},
			map[string]string{"p.go": miniCodeV2, "p_test.go": miniTest + "\n// more\n", "flaky_test.go": flaky},
		)
		got := selectMini(t, dir, shas[1], time.Minute)
		if len(got) != 1 || got[0].Reason != "flaky at commit" {
			t.Errorf("exclusions = %+v", got)
		}
	})
}

func TestSelectResumesPerProject(t *testing.T) {
	t.Parallel()
	f := newSelectFixture(t)
	work := t.TempDir()
	writeFile(t, work, "corpus.csv", "project,url,pinned\nalpha,"+f.dir+","+f.commits["big"]+"\nbeta,"+f.dir+","+f.commits["big"]+"\n")
	options := SelectOptions{
		CorpusCSV: filepath.Join(work, "corpus.csv"), WorkDir: filepath.Join(work, "w"), Out: filepath.Join(work, "sel.jsonl"),
		ExclusionsOut: filepath.Join(work, "excl.jsonl"), PerRepoTrue: 1, PerRepoFlaw: 1, PerRepoDestructive: 1, MaxScan: 100,
		TestTimeout: time.Minute,
	}
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if err := Select(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	full, fullExcl := read(options.Out), read(options.ExclusionsOut)
	for _, name := range []string{"alpha", "beta"} {
		if !strings.Contains(fullExcl, `"reason":"complete: `+name+`"`) {
			t.Fatalf("missing completion marker for %s:\n%s", name, fullExcl)
		}
	}

	// A rerun of finished work changes nothing and does not redo any project.
	if err := Select(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if read(options.Out) != full || read(options.ExclusionsOut) != fullExcl {
		t.Fatal("rerunning a complete selection must not change the files")
	}

	// An interrupted second project (no marker, partial lines) is redone; the first is kept as is.
	var keptExcl []string
	for _, line := range strings.Split(strings.TrimSpace(fullExcl), "\n") {
		if !strings.Contains(line, `"complete: beta"`) {
			keptExcl = append(keptExcl, line)
		}
	}
	if err := os.WriteFile(options.ExclusionsOut, []byte(strings.Join(keptExcl, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(work, "w", "work")); err != nil { // also prove it can restart with fresh worktrees
		t.Fatal(err)
	}
	if err := Select(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if got := read(options.Out); got != full {
		t.Errorf("selections after resume differ:\n%s\nwant:\n%s", got, full)
	}
	if got := read(options.ExclusionsOut); strings.Count(got, "\n") != strings.Count(fullExcl, "\n") {
		t.Errorf("exclusions after resume: %d lines, want %d (no duplicates)", strings.Count(got, "\n"), strings.Count(fullExcl, "\n"))
	}
	if !strings.Contains(read(options.ExclusionsOut), `"complete: beta"`) {
		t.Error("beta must be marked complete again")
	}
}
