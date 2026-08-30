package changeimpact_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/changeimpact"
	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestSnapshotIncludesFinalWorktreeAndChangedDeclarations(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod": "module example.test/change\n\ngo 1.25.0\n",
		"calc/calc.go": `package calc

func Add(left, right int) int {
	return left + right
}
`,
		"keep/keep.go": "package keep\n\nconst Stable = true\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")

	writeFile(t, repository, "calc/calc.go", `package calc

func Add(left, right int) int {
	return left + right + 1
}

func Sub(left, right int) int {
	return left - right
}
`)
	git(t, repository, "add", "calc/calc.go")
	writeFile(t, repository, "keep/keep.go", "package keep\n\nconst Stable = false\n")
	writeFile(t, repository, "newpkg/new.go", "package newpkg\n\nfunc Ready() bool { return true }\n")

	analysis := analyze(t, repository, changeimpact.Options{Base: base, Package: "./...", MaxPackages: 200})

	if analysis.Repository.BaseCommit != base || analysis.Repository.MergeBaseCommit != base {
		t.Fatalf("base identity = %#v, want %s", analysis.Repository, base)
	}
	if analysis.Repository.HeadCommit != base {
		t.Fatalf("head = %q, want %q", analysis.Repository.HeadCommit, base)
	}
	if analysis.Repository.SnapshotID == "" || !strings.HasPrefix(analysis.Repository.SnapshotID, "sha256:") {
		t.Fatalf("snapshot_id = %q, want sha256 identity", analysis.Repository.SnapshotID)
	}
	if !analysis.Repository.Dirty {
		t.Fatal("dirty = false, want true")
	}

	gotFiles := make([]string, 0, len(analysis.Change.Files))
	for _, file := range analysis.Change.Files {
		gotFiles = append(gotFiles, string(file.Change)+":"+file.Path)
		if file.BaseRanges == nil || file.CurrentRanges == nil {
			t.Fatalf("nil ranges for %#v", file)
		}
	}
	wantFiles := []string{"modified:calc/calc.go", "modified:keep/keep.go", "untracked:newpkg/new.go"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("changed files = %v, want %v", gotFiles, wantFiles)
	}

	gotDeclarations := make([]string, 0, len(analysis.Change.Declarations))
	for _, declaration := range analysis.Change.Declarations {
		gotDeclarations = append(gotDeclarations, string(declaration.Change)+":"+declaration.Package+":"+declaration.Name)
	}
	wantDeclarations := []string{
		"modified:example.test/change/calc:Add",
		"added:example.test/change/calc:Sub",
		"modified:example.test/change/keep:Stable",
		"untracked:example.test/change/newpkg:Ready",
	}
	if !reflect.DeepEqual(gotDeclarations, wantDeclarations) {
		t.Fatalf("changed declarations = %v, want %v", gotDeclarations, wantDeclarations)
	}
}

func TestSnapshotDistinguishesBaseDiffFromDirtyState(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":  "module example.test/change\n\ngo 1.25.0\n",
		"main.go": "package change\n\nconst Version = 1\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	writeFile(t, repository, "main.go", "package change\n\nconst Version = 2\n")
	git(t, repository, "add", "main.go")
	git(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "change version")

	analysis := analyze(t, repository, changeimpact.Options{Base: base})
	if analysis.Repository.Dirty {
		t.Fatal("dirty = true for a clean worktree with commits after base")
	}
	if len(analysis.Change.Files) != 1 || analysis.Change.Files[0].Path != "main.go" {
		t.Fatalf("change = %#v, want committed main.go change", analysis.Change)
	}
}

func TestSnapshotModelsRenameAndDeletion(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":  "module example.test/change\n\ngo 1.25.0\n",
		"old.go":  "package change\n\nfunc Kept() {}\n",
		"gone.go": "package change\n\nfunc Removed() {}\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	git(t, repository, "mv", "old.go", "new.go")
	if err := os.Remove(filepath.Join(repository, "gone.go")); err != nil {
		t.Fatalf("remove gone.go: %v", err)
	}

	analysis := analyze(t, repository, changeimpact.Options{Base: base})
	if got := analysis.Change.Files; len(got) != 2 || got[0].Change != "deleted" || got[1].Change != "renamed" || got[1].PreviousPath != "old.go" {
		t.Fatalf("files = %#v, want deletion and rename", got)
	}
	if got := analysis.Change.Declarations; len(got) != 1 || got[0].Name != "Removed" || got[0].Change != "deleted" {
		t.Fatalf("declarations = %#v, want deleted Removed", got)
	}
}

func TestSnapshotRejectsOptionLikeBase(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":  "module example.test/change\n\ngo 1.25.0\n",
		"main.go": "package change\n",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ws, err := workspace.Open(ctx, repository)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	analyzer, err := changeimpact.New(ws, runner)
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	_, err = analyzer.Analyze(ctx, changeimpact.Options{Base: "--output=outside", Package: "./...", MaxPackages: 200})
	if err == nil || !strings.Contains(err.Error(), "base") {
		t.Fatalf("Analyze error = %v, want invalid base error", err)
	}
}

func TestMaterializeBaseLeavesTargetWorktreeUntouched(t *testing.T) {
	repository := newRepository(t, map[string]string{
		"go.mod":  "module example.test/change\n\ngo 1.25.0\n",
		"main.go": "package change\n\nconst Version = 1\n",
	})
	base := git(t, repository, "rev-parse", "HEAD")
	writeFile(t, repository, "main.go", "package change\n\nconst Version = 2\n")
	before := git(t, repository, "status", "--porcelain=v1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, err := workspace.Open(ctx, repository)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	analyzer, err := changeimpact.New(ws, runner)
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	analysis, err := analyzer.Analyze(ctx, changeimpact.Options{Base: base})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	materialized, err := analyzer.MaterializeBase(ctx, analysis.Repository, t.TempDir())
	if err != nil {
		t.Fatalf("materialize base: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(materialized, "main.go"))
	if err != nil {
		t.Fatalf("read materialized source: %v", err)
	}
	if !strings.Contains(string(content), "Version = 1") {
		t.Fatalf("materialized content = %q, want base version", content)
	}
	if _, err := os.Stat(filepath.Join(materialized, ".git")); !os.IsNotExist(err) {
		t.Fatalf("materialized base contains persistent Git metadata: %v", err)
	}
	if after := git(t, repository, "status", "--porcelain=v1"); after != before {
		t.Fatalf("worktree status changed: before %q after %q", before, after)
	}
}

func analyze(t *testing.T, repository string, options changeimpact.Options) changeimpact.Analysis {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, err := workspace.Open(ctx, repository)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	analyzer, err := changeimpact.New(ws, runner)
	if err != nil {
		t.Fatalf("new analyzer: %v", err)
	}
	analysis, err := analyzer.Analyze(ctx, options)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	return analysis
}

func newRepository(t *testing.T, files map[string]string) string {
	t.Helper()
	repository := t.TempDir()
	git(t, repository, "init", "-b", "main")
	git(t, repository, "config", "user.name", "Fixture")
	git(t, repository, "config", "user.email", "fixture@example.test")
	for path, content := range files {
		writeFile(t, repository, path, content)
	}
	git(t, repository, "add", ".")
	git(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	return repository
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
