package intelligence

import (
	"context"
	"errors"
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

func TestSnapshotCapturesRepositoryToolchainAndSemanticIdentity(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	base := snapshotGit(t, root, "rev-parse", "HEAD")
	semantic := SemanticIdentity{Version: "v0.21.0", Capabilities: CapabilityManifest{Hover: true, References: true}}

	first, err := snapshotter.Capture(context.Background(), SnapshotRequest{Base: base, Semantic: semantic})
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshotter.Capture(context.Background(), SnapshotRequest{Base: base, Semantic: semantic})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("stable snapshots differ:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if !strings.HasPrefix(first.ID, "sha256:") || !strings.HasPrefix(first.RepositoryID, "sha256:") || !strings.HasPrefix(first.ContentDigest, "sha256:") {
		t.Fatalf("snapshot identities = %#v", first)
	}
	if first.BaseCommit != base || first.MergeBaseCommit != base || first.HeadCommit != base {
		t.Fatalf("commit identity = %#v", first)
	}
	if first.GoVersion == "" || first.GoplsVersion != "v0.21.0" || !first.Capabilities.Hover {
		t.Fatalf("provider identity = %#v", first)
	}
	if first.Build.GOOS == "" || first.Build.GOARCH == "" || first.Build.Tags == nil {
		t.Fatalf("build configuration = %#v", first.Build)
	}
}

func TestSnapshotChangesForFinalContentConfigurationAndBuildInputs(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	request := SnapshotRequest{Semantic: SemanticIdentity{Version: "v0.21.0"}}
	initial, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	tracked, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if tracked.ID == initial.ID {
		t.Fatal("tracked content did not change snapshot")
	}

	writeSnapshotFile(t, root, "untracked.go", "package fixture\n")
	untracked, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if untracked.ID == tracked.ID {
		t.Fatal("untracked content did not change snapshot")
	}

	writeSnapshotFile(t, root, "go.mod", "module example.test/snapshot\n\ngo 1.25.0\n\n// changed configuration\n")
	configured, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if configured.ID == untracked.ID {
		t.Fatal("module configuration did not change snapshot")
	}

	t.Setenv("GOFLAGS", "-tags=integration,linux")
	buildChanged, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if buildChanged.ID == configured.ID || strings.Join(buildChanged.Build.Tags, ",") != "integration,linux" {
		t.Fatalf("build configuration did not change snapshot: %#v", buildChanged.Build)
	}
}

func TestSnapshotValidationRejectsStaleReference(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	request := SnapshotRequest{Semantic: SemanticIdentity{Version: "v0.21.0"}}
	expected, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 3\n")

	_, err = snapshotter.Validate(context.Background(), expected)
	if !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("Validate() error = %v, want ErrSnapshotChanged", err)
	}
}

func TestObservationSourceVerifiesManifestOnCapturedSourceMiss(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	observation, err := snapshotter.observe(context.Background(), SnapshotRequest{
		Scope: "./...", Semantic: SemanticIdentity{Version: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	delete(observation.sources, "main.go")

	original := []byte("package fixture\n\nvar Value = 1\n")
	got, err := observation.source("main.go")
	if err != nil || string(got) != string(original) {
		t.Fatalf("observation.source(main.go) = %q, %v, want captured state", got, err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	if _, sourceErr := observation.source("main.go"); !errors.Is(sourceErr, ErrSnapshotChanged) {
		t.Fatalf("observation.source(main.go) after same-size rewrite error = %v, want ErrSnapshotChanged", sourceErr)
	}
	writeSnapshotFile(t, root, "main.go", string(original))
	got, err = observation.source("main.go")
	if err != nil || string(got) != string(original) {
		t.Fatalf("observation.source(main.go) after A-B-A rewrite = %q, %v, want manifest-matching state", got, err)
	}
}

func TestObservationSourceVerifiesActualRetentionCapMiss(t *testing.T) {
	root := snapshotRepository(t)
	prefix := "package fixture\n\n/*"
	suffix := "*/\n"
	padding := strings.Repeat("a", maximumObservationSourceBytes)
	writeSnapshotFile(t, root, "large.go", prefix+padding+suffix)
	snapshotter := newTestSnapshotter(t, root)
	observation, err := snapshotter.observe(context.Background(), SnapshotRequest{
		Scope: "./...", Semantic: SemanticIdentity{Version: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	if _, found := observation.sources["large.go"]; found {
		t.Fatal("large.go was captured despite exceeding the observation source cap")
	}
	if _, err := observation.source("large.go"); err != nil {
		t.Fatalf("observation.source(large.go) error = %v, want manifest-verified source", err)
	}
	writeSnapshotFile(t, root, "large.go", prefix+strings.Repeat("b", len(padding))+suffix)
	if _, err := observation.source("large.go"); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("observation.source(large.go) after rewrite error = %v, want ErrSnapshotChanged", err)
	}
}

func TestObservationIncludesGuidanceAndRejectsChangedGuidance(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "AGENTS.md", "original guidance\n")
	snapshotGit(t, root, "add", "AGENTS.md")
	snapshotGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "guidance")
	snapshotter := newTestSnapshotter(t, root)
	observation, err := snapshotter.observe(context.Background(), SnapshotRequest{
		Scope: "./...", Semantic: SemanticIdentity{Version: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()

	refs, err := inventoryGuidanceObserved(context.Background(), &observation)
	if err != nil || len(refs) != 1 || refs[0].File != "AGENTS.md" {
		t.Fatalf("inventoryGuidanceObserved() = %#v, %v, want AGENTS.md", refs, err)
	}
	writeSnapshotFile(t, root, "AGENTS.md", "changed guidance!\n")
	if _, err := inventoryGuidanceObserved(context.Background(), &observation); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("inventoryGuidanceObserved() after rewrite error = %v, want ErrSnapshotChanged", err)
	}
}

func TestSnapshotManifestLeaseProtectsActiveEntryUntilRelease(t *testing.T) {
	snapshotter := &Snapshotter{
		manifests: make(map[string][]contentRecord),
		active:    make(map[string]int),
	}
	for index := 0; index < maximumManifestEntries; index++ {
		id := fmt.Sprintf("snapshot-%d", index)
		if err := snapshotter.remember(SnapshotRef{ID: id}, []contentRecord{{Path: id, Kind: "file", Digest: id}}); err != nil {
			t.Fatalf("remember(%q) error = %v", id, err)
		}
	}
	lease, err := snapshotter.pin("snapshot-0")
	if err != nil {
		t.Fatalf("pin(snapshot-0) error = %v", err)
	}
	if err := snapshotter.remember(SnapshotRef{ID: "snapshot-new"}, []contentRecord{{Path: "new.go", Kind: "file", Digest: "new"}}); err != nil {
		t.Fatalf("remember(snapshot-new) error = %v", err)
	}
	if _, found := snapshotter.manifest("snapshot-0"); !found {
		t.Fatal("pinned manifest was evicted before lease release")
	}
	lease.release()
	if err := snapshotter.remember(SnapshotRef{ID: "snapshot-final"}, []contentRecord{{Path: "final.go", Kind: "file", Digest: "final"}}); err != nil {
		t.Fatalf("remember(snapshot-final) error = %v", err)
	}
	if _, found := snapshotter.manifest("snapshot-0"); found {
		t.Fatal("released manifest remained pinned during eviction")
	}
}

func TestSnapshotManifestAdmissionFailsWhenAllEntriesAreActive(t *testing.T) {
	snapshotter := &Snapshotter{
		manifests: make(map[string][]contentRecord),
		active:    make(map[string]int),
	}
	leases := make([]*manifestLease, 0, maximumManifestEntries)
	for index := 0; index < maximumManifestEntries; index++ {
		id := fmt.Sprintf("snapshot-%d", index)
		if err := snapshotter.remember(SnapshotRef{ID: id}, []contentRecord{{Path: id, Kind: "file", Digest: id}}); err != nil {
			t.Fatalf("remember(%q) error = %v", id, err)
		}
		lease, err := snapshotter.pin(id)
		if err != nil {
			t.Fatalf("pin(%q) error = %v", id, err)
		}
		leases = append(leases, lease)
	}
	err := snapshotter.remember(SnapshotRef{ID: "snapshot-overflow"}, []contentRecord{{Path: "overflow.go", Kind: "file", Digest: "overflow"}})
	if !errors.Is(err, errManifestCapacity) {
		t.Fatalf("remember(snapshot-overflow) error = %v, want errManifestCapacity", err)
	}
	for _, lease := range leases {
		lease.release()
	}
}

func TestSnapshotManifestReplacementAccountsForRetainedBytes(t *testing.T) {
	newSnapshotter := func() *Snapshotter {
		return &Snapshotter{
			manifests: make(map[string][]contentRecord),
			active:    make(map[string]int),
		}
	}
	record := func(path string, size int, value byte) []contentRecord {
		return []contentRecord{{Path: path, Kind: "file", Digest: strings.Repeat(string(value), size)}}
	}

	t.Run("evicts inactive entry", func(t *testing.T) {
		snapshotter := newSnapshotter()
		if err := snapshotter.remember(SnapshotRef{ID: "target"}, record("target.go", 1<<20, 'a')); err != nil {
			t.Fatal(err)
		}
		lease, err := snapshotter.pin("target")
		if err != nil {
			t.Fatal(err)
		}
		defer lease.release()
		if err := snapshotter.remember(SnapshotRef{ID: "inactive"}, record("inactive.go", 6<<20, 'b')); err != nil {
			t.Fatal(err)
		}
		if err := snapshotter.remember(SnapshotRef{ID: "target"}, record("target.go", 3<<20, 'c')); err != nil {
			t.Fatalf("remember(target replacement) error = %v", err)
		}
		if _, found := snapshotter.manifest("inactive"); found {
			t.Fatal("inactive manifest remained after replacement admission")
		}
		if got := manifestSize(snapshotter.manifests["target"]); got != 3<<20+len("target.go")+len("file") {
			t.Fatalf("replacement manifest size = %d, want %d", got, 3<<20+len("target.go")+len("file"))
		}
	})

	t.Run("rejects when all other entries are active", func(t *testing.T) {
		snapshotter := newSnapshotter()
		for _, item := range []struct {
			id    string
			path  string
			size  int
			value byte
		}{
			{id: "target", path: "target.go", size: 1 << 20, value: 'a'},
			{id: "active", path: "active.go", size: 6 << 20, value: 'b'},
		} {
			if err := snapshotter.remember(SnapshotRef{ID: item.id}, record(item.path, item.size, item.value)); err != nil {
				t.Fatal(err)
			}
			lease, err := snapshotter.pin(item.id)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.release()
		}
		before := append([]contentRecord(nil), snapshotter.manifests["target"]...)
		err := snapshotter.remember(SnapshotRef{ID: "target"}, record("target.go", 3<<20, 'c'))
		if !errors.Is(err, errManifestCapacity) {
			t.Fatalf("remember(target replacement) error = %v, want errManifestCapacity", err)
		}
		if !reflect.DeepEqual(snapshotter.manifests["target"], before) {
			t.Fatal("failed replacement changed active target manifest")
		}
	})
}

func TestSnapshotIncludesIgnoredActiveInputsButNotInactiveFiles(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, ".gitignore", "generated.go\nassets/\nnotes.tmp\n")
	writeSnapshotFile(t, root, "embed.go", "package fixture\n\nimport _ \"embed\"\n\n//go:embed assets/data.txt\nvar Data string\n")
	snapshotGit(t, root, "add", ".gitignore", "embed.go")
	snapshotGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "embed fixture")
	writeSnapshotFile(t, root, "generated.go", "package fixture\n\nvar Generated = 1\n")
	writeSnapshotFile(t, root, "assets/data.txt", "one\n")
	snapshotter := newTestSnapshotter(t, root)
	request := SnapshotRequest{Semantic: SemanticIdentity{Version: "v0.21.0"}}

	initial, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "notes.tmp", "ignored and inactive\n")
	inactive, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if inactive.ID != initial.ID {
		t.Fatal("inactive ignored file changed semantic snapshot")
	}
	writeSnapshotFile(t, root, "generated.go", "package fixture\n\nvar Generated = 2\n")
	generated, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if generated.ID == inactive.ID {
		t.Fatal("ignored active Go file did not change semantic snapshot")
	}
	writeSnapshotFile(t, root, "assets/data.txt", "two\n")
	embedded, err := snapshotter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if embedded.ID == generated.ID {
		t.Fatal("ignored embedded asset did not change semantic snapshot")
	}
}

func TestSnapshotIgnoresGitDirectoryMarkers(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, ".gitignore", "ignored/\n")
	writeSnapshotFile(t, root, "ignored/checkout/go.mod", "module example.test/ignored\n\ngo 1.25.0\n")

	snapshotter := newTestSnapshotter(t, root)
	if _, err := snapshotter.Capture(context.Background(), SnapshotRequest{Semantic: SemanticIdentity{Version: "test"}}); err != nil {
		t.Fatalf("Capture() with ignored nested checkout = %v", err)
	}
}

func newTestSnapshotter(t *testing.T, root string) *Snapshotter {
	t.Helper()
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go toolchain unavailable")
	}
	t.Setenv("PATH", filepath.Dir(goPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	ws, err := workspace.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := execution.New(ws, execution.Config{})
	if err != nil {
		t.Fatal(err)
	}
	snapshotter, err := NewSnapshotter(ws, runner)
	if err != nil {
		t.Fatal(err)
	}
	return snapshotter
}

func snapshotRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	snapshotGit(t, root, "init", "-b", "main")
	snapshotGit(t, root, "config", "user.name", "Fixture")
	snapshotGit(t, root, "config", "user.email", "fixture@example.test")
	writeSnapshotFile(t, root, "go.mod", "module example.test/snapshot\n\ngo 1.25.0\n")
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 1\n")
	snapshotGit(t, root, "add", ".")
	snapshotGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	return root
}

func writeSnapshotFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func snapshotGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
