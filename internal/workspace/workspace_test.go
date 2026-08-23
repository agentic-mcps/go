package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGoVersion(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		want   string
	}{
		{name: "Go 1.25", output: "go version go1.25.0 darwin/arm64", want: "go1.25.0"},
		{name: "Go 1.26", output: "go version go1.26.7 linux/amd64", want: "go1.26.7"},
		{name: "Go 1.27", output: "go version go1.27.0 darwin/arm64", want: "go1.27.0"},
		{name: "future development", output: "go version devel go1.28-deadbeef darwin/arm64", want: "go1.28-deadbeef"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseGoVersion([]byte(test.output))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("parseGoVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseGoVersionRejectsMalformedOutput(t *testing.T) {
	if _, err := parseGoVersion([]byte("not a Go version")); err == nil {
		t.Fatal("parseGoVersion() accepted malformed output")
	}
}

func TestOpenRejectsUnsupportedGoToolchain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	installFakeGo(t, "go version go1.24.2 darwin/arm64", 0)

	_, err := Open(context.Background(), root)
	if err == nil {
		t.Fatal("Open() accepted Go 1.24.2, want unsupported toolchain error")
	}
	if !strings.Contains(err.Error(), "go1.24.2") || !strings.Contains(err.Error(), "Go 1.25 or newer") {
		t.Fatalf("Open() error = %q, want version and minimum", err)
	}
}

func TestOpenRejectsPrereleaseGoToolchain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	installFakeGo(t, "go version go1.25rc1 darwin/arm64", 0)

	_, err := Open(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "go1.25rc1") {
		t.Fatalf("Open() error = %q, want unsupported prerelease error", err)
	}
}

func TestOpenAcceptsSupportedGoToolchains(t *testing.T) {
	for _, selected := range []string{"go1.25.0", "go1.26.7", "go1.27.0"} {
		t.Run(selected, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
			installFakeGo(t, "go version "+selected+" darwin/arm64", 0)

			if _, err := Open(context.Background(), root); err != nil {
				t.Fatalf("Open() error = %v", err)
			}
		})
	}
}

func TestOpenAcceptsFutureDevelopmentToolchainWithoutClaimingSupport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	installFakeGo(t, "go version devel go1.28-deadbeef darwin/arm64", 0)

	if _, err := Open(context.Background(), root); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
}

func TestOpenRejectsToolchainOlderThanWorkspace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.26.0\n")
	installFakeGo(t, "go version go1.25.0 darwin/arm64", 0)

	_, err := Open(context.Background(), root)
	if err == nil {
		t.Fatal("Open() accepted Go 1.25 for a Go 1.26 workspace")
	}
	if !strings.Contains(err.Error(), "go1.25.0") || !strings.Contains(err.Error(), "Go 1.26.0") {
		t.Fatalf("Open() error = %q, want selected and required versions", err)
	}
}

func TestOpenUsesHighestActiveWorkspaceGoVersion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.work"), "go 1.26.0\n\nuse (\n\t./older\n\t./newer\n)\n")
	writeFile(t, filepath.Join(root, "older", "go.mod"), "module example.com/older\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "newer", "go.mod"), "module example.com/newer\n\ngo 1.26.0\n")
	installFakeGo(t, "go version go1.25.0 darwin/arm64", 0)

	_, err := Open(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "Go 1.26.0") {
		t.Fatalf("Open() error = %q, want highest active module requirement", err)
	}
}

func TestOpenIgnoresInactiveWorkspaceModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.work"), "go 1.25.0\n\nuse ./active\n")
	writeFile(t, filepath.Join(root, "active", "go.mod"), "module example.com/active\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "inactive", "go.mod"), "module example.com/inactive\n\ngo 1.27.0\n")
	installFakeGo(t, "go version go1.25.0 darwin/arm64", 0)

	if _, err := Open(context.Background(), root); err != nil {
		t.Fatalf("Open() error = %v, want inactive module ignored", err)
	}
}

func TestOpenRejectsMissingGoToolchain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	t.Setenv("PATH", t.TempDir())

	_, err := Open(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "MCP process PATH") {
		t.Fatalf("Open() error = %q, want missing Go toolchain error", err)
	}
}

func TestOpenReportsGoVersionCommandFailure(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	installFakeGo(t, "selected Go toolchain is unavailable", 42)

	_, err := Open(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "selected Go toolchain is unavailable") || !strings.Contains(err.Error(), filepath.Join(os.Getenv("PATH"), "go")) {
		t.Fatalf("Open() error = %q, want Go version command failure", err)
	}
}

func TestOpenRejectsMalformedGoVersionOutput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	installFakeGo(t, "not a Go version", 0)

	_, err := Open(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "unexpected go version output") || !strings.Contains(err.Error(), filepath.Join(os.Getenv("PATH"), "go")) {
		t.Fatalf("Open() error = %q, want malformed version and executable path", err)
	}
}

func TestOpenAndResolve(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "internal", "value.go"), "package internal\n")

	ws, err := Open(context.Background(), filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	got, err := ws.Resolve("value.go")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(root, "internal", "value.go"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolveRejectsEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside.go")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, outside, "package outside\n")

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	for _, path := range []string{"../outside.go", outside} {
		t.Run(path, func(t *testing.T) {
			if _, err := ws.Resolve(path); err == nil {
				t.Fatalf("Resolve(%q) succeeded, want containment error", path)
			}
		})
	}
}

func TestResolveRejectsEscapingSymlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside.go")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, outside, "package outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := ws.Resolve("link.go"); err == nil {
		t.Fatal("Resolve() succeeded for symlink outside workspace")
	}
}

func TestRelativeReturnsWorkspacePath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ws.Relative(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "go.mod" {
		t.Fatalf("Relative(go.mod) = %q, want go.mod", got)
	}
}

func TestOpenRejectsNonWorkspace(t *testing.T) {
	if _, err := Open(context.Background(), t.TempDir()); err == nil {
		t.Fatal("Open() succeeded without go.mod or go.work")
	}
}

func TestOpenAcceptsGoWorkRoot(t *testing.T) {
	root := t.TempDir()
	moduleRoot := filepath.Join(root, "module")
	writeFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "go.work"), "go 1.25.0\n\nuse ./module\n")

	ws, err := Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if ws.Root() == "" {
		t.Fatal("Open() returned an empty root")
	}
}

func TestOpenHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Open(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled", err)
	}
}

func TestOpenHonorsCancellationDuringGoVersion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25.0\n")
	installFakeGo(t, "go version go1.27.0 darwin/arm64", 0)
	t.Setenv("FAKE_GO_VERSION_DELAY", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := Open(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open() error = %v, want context.DeadlineExceeded", err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installFakeGo(t *testing.T, output string, exitCode int) {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "version" ]; then
  if [ -n "$FAKE_GO_VERSION_DELAY" ]; then
    /bin/sleep "$FAKE_GO_VERSION_DELAY"
  fi
  printf '%s\n' "$FAKE_GO_VERSION"
  exit "$FAKE_GO_VERSION_EXIT"
fi
exec "$REAL_GO" "$@"
`
	path := filepath.Join(bin, "go")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_GO", realGo)
	t.Setenv("FAKE_GO_VERSION", output)
	t.Setenv("FAKE_GO_VERSION_EXIT", fmt.Sprintf("%d", exitCode))
	t.Setenv("FAKE_GO_VERSION_DELAY", "")
	t.Setenv("PATH", bin)
}
