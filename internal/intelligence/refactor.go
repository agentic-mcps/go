package intelligence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxRefactorFiles       = 64
	maxRefactorSourceBytes = 4 << 20
	maxRefactorDiffBytes   = 256 << 10
)

// Refactor previews or applies one deterministic, snapshot-bound source edit
// plan. It never invokes Git or mutates files not named by the stored plan.
func (c *Core) Refactor(ctx context.Context, request RefactorRequest) (RefactorResult, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if strings.TrimSpace(request.ExpectedSnapshotID) == "" {
		return RefactorResult{}, fmt.Errorf("expected snapshot ID is required")
	}
	var result RefactorResult
	var err error
	if request.Apply {
		result, err = c.applyRefactor(ctx, request)
	} else {
		result, err = c.previewRefactor(ctx, request)
	}
	if err != nil {
		return RefactorResult{}, err
	}
	c.recordRefactorProvenance(result, request.ExpectedSnapshotID)
	return result, nil
}

func (c *Core) previewRefactor(ctx context.Context, request RefactorRequest) (RefactorResult, error) {
	semanticRequest := semanticRefactorRequest{
		Operation: request.Operation, NewName: request.NewName, Files: append([]string(nil), request.Files...),
	}
	base, scope := "", "./..."
	if request.Operation == RefactorRename {
		identity, err := decodeSymbolRef(request.Ref)
		if err != nil {
			return RefactorResult{}, fmt.Errorf("rename requires a valid symbol reference: %w", err)
		}
		base, scope = identity.Base, identity.Scope
		semanticRequest.File, semanticRequest.Position = identity.Path, identity.Position
	}
	if capabilityErr := c.requireRefactorCapability(request.Operation); capabilityErr != nil {
		return RefactorResult{}, capabilityErr
	}
	snapshot, err := c.capture(ctx, base, scope, request.ExpectedSnapshotID)
	if err != nil {
		return RefactorResult{}, err
	}
	if request.Operation == RefactorRename {
		identity, decodeErr := decodeSymbolRef(request.Ref)
		if decodeErr != nil {
			return RefactorResult{}, decodeErr
		}
		if snapshotErr := requireSymbolSnapshot(identity, snapshot); snapshotErr != nil {
			return RefactorResult{}, snapshotErr
		}
	}
	_, pendingErr := c.refactors.pending(ctx, snapshot.RepositoryID)
	if pendingErr == nil {
		return RefactorResult{}, errRefactorRecoveryRequired
	} else if !errors.Is(pendingErr, errRefactorRecoveryNotFound) {
		return RefactorResult{}, pendingErr
	}
	semanticEdits, err := c.mutator.Refactor(ctx, snapshot, semanticRequest)
	if err != nil {
		return RefactorResult{}, err
	}
	if _, snapshotErr := c.snapshots.Validate(ctx, snapshot); snapshotErr != nil {
		return RefactorResult{}, snapshotErr
	}
	plan, uncertainties, err := c.buildRefactorPlan(ctx, snapshot, request.Operation, semanticEdits)
	if err != nil {
		return RefactorResult{}, err
	}
	if len(plan.Files) == 0 {
		return emptyRefactorResult(request.Operation, snapshot), nil
	}
	if _, snapshotErr := c.snapshots.Validate(ctx, snapshot); snapshotErr != nil {
		return RefactorResult{}, snapshotErr
	}
	plan, err = c.refactors.savePlan(ctx, plan)
	if err != nil {
		return RefactorResult{}, err
	}
	return refactorResult(plan, snapshot, false, uncertainties), nil
}

func (c *Core) applyRefactor(ctx context.Context, request RefactorRequest) (RefactorResult, error) {
	if request.PlanID == "" {
		return RefactorResult{}, fmt.Errorf("plan ID is required for apply")
	}
	observed, err := c.capture(ctx, "", "./...", "")
	if err != nil {
		return RefactorResult{}, err
	}
	plan, err := c.refactors.loadPlan(ctx, observed.RepositoryID, request.PlanID)
	if err != nil {
		return RefactorResult{}, err
	}
	if request.ExpectedSnapshotID != plan.Snapshot.ID {
		return RefactorResult{}, fmt.Errorf("%w: plan expects %s, caller supplied %s", ErrSnapshotChanged, plan.Snapshot.ID, request.ExpectedSnapshotID)
	}
	if _, snapshotErr := c.snapshots.Validate(ctx, plan.Snapshot); snapshotErr != nil {
		return RefactorResult{}, snapshotErr
	}
	_, pendingErr := c.refactors.pending(ctx, plan.RepositoryID)
	if pendingErr == nil {
		return RefactorResult{}, errRefactorRecoveryRequired
	} else if !errors.Is(pendingErr, errRefactorRecoveryNotFound) {
		return RefactorResult{}, pendingErr
	}
	targets, err := c.validateRefactorPreimages(ctx, plan)
	if err != nil {
		return RefactorResult{}, err
	}
	if journalErr := c.refactors.beginRecovery(ctx, plan); journalErr != nil {
		return RefactorResult{}, journalErr
	}
	rollback := func(cause error) (RefactorResult, error) {
		_, recoverErr := c.refactors.recover(context.Background(), plan.RepositoryID, c.workspace.Root())
		if recoverErr != nil {
			return RefactorResult{}, fmt.Errorf("%w: apply failed: %v; rollback failed: %v", errRefactorRecoveryRequired, cause, recoverErr)
		}
		return RefactorResult{}, cause
	}
	for index, file := range plan.Files {
		if contextErr := contextError(ctx); contextErr != nil {
			return rollback(contextErr)
		}
		current, readErr := os.ReadFile(targets[index])
		if readErr != nil {
			return rollback(fmt.Errorf("reading refactor preimage %s: %w", file.Path, readErr))
		}
		if digestBytes(current) != file.PreimageDigest {
			return rollback(fmt.Errorf("%w: preimage changed for %s", ErrSnapshotChanged, file.Path))
		}
		if writeErr := c.refactorWrite(targets[index], file.Postimage, os.FileMode(file.Mode)); writeErr != nil {
			return rollback(fmt.Errorf("applying refactor to %s: %w", file.Path, writeErr))
		}
	}
	appliedSnapshot, err := c.snapshots.Capture(ctx, SnapshotRequest{
		Base: plan.Snapshot.RequestedBase, Scope: plan.Snapshot.Scope, Semantic: c.semantic.Identity(),
	})
	if err != nil {
		return rollback(err)
	}
	if clearErr := c.refactors.completeRecovery(ctx, plan.RepositoryID); clearErr != nil {
		return RefactorResult{}, fmt.Errorf("%w: refactor applied but recovery journal could not be cleared: %v", errRefactorRecoveryRequired, clearErr)
	}
	return refactorResult(plan, appliedSnapshot, true, []Uncertainty{}), nil
}

func (c *Core) requireRefactorCapability(operation string) error {
	capabilities := c.semantic.Identity().Capabilities
	switch operation {
	case RefactorRename:
		if !capabilities.Rename {
			return fmt.Errorf("the active semantic provider does not support rename")
		}
	case RefactorFormat:
		if !capabilities.Formatting {
			return fmt.Errorf("the active semantic provider does not support formatting")
		}
	case RefactorOrganizeImports, RefactorFixAll:
		if !capabilities.CodeAction {
			return fmt.Errorf("the active semantic provider does not support code actions")
		}
	default:
		return fmt.Errorf("unsupported refactor operation %q", operation)
	}
	return nil
}

func (c *Core) buildRefactorPlan(
	ctx context.Context,
	snapshot SnapshotRef,
	operation string,
	semanticEdits []semanticFileEdits,
) (refactorPlan, []Uncertainty, error) {
	if len(semanticEdits) > maxRefactorFiles {
		return refactorPlan{}, nil, fmt.Errorf("refactor affects %d files; maximum is %d", len(semanticEdits), maxRefactorFiles)
	}
	sort.Slice(semanticEdits, func(i, j int) bool { return semanticEdits[i].Path < semanticEdits[j].Path })
	plan := refactorPlan{
		SchemaVersion: refactorPlanSchemaVersion, RepositoryID: snapshot.RepositoryID,
		Operation: operation, Snapshot: snapshot, Files: []refactorFileEdit{},
	}
	totalBytes := 0
	var diff strings.Builder
	for _, source := range semanticEdits {
		if err := contextError(ctx); err != nil {
			return refactorPlan{}, nil, err
		}
		absolute, err := c.workspace.Resolve(source.Path)
		if err != nil {
			return refactorPlan{}, nil, fmt.Errorf("containing refactor target %s: %w", source.Path, err)
		}
		relative, err := c.workspace.Relative(absolute)
		if err != nil || relative != source.Path {
			return refactorPlan{}, nil, fmt.Errorf("refactor target %s is not a stable contained path", source.Path)
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			return refactorPlan{}, nil, fmt.Errorf("refactor target %s must be an existing regular file", source.Path)
		}
		preimage, err := os.ReadFile(absolute)
		if err != nil {
			return refactorPlan{}, nil, fmt.Errorf("reading refactor target %s: %w", source.Path, err)
		}
		if isGeneratedSource(preimage) {
			return refactorPlan{}, nil, fmt.Errorf("refactor target %s is generated source and cannot be modified", source.Path)
		}
		postimage, err := applySemanticEdits(preimage, source.Edits)
		if err != nil {
			return refactorPlan{}, nil, fmt.Errorf("applying preview edits for %s: %w", source.Path, err)
		}
		if bytes.Equal(preimage, postimage) {
			continue
		}
		totalBytes += len(preimage) + len(postimage)
		if totalBytes > maxRefactorSourceBytes {
			return refactorPlan{}, nil, fmt.Errorf("refactor source payload exceeds %d bytes", maxRefactorSourceBytes)
		}
		file := refactorFileEdit{
			Path: relative, Mode: uint32(info.Mode().Perm()),
			PreimageDigest: digestBytes(preimage), PostimageDigest: digestBytes(postimage),
			Preimage: preimage, Postimage: postimage,
		}
		plan.Files = append(plan.Files, file)
		diff.WriteString(fullFileDiff(relative, preimage, postimage))
	}
	plan.Diff = diff.String()
	uncertainties := []Uncertainty{}
	if len(plan.Diff) > maxRefactorDiffBytes {
		plan.Diff = truncateUTF8(plan.Diff, maxRefactorDiffBytes)
		plan.DiffTruncated = true
		uncertainties = append(uncertainties, Uncertainty{
			Code: "refactor.diff_truncated", Message: "the review diff exceeded the response budget; preimage validation still covers every affected file", Locations: []Location{},
		})
	}
	return plan, uncertainties, nil
}

func (c *Core) validateRefactorPreimages(ctx context.Context, plan refactorPlan) ([]string, error) {
	targets := make([]string, 0, len(plan.Files))
	for _, file := range plan.Files {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		absolute, err := c.workspace.Resolve(file.Path)
		if err != nil {
			return nil, fmt.Errorf("containing refactor target %s: %w", file.Path, err)
		}
		relative, err := c.workspace.Relative(absolute)
		if err != nil || relative != file.Path {
			return nil, fmt.Errorf("%w: refactor path changed for %s", ErrSnapshotChanged, file.Path)
		}
		contents, err := os.ReadFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("reading refactor preimage %s: %w", file.Path, err)
		}
		if digestBytes(contents) != file.PreimageDigest {
			return nil, fmt.Errorf("%w: preimage changed for %s", ErrSnapshotChanged, file.Path)
		}
		targets = append(targets, absolute)
	}
	return targets, nil
}

func applySemanticEdits(preimage []byte, edits []semanticTextEdit) ([]byte, error) {
	edits = append([]semanticTextEdit(nil), edits...)
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].Start != edits[j].Start {
			return edits[i].Start < edits[j].Start
		}
		return edits[i].End < edits[j].End
	})
	result := append([]byte(nil), preimage...)
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		if edit.Start < 0 || edit.End < edit.Start || edit.End > len(preimage) {
			return nil, fmt.Errorf("edit range %d..%d is outside the source", edit.Start, edit.End)
		}
		if index < len(edits)-1 && edit.End > edits[index+1].Start {
			return nil, fmt.Errorf("edits overlap")
		}
		replaced := make([]byte, 0, len(result)-(edit.End-edit.Start)+len(edit.NewText))
		replaced = append(replaced, result[:edit.Start]...)
		replaced = append(replaced, edit.NewText...)
		replaced = append(replaced, result[edit.End:]...)
		result = replaced
	}
	return result, nil
}

func fullFileDiff(path string, preimage, postimage []byte) string {
	var result strings.Builder
	fmt.Fprintf(&result, "--- a/%s\n+++ b/%s\n", path, path)
	oldLines := strings.Split(strings.TrimSuffix(string(preimage), "\n"), "\n")
	newLines := strings.Split(strings.TrimSuffix(string(postimage), "\n"), "\n")
	fmt.Fprintf(&result, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, line := range oldLines {
		result.WriteByte('-')
		result.WriteString(line)
		result.WriteByte('\n')
	}
	for _, line := range newLines {
		result.WriteByte('+')
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

func truncateUTF8(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func refactorResult(plan refactorPlan, snapshot SnapshotRef, applied bool, uncertainties []Uncertainty) RefactorResult {
	files := make([]string, 0, len(plan.Files))
	preimages := make([]RefactorPreimage, 0, len(plan.Files))
	for _, file := range plan.Files {
		files = append(files, file.Path)
		preimages = append(preimages, RefactorPreimage{Path: file.Path, Digest: file.PreimageDigest})
	}
	if uncertainties == nil {
		uncertainties = []Uncertainty{}
	}
	if plan.DiffTruncated && !hasUncertainty(uncertainties, "refactor.diff_truncated") {
		uncertainties = append(uncertainties, Uncertainty{
			Code: "refactor.diff_truncated", Message: "the review diff exceeded the response budget; preimage validation still covers every affected file", Locations: []Location{},
		})
	}
	return RefactorResult{
		PlanID: plan.ID, Operation: plan.Operation, Snapshot: snapshot, Applied: applied, Diff: plan.Diff,
		AffectedFiles: files, Preimages: preimages, Risks: []RiskArea{}, Uncertainties: uncertainties,
	}
}

func hasUncertainty(values []Uncertainty, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

func emptyRefactorResult(operation string, snapshot SnapshotRef) RefactorResult {
	return RefactorResult{
		Operation: operation, Snapshot: snapshot, AffectedFiles: []string{}, Preimages: []RefactorPreimage{},
		Risks: []RiskArea{}, Uncertainties: []Uncertainty{},
	}
}
