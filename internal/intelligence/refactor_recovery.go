package intelligence

import (
	"context"
	"errors"
	"fmt"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

const (
	RefactorRecoveryClean     = "clean"
	RefactorRecoveryRequired  = "recovery_required"
	RefactorRecoveryRecovered = "recovered"
)

// RefactorRecoveryResult reports private journal state without exposing cache
// paths or source contents.
type RefactorRecoveryResult struct {
	Status         string
	RecoveredFiles int
}

// RecoverGuardedRefactor inspects or safely restores the current repository's
// interrupted apply journal. Restore occurs only when every target still
// matches a recorded preimage or postimage.
func RecoverGuardedRefactor(
	ctx context.Context,
	ws *workspace.Workspace,
	runner *execution.Runner,
	store *RefactorStore,
	recoverState bool,
) (RefactorRecoveryResult, error) {
	if ws == nil || runner == nil || store == nil {
		return RefactorRecoveryResult{}, fmt.Errorf("refactor recovery dependencies are incomplete")
	}
	snapshots, err := NewSnapshotter(ws, runner)
	if err != nil {
		return RefactorRecoveryResult{}, err
	}
	observed, err := snapshots.Capture(ctx, SnapshotRequest{
		Scope: "./...", Semantic: SemanticIdentity{Version: "doctor-recovery"},
	})
	if err != nil {
		return RefactorRecoveryResult{}, fmt.Errorf("identifying refactor repository: %w", err)
	}
	if _, err := store.Pending(ctx, observed.RepositoryID); errors.Is(err, ErrRefactorRecoveryNotFound) {
		return RefactorRecoveryResult{Status: RefactorRecoveryClean}, nil
	} else if err != nil {
		return RefactorRecoveryResult{}, err
	}
	if !recoverState {
		return RefactorRecoveryResult{Status: RefactorRecoveryRequired}, nil
	}
	recovered, err := store.Recover(ctx, observed.RepositoryID, ws.Root())
	if err != nil {
		return RefactorRecoveryResult{}, err
	}
	return RefactorRecoveryResult{Status: RefactorRecoveryRecovered, RecoveredFiles: recovered}, nil
}
