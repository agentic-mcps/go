package gateeval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/gate"
)

func TestGateTextBytesLeavesUndecodableOutputUnset(t *testing.T) {
	valid, err := json.Marshal(gate.Result{
		SchemaVersion: gate.SchemaVersion, Verdict: gate.VerdictPass,
		Base: gate.Base{Ref: "main", Commit: "0123456789abcdef"}, Items: []gate.Item{}, Notes: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := gateTextBytes(valid); got <= 0 || got > gateTextLimit {
		t.Fatalf("gateTextBytes(valid) = %d, want a positive size within %d", got, gateTextLimit)
	}
	for _, stdout := range []string{"", "not json", "{", "agentic-go check: PASS"} {
		got := gateTextBytes([]byte(stdout))
		if got != 0 {
			t.Fatalf("gateTextBytes(%q) = %d, want 0 (unset)", stdout, got)
		}
		encoded, err := json.Marshal(Run{TextBytes: got})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "text_bytes") {
			t.Fatalf("run for %q records text_bytes: %s", stdout, encoded)
		}
	}
}
