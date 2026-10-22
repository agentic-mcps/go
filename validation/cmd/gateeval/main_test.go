package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/validation/internal/gateeval"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsageErrorsExitTwo(t *testing.T) {
	tests := []struct { //nolint:govet // Table layout favors readability.
		name string
		args []string
		want string
	}{
		{name: "no command", args: nil, want: "a command is required"},
		{name: "unknown command", args: []string{"frobnicate"}, want: `unknown command "frobnicate"`},
		{name: "select needs paths", args: []string{"select"}, want: "--exclusions is required"},
		{name: "generate needs paths", args: []string{"generate", "--work", "w"}, want: "--exclusions is required"},
		{name: "run needs variants", args: []string{"run", "--work", "w", "--out", "o"}, want: "--variants is required"},
		{name: "summarize needs runs", args: []string{"summarize", "--variants", "v"}, want: "--exclusions is required"},
		{name: "unknown flag", args: []string{"select", "--bogus"}, want: "flag provided but not defined"},
		{name: "stray argument", args: []string{"summarize", "--variants", "v", "--runs", "r", "--exclusions", "e", "--out", "o", "extra"}, want: `unexpected argument "extra"`},
		{name: "unknown arm", args: []string{"run", "--work", "w", "--variants", "v", "--out", "o", "--arms", "B0,Z9"}, want: `unknown arm "Z9"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, tt.args...)
			if code != 2 {
				t.Errorf("exit = %d, want 2; stderr: %s", code, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
		})
	}
}

func TestHelpExitsZero(t *testing.T) {
	if code, stdout, _ := runCLI(t, "help"); code != 0 || !strings.Contains(stdout, "summarize") {
		t.Errorf("help: exit %d, stdout %q", code, stdout)
	}
	if code, _, _ := runCLI(t, "select", "-h"); code != 0 {
		t.Errorf("select -h exit = %d, want 0", code)
	}
}

func TestRuntimeFailureExitsOne(t *testing.T) {
	code, _, stderr := runCLI(t, "summarize", "--variants", "missing.jsonl", "--runs", "r", "--exclusions", "e", "--out", t.TempDir())
	if code != 1 || !strings.Contains(stderr, "missing.jsonl") {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestParseArms(t *testing.T) {
	got, err := parseArms("B0, B2s ,Gci")
	if err != nil {
		t.Fatal(err)
	}
	if want := []gateeval.Arm{gateeval.ArmB0, gateeval.ArmB2Star, gateeval.ArmGCI}; !reflect.DeepEqual(got, want) {
		t.Errorf("arms = %v, want %v", got, want)
	}
	for _, bad := range []string{"", "B0,,B1", "b0"} {
		if _, parseErr := parseArms(bad); parseErr == nil {
			t.Errorf("parseArms(%q) should fail", bad)
		}
	}
	all, err := parseArms("B0,B1,B2,B2s,B3,Gci,Ghook")
	if err != nil || len(all) != 7 {
		t.Errorf("default arm list: %v %v", all, err)
	}
}

func TestSummarizeWritesFiles(t *testing.T) {
	dir := t.TempDir()
	variants := filepath.Join(dir, "variants.jsonl")
	runs := filepath.Join(dir, "runs.jsonl")
	selectExcl := filepath.Join(dir, "select-excl.jsonl")
	genExcl := filepath.Join(dir, "gen-excl.jsonl")
	for _, v := range []gateeval.Variant{
		{ID: "p-aaaaaaa-T", Project: "p", Class: gateeval.ClassTrue, Effective: true},
		{ID: "p-aaaaaaa-M", Project: "p", Class: gateeval.ClassMutant, Effective: true},
	} {
		if err := gateeval.AppendJSONL(variants, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []gateeval.Run{
		{VariantID: "p-aaaaaaa-T", Arm: gateeval.ArmB0, Attempt: 1},
		{VariantID: "p-aaaaaaa-M", Arm: gateeval.ArmB0, Attempt: 1, Blocked: true},
	} {
		if err := gateeval.AppendJSONL(runs, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := gateeval.AppendJSONL(selectExcl, gateeval.Exclusion{Project: "p", Reason: "static: 3 scanned, 1 kept"}); err != nil {
		t.Fatal(err)
	}
	if err := gateeval.AppendJSONL(genExcl, gateeval.Exclusion{Project: "p", Commit: "c", Class: gateeval.ClassConsumerBreak, Reason: "no consumer break"}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "summary")
	code, stdout, stderr := runCLI(t, "summarize", "--variants", variants, "--runs", runs, "--exclusions", selectExcl+","+genExcl, "--out", out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "summary.md") {
		t.Errorf("stdout = %q", stdout)
	}
	for _, name := range []string{"summary.json", "summary.md"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || len(data) == 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "no consumer break") || !strings.Contains(string(data), "static: 3 scanned, 1 kept") {
		t.Errorf("exclusions from both files must be counted:\n%s", data)
	}
}
