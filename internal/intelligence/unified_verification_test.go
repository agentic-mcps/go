package intelligence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/agentic-mcps/go/internal/changeimpact"
	"github.com/agentic-mcps/go/internal/verification"
)

type collectingVerifier struct {
	analyzer verification.ChangeAnalyzer
}

func (v collectingVerifier) Collect(ctx context.Context, request verification.Request) (verification.Collection, error) {
	analysis, err := v.analyzer.Analyze(ctx, verification.ChangeOptions{
		Base: request.Base, Package: request.Package, MaxPackages: request.MaxPackages,
	})
	if err != nil {
		return verification.Collection{}, err
	}
	report := verification.NewReport("0.7.0-test", analysis.Repository)
	report.Change = analysis.Change
	report.Impact = analysis.Impact
	report.Risks = append(report.Risks, analysis.Risks...)
	report.Uncertainties = append(report.Uncertainties, analysis.Uncertainties...)
	return verification.Collection{
		Report: report, Analysis: analysis,
		Policy: verification.Policy{FailOn: request.FailOn, MinChangedCoverage: request.MinChangedCoverage},
	}, nil
}

func TestUnifiedVerifyAddsSnapshotDiagnosticsContractAndProvenance(t *testing.T) {
	root := snapshotRepository(t)
	base := snapshotGit(t, root, "rev-parse", "HEAD")
	core := newUnifiedVerificationTestCore(t, root, []Diagnostic{{
		Source: "compiler", Code: "example", Severity: "warning", Message: "review changed declaration",
		Location: Location{File: "main.go", Line: 3, Column: 1},
	}})
	contract, err := core.Begin(context.Background(), BeginRequest{
		Base: base, Goal: "change Value", Scope: "./...", AllowedPaths: []string{"go.mod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	checkpoint, err := core.Checkpoint(context.Background(), CheckpointRequest{
		ContractID: contract.ID, ExpectedSnapshot: contract.LatestSnapshot.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	core.recordContextProvenance("brief", checkpoint.Current)
	core.recordRefactorProvenance(RefactorResult{
		PlanID: "rfp_example", Operation: RefactorRename, Snapshot: checkpoint.Current, Applied: true,
	}, checkpoint.Previous.ID)

	report, err := core.Verify(context.Background(), verification.Request{
		Base: base, Package: "./...", FailOn: verification.FailOnNone, MaxPackages: 200,
		ContractID: contract.ID, ExpectedSnapshotID: checkpoint.Current.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != verification.SchemaVersion || report.ID == "" || report.Snapshot.CurrentID != checkpoint.Current.ID {
		t.Fatalf("report identity = %#v", report)
	}
	if report.Snapshot.ContractInitial != contract.InitialSnapshot.ID || report.Snapshot.ContractLatest != checkpoint.Current.ID || len(report.Snapshot.Transitions) != 1 {
		t.Fatalf("snapshot lineage = %#v", report.Snapshot)
	}
	if len(report.Providers) != 3 || len(report.Provenance.Context) != 1 || len(report.Provenance.Refactors) != 1 || !report.Provenance.Refactors[0].Applied {
		t.Fatalf("providers/provenance = %#v / %#v", report.Providers, report.Provenance)
	}
	diagnostics := evidenceForKind(t, report, verification.CheckDiagnostics)
	if diagnostics.Diagnostics == nil || diagnostics.Diagnostics.Total != 1 || diagnostics.Diagnostics.Items == nil {
		t.Fatalf("diagnostic evidence = %#v", diagnostics)
	}
	contractEvidence := evidenceForKind(t, report, verification.CheckContract)
	if contractEvidence.Contract == nil || contractEvidence.Contract.Forbidden != 1 || contractEvidence.Contract.ViolationsTotal != 1 {
		t.Fatalf("contract evidence = %#v", contractEvidence)
	}
	if report.Result.Status != verification.ResultFindings || report.Result.ExitCode != 1 || report.Result.BlockingFindings != 1 {
		t.Fatalf("contract result = %#v", report.Result)
	}
	current, err := core.CurrentVerification(context.Background())
	if err != nil || current.ID != report.ID {
		t.Fatalf("current verification = %q, error %v", current.ID, err)
	}
	linked, err := core.contracts.Load(context.Background(), checkpoint.Current.RepositoryID, contract.ID)
	if err != nil || linked.LatestVerification != report.ID {
		t.Fatalf("contract verification link = %q, error %v", linked.LatestVerification, err)
	}
}

func TestUnifiedVerifyRejectsStaleContractLineage(t *testing.T) {
	root := snapshotRepository(t)
	base := snapshotGit(t, root, "rev-parse", "HEAD")
	core := newUnifiedVerificationTestCore(t, root, []Diagnostic{})
	contract, err := core.Begin(context.Background(), BeginRequest{Base: base, Goal: "change Value", Scope: "./..."})
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, root, "main.go", "package fixture\n\nvar Value = 2\n")
	if _, err := core.Verify(context.Background(), verification.Request{
		Base: base, Package: "./...", ContractID: contract.ID,
	}); err == nil {
		t.Fatal("verification accepted a Change Contract from an older snapshot")
	}
}

func evidenceForKind(t *testing.T, report verification.Report, kind verification.CheckKind) verification.Evidence {
	t.Helper()
	for _, item := range report.Evidence {
		if item.Kind == kind {
			return item
		}
	}
	t.Fatalf("missing %s evidence: %#v", kind, report.Evidence)
	return verification.Evidence{}
}

func newUnifiedVerificationTestCore(t *testing.T, root string, diagnostics []Diagnostic) *Core {
	t.Helper()
	snapshots := newTestSnapshotter(t, root)
	artifacts, err := NewArtifactStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := NewContractStore(filepath.Join(t.TempDir(), "contracts"))
	if err != nil {
		t.Fatal(err)
	}
	refactors, err := NewRefactorStore(filepath.Join(t.TempDir(), "refactors"))
	if err != nil {
		t.Fatal(err)
	}
	changes, err := changeimpact.New(snapshots.workspace, snapshots.runner)
	if err != nil {
		t.Fatal(err)
	}
	semantic := &fakeSemanticProvider{
		identity: SemanticIdentity{Version: "v0.21.0", Capabilities: CapabilityManifest{Diagnostics: true}},
		reader:   &fakeSemanticReader{diagnostics: diagnostics},
	}
	core, err := newCore(
		snapshots.workspace, snapshots.runner, snapshots, semantic, artifacts, contracts, refactors,
		newTestVerificationStore(t), changes, collectingVerifier{analyzer: changes},
	)
	if err != nil {
		t.Fatal(err)
	}
	return core
}
