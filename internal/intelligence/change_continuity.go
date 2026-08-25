package intelligence

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"go/ast"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

const checkpointPackageLimit = 200

// Begin creates one private, snapshot-bound Change Contract. Goal text is
// retained exactly and never interpreted as a policy.
func (c *Core) Begin(ctx context.Context, request BeginRequest) (ChangeContract, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if strings.TrimSpace(request.Base) == "" {
		return ChangeContract{}, fmt.Errorf("base is required")
	}
	if strings.TrimSpace(request.Goal) == "" {
		return ChangeContract{}, fmt.Errorf("goal is required")
	}
	if request.Scope == "" {
		request.Scope = "./..."
	}
	if err := normalizeStructuralPolicies(&request.Policies); err != nil {
		return ChangeContract{}, err
	}
	focusedPaths, err := normalizeContractPaths(c, request.FocusedPaths)
	if err != nil {
		return ChangeContract{}, fmt.Errorf("focused paths: %w", err)
	}
	allowedPaths, err := normalizeContractPaths(c, request.AllowedPaths)
	if err != nil {
		return ChangeContract{}, fmt.Errorf("allowed paths: %w", err)
	}
	focusedPackages, err := normalizeContractStrings(request.FocusedPackages, "focused package")
	if err != nil {
		return ChangeContract{}, err
	}
	snapshot, err := c.capture(ctx, request.Base, request.Scope, "")
	if err != nil {
		return ChangeContract{}, err
	}
	focusedSymbols := append(make([]SymbolRef, 0, len(request.FocusedSymbols)), request.FocusedSymbols...)
	for _, ref := range focusedSymbols {
		identity, decodeErr := decodeSymbolRef(ref)
		if decodeErr != nil {
			return ChangeContract{}, fmt.Errorf("focused symbol: %w", decodeErr)
		}
		if identity.SnapshotID != snapshot.ID {
			return ChangeContract{}, fmt.Errorf("%w: focused symbol belongs to %s, observed %s", ErrSnapshotChanged, identity.SnapshotID, snapshot.ID)
		}
	}
	sort.Slice(focusedSymbols, func(i, j int) bool { return focusedSymbols[i] < focusedSymbols[j] })
	focusedSymbols = dedupeSymbolRefs(focusedSymbols)
	id, err := opaqueID("chg_")
	if err != nil {
		return ChangeContract{}, err
	}
	now := time.Now().UTC()
	contract := ChangeContract{
		SchemaVersion: ChangeSchemaVersion, ID: id, RepositoryID: snapshot.RepositoryID,
		Goal: request.Goal, Base: request.Base, Scope: request.Scope,
		InitialSnapshot: snapshot, LatestSnapshot: snapshot,
		FocusedPaths: focusedPaths, FocusedPackages: focusedPackages, FocusedSymbols: focusedSymbols,
		AllowedPaths: allowedPaths, Policies: request.Policies,
		Decisions: []Decision{}, UnresolvedQuestions: []string{}, Checkpoints: []CheckpointRef{},
		Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := c.contracts.Save(ctx, contract); err != nil {
		return ChangeContract{}, err
	}
	return contract, nil
}

// Checkpoint records intentional worktree drift from the contract's latest
// snapshot and rejects callers that do not name that exact lineage point.
func (c *Core) Checkpoint(ctx context.Context, request CheckpointRequest) (Checkpoint, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if !validContractID(request.ContractID) {
		return Checkpoint{}, fmt.Errorf("contract_id is invalid")
	}
	if strings.TrimSpace(request.ExpectedSnapshot) == "" {
		return Checkpoint{}, fmt.Errorf("expected_snapshot_id is required")
	}
	repositorySnapshot, err := c.capture(ctx, "", "./...", "")
	if err != nil {
		return Checkpoint{}, err
	}
	contract, err := c.contracts.Load(ctx, repositorySnapshot.RepositoryID, request.ContractID)
	if err != nil {
		return Checkpoint{}, err
	}
	if !contract.Active {
		return Checkpoint{}, fmt.Errorf("change contract %s is closed", contract.ID)
	}
	if request.ExpectedSnapshot != contract.LatestSnapshot.ID {
		return Checkpoint{}, fmt.Errorf("%w: expected lineage %s, contract is at %s", ErrSnapshotChanged, request.ExpectedSnapshot, contract.LatestSnapshot.ID)
	}
	current, err := c.capture(ctx, contract.Base, contract.Scope, "")
	if err != nil {
		return Checkpoint{}, err
	}
	analysis, err := c.changes.Analyze(ctx, verification.ChangeOptions{Base: contract.Base, Package: contract.Scope, MaxPackages: checkpointPackageLimit})
	if err != nil {
		return Checkpoint{}, err
	}
	violations, uncertainties := evaluateCheckpointPolicies(contract, analysis)
	diagnostics, diagnosticUncertainties, err := c.checkpointDiagnostics(ctx, current, analysis)
	if err != nil {
		return Checkpoint{}, err
	}
	uncertainties = append(uncertainties, diagnosticUncertainties...)
	for _, item := range analysis.Uncertainties {
		uncertainties = append(uncertainties, normalizeVerificationUncertainty(item))
	}
	uncertainties = dedupeUncertainties(uncertainties)
	affected := make([]string, 0, len(analysis.Impact.Packages))
	for _, pkg := range analysis.Impact.Packages {
		affected = append(affected, pkg.ID)
	}
	sort.Strings(affected)
	total := len(affected)
	if len(affected) > checkpointPackageLimit {
		affected = affected[:checkpointPackageLimit]
	}
	if _, validationErr := c.snapshots.Validate(ctx, current); validationErr != nil {
		return Checkpoint{}, validationErr
	}
	id, err := opaqueID("cp_")
	if err != nil {
		return Checkpoint{}, err
	}
	now := time.Now().UTC()
	checkpoint := Checkpoint{
		ID: id, ContractID: contract.ID, Previous: contract.LatestSnapshot, Current: current,
		AffectedPackages: affected, AffectedTotal: total, AffectedTruncated: total > len(affected),
		Diagnostics: diagnostics, Violations: violations, Uncertainties: uncertainties,
		Complete: analysis.Complete, RecordedAt: now,
	}
	contract.LatestSnapshot = current
	contract.Decisions = append(contract.Decisions, decisions(request.Decisions, now)...)
	contract.Decisions = dedupeDecisions(contract.Decisions)
	contract.UnresolvedQuestions = normalizeProse(request.UnresolvedQuestions)
	contract.Checkpoints = append(contract.Checkpoints, CheckpointRef{
		ID: id, PreviousSnapshotID: checkpoint.Previous.ID, CurrentSnapshotID: current.ID, RecordedAt: now,
	})
	contract.UpdatedAt = now
	if err := c.contracts.Save(ctx, contract); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// CurrentChangeContract returns the repository's latest active private
// contract without exposing its cache location.
func (c *Core) CurrentChangeContract(ctx context.Context) (ChangeContract, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	snapshot, err := c.capture(ctx, "", "./...", "")
	if err != nil {
		return ChangeContract{}, err
	}
	return c.contracts.Current(ctx, snapshot.RepositoryID)
}

func (c *Core) checkpointDiagnostics(ctx context.Context, snapshot SnapshotRef, analysis verification.ChangeAnalysis) ([]Diagnostic, []Uncertainty, error) {
	if !snapshot.Capabilities.Diagnostics {
		return []Diagnostic{}, []Uncertainty{{
			Code: "semantic.diagnostics_unavailable", Message: "the active semantic provider does not support focused diagnostics", Locations: []Location{},
		}}, nil
	}
	paths := make([]string, 0, len(analysis.Change.Files))
	for _, changed := range analysis.Change.Files {
		if changed.Change != verification.ChangeDeleted && strings.HasSuffix(changed.Path, ".go") {
			paths = append(paths, changed.Path)
		}
	}
	sort.Strings(paths)
	paths = dedupeStrings(paths)
	diagnostics := []Diagnostic{}
	if len(paths) == 0 {
		return diagnostics, []Uncertainty{}, nil
	}
	err := c.semantic.Read(ctx, snapshot, func(reader semanticReader) error {
		for _, path := range paths {
			if err := contextError(ctx); err != nil {
				return err
			}
			items, readErr := reader.Diagnostics(ctx, path)
			if readErr != nil {
				return readErr
			}
			diagnostics = append(diagnostics, items...)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sortDiagnostics(diagnostics)
	return diagnostics, []Uncertainty{}, nil
}

func evaluateCheckpointPolicies(contract ChangeContract, analysis verification.ChangeAnalysis) ([]PolicyViolation, []Uncertainty) {
	violations := []PolicyViolation{}
	if len(contract.AllowedPaths) > 0 {
		locations := []Location{}
		for _, changed := range analysis.Change.Files {
			for _, path := range changedPaths(changed) {
				if !withinAny(path, contract.AllowedPaths) {
					locations = append(locations, Location{File: path, Line: changedLine(changed), Column: 1})
				}
			}
		}
		violations = appendViolation(violations, "outside_allowed_paths", contract.Policies.OutsideAllowedPaths,
			"changed files extend outside the contract's explicit allowed paths", locations)
	} else if len(contract.FocusedPaths) > 0 || len(contract.FocusedPackages) > 0 {
		locations := []Location{}
		for _, changed := range analysis.Change.Files {
			if len(contract.FocusedPaths) > 0 && !withinAny(changed.Path, contract.FocusedPaths) {
				locations = append(locations, Location{File: changed.Path, Line: changedLine(changed), Column: 1})
			}
		}
		packageDrift := false
		for _, pkg := range analysis.Impact.Packages {
			if pkg.Distance == 0 && len(contract.FocusedPackages) > 0 && !containsString(contract.FocusedPackages, pkg.ID) {
				packageDrift = true
			}
		}
		if packageDrift || len(locations) > 0 {
			violations = appendFactViolation(violations, "outside_focus", contract.Policies.OutsideFocus,
				"changed files or directly affected packages extend outside the contract focus", locations)
		}
	}

	exportedLocations := []Location{}
	for _, declaration := range analysis.Change.Declarations {
		name := declaration.Name
		if index := strings.LastIndex(name, "."); index >= 0 {
			name = name[index+1:]
		}
		if ast.IsExported(name) {
			exportedLocations = append(exportedLocations, declarationLocation(declaration))
		}
	}
	violations = appendViolation(violations, "exported_api_change", contract.Policies.ExportedAPI,
		"exported Go declarations changed", exportedLocations)

	dependencyLocations := []Location{}
	generatedLocations := []Location{}
	testDeletionLocations := []Location{}
	for _, source := range analysis.Files {
		changed := source.Change
		if isDependencyPath(changed.Path) || isDependencyPath(changed.PreviousPath) {
			dependencyLocations = append(dependencyLocations, Location{File: changed.Path, Line: changedLine(changed), Column: 1})
		}
		contents := source.CurrentContent
		if len(contents) == 0 {
			contents = source.BaseContent
		}
		if isGeneratedSource(contents) {
			generatedLocations = append(generatedLocations, Location{File: changed.Path, Line: 1, Column: 1})
		}
		if changed.Change == verification.ChangeDeleted && strings.HasSuffix(changed.Path, "_test.go") ||
			changed.Change == verification.ChangeRenamed && strings.HasSuffix(changed.PreviousPath, "_test.go") && !strings.HasSuffix(changed.Path, "_test.go") {
			testDeletionLocations = append(testDeletionLocations, Location{File: changed.Path, Line: changedLine(changed), Column: 1})
		}
	}
	violations = appendViolation(violations, "dependency_change", contract.Policies.Dependency,
		"Go module or workspace metadata changed", dependencyLocations)
	violations = appendViolation(violations, "generated_file_change", contract.Policies.GeneratedFile,
		"generated source changed", generatedLocations)
	violations = appendViolation(violations, "test_deletion", contract.Policies.TestDeletion,
		"test source was deleted or renamed out of test scope", testDeletionLocations)

	modules := map[string]struct{}{}
	for _, pkg := range analysis.Packages {
		if pkg.Distance == 0 && pkg.ModulePath != "" {
			modules[pkg.ModulePath] = struct{}{}
		}
	}
	if len(modules) > 1 {
		violations = appendFactViolation(violations, "cross_module_change", contract.Policies.CrossModule,
			"directly affected packages span multiple Go modules", []Location{})
	}

	uncertainties := []Uncertainty{}
	if !analysis.Complete && !hasVerificationUncertainty(analysis.Uncertainties, "package_limit") {
		uncertainties = append(uncertainties, Uncertainty{
			Code: "package_limit", Message: fmt.Sprintf("affected package closure contains %d packages, exceeding the contract limit of %d", analysis.ObservedPackages, checkpointPackageLimit), Locations: []Location{},
		})
	}
	sort.Slice(violations, func(i, j int) bool { return violations[i].Code < violations[j].Code })
	return violations, uncertainties
}

func normalizeContractPaths(c *Core, paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" || filepath.IsAbs(filepath.FromSlash(path)) {
			return nil, fmt.Errorf("path %q must be workspace-relative", path)
		}
		resolved, err := c.workspace.Resolve(path)
		if err != nil {
			return nil, err
		}
		relative, err := c.workspace.Relative(resolved)
		if err != nil {
			return nil, err
		}
		result = append(result, relative)
	}
	sort.Strings(result)
	return dedupeStrings(result), nil
}

func normalizeContractStrings(values []string, label string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.HasPrefix(value, "-") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("%s %q is invalid", label, value)
		}
		result = append(result, value)
	}
	sort.Strings(result)
	return dedupeStrings(result), nil
}

func decisions(values []string, created time.Time) []Decision {
	result := make([]Decision, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, Decision{Text: value, CreatedAt: created})
		}
	}
	return result
}

func normalizeProse(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return dedupeStrings(result)
}

func dedupeDecisions(values []Decision) []Decision {
	seen := map[string]struct{}{}
	result := make([]Decision, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value.Text]; exists {
			continue
		}
		seen[value.Text] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Text < result[j].Text })
	return result
}

func opaqueID(prefix string) (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generating private identifier: %w", err)
	}
	return prefix + hex.EncodeToString(data), nil
}

func withinAny(path string, roots []string) bool {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	for _, root := range roots {
		if root == "." || path == root || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

func changedPaths(changed verification.ChangedFile) []string {
	result := []string{changed.Path}
	if changed.PreviousPath != "" && changed.PreviousPath != changed.Path {
		result = append(result, changed.PreviousPath)
	}
	return result
}

func changedLine(changed verification.ChangedFile) int {
	if len(changed.CurrentRanges) > 0 {
		return changed.CurrentRanges[0].Start
	}
	if len(changed.BaseRanges) > 0 {
		return changed.BaseRanges[0].Start
	}
	return 1
}

func declarationLocation(declaration verification.ChangedDeclaration) Location {
	location := declaration.CurrentLocation
	if location == nil {
		location = declaration.BaseLocation
	}
	if location == nil {
		return Location{}
	}
	return Location{File: location.File, Line: location.Line, Column: max(location.Col, 1)}
}

func appendViolation(values []PolicyViolation, code string, policy PolicyMode, message string, locations []Location) []PolicyViolation {
	if policy == PolicyAllow || len(locations) == 0 {
		return values
	}
	locations = normalizeLocations(locations)
	return append(values, PolicyViolation{Code: code, Policy: policy, Message: message, Locations: locations})
}

func appendFactViolation(values []PolicyViolation, code string, policy PolicyMode, message string, locations []Location) []PolicyViolation {
	if policy == PolicyAllow {
		return values
	}
	return append(values, PolicyViolation{Code: code, Policy: policy, Message: message, Locations: normalizeLocations(locations)})
}

func normalizeLocations(values []Location) []Location {
	result := append([]Location(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		left := fmt.Sprintf("%s:%09d:%09d", result[i].File, result[i].Line, result[i].Column)
		right := fmt.Sprintf("%s:%09d:%09d", result[j].File, result[j].Line, result[j].Column)
		return left < right
	})
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}

func isDependencyPath(path string) bool {
	base := filepath.Base(filepath.FromSlash(path))
	return base == "go.mod" || base == "go.sum" || base == "go.work" || base == "go.work.sum"
}

func isGeneratedSource(contents []byte) bool {
	for _, line := range bytes.Split(contents, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("// Code generated ")) && bytes.HasSuffix(line, []byte(" DO NOT EDIT.")) {
			return true
		}
	}
	return false
}

func normalizeVerificationUncertainty(item verification.Uncertainty) Uncertainty {
	locations := make([]Location, 0, len(item.Locations))
	for _, location := range item.Locations {
		locations = append(locations, Location{File: location.File, Line: location.Line, Column: max(location.Col, 1)})
	}
	return Uncertainty{Code: item.Code, Message: item.Message, Locations: normalizeLocations(locations)}
}

func dedupeUncertainties(values []Uncertainty) []Uncertainty {
	sort.Slice(values, func(i, j int) bool {
		return values[i].Code+"\x00"+values[i].Message < values[j].Code+"\x00"+values[j].Message
	})
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1].Code != value.Code || result[len(result)-1].Message != value.Message {
			result = append(result, value)
		}
	}
	return result
}

func hasVerificationUncertainty(values []verification.Uncertainty, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

func dedupeStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func dedupeSymbolRefs(values []SymbolRef) []SymbolRef {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func containsString(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	return index < len(values) && values[index] == target
}
