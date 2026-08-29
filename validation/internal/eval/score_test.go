package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCandidatePathsCoversAllWorktreeStates(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	runGit(t, root, "config", "user.name", "Evaluation Test")
	runGit(t, root, "config", "user.email", "evaluation@invalid")
	writeTestFile(t, filepath.Join(root, "staged.go"), "package fixture\n")
	writeTestFile(t, filepath.Join(root, "unstaged.go"), "package fixture\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "--quiet", "--no-gpg-sign", "-m", "fixture")

	writeTestFile(t, filepath.Join(root, "staged.go"), "package fixture\n// staged\n")
	runGit(t, root, "add", "staged.go")
	writeTestFile(t, filepath.Join(root, "unstaged.go"), "package fixture\n// unstaged\n")
	writeTestFile(t, filepath.Join(root, "untracked.go"), "package fixture\n")

	paths, err := candidatePaths(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"staged.go", "unstaged.go", "untracked.go"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("candidate paths = %v, want %v", paths, want)
	}
}

func TestEnvironmentalFailuresRemainIncomplete(t *testing.T) {
	for _, stderr := range []string{
		"module lookup disabled by GOPROXY=off",
		"go.mod requires go >= 1.27",
		"toolchain not available",
	} {
		if !environmentalFailure(stderr) {
			t.Fatalf("environmental failure not recognized: %q", stderr)
		}
	}
	if environmentalFailure("undefined: CandidateSymbol") {
		t.Fatal("candidate compile failure was classified as environmental")
	}
}

func TestGitOutputLimitFailsClosed(t *testing.T) {
	buffer := &limitedBuffer{limit: 2}
	if _, err := buffer.Write([]byte("abc")); err == nil {
		t.Fatal("oversized Git output was accepted")
	}
	if buffer.Len() != 0 {
		t.Fatalf("partial oversized output retained: %d bytes", buffer.Len())
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
