package intelligence

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPublishedContextPackSchemaIdentityAndRequiredFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "context-pack-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		ID         string                     `json:"$id"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.ID != ContextSchemaVersion {
		t.Fatalf("schema id = %q, want %q", schema.ID, ContextSchemaVersion)
	}
	want := []string{"schema_version", "provider", "snapshot", "modules", "packages", "symbols", "diagnostics", "guidance", "risks", "uncertainties", "totals", "truncated"}
	if !reflect.DeepEqual(schema.Required, want) {
		t.Fatalf("required fields = %v, want %v", schema.Required, want)
	}
	for _, field := range want {
		if _, found := schema.Properties[field]; !found {
			t.Fatalf("schema has no %q property", field)
		}
	}
}

func TestContextPackJSONKeepsCollectionsNonNull(t *testing.T) {
	pack := ContextPack{
		SchemaVersion: ContextSchemaVersion, Modules: []ModuleSummary{}, Packages: []PackageSummary{},
		Symbols: []SymbolMatch{}, Diagnostics: []Diagnostic{}, Guidance: []GuidanceRef{}, Risks: []RiskArea{},
		Uncertainties: []Uncertainty{},
	}
	encoded, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"modules", "packages", "symbols", "diagnostics", "guidance", "risks", "uncertainties"} {
		if document[field] == nil {
			t.Fatalf("%s encoded as null", field)
		}
	}
}

func TestPublishedChangeContractSchemaIdentityAndRequiredFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "change-contract-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		ID         string                     `json:"$id"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.ID != ChangeSchemaVersion {
		t.Fatalf("schema id = %q, want %q", schema.ID, ChangeSchemaVersion)
	}
	want := []string{
		"schema_version", "id", "repository_id", "goal", "base", "scope",
		"initial_snapshot", "latest_snapshot", "focused_paths", "focused_packages",
		"focused_symbols", "allowed_paths", "policies", "decisions",
		"unresolved_questions", "checkpoints", "active", "created_at", "updated_at",
	}
	if !reflect.DeepEqual(schema.Required, want) {
		t.Fatalf("required fields = %v, want %v", schema.Required, want)
	}
	for _, field := range want {
		if _, found := schema.Properties[field]; !found {
			t.Fatalf("schema has no %q property", field)
		}
	}
}

func TestChangeContractJSONKeepsCollectionsNonNull(t *testing.T) {
	contract := ChangeContract{
		FocusedPaths: []string{}, FocusedPackages: []string{}, FocusedSymbols: []SymbolRef{},
		AllowedPaths: []string{}, Decisions: []Decision{}, UnresolvedQuestions: []string{}, Checkpoints: []CheckpointRef{},
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"focused_paths", "focused_packages", "focused_symbols", "allowed_paths", "decisions", "unresolved_questions", "checkpoints"} {
		if document[field] == nil {
			t.Fatalf("%s encoded as null", field)
		}
	}
}

func TestFrozenDomainGoldensRoundTripExactly(t *testing.T) {
	tests := []struct {
		target any
		name   string
	}{
		{name: "context-pack-v1.json", target: &ContextPack{}},
		{name: "change-contract-v1.json", target: &ChangeContract{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join("testdata", test.name))
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.DisallowUnknownFields()
			if decodeErr := decoder.Decode(test.target); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			roundTrip, err := json.MarshalIndent(test.target, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			roundTrip = append(roundTrip, '\n')
			if !bytes.Equal(roundTrip, encoded) {
				t.Fatal("golden does not match the canonical Go JSON encoding")
			}
		})
	}
}

func TestArchivedSchemasRemainByteStable(t *testing.T) {
	tests := []struct {
		name   string
		digest string
	}{
		{name: "context-pack-v1alpha1.json", digest: "802555b1d7cea9c31687a3ce3c0a1ae01bdde59aba8cf0fe67f317c58d75db7a"},
		{name: "change-contract-v1alpha1.json", digest: "5cc742b3f0b3cd8083dae731ebfceec7cf04bda1527d8c28821145cfc2b0d606"},
		{name: "verification-report-v1alpha1.json", digest: "355ed3ad7a7cec518d5d937bcc0b59e807443166aa464edc937c998de5188dc4"},
		{name: "verification-report-v1beta1.json", digest: "7c65e0f8c1cfeb4ce8dc484d7c79545faa0999506f8d201ce93b878f0dddf722"},
	}
	for _, test := range tests {
		encoded, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "archive", test.name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != test.digest {
			t.Fatalf("%s digest = %s, want %s", test.name, got, test.digest)
		}
	}
}
