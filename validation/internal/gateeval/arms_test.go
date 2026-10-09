package gateeval

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func failures(tests []string, packages []string) testFailures {
	f := newTestFailures()
	for _, test := range tests {
		f.Tests[test] = true
	}
	for _, pkg := range packages {
		f.Packages[pkg] = true
	}
	return f
}

func withVet(f testFailures, packages ...string) testFailures {
	for _, pkg := range packages {
		f.Vet[pkg] = true
	}
	return f
}

func TestDecideB2Star(t *testing.T) {
	tests := []struct {
		current     testFailures
		base        testFailures
		name        string
		wantReason  string
		build       bool
		vet         bool
		lint        bool
		wantBlocked bool
	}{
		{name: "clean", current: failures(nil, nil), base: failures(nil, nil)},
		{
			name: "new test failure blocks", current: failures([]string{"p.TestA"}, []string{"p"}),
			base: failures(nil, nil), wantBlocked: true, wantReason: "new test failure: p.TestA",
		},
		{
			name: "base test failure ignored", current: failures([]string{"p.TestA"}, []string{"p"}),
			base: failures([]string{"p.TestA"}, []string{"p"}),
		},
		{
			name: "base package failure ignored", current: failures(nil, []string{"q"}),
			base: failures(nil, []string{"q"}),
		},
		{
			name: "new package failure blocks", current: failures(nil, []string{"q"}),
			base: failures(nil, []string{"p"}), wantBlocked: true, wantReason: "new package failure: q",
		},
		{
			name:    "new test failure beside base failure in same package blocks",
			current: failures([]string{"p.TestA", "p.TestB"}, []string{"p"}),
			base:    failures([]string{"p.TestA"}, []string{"p"}), wantBlocked: true, wantReason: "new test failure: p.TestB",
		},
		{
			name:    "same test name in another package blocks",
			current: failures([]string{"q.TestA"}, []string{"q"}),
			base:    failures([]string{"p.TestA"}, []string{"p"}), wantBlocked: true, wantReason: "new test failure: q.TestA",
		},
		{name: "build failure blocks", build: true, current: failures(nil, nil), base: failures(nil, nil), wantBlocked: true, wantReason: "go build failed"},
		{
			name: "build failure blocks even when base fails", build: true, current: failures(nil, []string{"p"}),
			base: failures(nil, []string{"p"}), wantBlocked: true, wantReason: "go build failed",
		},
		{name: "vet failure blocks", vet: true, current: failures(nil, nil), base: failures(nil, nil), wantBlocked: true, wantReason: "go vet failed"},
		{
			name: "vet failure in a package that also fails vet at base is ignored", vet: true,
			current: withVet(failures(nil, nil), "p"), base: withVet(failures(nil, nil), "p"),
		},
		{
			name: "vet failure in a new package blocks", vet: true,
			current: withVet(failures(nil, nil), "p", "q"), base: withVet(failures(nil, nil), "p"),
			wantBlocked: true, wantReason: "go vet failed: q",
		},
		{
			name: "vet failure without attribution blocks even when base vet fails", vet: true,
			current: failures(nil, nil), base: withVet(failures(nil, nil), "p"),
			wantBlocked: true, wantReason: "go vet failed",
		},
		{
			name: "vet at base does not excuse a new test failure", vet: true,
			current: withVet(failures([]string{"p.TestA"}, []string{"p"}), "p"), base: withVet(failures(nil, nil), "p"),
			wantBlocked: true, wantReason: "new test failure: p.TestA",
		},
		{name: "lint blocks", lint: true, current: failures(nil, nil), base: failures(nil, nil), wantBlocked: true, wantReason: "lint reported issues"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason := decideB2Star(tt.build, tt.vet, tt.lint, tt.current, tt.base)
			if blocked != tt.wantBlocked || reason != tt.wantReason {
				t.Fatalf("decideB2Star() = %v, %q; want %v, %q", blocked, reason, tt.wantBlocked, tt.wantReason)
			}
		})
	}
}

func TestParseTestFailures(t *testing.T) {
	stream := strings.Join([]string{
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"fail","Package":"p","Test":"TestA/sub","Elapsed":0}`,
		`{"Action":"fail","Package":"p","Test":"TestA","Elapsed":0}`,
		`{"Action":"pass","Package":"p","Test":"TestB"}`,
		`{"Action":"fail","Package":"p","Elapsed":0}`,
		`{"ImportPath":"q [q.test]","Action":"build-fail"}`,
		`{"Action":"fail","Package":"q","FailedBuild":"q [q.test]"}`,
		`FAIL	r [setup failed]`,
		`{not json`,
		``,
	}, "\n")
	got := parseTestFailures([]byte(stream))
	want := failures([]string{"p.TestA"}, []string{"p", "q"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseTestFailures() = %#v, want %#v", got, want)
	}
	if clean := parseTestFailures([]byte(`{"Action":"pass","Package":"p"}`)); len(clean.Tests)+len(clean.Packages) != 0 {
		t.Fatalf("clean stream reported failures: %#v", clean)
	}
}

func TestParseVetPackages(t *testing.T) {
	root := "/work/proj"
	tests := []struct {
		want   map[string]bool
		name   string
		output string
	}{
		{
			name:   "headers (Go 1.25 and older)",
			output: "# example.com/a\na.go:3:2: unreachable code\n# example.com/b [example.com/b.test]\nb_test.go:5: bad\nvet: other\n# [example.com/c]\nc.go:1: bad\n",
			want:   map[string]bool{"example.com/a": true, "example.com/b": true, "example.com/c": true},
		},
		{
			name:   "no headers (Go 1.26): file paths map to package directories",
			output: "p/p.go:5:24: fmt.Printf format %d has arg \"x\" of wrong type string\np/q_test.go:3:1: bad\n./a.go:1:1: bad\nvet: sub/deep/s.go:2:3: undefined: x\n",
			want:   map[string]bool{"./p": true, ".": true, "./sub/deep": true},
		},
		{
			name:   "absolute paths are made relative to the module root",
			output: "/work/proj/abs/x.go:1:1: bad\n",
			want:   map[string]bool{"./abs": true},
		},
		{
			name:   "continuation lines and prose are not diagnostics",
			output: "    see other.go:1: note\nexit status 1\n",
			want:   map[string]bool{},
		},
		{name: "empty", output: "", want: map[string]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseVetPackages([]byte(tt.output), root); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseVetPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVetFailureKeysSentinelMatchesOnlyIdenticalOutput(t *testing.T) {
	root := "/work/proj"
	unattributable := stepResult{stderr: []byte("vet: something went wrong in a way we cannot attribute\n"), exit: 1}
	other := stepResult{stderr: []byte("vet: a different unattributable failure\n"), exit: 1}
	a, again, b := vetFailureKeys(unattributable, root), vetFailureKeys(unattributable, root), vetFailureKeys(other, root)
	if len(a) != 1 || !reflect.DeepEqual(a, again) {
		t.Fatalf("identical output gave different keys: %v vs %v", a, again)
	}
	if reflect.DeepEqual(a, b) {
		t.Fatalf("different unattributable output shares a key: %v", a)
	}
	// An unattributable current failure is not masked by a different one at base.
	if blocked, _ := decideB2Star(false, true, false, withVet(failures(nil, nil), keysOf(b)...), withVet(failures(nil, nil), keysOf(a)...)); !blocked {
		t.Error("a new unattributable vet failure was masked by a different one at base")
	}
	if blocked, _ := decideB2Star(false, true, false, withVet(failures(nil, nil), keysOf(a)...), withVet(failures(nil, nil), keysOf(a)...)); blocked {
		t.Error("an identical unattributable vet failure at base should be ignored")
	}
	attributed := stepResult{stderr: []byte("p/p.go:1:1: bad\n"), exit: 1}
	if got := vetFailureKeys(attributed, root); !reflect.DeepEqual(got, map[string]bool{"./p": true}) {
		t.Fatalf("attributable output gave %v", got)
	}
}

func keysOf(set map[string]bool) []string { return newKeys(set, nil) }

func TestCommandEnvDropsCI(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("CIRCLE", "keep")
	var sawCircle bool
	for _, entry := range commandEnv() {
		if strings.HasPrefix(entry, "CI=") {
			t.Fatalf("commandEnv() kept %q", entry)
		}
		if entry == "CIRCLE=keep" {
			sawCircle = true
		}
	}
	if !sawCircle {
		t.Error("commandEnv() dropped unrelated variables")
	}
	result := runCommand(context.Background(), t.TempDir(), time.Minute, "sh", "-c", "echo ci=${CI-unset}")
	if got := strings.TrimSpace(string(result.stdout)); got != "ci=unset" {
		t.Fatalf("a command saw %q, want the CI variable removed", got)
	}
}

func TestParseGateOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    gateOutcome
		wantErr bool
	}{
		{name: "pass", input: `{"verdict":"pass","items":[]}`},
		{name: "block", input: `{"verdict":"block","items":[{"severity":"block"}]}`, want: gateOutcome{blocked: true}},
		{name: "unknown", input: `{"verdict":"unknown","items":[]}`, want: gateOutcome{unknown: true}},
		{name: "warn", input: `{"verdict":"pass","items":[{"severity":"info"},{"severity":"warn"}]}`, want: gateOutcome{warned: true}},
		{name: "block with warn", input: `{"verdict":"block","items":[{"severity":"warn"},{"severity":"block"}]}`, want: gateOutcome{blocked: true, warned: true}},
		{name: "info only is not a warning", input: `{"verdict":"pass","items":[{"severity":"info"}]}`},
		{name: "surrounding whitespace", input: "\n {\"verdict\":\"pass\"} \n"},
		{name: "garbage", input: `panic: nope`, wantErr: true},
		{name: "empty", input: ``, wantErr: true},
		{name: "unrecognized verdict", input: `{"verdict":"maybe"}`, wantErr: true},
		{name: "missing verdict", input: `{"items":[]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGateOutput([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseGateOutput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseGateOutput() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTruncateKeepsRunes(t *testing.T) {
	text := strings.Repeat("a", reasonLimit-1) + "é" + "tail"
	got := truncate(text)
	if len(got) > reasonLimit || !strings.HasPrefix(text, got) {
		t.Fatalf("truncate() length %d is not a prefix within %d bytes", len(got), reasonLimit)
	}
	if got != strings.Repeat("a", reasonLimit-1) {
		t.Fatalf("truncate() split the rune: %q", got[len(got)-2:])
	}
	if short := truncate("short"); short != "short" {
		t.Fatalf("truncate(short) = %q", short)
	}
}
