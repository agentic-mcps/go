package gate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/execution"
	"github.com/agentic-mcps/go/internal/workspace"
)

// baseFixture is a throwaway git repository used by the base and state tests.
type baseFixture struct {
	t    *testing.T
	root string
}

func newBaseFixture(t *testing.T) *baseFixture {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	f := &baseFixture{t: t, root: root}
	f.git("init", "-q", "-b", "main")
	f.git("config", "user.name", "Test")
	f.git("config", "user.email", "test@example.com")
	f.git("config", "commit.gpgsign", "false")
	return f
}

func (f *baseFixture) git(args ...string) string {
	f.t.Helper()
	return f.gitIn(f.root, args...)
}

func (f *baseFixture) gitIn(dir string, args ...string) string {
	f.t.Helper()
	out, err := baseRunGit(context.Background(), dir, args...)
	if err != nil {
		f.t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func baseRunGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (f *baseFixture) write(rel, content string) {
	f.t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *baseFixture) remove(rel string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(rel))); err != nil {
		f.t.Fatal(err)
	}
}

// commit stages everything and commits, returning the new HEAD.
func (f *baseFixture) commit(message string) string {
	f.t.Helper()
	f.git("add", "-A")
	f.git("commit", "-q", "-m", message)
	return f.git("rev-parse", "HEAD")
}

// remote fakes a remote-tracking ref at sha.
func (f *baseFixture) remote(name, sha string) {
	f.t.Helper()
	f.git("update-ref", "refs/remotes/origin/"+name, sha)
}

// gitFunc returns a GitFunc running git in dir with a hermetic environment.
func (f *baseFixture) gitFunc(dir string) GitFunc {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		return baseRunGit(ctx, dir, args...)
	}
}

func noEnv(string) string { return "" }

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestNewGitRunsGitInWorkspaceRoot(t *testing.T) {
	f := newBaseFixture(t)
	f.write("go.mod", "module example.com/fixture\n\ngo 1.25\n")
	f.write("main.go", "package main\n\nfunc main() {}\n")
	head := f.commit("init")

	ws, err := workspace.Open(context.Background(), f.root)
	if err != nil {
		t.Skipf("workspace.Open unavailable in this environment: %v", err)
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		t.Fatal(err)
	}
	git := NewGit(runner, ws.Root())

	out, err := git(context.Background(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != head {
		t.Fatalf("HEAD = %q, want %q", got, head)
	}

	if _, err = git(context.Background(), "rev-parse", "--verify", "--quiet", "refs/heads/nope^{commit}"); err == nil {
		t.Fatal("expected error for non-zero git exit")
	}
	_, err = git(context.Background(), "cat-file", "-p", "deadbeef")
	if err == nil || strings.Contains(err.Error(), "exited with status") {
		t.Fatalf("expected git stderr in error, got %v", err)
	}
}

type baseRepo struct {
	f       *baseFixture
	first   string
	second  string
	feature string
}

// newBaseRepo builds main@first with origin/main (and origin/HEAD) at first and
// a checked-out feature branch with one extra commit.
func newBaseRepo(t *testing.T) *baseRepo {
	t.Helper()
	f := newBaseFixture(t)
	f.write("a.go", "package a\n")
	first := f.commit("first")
	f.remote("main", first)
	f.git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	f.git("checkout", "-q", "-b", "feature")
	f.write("b.go", "package a\n")
	feature := f.commit("feature work")
	return &baseRepo{f: f, first: first, feature: feature}
}

func TestDetectBase(t *testing.T) {
	t.Parallel()

	dropRemote := func(r *baseRepo) {
		r.f.git("symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
		r.f.git("update-ref", "-d", "refs/remotes/origin/main")
	}
	originHead := func(r *baseRepo) Base {
		return Base{Ref: "origin/HEAD", Commit: r.first, Source: "origin/HEAD"}
	}

	tests := []struct {
		name        string
		prepare     func(r *baseRepo)
		explicit    string
		sessionBase func(r *baseRepo) string
		env         map[string]string
		want        func(r *baseRepo) Base
		wantErr     string
	}{
		{
			name:     "explicit ref wins and reports flag",
			explicit: "origin/main",
			want:     func(r *baseRepo) Base { return Base{Ref: "origin/main", Commit: r.first, Source: "flag"} },
		},
		{
			name:     "explicit unresolvable is an error with no fallback",
			explicit: "no-such-ref",
			wantErr:  "no-such-ref",
		},
		{
			name:        "explicit beats session base",
			explicit:    "origin/main",
			sessionBase: func(r *baseRepo) string { return r.feature },
			want:        func(r *baseRepo) Base { return Base{Ref: "origin/main", Commit: r.first, Source: "flag"} },
		},
		{
			name:        "session base that is an ancestor of HEAD",
			sessionBase: func(r *baseRepo) string { return r.first },
			want:        func(r *baseRepo) Base { return Base{Ref: r.first, Commit: r.first, Source: "session"} },
		},
		{
			name: "session base not an ancestor falls through to origin/HEAD",
			prepare: func(r *baseRepo) {
				r.f.git("checkout", "-q", "-b", "other", r.first)
				r.f.write("other.go", "package a\n")
				r.second = r.f.commit("other branch")
				r.f.git("checkout", "-q", "feature")
			},
			sessionBase: func(r *baseRepo) string { return r.second },
			want:        originHead,
		},
		{
			name:        "session base that does not exist falls through",
			sessionBase: func(*baseRepo) string { return strings.Repeat("a", 40) },
			want:        originHead,
		},
		{
			name: "GITHUB_BASE_REF prefers origin/<ref>",
			prepare: func(r *baseRepo) {
				r.f.git("update-ref", "refs/remotes/origin/release", r.first)
				r.f.git("branch", "release", r.feature)
			},
			env:  map[string]string{"GITHUB_BASE_REF": "release"},
			want: func(r *baseRepo) Base { return Base{Ref: "origin/release", Commit: r.first, Source: "github"} },
		},
		{
			name:    "GITHUB_BASE_REF falls back to local branch",
			prepare: func(r *baseRepo) { r.f.git("branch", "release", r.first) },
			env:     map[string]string{"GITHUB_BASE_REF": "release"},
			want:    func(r *baseRepo) Base { return Base{Ref: "release", Commit: r.first, Source: "github"} },
		},
		{
			name: "GITHUB_BASE_REF unresolvable falls through to origin/HEAD",
			env:  map[string]string{"GITHUB_BASE_REF": "ghost"},
			want: originHead,
		},
		{
			name: "feature branch uses merge-base not the moved tip of origin/main",
			prepare: func(r *baseRepo) {
				r.f.git("checkout", "-q", "main")
				r.f.write("main-only.go", "package a\n")
				r.second = r.f.commit("main moves on")
				r.f.remote("main", r.second)
				r.f.git("checkout", "-q", "feature")
			},
			want: originHead,
		},
		{
			name:    "origin/main when origin/HEAD is not set",
			prepare: func(r *baseRepo) { r.f.git("symbolic-ref", "--delete", "refs/remotes/origin/HEAD") },
			want:    func(r *baseRepo) Base { return Base{Ref: "origin/main", Commit: r.first, Source: "origin/main"} },
		},
		{
			name: "origin/master when only it exists",
			prepare: func(r *baseRepo) {
				dropRemote(r)
				r.f.git("update-ref", "refs/remotes/origin/master", r.first)
			},
			want: func(r *baseRepo) Base { return Base{Ref: "origin/master", Commit: r.first, Source: "origin/master"} },
		},
		{
			name:    "local main when there is no remote",
			prepare: dropRemote,
			want:    func(r *baseRepo) Base { return Base{Ref: "main", Commit: r.first, Source: "main"} },
		},
		{
			name: "local master when there is no remote and no main",
			prepare: func(r *baseRepo) {
				dropRemote(r)
				r.f.git("branch", "-m", "main", "master")
			},
			want: func(r *baseRepo) Base { return Base{Ref: "master", Commit: r.first, Source: "master"} },
		},
		{
			name: "candidate with unrelated history is skipped",
			prepare: func(r *baseRepo) {
				dropRemote(r)
				r.f.git("checkout", "-q", "--orphan", "unrelated")
				r.f.git("rm", "-rfq", ".")
				r.f.write("z.txt", "z\n")
				r.f.git("add", "-A")
				r.f.git("commit", "-q", "-m", "orphan")
				orphan := r.f.git("rev-parse", "HEAD")
				r.f.git("checkout", "-q", "feature")
				r.f.git("update-ref", "refs/remotes/origin/master", orphan)
				r.f.git("branch", "master", r.first)
				r.f.git("branch", "-D", "unrelated")
				// origin/master now has no common ancestor with HEAD; main does.
			},
			want: func(r *baseRepo) Base { return Base{Ref: "main", Commit: r.first, Source: "main"} },
		},
		{
			name: "no candidates falls back to HEAD",
			prepare: func(r *baseRepo) {
				dropRemote(r)
				r.f.git("branch", "-m", "main", "trunk")
			},
			want: func(r *baseRepo) Base { return Base{Ref: "HEAD", Commit: r.feature, Source: "head"} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newBaseRepo(t)
			if tt.prepare != nil {
				tt.prepare(r)
			}
			session := ""
			if tt.sessionBase != nil {
				session = tt.sessionBase(r)
			}
			got, err := DetectBase(context.Background(), r.f.gitFunc(r.f.root), tt.explicit, session, envOf(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectBase: %v", err)
			}
			if want := tt.want(r); got != want {
				t.Fatalf("base = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDetectBaseOnDefaultBranchWithRemoteAtHead(t *testing.T) {
	t.Parallel()
	f := newBaseFixture(t)
	f.write("a.go", "package a\n")
	head := f.commit("first")
	f.remote("main", head)
	f.git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	got, err := DetectBase(context.Background(), f.gitFunc(f.root), "", "", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Base{Ref: "origin/HEAD", Commit: head, Source: "origin/HEAD"}); got != want {
		t.Fatalf("base = %+v, want %+v", got, want)
	}
}

func TestDetectBaseNoCommits(t *testing.T) {
	t.Parallel()
	f := newBaseFixture(t)
	for _, explicit := range []string{"", "main"} {
		_, err := DetectBase(context.Background(), f.gitFunc(f.root), explicit, "", noEnv)
		if err == nil || !strings.Contains(err.Error(), "repository has no commits") {
			t.Fatalf("explicit=%q err = %v, want no-commits error", explicit, err)
		}
	}
}

func TestDetectBaseShallowFallback(t *testing.T) {
	t.Parallel()
	f := newBaseFixture(t)
	f.write("a.go", "package a\n")
	f.commit("first")
	f.write("b.go", "package a\n")
	f.commit("second")

	clone := t.TempDir()
	f.gitIn(clone, "clone", "-q", "--depth", "1", "file://"+f.root, "work")
	work := filepath.Join(clone, "work")
	// Drop remote refs and rename the branch so no candidate resolves.
	f.gitIn(work, "remote", "remove", "origin")
	f.gitIn(work, "branch", "-m", "main", "trunk")

	got, err := DetectBase(context.Background(), f.gitFunc(work), "", "", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	head := f.gitIn(work, "rev-parse", "HEAD")
	want := Base{Ref: "HEAD", Commit: head, Source: "head (shallow clone; pass --base)"}
	if got != want {
		t.Fatalf("base = %+v, want %+v", got, want)
	}
}

func TestChangedFiles(t *testing.T) {
	t.Parallel()

	f := newBaseFixture(t)
	f.write("keep.go", "package a\n")
	f.write("edit.go", "package a\n")
	f.write("staged.go", "package a\n")
	f.write("gone.go", "package a\n")
	f.write("notes.txt", "x\n")
	f.write(".gitignore", "ignored.go\n")
	base := f.commit("base")

	f.write("edit.go", "package a // edited\n")
	f.write("staged.go", "package a // staged\n")
	f.git("add", "staged.go")
	f.remove("gone.go")
	f.write("new_untracked.go", "package a\n")
	f.write("pkg/deep/new.go", "package deep\n")
	f.write("readme.md", "hello\n")
	f.write("ignored.go", "package a\n")
	f.git("mv", "notes.txt", "renamed.txt")

	ctx := context.Background()
	git := f.gitFunc(f.root)

	tests := []struct {
		name string
		call func() ([]string, error)
		want []string
	}{
		{
			name: "all files without deleted",
			call: func() ([]string, error) { return ChangedFiles(ctx, git, base, false) },
			want: []string{"edit.go", "new_untracked.go", "pkg/deep/new.go", "readme.md", "renamed.txt", "staged.go"},
		},
		{
			name: "all files with deleted",
			call: func() ([]string, error) { return ChangedFiles(ctx, git, base, true) },
			want: []string{"edit.go", "gone.go", "new_untracked.go", "notes.txt", "pkg/deep/new.go", "readme.md", "renamed.txt", "staged.go"},
		},
		{
			name: "go files only without deleted",
			call: func() ([]string, error) { return ChangedGoFiles(ctx, git, base, false) },
			want: []string{"edit.go", "new_untracked.go", "pkg/deep/new.go", "staged.go"},
		},
		{
			name: "go files only with deleted",
			call: func() ([]string, error) { return ChangedGoFiles(ctx, git, base, true) },
			want: []string{"edit.go", "gone.go", "new_untracked.go", "pkg/deep/new.go", "staged.go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.call()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("files = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangedFilesCleanTreeIsNonNilEmpty(t *testing.T) {
	t.Parallel()
	f := newBaseFixture(t)
	f.write("a.go", "package a\n")
	base := f.commit("base")

	for name, fn := range map[string]func(context.Context, GitFunc, string, bool) ([]string, error){
		"all": ChangedFiles,
		"go":  ChangedGoFiles,
	} {
		got, err := fn(context.Background(), f.gitFunc(f.root), base, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("%s: got %#v, want non-nil empty", name, got)
		}
	}
}

func TestChangedFilesDeduplicatesAndSortsAcrossSources(t *testing.T) {
	t.Parallel()
	git := GitFunc(func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "diff" {
			return []byte("b.go\x00a.go\x00"), nil
		}
		return []byte("a.go\x00c.go\x00"), nil
	})
	got, err := ChangedFiles(context.Background(), git, "abc", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go", "b.go", "c.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestChangedFilesSubdirectoryWorkspace(t *testing.T) {
	t.Parallel()

	f := newBaseFixture(t)
	f.write("top.go", "package a\n")
	f.write("svc/main.go", "package main\n")
	f.write("svc/internal/x.go", "package internal\n")
	base := f.commit("base")

	f.write("top.go", "package a // changed outside the workspace\n")
	f.write("svc/main.go", "package main // changed\n")
	f.write("svc/fresh.go", "package main\n")
	f.write("other/fresh.go", "package other\n")
	f.remove("svc/internal/x.go")

	git := f.gitFunc(filepath.Join(f.root, "svc"))
	got, err := ChangedFiles(context.Background(), git, base, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fresh.go", "internal/x.go", "main.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestChangedFilesPropagatesGitErrors(t *testing.T) {
	t.Parallel()
	f := newBaseFixture(t)
	f.write("a.go", "package a\n")
	f.commit("base")
	if _, err := ChangedFiles(context.Background(), f.gitFunc(f.root), "not-a-commit", false); err == nil {
		t.Fatal("expected error for bad base commit")
	}
}

func TestDetectBaseRejectsOptionLikeRefs(t *testing.T) {
	t.Parallel()
	r := newBaseRepo(t)
	runGit := r.f.gitFunc(r.f.root)
	originHead := Base{Ref: "origin/HEAD", Commit: r.first, Source: "origin/HEAD"}

	for _, ref := range []string{"--all", "-x", "--end-of-options"} {
		_, err := DetectBase(context.Background(), runGit, ref, "", noEnv)
		if err == nil || !strings.Contains(err.Error(), "must not start with '-'") {
			t.Errorf("explicit %q: err = %v, want option-like ref error", ref, err)
		}

		got, err := DetectBase(context.Background(), runGit, "", ref, noEnv)
		if err != nil || got != originHead {
			t.Errorf("session %q: got %+v, %v; want fall through to %+v", ref, got, err, originHead)
		}

		got, err = DetectBase(context.Background(), runGit, "", "", envOf(map[string]string{"GITHUB_BASE_REF": ref}))
		if err != nil || got != originHead {
			t.Errorf("GITHUB_BASE_REF %q: got %+v, %v; want fall through to %+v", ref, got, err, originHead)
		}

		if _, err = ChangedFiles(context.Background(), runGit, ref, false); err == nil {
			t.Errorf("ChangedFiles base %q: want error", ref)
		}
	}

	// Near miss: a dash inside the ref is fine.
	r.f.git("branch", "feature-x", r.first)
	got, err := DetectBase(context.Background(), runGit, "feature-x", "", noEnv)
	if err != nil || got.Commit != r.first {
		t.Fatalf("feature-x: got %+v, %v", got, err)
	}
}
