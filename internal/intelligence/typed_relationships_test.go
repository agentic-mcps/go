package intelligence

import (
	"context"
	"strings"
	"testing"
)

func TestTypedEvidenceInterfaceMethodsEmbeddingAndLifecycle(t *testing.T) {
	core, observation := typedFixture(t, `package fixture

type Runner interface { Run() }
type Embedded struct{}
type Worker struct { Embedded }
func (*Worker) Run() {}
func (*Worker) Close() {}
func use(w *Worker) { w.Close() }
type NearMiss struct{}
func (NearMiss) Close() {}
`)
	result := core.typedEvidence(context.Background(), observation, typedSymbol("Worker", "go.type", 5), nil)
	for _, kind := range []string{"defined_type", "embedded_field", "pointer_method", "pointer_interface_obligation", "lifecycle_site"} {
		if !hasRelationship(result.Relationships, kind, "") {
			t.Errorf("missing %s in %#v", kind, result)
		}
	}
	if hasRelationship(result.Relationships, "interface_obligation", "Runner") {
		t.Fatal("value Worker incorrectly reported as implementing Runner")
	}
	for _, relationship := range result.Relationships {
		if relationship.Location.File != "main.go" || relationship.Build.GOOS == "" || relationship.Availability != "available" {
			t.Fatalf("unattributed relationship = %#v", relationship)
		}
		if relationship.Kind == "lifecycle_site" && relationship.Subject != "Worker" {
			t.Fatalf("near-miss lifecycle site included: %#v", relationship)
		}
	}
}

func TestTypedEvidenceAliasesGenericsImplementationsAndExamples(t *testing.T) {
	core, observation := typedFixture(t, `package fixture

type Box[T interface{ ~int }] struct { Value T }
type IntBox = Box[int]
var _ Box[int]
`)
	box := core.typedEvidence(context.Background(), observation, typedSymbol("Box", "go.type", 3), []SymbolMatch{{Name: "ExampleBox", Qualified: "fixture.ExampleBox", Location: Location{File: "main_test.go", Line: 3, Column: 1}}, {Name: "helper", Qualified: "fixture.helper", Location: Location{File: "main_test.go", Line: 8, Column: 1}}})
	if !hasRelationship(box.Relationships, "generic_parameter", "~int") || !hasRelationship(box.Relationships, "generic_origin", "Box") || !hasRelationship(box.Relationships, "typed_example", "ExampleBox") {
		t.Fatalf("generic/example evidence = %#v", box)
	}
	if hasRelationship(box.Relationships, "typed_example", "helper") || hasRelationship(box.Relationships, "existing_implementation", "") || hasRelationship(box.Relationships, "type_alias", "") {
		t.Fatalf("near-miss evidence included = %#v", box)
	}
	alias := core.typedEvidence(context.Background(), observation, typedSymbol("IntBox", "go.type", 4), nil)
	if !hasRelationship(alias.Relationships, "type_alias", "") || !hasRelationship(alias.Relationships, "generic_origin", "Box") {
		t.Fatalf("alias evidence = %#v", alias)
	}
	withImplementation := typedSymbol("Box", "go.type", 3)
	withImplementation.Implementations.Items = []SymbolMatch{{Qualified: "fixture.IntBox", Location: Location{File: "main.go", Line: 4, Column: 1}}}
	implemented := core.typedEvidence(context.Background(), observation, withImplementation, nil)
	if !hasRelationship(implemented.Relationships, "existing_implementation", "fixture.IntBox") {
		t.Fatalf("implementation evidence = %#v", implemented)
	}
}

func TestTypedEvidenceMarksPartialTypingAndExcludesBuildVariants(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "main.go", "package fixture\n\ntype Worker struct { Missing }\n")
	writeSnapshotFile(t, root, "ignored.go", "//go:build never\n\npackage fixture\n\ntype Hidden struct{}\n")
	snapshotter := newTestSnapshotter(t, root)
	core := newTestCore(t, snapshotter, &fakeSemanticReader{})
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observation.release)
	result := core.typedEvidence(context.Background(), &observation, typedSymbol("Worker", "go.type", 3), nil)
	if result.Complete || !typedHasUncertainty(result.Uncertainties, "typed.partial") || !typedHasUncertainty(result.Uncertainties, "typed.build_variants_unexamined") {
		t.Fatalf("partial evidence = %#v", result)
	}
	if hasRelationship(result.Relationships, "defined_type", "Hidden") {
		t.Fatalf("inactive build variant included: %#v", result)
	}
}

func TestFocusFileAndPackageSelectionReturnBoundedTypedDeclarations(t *testing.T) {
	core, observation := typedFixture(t, "package fixture\n\ntype Worker struct{}\ntype Other struct{}\n")
	byFile, err := core.focusFileOrPackage(context.Background(), observation, FocusRequest{FocusFile: "main.go", MaxBytes: DefaultBriefBytes})
	if err != nil {
		t.Fatal(err)
	}
	if len(byFile.Candidates) != 2 || byFile.Candidates[0].Ref == "" || byFile.TypedEvidence == nil {
		t.Fatalf("file focus = %#v", byFile)
	}
	byPackage, err := core.focusFileOrPackage(context.Background(), observation, FocusRequest{FocusPackage: "fixture", MaxBytes: DefaultBriefBytes})
	if err != nil {
		t.Fatal(err)
	}
	if len(byPackage.Candidates) != 2 || byPackage.Selection.Package != "fixture" {
		t.Fatalf("package focus = %#v", byPackage)
	}
	request := FocusRequest{Base: "HEAD", FocusFile: "main.go", Query: "Worker"}
	if err := normalizeFocusRequest(&request); err == nil {
		t.Fatal("multiple selectors accepted")
	}
}

func typedFixture(t *testing.T, source string) (*Core, *snapshotObservation) {
	t.Helper()
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "main.go", source)
	snapshotter := newTestSnapshotter(t, root)
	core := newTestCore(t, snapshotter, &fakeSemanticReader{})
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observation.release)
	return core, &observation
}

func typedSymbol(name, kind string, line int) *SymbolContext {
	return &SymbolContext{Symbol: SymbolMatch{Name: name, Qualified: "fixture." + name, Kind: kind, Location: Location{File: "main.go", Line: line, Column: 6}}, Implementations: SymbolSet{Items: []SymbolMatch{}}, Uncertainties: []Uncertainty{}}
}

func hasRelationship(items []GoRelationship, kind, contains string) bool {
	for _, item := range items {
		if item.Kind == kind && (contains == "" || strings.Contains(item.Object, contains)) {
			return true
		}
	}
	return false
}

func typedHasUncertainty(items []Uncertainty, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}
