//nolint:govet // Schema tests keep decode failures adjacent to each document.
package intelligence

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/internal/verification"
	"github.com/google/jsonschema-go/jsonschema"
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

func TestPublishedFocusSchemaAndRepresentativeGolden(t *testing.T) {
	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", "focus-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.ID != FocusSchemaVersion {
		t.Fatalf("schema id = %q, want %q", schema.ID, FocusSchemaVersion)
	}
	inferred, err := jsonschema.For[FocusResult](nil)
	if err != nil {
		t.Fatal(err)
	}
	inferred.ID, inferred.Schema, inferred.Title = FocusSchemaVersion, "https://json-schema.org/draft/2020-12/schema", "Agentic Focus Evidence Pack"
	constSchemaVersion := any(FocusSchemaVersion)
	inferred.Properties["schema_version"] = &jsonschema.Schema{Const: &constSchemaVersion}
	inferredBytes, err := json.Marshal(inferred)
	if err != nil {
		t.Fatal(err)
	}
	var publishedDocument, inferredDocument any
	if err := json.Unmarshal(schemaBytes, &publishedDocument); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(inferredBytes, &inferredDocument); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(publishedDocument, inferredDocument) {
		t.Fatal("published focus schema differs from the Go result contract")
	}
	for _, field := range []string{"schema_version", "provider", "snapshot", "change", "impact", "risks", "uncertainties", "verification", "observed_packages", "complete"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("focus schema has no %q property", field)
		}
	}
	golden := representativeFocusResult()
	encoded, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join("testdata", "focus-v1.json")
	if os.Getenv("AGENTIC_GO_UPDATE_FOCUS_GOLDEN") == "1" {
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatal("focus golden does not match canonical Go JSON encoding")
	}
	var instance any
	if err := json.Unmarshal(want, &instance); err != nil {
		t.Fatal(err)
	}
	validationSchema := schema
	validationSchema.ID = ""
	resolved, err := validationSchema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(instance); err != nil {
		t.Fatalf("focus golden violates published schema: %v", err)
	}
	document := instance.(map[string]any)
	document["schema_version"] = "agentic.focus/v2"
	if err := resolved.Validate(document); err == nil {
		t.Fatal("focus schema accepted another schema version")
	}
	document["schema_version"] = FocusSchemaVersion
	document["unexpected"] = true
	if err := resolved.Validate(document); err == nil {
		t.Fatal("focus schema accepted an unknown top-level field")
	}
	var decoded FocusResult
	decoder := json.NewDecoder(bytes.NewReader(want))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
}

func representativeFocusResult() FocusResult {
	location := Location{File: "worker.go", Line: 7, Column: 6, EndLine: 7, EndColumn: 12}
	snapshot := SnapshotRef{ID: "sha256:" + strings.Repeat("1", 64), RepositoryID: "sha256:" + strings.Repeat("2", 64), Workspace: ".", RequestedBase: "HEAD~1", BaseCommit: strings.Repeat("3", 40), MergeBaseCommit: strings.Repeat("3", 40), HeadCommit: strings.Repeat("4", 40), ContentDigest: "sha256:" + strings.Repeat("5", 64), GoVersion: "go1.25", GoplsVersion: "v0.21.0", Capabilities: CapabilityManifest{WorkspaceSymbol: true, Hover: true, Definition: true, TypeDefinition: true, References: true, Implementation: true, DocumentSymbol: true, CallHierarchy: true, Diagnostics: true}, Build: BuildConfig{GOOS: "linux", GOARCH: "amd64", Tags: []string{}, Workspace: "go.work"}, Scope: "./..."}
	ref, _ := encodeSymbolRef(symbolIdentity{SnapshotID: snapshot.ID, Base: snapshot.RequestedBase, Scope: snapshot.Scope, Path: location.File, Kind: "go.type", Package: "example.com/focus", Qualified: "example.com/focus.Worker", Position: Position{Line: 6, Character: 5}})
	match := SymbolMatch{Ref: ref, Kind: "go.type", Name: "Worker", Qualified: "example.com/focus.Worker", Package: "example.com/focus", Location: location}
	symbol := &SymbolContext{SchemaVersion: ContextSchemaVersion, Provider: Provider{Name: "agentic-go-gopls", Version: "v0.21.0"}, Snapshot: snapshot, Symbol: match, Hover: "type Worker struct", Definitions: LocationSet{Items: []Location{location}, Total: 1}, TypeDefinitions: LocationSet{Items: []Location{}}, References: LocationSet{Items: []Location{}}, Implementations: SymbolSet{Items: []SymbolMatch{}}, RelatedTests: LocationSet{Items: []Location{}}, Diagnostics: []Diagnostic{}, Calls: CallSet{Items: []CallEdge{}}, Uncertainties: []Uncertainty{}}
	return FocusResult{SchemaVersion: FocusSchemaVersion, Provider: Provider{Name: "agentic-go-gopls", Version: "v0.21.0"}, Snapshot: snapshot, Change: verification.Change{Files: []verification.ChangedFile{}, Declarations: []verification.ChangedDeclaration{}}, Impact: verification.Impact{Packages: []verification.ImpactedPackage{}}, Risks: []verification.RiskArea{}, Uncertainties: []verification.Uncertainty{}, Verification: VerificationApplicability{ReportID: "verify_" + strings.Repeat("6", 64), Outcome: verification.ResultPass, Reasons: []string{"workspace snapshot differs"}, NextAction: "request verification for the current snapshot and policy", Present: true}, ObservedPackages: 1, Complete: true, Context: &FocusContext{Selection: FocusSelection{Query: "Worker"}, Candidates: []SymbolMatch{match}, Symbol: symbol, TestDeclarations: []SymbolMatch{}, Excerpts: []SourceExcerpt{{Location: location, Text: "type Worker struct{}\n"}}, CallSites: []FocusCallSite{}, Reasons: []string{"query resolved to its only workspace declaration"}, Uncertainties: []Uncertainty{}, EvidenceStates: []EvidenceState{{Facet: "related_tests", State: "examined_and_absent"}}, TypedEvidence: &TypedEvidence{Relationships: []GoRelationship{{Kind: "defined_type", Subject: "Worker", Object: "struct{}", Location: location, Evidence: "go/types defined type", Build: snapshot.Build, Availability: "available", Limits: []string{"does not imply behavioral equivalence"}}}, Uncertainties: []Uncertainty{}, Complete: true}}, PackID: strings.Repeat("a", 64), Refresh: &FocusRefresh{PreviousPackID: strings.Repeat("b", 64), Status: "replaced", Message: "previous evidence was reselected against the current observation"}}
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
