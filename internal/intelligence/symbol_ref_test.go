package intelligence

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestSymbolRefRoundTripAndSnapshotBinding(t *testing.T) {
	want := symbolIdentity{
		SnapshotID: "sha256:snapshot", Base: "main", Scope: "./pkg/...", Path: "pkg/value.go", Position: Position{Line: 4, Character: 7},
		Kind: "go.function", Package: "example.test/pkg", Qualified: "example.test/pkg.Value",
	}
	ref, err := encodeSymbolRef(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSymbolRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != want.SnapshotID || got.Base != want.Base || got.Scope != want.Scope || got.Path != want.Path || got.Position != want.Position || got.Qualified != want.Qualified {
		t.Fatalf("decoded identity = %#v", got)
	}
	if err := requireSymbolSnapshot(got, SnapshotRef{ID: want.SnapshotID}); err != nil {
		t.Fatal(err)
	}
	if err := requireSymbolSnapshot(got, SnapshotRef{ID: "sha256:new"}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("stale error = %v", err)
	}
}

func TestSymbolRefRejectsTamperingAndUnsafePaths(t *testing.T) {
	if _, err := encodeSymbolRef(symbolIdentity{SnapshotID: "s", Path: "../escape.go", Kind: "go.function", Qualified: "Value"}); err == nil {
		t.Fatal("encodeSymbolRef accepted escaping path")
	}
	ref, err := encodeSymbolRef(symbolIdentity{SnapshotID: "s", Path: "value.go", Kind: "go.function", Qualified: "Value"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(ref))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatal(err)
	}
	payload["qualified"] = "Tampered"
	decoded, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	tampered := SymbolRef(base64.RawURLEncoding.EncodeToString(decoded))
	if _, err := decodeSymbolRef(tampered); !errors.Is(err, errInvalidSymbolRef) {
		t.Fatalf("tampered error = %v", err)
	}
}
