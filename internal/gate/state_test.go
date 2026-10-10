package gate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFingerprint(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*baseFixture, GitFunc, string) {
		f := newBaseFixture(t)
		f.write("a.go", "package a\n")
		f.write("b.go", "package a\n")
		base := f.commit("base")
		return f, f.gitFunc(f.root), base
	}
	fingerprint := func(t *testing.T, f *baseFixture, git GitFunc, base, salt string) string {
		t.Helper()
		fp, err := Fingerprint(context.Background(), git, f.root, base, salt)
		if err != nil {
			t.Fatalf("Fingerprint: %v", err)
		}
		return fp
	}

	t.Run("stable across calls", func(t *testing.T) {
		t.Parallel()
		f, git, base := setup(t)
		f.write("a.go", "package a // dirty\n")
		f.write("new.go", "package a\n")
		first := fingerprint(t, f, git, base, "v1")
		if len(first) != 64 {
			t.Fatalf("fingerprint %q is not 64 hex chars", first)
		}
		if second := fingerprint(t, f, git, base, "v1"); second != first {
			t.Fatalf("fingerprint changed between identical calls: %s vs %s", first, second)
		}
	})

	tests := []struct {
		mutate func(f *baseFixture) (base, salt string)
		name   string
	}{
		{
			name: "content edit of a tracked file",
			mutate: func(f *baseFixture) (string, string) {
				f.write("a.go", "package a // edited\n")
				return "", "v1"
			},
		},
		{
			name: "new untracked file",
			mutate: func(f *baseFixture) (string, string) {
				f.write("untracked.go", "package a\n")
				return "", "v1"
			},
		},
		{
			name: "deleted file",
			mutate: func(f *baseFixture) (string, string) {
				f.remove("b.go")
				return "", "v1"
			},
		},
		{
			name: "HEAD moves with a clean tree",
			mutate: func(f *baseFixture) (string, string) {
				f.git("commit", "-q", "--allow-empty", "-m", "empty")
				return "", "v1"
			},
		},
		{
			name: "salt changes",
			mutate: func(*baseFixture) (string, string) {
				return "", "v2"
			},
		},
		{
			name: "base commit changes",
			mutate: func(_ *baseFixture) (string, string) {
				return strings.Repeat("0", 40), "v1"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name+" changes the fingerprint", func(t *testing.T) {
			t.Parallel()
			f, git, base := setup(t)
			before := fingerprint(t, f, git, base, "v1")
			newBase, salt := tt.mutate(f)
			if newBase == "" {
				newBase = base
			}
			after := fingerprint(t, f, git, newBase, salt)
			if before == after {
				t.Fatalf("fingerprint did not change: %s", before)
			}
		})
	}

	t.Run("second edit of an already dirty file has identical status but new fingerprint", func(t *testing.T) {
		t.Parallel()
		f, git, base := setup(t)
		f.write("a.go", "package a // edited\n")
		statusBefore := f.git("status", "--porcelain")
		before := fingerprint(t, f, git, base, "v1")
		f.write("a.go", "package a // edited again\n")
		if statusAfter := f.git("status", "--porcelain"); statusAfter != statusBefore {
			t.Fatalf("test premise broken: status changed %q -> %q", statusBefore, statusAfter)
		}
		if after := fingerprint(t, f, git, base, "v1"); after == before {
			t.Fatal("content-only change did not change fingerprint")
		}
	})

	t.Run("near miss: touching mtime without changing content keeps fingerprint", func(t *testing.T) {
		t.Parallel()
		f, git, base := setup(t)
		f.write("a.go", "package a // edited\n")
		before := fingerprint(t, f, git, base, "v1")
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(filepath.Join(f.root, "a.go"), future, future); err != nil {
			t.Fatal(err)
		}
		if after := fingerprint(t, f, git, base, "v1"); after != before {
			t.Fatalf("fingerprint changed on mtime-only touch: %s vs %s", before, after)
		}
	})

	t.Run("near miss: gitignored file keeps fingerprint", func(t *testing.T) {
		t.Parallel()
		f := newBaseFixture(t)
		f.write(".gitignore", "*.log\n")
		f.write("a.go", "package a\n")
		base := f.commit("base")
		git := f.gitFunc(f.root)
		before := fingerprint(t, f, git, base, "v1")
		f.write("debug.log", "noise\n")
		if after := fingerprint(t, f, git, base, "v1"); after != before {
			t.Fatalf("ignored file changed fingerprint: %s vs %s", before, after)
		}
	})

	t.Run("length prefixing separates salt from base commit", func(t *testing.T) {
		t.Parallel()
		f, git, _ := setup(t)
		a := fingerprint(t, f, git, "bc", "a")
		b := fingerprint(t, f, git, "c", "ab")
		if a == b {
			t.Fatal("fingerprints collide when field boundaries move")
		}
	})

	t.Run("rename and untracked files in subdirectories", func(t *testing.T) {
		t.Parallel()
		f, git, base := setup(t)
		f.git("mv", "a.go", "renamed.go")
		f.write("pkg/deep/x.go", "package deep\n")
		before := fingerprint(t, f, git, base, "v1")
		f.write("renamed.go", "package a // new content\n")
		if after := fingerprint(t, f, git, base, "v1"); after == before {
			t.Fatal("content edit of renamed file did not change fingerprint")
		}
		f.write("pkg/deep/x.go", "package deep // changed\n")
		mid := fingerprint(t, f, git, base, "v1")
		if mid == before {
			t.Fatal("fingerprint unchanged after edits")
		}
	})

	t.Run("workspace in a subdirectory of the repository", func(t *testing.T) {
		t.Parallel()
		f := newBaseFixture(t)
		f.write("svc/main.go", "package main\n")
		base := f.commit("base")
		sub := filepath.Join(f.root, "svc")
		git := f.gitFunc(sub)
		before, err := Fingerprint(context.Background(), git, sub, base, "v1")
		if err != nil {
			t.Fatal(err)
		}
		f.write("svc/main.go", "package main // edited\n")
		after, err := Fingerprint(context.Background(), git, sub, base, "v1")
		if err != nil {
			t.Fatal(err)
		}
		if before == after {
			t.Fatal("edit under a subdirectory workspace did not change fingerprint")
		}
	})

	t.Run("git failure is an error", func(t *testing.T) {
		t.Parallel()
		f := newBaseFixture(t) // no commits: rev-parse HEAD fails
		if _, err := Fingerprint(context.Background(), f.gitFunc(f.root), f.root, "x", "v1"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFingerprintRejectsPathsEscapingRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tests := []struct {
		name   string
		status string
	}{
		{name: "parent traversal", status: " M ../../etc/passwd\x00"},
		{name: "absolute path", status: "?? /etc/passwd\x00"},
		{name: "sneaky traversal", status: " M a/../../b\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			git := GitFunc(func(_ context.Context, args ...string) ([]byte, error) {
				switch args[0] {
				case "status":
					return []byte(tt.status), nil
				case "rev-parse":
					if args[1] == "--show-prefix" {
						return []byte("\n"), nil
					}
					return []byte("abc123\n"), nil
				}
				return nil, fmt.Errorf("unexpected git %v", args)
			})
			_, err := Fingerprint(context.Background(), git, root, "base", "v1")
			if err == nil || !strings.Contains(err.Error(), "escapes") {
				t.Fatalf("err = %v, want path escape error", err)
			}
		})
	}
}

func TestStateParseStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: []string{}},
		{name: "modified and untracked", raw: " M a.go\x00?? b.go\x00", want: []string{"a.go", "b.go"}},
		{name: "rename skips original path", raw: "R  new.go\x00old.go\x00 M c.go\x00", want: []string{"new.go", "c.go"}},
		{name: "copy skips original path", raw: "C  copy.go\x00src.go\x00", want: []string{"copy.go"}},
		{name: "worktree rename skips original path", raw: " R new.go\x00old.go\x00", want: []string{"new.go"}},
		{name: "near miss: modified entry does not consume next field", raw: "M  a.go\x00M  b.go\x00", want: []string{"a.go", "b.go"}},
		{name: "path with spaces", raw: "?? my file.go\x00", want: []string{"my file.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stateStatusPaths([]byte(tt.raw))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("paths = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func sampleResult(verdict Verdict, marker string) Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Verdict:       verdict,
		Base:          Base{Ref: "origin/main", Commit: "abc", Source: "origin/main"},
		Items: []Item{
			{Severity: SeverityBlock, Code: CodeSyntax, File: "a.go", Line: 3, Message: marker, Fix: "fix it"},
		},
		Notes: []string{"note"},
		Stats: Stats{ChangedFiles: 1, ChangedGoFiles: 1, DurationMS: 12},
	}
}

func TestStoreResultRoundtrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "store")
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("store dir perm = %o, want 700", perm)
	}

	if _, ok := store.LoadResult("deadbeef"); ok {
		t.Fatal("unexpected hit in empty store")
	}
	want := sampleResult(VerdictBlock, "first")
	if err = store.SaveResult("deadbeef", want); err != nil {
		t.Fatal(err)
	}
	got, ok := store.LoadResult("deadbeef")
	if !ok {
		t.Fatal("expected hit after save")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roundtrip = %+v, want %+v", got, want)
	}
	info, err = os.Stat(filepath.Join(dir, "results", "deadbeef.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("result perm = %o, want 600", perm)
	}

	// Overwrite replaces.
	if err := store.SaveResult("deadbeef", sampleResult(VerdictPass, "second")); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.LoadResult("deadbeef"); got.Items[0].Message != "second" {
		t.Fatalf("overwrite not visible: %+v", got)
	}

	// Different fingerprint is a miss (near miss).
	if _, ok := store.LoadResult("deadbee"); ok {
		t.Fatal("different fingerprint must miss")
	}
}

func TestStoreResultRejectsUnsafeFingerprints(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, fp := range []string{"", "../escape", "a/b", strings.Repeat("a", 200)} {
		if err := store.SaveResult(fp, sampleResult(VerdictPass, "x")); err == nil {
			t.Errorf("SaveResult(%q) succeeded, want error", fp)
		}
		if _, ok := store.LoadResult(fp); ok {
			t.Errorf("LoadResult(%q) hit, want miss", fp)
		}
	}
}

func TestStoreResultPrunesToNewest20(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	const total = 25
	base := time.Now().Add(-time.Hour)
	for i := range total {
		fp := fmt.Sprintf("fp%02d", i)
		if err = store.SaveResult(fp, sampleResult(VerdictPass, fp)); err != nil {
			t.Fatal(err)
		}
		// Age earlier saves deterministically; the latest save keeps "now".
		past := base.Add(time.Duration(i) * time.Second)
		if err = os.Chtimes(filepath.Join(dir, "results", fp+".json"), past, past); err != nil {
			t.Fatal(err)
		}
	}
	// One more save, newest of all.
	if err = store.SaveResult("newest", sampleResult(VerdictPass, "newest")); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "results"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("%d result files kept, want 20", len(entries))
	}
	if _, ok := store.LoadResult("newest"); !ok {
		t.Fatal("newest result was pruned")
	}
	if _, ok := store.LoadResult("fp24"); !ok {
		t.Fatal("fp24 (second newest) was pruned")
	}
	if _, ok := store.LoadResult("fp00"); ok {
		t.Fatal("fp00 (oldest) should have been pruned")
	}
}

func TestStoreCorruptFilesAreMisses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveResult("good", sampleResult(VerdictPass, "x")); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"empty": "", "garbage": "{not json", "truncated": `{"verdict":"pa`} {
		if err = os.WriteFile(filepath.Join(dir, "results", name+".json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := store.LoadResult(name); ok {
			t.Errorf("corrupt result %q should be a miss", name)
		}
	}

	if err = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "sessions", "broken.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := store.LoadSession("broken")
	if err != nil {
		t.Fatalf("corrupt session must not be an error: %v", err)
	}
	if want := (Session{ID: "broken"}); !reflect.DeepEqual(session, want) {
		t.Fatalf("session = %+v, want fresh %+v", session, want)
	}
}

func TestStoreSessionRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	fresh, err := store.LoadSession("abc-123_XYZ")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Session{ID: "abc-123_XYZ"}); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("missing session = %+v, want %+v", fresh, want)
	}

	want := Session{
		ID:                     "abc-123_XYZ",
		StartHead:              "deadbeef",
		LastBlockedFingerprint: "fp1",
		UpdatedAt:              time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		Blocks:                 2,
	}
	if err = store.SaveSession(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSession("abc-123_XYZ")
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
	got.UpdatedAt = want.UpdatedAt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("session = %+v, want %+v", got, want)
	}
	info, err := os.Stat(filepath.Join(dir, "sessions", "abc-123_XYZ.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("session perm = %o, want 600", perm)
	}
}

func TestStoreSessionUnsafeIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"../../etc/passwd", "has space", "a/b", "", strings.Repeat("x", 129), "unié"}
	for _, id := range ids {
		if err = store.SaveSession(Session{ID: id, Blocks: 3}); err != nil {
			t.Fatalf("SaveSession(%q): %v", id, err)
		}
		got, loadErr := store.LoadSession(id)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if got.ID != id || got.Blocks != 3 {
			t.Fatalf("session %q roundtrip = %+v", id, got)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(ids) {
		t.Fatalf("%d session files for %d distinct unsafe ids", len(entries), len(ids))
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")
		if !stateSafeIDPattern.MatchString(name) {
			t.Errorf("unsafe file name %q", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "etc")); err == nil {
		t.Fatal("traversal wrote outside the store")
	}
	// A safe id keeps its own name (near miss for hashing).
	if err := store.SaveSession(Session{ID: "plain-id"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "plain-id.json")); err != nil {
		t.Fatalf("safe id should be used verbatim: %v", err)
	}
}

func TestStoreSessionPrunesOldFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(Session{ID: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(Session{ID: "recent"}); err != nil {
		t.Fatal(err)
	}
	chtimes := func(id string, age time.Duration) {
		t.Helper()
		when := time.Now().Add(-age)
		if err := os.Chtimes(filepath.Join(dir, "sessions", id+".json"), when, when); err != nil {
			t.Fatal(err)
		}
	}
	chtimes("old", 8*24*time.Hour)
	chtimes("recent", 6*24*time.Hour)

	if err := store.SaveSession(Session{ID: "current"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "old.json")); !os.IsNotExist(err) {
		t.Fatalf("8-day-old session should be pruned, stat err = %v", err)
	}
	for _, id := range []string{"recent", "current"} {
		if _, err := os.Stat(filepath.Join(dir, "sessions", id+".json")); err != nil {
			t.Fatalf("session %q should be kept: %v", id, err)
		}
	}
}

func TestStoreLock(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	unlock, err := store.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func(), 1)
	go func() {
		second, lockErr := store.Lock(ctx)
		if lockErr != nil {
			t.Errorf("second Lock: %v", lockErr)
			acquired <- func() {}
			return
		}
		acquired <- second
	}()

	select {
	case <-acquired:
		t.Fatal("second locker acquired while first holds the lock")
	case <-time.After(300 * time.Millisecond):
	}

	unlock()
	unlock() // idempotent

	select {
	case second := <-acquired:
		second()
	case <-time.After(5 * time.Second):
		t.Fatal("second locker never acquired after unlock")
	}

	// Lock is reusable after full release.
	again, err := store.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestStoreLockRespectsContext(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := store.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := store.Lock(ctx); err == nil {
		t.Fatal("expected context error while lock is held")
	} else if ctx.Err() == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Lock took %v to notice cancellation", elapsed)
	}

	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := store.Lock(cancelled); err == nil {
		t.Fatal("expected error for already-cancelled context while held")
	}
}

func TestOpenStore(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)

	rootA := t.TempDir()
	rootB := t.TempDir()
	a1, err := OpenStore(rootA)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := OpenStore(rootA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenStore(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if a1.dir != a2.dir {
		t.Fatalf("same root gave different dirs: %s vs %s", a1.dir, a2.dir)
	}
	if a1.dir == b.dir {
		t.Fatal("different roots share a store dir")
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a1.dir, filepath.Join(userCache, "agentic-go", "gate")+string(filepath.Separator)) {
		t.Fatalf("store dir %s is not under the user cache dir", a1.dir)
	}
	if name := filepath.Base(a1.dir); len(name) != 16 {
		t.Fatalf("store dir name %q, want 16 hex chars", name)
	}
	info, err := os.Stat(a1.dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm = %o, want 700", perm)
	}
}

func TestFingerprintIsScopedToSubdirectoryWorkspace(t *testing.T) {
	t.Parallel()
	f := newBaseFixture(t)
	f.write("svc/main.go", "package main\n")
	f.write("other/lib.go", "package other\n")
	f.write("top.go", "package top\n")
	base := f.commit("base")
	sub := filepath.Join(f.root, "svc")
	runGit := f.gitFunc(sub)
	fingerprint := func() string {
		t.Helper()
		// The trailing separator also covers an unclean workspace root.
		fp, err := Fingerprint(context.Background(), runGit, sub+string(filepath.Separator), base, "v1")
		if err != nil {
			t.Fatal(err)
		}
		return fp
	}

	clean := fingerprint()
	f.write("other/lib.go", "package other // sibling edit\n")
	f.write("other/new.go", "package other\n")
	f.write("top.go", "package top // root edit\n")
	if got := fingerprint(); got != clean {
		t.Fatal("edits outside the workspace changed the fingerprint")
	}

	f.write("svc/main.go", "package main // inside edit\n")
	inside := fingerprint()
	if inside == clean {
		t.Fatal("edit inside the workspace did not change the fingerprint")
	}
	f.write("svc/main.go", "package main // inside edit two\n")
	if got := fingerprint(); got == inside {
		t.Fatal("second content edit inside the workspace did not change the fingerprint")
	}
}

func TestFingerprintRejectsStatusPathsOutsideWorkspacePrefix(t *testing.T) {
	t.Parallel()
	runGit := GitFunc(func(_ context.Context, args ...string) ([]byte, error) {
		switch {
		case args[0] == "status":
			return []byte(" M svc/ok.go\x00 M other/leak.go\x00"), nil
		case args[1] == "--show-prefix":
			return []byte("svc/\n"), nil
		}
		return []byte("abc\n"), nil
	})
	_, err := Fingerprint(context.Background(), runGit, t.TempDir(), "base", "v1")
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v, want escape error", err)
	}
}

func TestStoreLoadSessionNeverErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the session file should be makes ReadFile fail with an
	// error that is not "does not exist".
	if err = os.MkdirAll(filepath.Join(dir, "sessions", "blocked.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSession("blocked")
	if err != nil {
		t.Fatalf("LoadSession returned error: %v", err)
	}
	if want := (Session{ID: "blocked"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("session = %+v, want %+v", got, want)
	}
}

func TestStoreRemovesStaleTempFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"results", "sessions"} {
		if err = os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
		for name, age := range map[string]time.Duration{
			".tmp-stale": 2 * time.Hour,
			".tmp-fresh": 5 * time.Minute,
			"keep.txt":   48 * time.Hour, // near miss: not a temp file
		} {
			path := filepath.Join(dir, sub, name)
			if err = os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			when := time.Now().Add(-age)
			if err = os.Chtimes(path, when, when); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = store.SaveResult("abc", sampleResult(VerdictPass, "x")); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveSession(Session{ID: "s"}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"results", "sessions"} {
		if _, err = os.Stat(filepath.Join(dir, sub, ".tmp-stale")); !os.IsNotExist(err) {
			t.Errorf("%s/.tmp-stale should be removed, stat err = %v", sub, err)
		}
		for _, keep := range []string{".tmp-fresh", "keep.txt"} {
			if _, err = os.Stat(filepath.Join(dir, sub, keep)); err != nil {
				t.Errorf("%s/%s should be kept: %v", sub, keep, err)
			}
		}
	}
}
