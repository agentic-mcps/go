//nolint:govet // Focus tests keep setup errors close to their assertions.
package intelligence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/verification"
)

func TestVerificationApplicabilitySeparatesOutcomeFromApplicability(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	core := newTestCore(t, snapshotter, &fakeSemanticReader{})
	snapshot, err := core.capture(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	request := FocusRequest{Base: "HEAD", Scope: "./..."}
	if err := normalizeFocusRequest(&request); err != nil {
		t.Fatal(err)
	}
	identity := focusIdentity(request)
	report := verification.NewReport("test", verification.Repository{
		RequestedBase: "HEAD", BaseCommit: snapshot.BaseCommit, MergeBaseCommit: snapshot.MergeBaseCommit,
		HeadCommit: snapshot.HeadCommit, SnapshotID: snapshot.ID,
	})
	report.Snapshot.CurrentID = snapshot.ID
	report.Findings = append(report.Findings, verification.Finding{Kind: "analysis", Severity: verification.SeverityError, Message: "example finding"})
	if err := report.Finalize(verification.Policy{FailOn: verification.FailOnError}); err != nil {
		t.Fatal(err)
	}
	if err := core.verifications.saveFocus(context.Background(), snapshot.RepositoryID, report, verificationFocusMetadata{Snapshot: snapshot, Request: identity}); err != nil {
		t.Fatal(err)
	}

	got, err := core.verificationApplicability(context.Background(), snapshot, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || !got.Applicable || got.Outcome == verification.ResultPass || len(got.Reasons) != 0 {
		t.Fatalf("verificationApplicability() = %#v, want applicable non-pass report", got)
	}
	changed := identity
	changed.Race = true
	got, err = core.verificationApplicability(context.Background(), snapshot, changed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Applicable || len(got.Reasons) == 0 {
		t.Fatalf("verificationApplicability(policy mismatch) = %#v", got)
	}
	checks := []struct {
		name   string
		modify func(*SnapshotRef, *focusPolicyIdentity)
		want   string
	}{
		{name: "base", modify: func(_ *SnapshotRef, request *focusPolicyIdentity) { request.Base = "HEAD~1" }, want: "base or resolved commit"},
		{name: "scope", modify: func(_ *SnapshotRef, request *focusPolicyIdentity) { request.Scope = "./internal/..." }, want: "package scope"},
		{name: "snapshot", modify: func(snapshot *SnapshotRef, _ *focusPolicyIdentity) { snapshot.ID = "sha256:different" }, want: "workspace snapshot"},
		{name: "build", modify: func(snapshot *SnapshotRef, _ *focusPolicyIdentity) { snapshot.Build.GOOS = "different" }, want: "build configuration"},
		{name: "provider", modify: func(snapshot *SnapshotRef, _ *focusPolicyIdentity) { snapshot.GoplsVersion = "different" }, want: "semantic provider"},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			current, requested := snapshot, identity
			check.modify(&current, &requested)
			result, err := core.verificationApplicability(context.Background(), current, requested)
			if err != nil {
				t.Fatal(err)
			}
			if result.Applicable || !containsReason(result.Reasons, check.want) {
				t.Fatalf("verificationApplicability(%s mismatch) = %#v, want reason containing %q", check.name, result, check.want)
			}
		})
	}
}

func TestFocusPackPersistsDeliveredEvidenceAndRefreshSelection(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nfunc Value() {}\n")
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{search: semanticSymbols{Items: []SymbolMatch{{Name: "Value", Qualified: "fixture.Value", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 3, Column: 1}}}}}
	core := newTestCore(t, snapshotter, reader)
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	focused := &FocusContext{Selection: FocusSelection{Query: "Value"}, Candidates: []SymbolMatch{}, Symbol: &SymbolContext{Symbol: SymbolMatch{Name: "Value", Qualified: "fixture.Value", Kind: "go.function", Location: Location{File: "main.go", Line: 3, Column: 1}, Ref: "current"}}, TestDeclarations: []SymbolMatch{}, Excerpts: []SourceExcerpt{{Location: Location{File: "main.go", Line: 3, Column: 1}, Text: "func Value() {}\n"}}, CallSites: []FocusCallSite{}, Reasons: []string{}, Uncertainties: []Uncertainty{}, EvidenceStates: []EvidenceState{}}
	id, err := core.saveFocusPack(observation.snapshot, FocusRequest{MaxBytes: DefaultBriefBytes}, focused.Selection, focused)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := core.loadFocusPack(id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Selected == nil || stored.Selected.Qualified != "fixture.Value" || len(stored.Delivered) != 2 || stored.Request.Query != "Value" {
		t.Fatalf("stored pack = %#v", stored)
	}
	resolved, err := core.resolveRefreshSymbol(context.Background(), &observation, *stored.Selected)
	if err != nil || len(resolved) != 1 || resolved[0].Ref == "" {
		t.Fatalf("resolved = %#v, err %v", resolved, err)
	}
}

func TestFocusRefreshRejectsChangedSelectionAndExpiredMetadata(t *testing.T) {
	request := FocusRequest{Base: "HEAD", PreviousPackID: strings.Repeat("a", 64), Query: "changed"}
	if err := normalizeFocusRequest(&request); err == nil {
		t.Fatal("normalizeFocusRequest accepted a changed selection during refresh")
	}
	store, err := NewArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.Put("snapshot", "focus-pack/v1", []byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, artifact.ID)
	old := time.Now().Add(-focusPackTTL - time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := store.getFocusPack(artifact.ID); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expired focus pack error = %v", err)
	}
}

func TestUnresolvedRefreshDoesNotClaimDeletion(t *testing.T) {
	context := unresolvedRefreshContext(&storedFocusPack{Request: storedFocusRequest{Query: "Value"}}, "not found", nil)
	if len(context.EvidenceStates) != 1 || context.EvidenceStates[0].State != "unavailable" || strings.Contains(strings.Join(context.Reasons, " "), "confirmed deletion") {
		t.Fatalf("unresolved refresh = %#v", context)
	}
}

func TestFocusWorkflowBeforeEditRefreshAndVerificationStaleness(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "worker.go", "package fixture\n\ntype Worker struct{}\n")
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{
		search: semanticSymbols{Items: []SymbolMatch{{Name: "Worker", Qualified: "fixture.Worker", Kind: "go.type", Package: "fixture", Location: Location{File: "worker.go", Line: 3, Column: 6}}}},
		symbol: SymbolMatch{Name: "Worker", Qualified: "fixture.Worker", Kind: "go.type", Package: "fixture", Location: Location{File: "worker.go", Line: 3, Column: 6}},
	}
	core := newTestCore(t, snapshotter, reader)
	seed, err := core.capture(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	analysis := verification.ChangeAnalysis{Repository: verification.Repository{RequestedBase: "HEAD", BaseCommit: seed.BaseCommit, MergeBaseCommit: seed.MergeBaseCommit, HeadCommit: seed.HeadCommit}, Change: verification.Change{Files: []verification.ChangedFile{}, Declarations: []verification.ChangedDeclaration{}}, Impact: verification.Impact{Packages: []verification.ImpactedPackage{}}, Risks: []verification.RiskArea{}, Uncertainties: []verification.Uncertainty{}, Complete: true}
	core.changes = fakeChangeAnalyzer{analysis: analysis}
	request := FocusRequest{Base: "HEAD", Scope: "./...", Query: "Worker", MaxBytes: DefaultBriefBytes}
	before, err := core.Focus(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if before.PackID == "" || before.Context == nil || before.Context.Symbol == nil {
		t.Fatalf("before edit = %#v", before)
	}
	oldRef := before.Context.Symbol.Symbol.Ref
	report := verification.NewReport("test", verification.Repository{RequestedBase: "HEAD", BaseCommit: before.Snapshot.BaseCommit, MergeBaseCommit: before.Snapshot.MergeBaseCommit, HeadCommit: before.Snapshot.HeadCommit, SnapshotID: before.Snapshot.ID})
	report.Snapshot.CurrentID = before.Snapshot.ID
	if err := report.Finalize(verification.Policy{FailOn: verification.FailOnError}); err != nil {
		t.Fatal(err)
	}
	if err := core.verifications.saveFocus(context.Background(), before.Snapshot.RepositoryID, report, verificationFocusMetadata{Snapshot: before.Snapshot, Request: focusIdentity(normalizedFocusRequest(t, request))}); err != nil {
		t.Fatal(err)
	}
	verified, err := core.Focus(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Verification.Applicable || verified.Verification.Outcome != verification.ResultPass {
		t.Fatalf("before-edit verification = %#v", verified.Verification)
	}
	writeSnapshotFile(t, root, "worker.go", "package fixture\n\n// shifted\ntype Worker struct{}\n")
	reader.search.Items[0].Location.Line = 4
	reader.symbol.Location.Line = 4
	after, err := core.Focus(context.Background(), FocusRequest{Base: "HEAD", Scope: "./...", PreviousPackID: before.PackID})
	if err != nil {
		t.Fatal(err)
	}
	if after.Refresh == nil || after.Refresh.Status != "replaced" || after.PackID == before.PackID || after.Context == nil || after.Context.Symbol.Symbol.Location.Line != 4 || after.Context.Symbol.Symbol.Ref == oldRef {
		t.Fatalf("after edit = %#v", after)
	}
	if after.Verification.Applicable || !containsReason(after.Verification.Reasons, "workspace snapshot") {
		t.Fatalf("verification applicability = %#v", after.Verification)
	}
	if _, err := core.Symbol(context.Background(), SymbolRequest{Ref: oldRef, MaxBytes: DefaultSymbolBytes}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("old Symbol Ref error = %v", err)
	}
}

func normalizedFocusRequest(t *testing.T, request FocusRequest) FocusRequest {
	t.Helper()
	if err := normalizeFocusRequest(&request); err != nil {
		t.Fatal(err)
	}
	return request
}

func containsReason(reasons []string, substring string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, substring) {
			return true
		}
	}
	return false
}

func TestVerificationApplicabilityHandlesMissingAndLegacyMetadata(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	core := newTestCore(t, snapshotter, &fakeSemanticReader{})
	snapshot, err := core.capture(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	request := FocusRequest{Base: "HEAD", Scope: "./..."}
	if err := normalizeFocusRequest(&request); err != nil {
		t.Fatal(err)
	}
	missing, err := core.verificationApplicability(context.Background(), snapshot, focusIdentity(request))
	if err != nil || missing.Present || missing.Applicable {
		t.Fatalf("missing applicability = %#v, %v", missing, err)
	}
	report := verification.NewReport("test", verification.Repository{SnapshotID: snapshot.ID})
	report.Snapshot.CurrentID = snapshot.ID
	if err := report.Finalize(verification.Policy{}); err != nil {
		t.Fatal(err)
	}
	if err := core.verifications.Save(context.Background(), snapshot.RepositoryID, report); err != nil {
		t.Fatal(err)
	}
	legacy, err := core.verificationApplicability(context.Background(), snapshot, focusIdentity(request))
	if err != nil || !legacy.Present || legacy.Applicable || len(legacy.Reasons) != 1 {
		t.Fatalf("legacy applicability = %#v, %v", legacy, err)
	}
}

func TestFocusContextReturnsAmbiguousCandidatesBeforeExpansion(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{search: semanticSymbols{Items: []SymbolMatch{
		{Name: "A", Qualified: "fixture.A", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 3, Column: 1}},
		{Name: "B", Qualified: "fixture.B", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 3, Column: 1}},
	}}}
	core := newTestCore(t, snapshotter, reader)
	core.semantic.(*fakeSemanticProvider).identity.Capabilities.CallHierarchy = true
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	result, err := core.focusContext(context.Background(), &observation, FocusRequest{Query: "value", MaxBytes: DefaultBriefBytes})
	if err != nil {
		t.Fatal(err)
	}
	if result.Symbol != nil || len(result.Candidates) != 2 || len(result.Reasons) == 0 || !hasEvidenceState(result.EvidenceStates, "relationships", "unexamined") {
		t.Fatalf("focusContext(ambiguous query) = %#v", result)
	}
}

func hasEvidenceState(states []EvidenceState, facet, state string) bool {
	for _, candidate := range states {
		if candidate.Facet == facet && candidate.State == state {
			return true
		}
	}
	return false
}

func TestFocusContextIncludesObservedExcerptAndDirectCallSite(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nfunc Value() {}\nfunc Caller() { Value() }\n")
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{
		symbol: SymbolMatch{Name: "Value", Qualified: "fixture.Value", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 3, Column: 1, EndLine: 3, EndColumn: 16}},
		calls:  semanticCalls{Items: []CallEdge{{Direction: "incoming", Symbol: SymbolMatch{Name: "Caller", Qualified: "fixture.Caller", Kind: "go.function", Package: "fixture", Location: Location{File: "main.go", Line: 4, Column: 1}}, CallSites: []Location{{File: "main.go", Line: 4, Column: 17, EndLine: 4, EndColumn: 24}}}}},
	}
	core := newTestCore(t, snapshotter, reader)
	core.semantic.(*fakeSemanticProvider).identity.Capabilities.CallHierarchy = true
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	result, err := core.focusContext(context.Background(), &observation, FocusRequest{Position: &SourcePosition{File: "main.go", Line: 3, Column: 6}, MaxBytes: DefaultBriefBytes})
	if err != nil {
		t.Fatal(err)
	}
	if result.Symbol == nil || len(result.Excerpts) == 0 || len(result.CallSites) != 1 || result.CallSites[0].Location.Column != 17 {
		t.Fatalf("focusContext(position) = %#v", result)
	}
}

func TestFocusQueryAndRelatedTestConvertUTF8ColumnsToUTF16(t *testing.T) {
	root := snapshotRepository(t)
	writeSnapshotFile(t, root, "main.go", "package fixture\nfunc (π *Worker) Work() {}\ntype Worker struct{}\n")
	writeSnapshotFile(t, root, "main_test.go", "package fixture\nfunc TestWork(){ _ = π; Work() }\n")
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{
		search: semanticSymbols{Items: []SymbolMatch{{Name: "Work", Qualified: "fixture.(*Worker).Work", Kind: "go.method", Package: "fixture", Location: Location{File: "main.go", Line: 2, Column: 14}}}},
		symbol: SymbolMatch{Name: "Work", Qualified: "fixture.(*Worker).Work", Kind: "go.method", Package: "fixture", Location: Location{File: "main.go", Line: 2, Column: 14}},
	}
	core := newTestCore(t, snapshotter, reader)
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	if _, err := core.focusContext(context.Background(), &observation, FocusRequest{Query: "Work", MaxBytes: DefaultBriefBytes}); err != nil {
		t.Fatal(err)
	}
	if reader.position.Line != 1 || reader.position.Character != 12 {
		t.Fatalf("query follow-up position = %#v", reader.position)
	}
	reader.symbol = SymbolMatch{Name: "TestWork", Qualified: "fixture.TestWork", Kind: "go.function", Package: "fixture", Location: Location{File: "main_test.go", Line: 2, Column: 1}}
	if _, uncertainties := core.focusTestDeclarations(context.Background(), reader, &observation, []Location{{File: "main_test.go", Line: 2, Column: 25}}); len(uncertainties) != 0 {
		t.Fatalf("test uncertainties = %#v", uncertainties)
	}
	if reader.position.Line != 1 || reader.position.Character != 23 {
		t.Fatalf("test follow-up position = %#v", reader.position)
	}
}

func TestFocusUnsupportedDocumentSymbolsReturnsUnavailableEvidence(t *testing.T) {
	root := snapshotRepository(t)
	snapshotter := newTestSnapshotter(t, root)
	reader := &fakeSemanticReader{}
	core := newTestCore(t, snapshotter, reader)
	core.semantic.(*fakeSemanticProvider).identity.Capabilities.DocumentSymbol = false
	observation, err := core.observe(context.Background(), "HEAD", "./...", "")
	if err != nil {
		t.Fatal(err)
	}
	defer observation.release()
	result, err := core.focusContext(context.Background(), &observation, FocusRequest{Position: &SourcePosition{File: "main.go", Line: 1, Column: 1}, MaxBytes: DefaultBriefBytes})
	if err != nil {
		t.Fatal(err)
	}
	if result.Symbol != nil || !hasEvidenceState(result.EvidenceStates, "declaration_relationships", "unavailable") {
		t.Fatalf("unsupported focus = %#v", result)
	}
}
