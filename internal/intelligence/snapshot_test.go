package intelligence

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
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

func newTestSnapshotter(t *testing.T, root string) *Snapshotter {
	t.Helper()
	goPath := "/Users/ashwin/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.darwin-arm64/bin/go"
	if _, err := os.Stat(goPath); err != nil {
		if resolved, lookErr := exec.LookPath("go"); lookErr == nil {
			goPath = resolved
		} else {
			t.Skip("Go toolchain unavailable")
		}
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
