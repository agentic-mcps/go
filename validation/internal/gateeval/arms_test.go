package gateeval

import (
	"reflect"
	"strings"
	"testing"
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
