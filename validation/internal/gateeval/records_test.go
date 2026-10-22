package gateeval

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJSONLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	want := []Run{
		{VariantID: "a", Arm: ArmB2, Blocked: true, ExitCode: 1, DurationMS: 12},
		{VariantID: "b", Arm: ArmGCI, Warned: true, Reason: "<html> stays"},
	}
	for _, run := range want {
		if err := AppendJSONL(path, run); err != nil {
			t.Fatalf("AppendJSONL() error = %v", err)
		}
	}
	got, err := ReadJSONL[Run](path)
	if err != nil {
		t.Fatalf("ReadJSONL() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestDecodeJSONLReportsLine(t *testing.T) {
	_, err := DecodeJSONL[Run](strings.NewReader("{\"variant_id\":\"a\"}\n\nnot json\n"))
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("DecodeJSONL() error = %v, want line 3", err)
	}
}

func TestClassIsFlaw(t *testing.T) {
	if ClassTrue.IsFlaw() || ClassDestructive.IsFlaw() {
		t.Fatal("true and destructive patches must not be flaws")
	}
	if !ClassDeleteTests.IsFlaw() || !ClassEnvGuardedSkip.IsFlaw() {
		t.Fatal("cover-ups must be flaws")
	}
}
