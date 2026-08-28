package intelligence

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

const maximumProvenanceReferences = 64

// Verify brackets the existing executed-evidence collector with one immutable
// semantic snapshot, then adds neutral diagnostics, optional Change Contract
// compliance, provider capabilities, and operation provenance before policy
// evaluation.
func (c *Core) Verify(ctx context.Context, request verification.Request) (verification.Report, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if request.ContractID != "" {
		release, err := c.lockStateMutation(ctx)
		if err != nil {
			return verification.Report{}, err
		}
		defer release()
	}
	if strings.TrimSpace(request.Base) == "" {
		return verification.Report{}, fmt.Errorf("base is required")
	}
	if request.Package == "" {
		request.Package = "./..."
	}
	snapshot, err := c.capture(ctx, request.Base, request.Package, request.ExpectedSnapshotID)
	if err != nil {
		return verification.Report{}, err
	}
	var contract *ChangeContract
	if request.ContractID != "" {
		loaded, loadErr := c.contracts.Load(ctx, snapshot.RepositoryID, request.ContractID)
		if loadErr != nil {
			return verification.Report{}, loadErr
		}
		if loaded.Base != request.Base || loaded.Scope != request.Package {
			return verification.Report{}, fmt.Errorf("change contract base and package scope do not match the verification request")
		}
		if loaded.LatestSnapshot.ID != snapshot.ID {
			return verification.Report{}, fmt.Errorf("%w: contract expects %s, observed %s", ErrSnapshotChanged, loaded.LatestSnapshot.ID, snapshot.ID)
		}
		contract = &loaded
	}

	collected, err := c.verifier.Collect(ctx, request)
	if err != nil {
		return verification.Report{}, err
	}
	if collected.Report.Repository.BaseCommit != snapshot.BaseCommit ||
		collected.Report.Repository.MergeBaseCommit != snapshot.MergeBaseCommit ||
		collected.Report.Repository.HeadCommit != snapshot.HeadCommit {
		return verification.Report{}, fmt.Errorf("%w while collecting verification evidence", ErrSnapshotChanged)
	}
	report := collected.Report
	report.Snapshot = verification.SnapshotLineage{
		CurrentID: snapshot.ID, ExpectedID: request.ExpectedSnapshotID,
		Transitions: make([]verification.SnapshotTransition, 0),
	}
	report.Providers = verificationProviders(snapshot, report.Provider.Version)
	report.Provenance = c.verificationProvenance()

	if err := c.attachDiagnosticEvidence(ctx, &report, snapshot, collected.Analysis); err != nil {
		return verification.Report{}, err
	}
	if contract != nil {
		c.attachContractEvidence(&report, *contract, collected.Analysis)
	}
	if _, err := c.snapshots.Validate(ctx, snapshot); err != nil {
		return verification.Report{}, err
	}
	if err := report.Finalize(collected.Policy); err != nil {
		return verification.Report{}, err
	}
	if err := c.verifications.Save(ctx, snapshot.RepositoryID, report); err != nil {
		return verification.Report{}, fmt.Errorf("persisting latest verification: %w", err)
	}
	if contract != nil {
		contract.LatestVerification = report.ID
		contract.UpdatedAt = time.Now().UTC()
		if err := c.contracts.Save(ctx, *contract); err != nil {
			return verification.Report{}, fmt.Errorf("linking verification to Change Contract: %w", err)
		}
	}
	return report, nil
}

// CurrentVerification returns the repository's latest private finalized report
// without exposing its cache path.
func (c *Core) CurrentVerification(ctx context.Context) (verification.Report, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	snapshot, err := c.capture(ctx, "", "./...", "")
	if err != nil {
		return verification.Report{}, err
	}
	return c.verifications.Current(ctx, snapshot.RepositoryID)
}

func (c *Core) attachDiagnosticEvidence(ctx context.Context, report *verification.Report, snapshot SnapshotRef, analysis verification.ChangeAnalysis) error {
	check := verification.Check{
		ID: "diagnostics", Kind: verification.CheckDiagnostics, Required: true,
		Targets: changedDiagnosticPaths(analysis), Reason: "collect compiler and pinned semantic diagnostics for changed Go files",
	}
	report.Plan = append(report.Plan, check)
	started := time.Now()
	if !snapshot.Capabilities.Diagnostics {
		report.Evidence = append(report.Evidence, verification.Evidence{
			CheckID: check.ID, Kind: check.Kind, Status: verification.EvidenceError,
			DurationMS: time.Since(started).Milliseconds(), Summary: "semantic diagnostics are unavailable",
			Error: "the negotiated semantic provider does not support diagnostics",
		})
		report.Uncertainties = append(report.Uncertainties, verification.Uncertainty{
			Code: "semantic.diagnostics_unavailable", CheckID: check.ID,
			Message: "the negotiated semantic provider does not support focused diagnostics", Locations: []verification.Location{},
		})
		return nil
	}
	diagnostics, uncertainties, err := c.checkpointDiagnostics(ctx, snapshot, analysis)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		report.Evidence = append(report.Evidence, verification.Evidence{
			CheckID: check.ID, Kind: check.Kind, Status: verification.EvidenceError,
			DurationMS: time.Since(started).Milliseconds(), Summary: "semantic diagnostics could not produce trustworthy evidence",
			Error: "focused semantic diagnostics failed",
		})
		report.Uncertainties = append(report.Uncertainties, verification.Uncertainty{
			Code: "semantic.diagnostics_error", CheckID: check.ID,
			Message: "focused semantic diagnostics could not be collected", Locations: []verification.Location{},
		})
		return nil
	}
	items := make([]verification.Diagnostic, 0, len(diagnostics))
	errorsCount, warnings := 0, 0
	for _, item := range diagnostics {
		severity := strings.ToLower(item.Severity)
		switch severity {
		case "error":
			errorsCount++
		case "warning":
			warnings++
		}
		items = append(items, verification.Diagnostic{
			Source: item.Source, Code: item.Code, Severity: severity, Message: item.Message,
			Location: verification.Location{File: item.Location.File, Line: item.Location.Line, Col: item.Location.Column},
		})
	}
	status := verification.EvidencePassed
	if len(items) > 0 {
		status = verification.EvidenceFailed
		report.Uncertainties = append(report.Uncertainties, verification.Uncertainty{
			Code: "semantic.diagnostics_current_only", CheckID: check.ID,
			Message: "current diagnostics are evidence but are not classified as introduced without a base semantic comparison", Locations: []verification.Location{},
		})
	}
	for _, item := range uncertainties {
		report.Uncertainties = append(report.Uncertainties, verificationUncertainty(item, check.ID))
	}
	report.Evidence = append(report.Evidence, verification.Evidence{
		CheckID: check.ID, Kind: check.Kind, Status: status, DurationMS: time.Since(started).Milliseconds(),
		Summary:     fmt.Sprintf("collected %d current compiler and semantic diagnostics", len(items)),
		Diagnostics: &verification.DiagnosticSummary{Items: items, Total: len(items), Errors: errorsCount, Warnings: warnings},
	})
	return nil
}

func (c *Core) attachContractEvidence(report *verification.Report, contract ChangeContract, analysis verification.ChangeAnalysis) {
	targets := make([]string, 0, len(report.Impact.Packages))
	for _, item := range report.Impact.Packages {
		targets = append(targets, item.ID)
	}
	check := verification.Check{
		ID: "contract", Kind: verification.CheckContract, Required: true, Targets: targets,
		Reason: "evaluate machine-checkable structural policies from the requested Change Contract",
	}
	report.Plan = append(report.Plan, check)
	violations, uncertainties := evaluateCheckpointPolicies(contract, analysis)
	summary := &verification.ContractSummary{ContractID: contract.ID, Violations: make([]verification.ContractViolation, 0, len(violations))}
	for _, item := range violations {
		locations := verificationLocations(item.Locations)
		summary.Violations = append(summary.Violations, verification.ContractViolation{
			Code: item.Code, Policy: string(item.Policy), Message: item.Message, Locations: locations,
		})
		severity := verification.SeverityWarning
		if item.Policy == PolicyForbid {
			severity = verification.SeverityError
			summary.Forbidden++
		} else {
			summary.Warnings++
		}
		location := firstVerificationLocation(locations)
		report.Findings = append(report.Findings, verification.Finding{
			Kind: string(verification.CheckContract), Rule: item.Code, Severity: severity,
			Message: item.Message, Location: location, CheckID: check.ID,
		})
	}
	status := verification.EvidencePassed
	if len(violations) > 0 {
		status = verification.EvidenceFailed
	}
	report.Evidence = append(report.Evidence, verification.Evidence{
		CheckID: check.ID, Kind: check.Kind, Status: status,
		Summary: fmt.Sprintf("evaluated %d machine-checkable contract violations", len(violations)), Contract: summary,
	})
	for _, item := range uncertainties {
		report.Uncertainties = append(report.Uncertainties, verificationUncertainty(item, check.ID))
	}
	report.Snapshot.ContractInitial = contract.InitialSnapshot.ID
	report.Snapshot.ContractLatest = contract.LatestSnapshot.ID
	for _, checkpoint := range contract.Checkpoints {
		report.Snapshot.Transitions = append(report.Snapshot.Transitions, verification.SnapshotTransition{
			CheckpointID: checkpoint.ID, PreviousID: checkpoint.PreviousSnapshotID, CurrentID: checkpoint.CurrentSnapshotID,
		})
	}
}

func changedDiagnosticPaths(analysis verification.ChangeAnalysis) []string {
	paths := make([]string, 0, len(analysis.Change.Files))
	for _, changed := range analysis.Change.Files {
		if changed.Change != verification.ChangeDeleted && strings.HasSuffix(changed.Path, ".go") {
			paths = append(paths, changed.Path)
		}
	}
	sort.Strings(paths)
	return dedupeStrings(paths)
}

func verificationProviders(snapshot SnapshotRef, agenticVersion string) []verification.ProviderCapability {
	semantic := make([]string, 0)
	values := []struct {
		name    string
		enabled bool
	}{
		{"call_hierarchy", snapshot.Capabilities.CallHierarchy},
		{"code_action", snapshot.Capabilities.CodeAction},
		{"definition", snapshot.Capabilities.Definition},
		{"diagnostics", snapshot.Capabilities.Diagnostics},
		{"document_symbol", snapshot.Capabilities.DocumentSymbol},
		{"formatting", snapshot.Capabilities.Formatting},
		{"hover", snapshot.Capabilities.Hover},
		{"implementation", snapshot.Capabilities.Implementation},
		{"references", snapshot.Capabilities.References},
		{"rename", snapshot.Capabilities.Rename},
		{"type_definition", snapshot.Capabilities.TypeDefinition},
		{"workspace_symbol", snapshot.Capabilities.WorkspaceSymbol},
	}
	for _, value := range values {
		if value.enabled {
			semantic = append(semantic, value.name)
		}
	}
	return []verification.ProviderCapability{
		{Name: "agentic-go", Version: agenticVersion, Capabilities: []string{
			"change_impact", "changed_coverage", "contract_compliance", "diagnostics", "analyzer_baseline", "risk_guidance",
		}},
		{Name: "agentic-go-gopls", Version: snapshot.GoplsVersion, Capabilities: semantic},
		{Name: "go", Version: snapshot.GoVersion, Capabilities: []string{"build", "race", "test"}},
	}
}

func verificationLocations(items []Location) []verification.Location {
	result := make([]verification.Location, 0, len(items))
	for _, item := range items {
		result = append(result, verification.Location{File: item.File, Line: item.Line, Col: item.Column})
	}
	return result
}

func firstVerificationLocation(items []verification.Location) *verification.Location {
	if len(items) == 0 {
		return nil
	}
	value := items[0]
	return &value
}

func verificationUncertainty(item Uncertainty, checkID string) verification.Uncertainty {
	return verification.Uncertainty{
		Code: item.Code, Message: item.Message, CheckID: checkID, Locations: verificationLocations(item.Locations),
	}
}

func (c *Core) recordContextProvenance(operation string, snapshot SnapshotRef) {
	c.recordProvenance(verification.ProvenanceReference{
		Kind: "agentic.context", Operation: operation, Reference: ContextSchemaVersion,
		InputSnapshotID: snapshot.ID, OutputSnapshotID: snapshot.ID,
	})
}

func (c *Core) recordRefactorProvenance(result RefactorResult, inputSnapshotID string) {
	c.recordProvenance(verification.ProvenanceReference{
		Kind: "agentic.refactor", Operation: result.Operation, Reference: result.PlanID,
		InputSnapshotID: inputSnapshotID, OutputSnapshotID: result.Snapshot.ID, Applied: result.Applied,
	})
}

func (c *Core) recordProvenance(item verification.ProvenanceReference) {
	c.provenanceMu.Lock()
	defer c.provenanceMu.Unlock()
	c.provenance = append(c.provenance, item)
	if len(c.provenance) > maximumProvenanceReferences {
		c.provenance = append([]verification.ProvenanceReference(nil), c.provenance[len(c.provenance)-maximumProvenanceReferences:]...)
	}
}

func (c *Core) verificationProvenance() verification.Provenance {
	c.provenanceMu.Lock()
	defer c.provenanceMu.Unlock()
	result := verification.Provenance{Context: []verification.ProvenanceReference{}, Refactors: []verification.ProvenanceReference{}}
	for _, item := range c.provenance {
		switch item.Kind {
		case "agentic.context":
			result.Context = append(result.Context, item)
		case "agentic.refactor":
			result.Refactors = append(result.Refactors, item)
		}
	}
	return result
}
