package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadManifestValidation(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "manifest.csv")
	if err := os.WriteFile(p, []byte("project,repository,scan_root,patterns,commit\na,b,.,\"./... ./internal/...\",0123456789012345678901234567890123456789\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := readManifest(p)
	if err != nil || len(rows) != 1 || rows[0].Patterns != "./... ./internal/..." {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if _, err := readManifest(filepath.Join(d, "missing")); err == nil {
		t.Fatal("expected missing manifest error")
	}
}

func TestParseFindingsAndErrors(t *testing.T) {
	d := t.TempDir()
	file := filepath.Join(d, "x.go")
	if err := os.WriteFile(file, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := []byte(fmt.Sprintf(`{"pkg":{"concurrency":{"diagnostics":[{"category":"concurrency-01","message":"shared access","posn":%q}]}}}`, file+":7:2"))
	got, err := parseFindings(data, d)
	if err != nil || len(got) != 1 || got[0].Line != "7" || got[0].File != "x.go" || got[0].Severity != "error" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := parseFindings([]byte(`{"errors":{"error":"load failed"}}`), d); err == nil {
		t.Fatal("expected analyzer error")
	}
	if _, err := parseFindings([]byte("not json"), d); err == nil {
		t.Fatal("expected JSON error")
	}
	if _, err := parseFindings([]byte(`{"x":{"category":"errors-01","message":"bad","posn":"not-a-position"}}`), d); err == nil {
		t.Fatal("expected malformed position")
	}
	if _, err := parseFindings([]byte(`{"x":{"category":"errors-01","message":"bad","posn":"/tmp/outside.go:1:1"}}`), d); err == nil {
		t.Fatal("expected outside position")
	}
}

func TestDeduplicationKeyShape(t *testing.T) {
	a := finding{Rule: "errors-01", Posn: "/x.go:3:1", Message: "bad"}
	b := finding{Rule: "errors-01", Posn: "/x.go:3:1", Message: "bad"}
	key := func(f finding) string { return strings.Join([]string{f.Rule, f.Posn, f.Message}, "\x00") }
	if key(a) != key(b) {
		t.Fatal("identical findings must deduplicate")
	}
}

func TestSeverityMapIsExplicit(t *testing.T) {
	if len(ruleSeverity) != 34 {
		t.Fatalf("unexpected registered rule count: %d", len(ruleSeverity))
	}
	for rule, severity := range ruleSeverity {
		if severity != "error" && severity != "warning" && severity != "info" {
			t.Fatalf("%s has invalid severity %q", rule, severity)
		}
	}
}

func TestOutputCapAndEnvironment(t *testing.T) {
	var capped cappedBuffer
	if _, err := capped.Write(make([]byte, maxOutput+1)); err == nil || !capped.exceeded {
		t.Fatalf("writer cap err=%v exceeded=%v", err, capped.exceeded)
	}
	cmd := exec.Command("sh", "-c", "printf stderr >&2")
	out, stderr, err := runCommand(cmd)
	if err != nil || len(out) != 0 || string(stderr) != "stderr" {
		t.Fatalf("separate stderr capture: out=%q stderr=%q err=%v", out, stderr, err)
	}
	env := controlledEnv("/opt/go/bin/go", []string{"PATH=/bad", "GOTOOLCHAIN=auto", "GOPROXY=https://proxy", "GOSUMDB=sum", "X=y"})
	joined := strings.Join(env, "\n")
	for _, want := range []string{"GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "X=y", "PATH=/opt/go/bin:"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment missing %q: %s", want, joined)
		}
	}
}

func TestDiagnosticExit(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 3").Run()
	if !isDiagnosticExit(err) {
		t.Fatalf("exit 3 was not recognized: %v", err)
	}
	err = exec.Command("sh", "-c", "exit 1").Run()
	if isDiagnosticExit(err) {
		t.Fatalf("exit 1 was recognized as diagnostics: %v", err)
	}
}

func TestSanitizeText(t *testing.T) {
	got := sanitizeText("/work/corpus/echo/file.go /tmp/vet", "/work/corpus/echo", "/tmp/vet")
	if got != "file.go $AGENTIC_GO_VET" {
		t.Fatalf("sanitizeText() = %q", got)
	}
}

func TestCheckoutPathContainment(t *testing.T) {
	d := t.TempDir()
	if _, err := checkoutPath(d, "../escape"); err == nil {
		t.Fatal("expected containment error")
	}
}
