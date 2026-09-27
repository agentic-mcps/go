package sourceview

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/workspace"
)

func TestCreateDefaultsToMainAndSelectsExplicitFeatureBranch(t *testing.T) {
	repository, parent := sourceViewRepository(t)
	mainCommit := sourceViewGit(t, repository, "rev-parse", "HEAD")
	source, runner := sourceViewRunner(t, repository)

	mainPath := filepath.Join(parent, "main-view")
	mainView, err := Create(context.Background(), runner, source, Request{OutputPath: mainPath})
	if err != nil {
		t.Fatal(err)
	}
	if mainView.Branch != "main" || mainView.Ref != "refs/heads/main" || mainView.Commit != mainCommit {
		t.Fatalf("default view identity = %#v", mainView)
	}
	if got := sourceViewRead(t, mainPath, "branch.go"); !strings.Contains(got, `return "main"`) {
		t.Fatalf("default branch contents = %q", got)
	}
	if got := sourceViewGit(t, repository, "branch", "--show-current"); got != "main" {
		t.Fatalf("source checkout branch = %q, want main", got)
	}

	featurePath := filepath.Join(parent, "feature-view")
	featureView, err := Create(context.Background(), runner, source, Request{Branch: "feature", OutputPath: featurePath})
	if err != nil {
		t.Fatal(err)
	}
	if featureView.Branch != "feature" || featureView.Ref != "refs/heads/feature" || featureView.Commit == mainView.Commit {
		t.Fatalf("feature view identity = %#v", featureView)
	}
	if got := sourceViewRead(t, featurePath, "branch.go"); !strings.Contains(got, `return "feature"`) {
		t.Fatalf("feature branch contents = %q", got)
	}
	if got := sourceViewRead(t, featurePath, "branch_test.go"); !strings.Contains(got, "TestFeatureBehavior") {
		t.Fatalf("feature branch test was not checked out: %q", got)
	}
	if got := sourceViewRead(t, featurePath, "README.md"); !strings.Contains(got, "feature behavior") {
		t.Fatalf("feature branch documentation was not checked out: %q", got)
	}

	if err := os.WriteFile(filepath.Join(featurePath, "branch.go"), []byte("package fixture\n\nfunc Branch() string { return \"edited view\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sourceViewRead(t, repository, "branch.go"); !strings.Contains(got, `return "main"`) {
		t.Fatalf("editing the returned view changed source checkout: %q", got)
	}
}

func TestCreateOverlaysStagedUnstagedUntrackedAddedAndDeletedFiles(t *testing.T) {
	repository, parent := sourceViewRepository(t)
	source, runner := sourceViewRunner(t, repository)
	sourceViewWrite(t, repository, "branch.go", "package fixture\n\nfunc Branch() string { return \"staged\" }\n")
	sourceViewGit(t, repository, "add", "branch.go")
	sourceViewWrite(t, repository, "branch.go", "package fixture\n\nfunc Branch() string { return \"unstaged final\" }\n")
	sourceViewWrite(t, repository, "staged-added.go", "package fixture\n\nfunc StagedAdded() {}\n")
	sourceViewGit(t, repository, "add", "staged-added.go")
	sourceViewWrite(t, repository, "untracked.go", "package fixture\n\nfunc Untracked() {}\n")
	if err := os.Remove(filepath.Join(repository, "remove.go")); err != nil {
		t.Fatal(err)
	}

	viewPath := filepath.Join(parent, "overlay-view")
	view, err := Create(context.Background(), runner, source, Request{OutputPath: viewPath, IncludeDirty: true})
	if err != nil {
		t.Fatal(err)
	}
	if !view.OverlayIncluded || len(view.OverlayDigest) != 64 {
		t.Fatalf("overlay identity = %#v", view)
	}
	if got := sourceViewRead(t, viewPath, "branch.go"); !strings.Contains(got, `return "unstaged final"`) {
		t.Fatalf("staged and unstaged overlay = %q", got)
	}
	if got := sourceViewRead(t, viewPath, "staged-added.go"); !strings.Contains(got, "StagedAdded") {
		t.Fatalf("staged addition = %q", got)
	}
	if got := sourceViewRead(t, viewPath, "untracked.go"); !strings.Contains(got, "Untracked") {
		t.Fatalf("untracked addition = %q", got)
	}
	if _, err := os.Stat(filepath.Join(viewPath, "remove.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted path stat error = %v, want not-exist", err)
	}
	if got := sourceViewRead(t, repository, "branch.go"); !strings.Contains(got, `return "unstaged final"`) {
		t.Fatalf("source overlay changed while materializing view: %q", got)
	}
}

func TestCreateRejectsOverlayAgainstDifferentBranch(t *testing.T) {
	repository, parent := sourceViewRepository(t)
	source, runner := sourceViewRunner(t, repository)
	viewPath := filepath.Join(parent, "wrong-base-view")
	_, err := Create(context.Background(), runner, source, Request{Branch: "feature", OutputPath: viewPath, IncludeDirty: true})
	if err == nil || !strings.Contains(err.Error(), "does not match selected branch commit") {
		t.Fatalf("Create() error = %v, want exact-base rejection", err)
	}
	if _, err := os.Lstat(viewPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched overlay output stat error = %v, want not-exist", err)
	}
}

func TestValidateRejectsMovedBranchAndCorruptMetadata(t *testing.T) {
	repository, parent := sourceViewRepository(t)
	source, runner := sourceViewRunner(t, repository)
	viewPath := filepath.Join(parent, "stale-view")
	view, err := Create(context.Background(), runner, source, Request{Branch: "feature", OutputPath: viewPath})
	if err != nil {
		t.Fatal(err)
	}
	viewWorkspace, viewRunner := sourceViewRunner(t, viewPath)
	if err := Validate(context.Background(), viewRunner, view.Commit); err != nil {
		t.Fatalf("Validate() before ref movement = %v", err)
	}
	mainCommit := sourceViewGit(t, repository, "rev-parse", "refs/heads/main")
	sourceViewGit(t, repository, "update-ref", "refs/heads/feature", mainCommit)
	if err := Validate(context.Background(), viewRunner, view.Commit); !errors.Is(err, ErrStale) {
		t.Fatalf("Validate() after ref movement = %v, want ErrStale", err)
	}

	gitDir := sourceViewGit(t, viewWorkspace.Root(), "rev-parse", "--absolute-git-dir")
	configPath := filepath.Join(gitDir, "config.worktree")
	sourceViewGit(t, viewPath, "config", "--file", configPath, "--replace-all", metadataConfigKey, "not-base64")
	if err := Validate(context.Background(), viewRunner, view.Commit); err == nil || !strings.Contains(err.Error(), "branch source view metadata") {
		t.Fatalf("Validate() with corrupt metadata = %v, want metadata error", err)
	}
	sourceViewGit(t, viewPath, "config", "--file", configPath, "--unset-all", metadataConfigKey)
	if err := Validate(context.Background(), viewRunner, view.Commit); err == nil || !strings.Contains(err.Error(), "branch source view marker is missing") {
		t.Fatalf("Validate() with missing metadata = %v, want fail-closed marker error", err)
	}
}

func TestCreateCleansPartialWorktreeWhenSelectedBranchIsNotGoWorkspace(t *testing.T) {
	repository, parent := sourceViewRepository(t)
	source, runner := sourceViewRunner(t, repository)
	sourceViewGit(t, repository, "checkout", "-b", "no-go-workspace")
	if err := os.Remove(filepath.Join(repository, "go.mod")); err != nil {
		t.Fatal(err)
	}
	sourceViewGit(t, repository, "add", "go.mod")
	sourceViewGit(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "remove Go workspace")
	sourceViewGit(t, repository, "checkout", "main")
	viewPath := filepath.Join(parent, "invalid-view")
	_, err := Create(context.Background(), runner, source, Request{Branch: "no-go-workspace", OutputPath: viewPath})
	if err == nil || !strings.Contains(err.Error(), "validating selected branch workspace") {
		t.Fatalf("Create() error = %v, want workspace validation error", err)
	}
	if _, err := os.Lstat(viewPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial view stat error = %v, want not-exist", err)
	}
	worktrees := sourceViewGit(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(worktrees, viewPath) {
		t.Fatalf("failed source view remains registered:\n%s", worktrees)
	}
}

func TestCreateRequiresNewOutputOutsideSourceRepository(t *testing.T) {
	repository, _ := sourceViewRepository(t)
	source, runner := sourceViewRunner(t, repository)
	for name, output := range map[string]string{
		"inside repository": filepath.Join(repository, "nested-view"),
		"existing path":    repository,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Create(context.Background(), runner, source, Request{OutputPath: output}); err == nil {
				t.Fatal("Create() succeeded for unsafe output path")
			}
		})
	}
}

func sourceViewRepository(t *testing.T) (string, string) {
	t.Helper()
	parent := t.TempDir()
	repository := filepath.Join(parent, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceViewGit(t, repository, "init", "-b", "main")
	sourceViewGit(t, repository, "config", "user.name", "Fixture")
	sourceViewGit(t, repository, "config", "user.email", "fixture@example.test")
	sourceViewWrite(t, repository, "go.mod", "module example.test/sourceview\n\ngo 1.25.0\n")
	sourceViewWrite(t, repository, "branch.go", "package fixture\n\nfunc Branch() string { return \"main\" }\n")
	sourceViewWrite(t, repository, "remove.go", "package fixture\n\nfunc RemoveMe() {}\n")
	sourceViewWrite(t, repository, "README.md", "main behavior\n")
	sourceViewGit(t, repository, "add", ".")
	sourceViewGit(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "main source")
	sourceViewGit(t, repository, "checkout", "-b", "feature")
	sourceViewWrite(t, repository, "branch.go", "package fixture\n\nfunc Branch() string { return \"feature\" }\n")
	sourceViewWrite(t, repository, "branch_test.go", "package fixture\n\nfunc TestFeatureBehavior() {}\n")
	sourceViewWrite(t, repository, "README.md", "feature behavior\n")
	sourceViewGit(t, repository, "add", ".")
	sourceViewGit(t, repository, "-c", "commit.gpgsign=false", "commit", "-m", "feature source")
	sourceViewGit(t, repository, "checkout", "main")
	return repository, parent
}

func sourceViewRunner(t *testing.T, root string) (*workspace.Workspace, *execution.Runner) {
	t.Helper()
	ws, err := workspace.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return ws, runner
}

func sourceViewRead(t *testing.T, root, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func sourceViewWrite(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sourceViewGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
