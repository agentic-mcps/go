package intelligence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/ashwingopalsamy/agentic-go/internal/changeimpact"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

func TestCoreBeginPersistsNormalizedContract(t *testing.T) {
	root := snapshotRepository(t)
	core := newContractTestCore(t, root)
	contract, err := core.Begin(context.Background(), BeginRequest{
		Base:         "HEAD",
		Goal:         "Preserve this exact human goal.\nDo not interpret it.",
		FocusedPaths: []string{"main.go", "main.go"},
		AllowedPaths: []string{"main.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contract.SchemaVersion != ChangeSchemaVersion || !contract.Active || !validContractID(contract.ID) {
		t.Fatalf("contract identity = %#v", contract)
	}
	if contract.Scope != "./..." || contract.Goal != "Preserve this exact human goal.\nDo not interpret it." {
		t.Fatalf("normalized contract = %#v", contract)
	}
	if !reflect.DeepEqual(contract.FocusedPaths, []string{"main.go"}) || !reflect.DeepEqual(contract.AllowedPaths, []string{"main.go"}) {
		t.Fatalf("paths = focused %v allowed %v", contract.FocusedPaths, contract.AllowedPaths)
	}
	if contract.FocusedPackages == nil || contract.FocusedSymbols == nil || contract.Decisions == nil || contract.UnresolvedQuestions == nil || contract.Checkpoints == nil {
		t.Fatalf("contract has nil collections: %#v", contract)
	}
	current, err := core.CurrentChangeContract(context.Background())
	if err != nil || current.ID != contract.ID {
		t.Fatalf("current contract = %#v, error %v", current, err)
	}
	if status := snapshotGit(t, root, "status", "--porcelain=v1"); status != "" {
		t.Fatalf("begin polluted worktree: %q", status)
	}
}

func TestChangeContractResumesAcrossFreshCores(t *testing.T) {
	root := snapshotRepository(t)
	contractRoot := filepath.Join(t.TempDir(), "contracts")
	first := newContractTestCoreWithStore(t, root, contractRoot)
	contract, err := first.Begin(context.Background(), BeginRequest{
		Base: "HEAD", Goal: "carry structural context across processes", FocusedPaths: []string{"main.go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	second := newContractTestCoreWithStore(t, root, contractRoot)
	resumed, err := second.CurrentChangeContract(context.Background())
	if err != nil || resumed.ID != contract.ID || resumed.LatestSnapshot.ID != contract.LatestSnapshot.ID {
		t.Fatalf("resumed contract = %#v, error %v", resumed, err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	checkpoint, err := second.Checkpoint(context.Background(), CheckpointRequest{
		ContractID: contract.ID, ExpectedSnapshot: resumed.LatestSnapshot.ID,
		Decisions: []string{"Keep the exported variable"}, UnresolvedQuestions: []string{"Should this become a constant?"},
	})
	if err != nil {
		t.Fatal(err)
	}

	third := newContractTestCoreWithStore(t, root, contractRoot)
	handoff, err := third.CurrentChangeContract(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if handoff.LatestSnapshot.ID != checkpoint.Current.ID || len(handoff.Checkpoints) != 1 || len(handoff.Decisions) != 1 || len(handoff.UnresolvedQuestions) != 1 {
		t.Fatalf("fresh-core handoff = %#v", handoff)
	}
}

func TestCoreBeginRejectsInvalidInputs(t *testing.T) {
	root := snapshotRepository(t)
	core := newContractTestCore(t, root)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		request BeginRequest
	}{
		{name: "missing base", request: BeginRequest{Goal: "goal"}},
		{name: "missing goal", request: BeginRequest{Base: "HEAD"}},
		{name: "absolute path", request: BeginRequest{Base: "HEAD", Goal: "goal", FocusedPaths: []string{filepath.Join(root, "main.go")}}},
		{name: "outside path", request: BeginRequest{Base: "HEAD", Goal: "goal", AllowedPaths: []string{outside}}},
		{name: "invalid policy", request: BeginRequest{Base: "HEAD", Goal: "goal", Policies: StructuralPolicies{Dependency: "block"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := core.Begin(context.Background(), test.request); err == nil {
				t.Fatal("Begin unexpectedly succeeded")
			}
		})
	}
}

func TestCoreBeginRejectsStaleFocusedSymbol(t *testing.T) {
	root := snapshotRepository(t)
	core := newContractTestCore(t, root)
	snapshot, err := core.capture(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := encodeSymbolRef(symbolIdentity{
		SnapshotID: snapshot.ID, Base: "HEAD", Scope: "./...", Path: "main.go",
		Kind: "go.variable", Qualified: "example.test/snapshot.Value",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	if _, err := core.Begin(context.Background(), BeginRequest{Base: "HEAD", Goal: "goal", FocusedSymbols: []SymbolRef{ref}}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("stale focused symbol error = %v", err)
	}
}

func TestCoreCheckpointRecordsStructuralDrift(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "main_test.go", "package fixture\n\nfunc TestValue(t *testing.T) {}\n")
	snapshotGit(t, root, "add", "main_test.go")
	snapshotGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "add test")
	core := newContractTestCore(t, root)
	contract, err := core.Begin(context.Background(), BeginRequest{
		Base:         "HEAD",
		Goal:         "change the public value",
		FocusedPaths: []string{"main.go"},
		AllowedPaths: []string{"main.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	writeSnapshotFile(t, root, "generated.go", "// Code generated by fixture. DO NOT EDIT.\npackage fixture\n")
	writeSnapshotFile(t, root, "go.sum", "")
	if removeErr := os.Remove(filepath.Join(root, "main_test.go")); removeErr != nil {
		t.Fatal(removeErr)
	}

	checkpoint, err := core.Checkpoint(context.Background(), CheckpointRequest{
		ContractID:          contract.ID,
		ExpectedSnapshot:    contract.LatestSnapshot.ID,
		Decisions:           []string{"Keep Value exported", "Keep Value exported"},
		UnresolvedQuestions: []string{"Should this become a constant?\nConfirm with the maintainer."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.ID == "" || checkpoint.Current.ID == contract.LatestSnapshot.ID || !checkpoint.Complete {
		t.Fatalf("checkpoint identity = %#v", checkpoint)
	}
	codes := make([]string, 0, len(checkpoint.Violations))
	for _, violation := range checkpoint.Violations {
		codes = append(codes, violation.Code)
	}
	sort.Strings(codes)
	wantCodes := []string{"dependency_change", "exported_api_change", "generated_file_change", "outside_allowed_paths", "test_deletion"}
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("violation codes = %v, want %v", codes, wantCodes)
	}
	loaded, err := core.CurrentChangeContract(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LatestSnapshot.ID != checkpoint.Current.ID || len(loaded.Checkpoints) != 1 || len(loaded.Decisions) != 1 || len(loaded.UnresolvedQuestions) != 1 {
		t.Fatalf("updated contract = %#v", loaded)
	}
	if _, err := core.Checkpoint(context.Background(), CheckpointRequest{ContractID: contract.ID, ExpectedSnapshot: contract.LatestSnapshot.ID}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("stale checkpoint error = %v", err)
	}
}

func TestEvaluateCheckpointPoliciesReportsFocusModulesAndLimit(t *testing.T) {
	contract := ChangeContract{
		FocusedPaths:    []string{"internal/focus"},
		FocusedPackages: []string{"example.test/one/focus"},
		AllowedPaths:    []string{},
		Policies:        DefaultStructuralPolicies(),
	}
	analysis := verification.ChangeAnalysis{
		Change: verification.Change{
			Files:        []verification.ChangedFile{{Path: "other/value.go", Change: verification.ChangeModified}},
			Declarations: []verification.ChangedDeclaration{{Name: "Exported", CurrentLocation: &verification.Location{File: "other/value.go", Line: 3}}},
		},
		Files: []verification.SourceFile{{
			Change:         verification.ChangedFile{Path: "other/value.go", Change: verification.ChangeModified},
			CurrentContent: []byte("// Code generated by test. DO NOT EDIT.\npackage other\n"),
		}},
		Packages: []verification.ExecutionTarget{
			{ID: "example.test/one/other", ModulePath: "example.test/one", Distance: 0},
			{ID: "example.test/two/other", ModulePath: "example.test/two", Distance: 0},
		},
		Impact: verification.Impact{Packages: []verification.ImpactedPackage{
			{ID: "example.test/one/other", Distance: 0},
			{ID: "example.test/two/other", Distance: 0},
		}},
		ObservedPackages: 201,
		Complete:         false,
	}
	violations, uncertainties := evaluateCheckpointPolicies(contract, analysis)
	codes := make([]string, 0, len(violations))
	for _, violation := range violations {
		codes = append(codes, violation.Code)
	}
	sort.Strings(codes)
	want := []string{"cross_module_change", "generated_file_change", "outside_focus"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("violation codes = %v, want %v", codes, want)
	}
	if !containsUncertainty(uncertainties, "package_limit") || !containsUncertainty(uncertainties, "exported_api_unknown") {
		t.Fatalf("uncertainties = %#v", uncertainties)
	}
}

func containsUncertainty(uncertainties []Uncertainty, code string) bool {
	for _, uncertainty := range uncertainties {
		if uncertainty.Code == code {
			return true
		}
	}
	return false
}

func TestEvaluateCheckpointPoliciesOnlyReportsExportedAPIShapeChanges(t *testing.T) {
	base := []byte("package fixture\n\nfunc Exported(v int) int { return v }\n")
	bodyOnly := []byte("package fixture\n\nfunc Exported(v int) int { return v + 1 }\n")
	changedSignature := []byte("package fixture\n\nfunc Exported(v string) int { return len(v) }\n")
	makeAnalysis := func(current []byte) verification.ChangeAnalysis {
		return verification.ChangeAnalysis{Change: verification.Change{Declarations: []verification.ChangedDeclaration{{Kind: "function", Package: "example.test/fixture", Name: "Exported", Change: verification.ChangeModified, BaseLocation: &verification.Location{File: "main.go", Line: 3}, CurrentLocation: &verification.Location{File: "main.go", Line: 3}}}}, Files: []verification.SourceFile{{Change: verification.ChangedFile{Path: "main.go", Change: verification.ChangeModified}, BaseContent: base, CurrentContent: current}}}
	}
	contract := ChangeContract{Policies: DefaultStructuralPolicies()}
	violations, _ := evaluateCheckpointPolicies(contract, makeAnalysis(bodyOnly))
	for _, violation := range violations {
		if violation.Code == "exported_api_change" {
			t.Fatalf("body-only edit reported API change: %#v", violations)
		}
	}
	violations, _ = evaluateCheckpointPolicies(contract, makeAnalysis(changedSignature))
	found := false
	for _, violation := range violations {
		if violation.Code == "exported_api_change" {
			found = true
		}
	}
	if !found {
		t.Fatal("signature change did not report exported API change")
	}
}

func TestDeclarationShapeTracksExportedSurface(t *testing.T) {
	tests := []struct {
		name        string
		declaration verification.ChangedDeclaration
		base        string
		current     string
		wantChange  bool
	}{
		{name: "function body", declaration: verification.ChangedDeclaration{Kind: "function", Name: "Exported", Change: verification.ChangeModified}, base: "func Exported(v int) int { return v }", current: "func Exported(v int) int { return v + 1 }"},
		{name: "function signature", declaration: verification.ChangedDeclaration{Kind: "function", Name: "Exported", Change: verification.ChangeModified}, base: "func Exported(v int) int { return v }", current: "func Exported(v string) int { return len(v) }", wantChange: true},
		{name: "method body", declaration: verification.ChangedDeclaration{Kind: "method", Name: "Widget.Value", Change: verification.ChangeModified}, base: "type Widget struct{}\nfunc (Widget) Value() int { return 1 }", current: "type Widget struct{}\nfunc (Widget) Value() int { return 2 }"},
		{name: "method receiver", declaration: verification.ChangedDeclaration{Kind: "method", Name: "Widget.Value", Change: verification.ChangeModified}, base: "type Widget struct{}\nfunc (Widget) Value() int { return 1 }", current: "type Widget struct{}\nfunc (*Widget) Value() int { return 1 }", wantChange: true},
		{name: "type shape", declaration: verification.ChangedDeclaration{Kind: "type", Name: "Widget", Change: verification.ChangeModified}, base: "type Widget struct{ Value int }", current: "type Widget struct{ Value string }", wantChange: true},
		{name: "field shape", declaration: verification.ChangedDeclaration{Kind: "field", Name: "Widget.Value", Change: verification.ChangeModified}, base: "type Widget struct{ Value int }", current: "type Widget struct{ Value string }", wantChange: true},
		{name: "inferred variable", declaration: verification.ChangedDeclaration{Kind: "variable", Name: "Value", Change: verification.ChangeModified}, base: "var Value = 1", current: "var Value = \"one\"", wantChange: true},
		{name: "constant value", declaration: verification.ChangedDeclaration{Kind: "constant", Name: "Value", Change: verification.ChangeModified}, base: "const Value = 1", current: "const Value = 2", wantChange: true},
		{name: "exported addition", declaration: verification.ChangedDeclaration{Kind: "function", Name: "Added", Change: verification.ChangeAdded}, current: "func Added() {}", wantChange: true},
		{name: "exported deletion", declaration: verification.ChangedDeclaration{Kind: "function", Name: "Removed", Change: verification.ChangeDeleted}, base: "func Removed() {}", wantChange: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			declaration := test.declaration
			declaration.BaseLocation = &verification.Location{File: "main.go", Line: 3}
			declaration.CurrentLocation = &verification.Location{File: "main.go", Line: 3}
			before, beforeErr := declarationShape([]byte("package fixture\n"+test.base+"\n"), declaration, true)
			after, afterErr := declarationShape([]byte("package fixture\n"+test.current+"\n"), declaration, false)
			if beforeErr != nil || afterErr != nil {
				t.Fatalf("shape errors: before=%v after=%v", beforeErr, afterErr)
			}
			changed := before.Exported != after.Exported || before.Shape != after.Shape
			if changed != test.wantChange {
				t.Fatalf("shape change = %v, want %v; before=%#v after=%#v", changed, test.wantChange, before, after)
			}
		})
	}
}

func TestStructuralPolicyNearMisses(t *testing.T) {
	if withinAny("internal/foobar/value.go", []string{"internal/foo"}) {
		t.Fatal("path boundary matched a sibling prefix")
	}
	if !withinAny("internal/foo/value.go", []string{"internal/foo"}) {
		t.Fatal("contained child path did not match")
	}
	for _, source := range [][]byte{
		[]byte("// generated by fixture. DO NOT EDIT.\npackage fixture\n"),
		[]byte("// Code generated by fixture.\npackage fixture\n"),
		[]byte("const text = \"// Code generated by fixture. DO NOT EDIT.\"\n"),
	} {
		if isGeneratedSource(source) {
			t.Fatalf("near-miss generated marker matched %q", source)
		}
	}
}

func newContractTestCore(t *testing.T, root string) *Core {
	t.Helper()
	return newContractTestCoreWithStore(t, root, filepath.Join(t.TempDir(), "contracts"))
}

func newContractTestCoreWithStore(t *testing.T, root, contractRoot string) *Core {
	t.Helper()
	snapshots := newTestSnapshotter(t, root)
	artifacts, err := NewArtifactStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := NewContractStore(contractRoot)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := changeimpact.New(snapshots.workspace, snapshots.runner)
	if err != nil {
		t.Fatal(err)
	}
	semantic := &fakeSemanticProvider{
		identity: SemanticIdentity{Version: "v0.21.0", Capabilities: CapabilityManifest{Diagnostics: true}},
		reader:   &fakeSemanticReader{diagnostics: []Diagnostic{}},
	}
	core, err := newCore(snapshots.workspace, snapshots.runner, snapshots, semantic, artifacts, contracts, changes, fakeVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return core
}
