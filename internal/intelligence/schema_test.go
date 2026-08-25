package intelligence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPublishedContextPackSchemaIdentityAndRequiredFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "context-pack-v1alpha1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		ID         string                     `json:"$id"`
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
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
	pack := ContextPack{SchemaVersion: ContextSchemaVersion, Modules: []ModuleSummary{}, Packages: []PackageSummary{},
		Symbols: []SymbolMatch{}, Diagnostics: []Diagnostic{}, Guidance: []GuidanceRef{}, Risks: []RiskArea{},
		Uncertainties: []Uncertainty{}}
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
