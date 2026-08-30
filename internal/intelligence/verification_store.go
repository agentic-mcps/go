package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentic-mcps/go/internal/verification"
)

var (
	// ErrVerificationNotFound means no private report exists for the repository.
	ErrVerificationNotFound = errors.New("verification report not found")
	// ErrVerificationCorrupt means private report state violates its identity.
	ErrVerificationCorrupt = errors.New("verification report is corrupt")
)

type latestVerification struct {
	ID string `json:"id"`
}

// VerificationStore persists content-addressed reports privately outside the
// target worktree.
type VerificationStore struct {
	root string
}

// NewVerificationStore creates private report storage. An empty root uses the
// platform user cache directory.
func NewVerificationStore(root string) (*VerificationStore, error) {
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("locating user cache: %w", err)
		}
		root = filepath.Join(cache, "agentic-go", "verifications")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving verification store: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("creating verification store: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("securing verification store: %w", err)
	}
	return &VerificationStore{root: absolute}, nil
}

// Save atomically persists a finalized report and advances the repository's
// private latest pointer.
func (s *VerificationStore) Save(ctx context.Context, repositoryID string, report verification.Report) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !validRepositoryID(repositoryID) || report.Snapshot.CurrentID == "" || report.ValidateID() != nil {
		return ErrVerificationCorrupt
	}
	repositoryDir := filepath.Join(s.root, strings.TrimPrefix(repositoryID, "sha256:"))
	if mkdirErr := os.MkdirAll(repositoryDir, 0o700); mkdirErr != nil {
		return fmt.Errorf("creating verification repository directory: %w", mkdirErr)
	}
	if chmodErr := os.Chmod(repositoryDir, 0o700); chmodErr != nil {
		return fmt.Errorf("securing verification repository directory: %w", chmodErr)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding verification report: %w", err)
	}
	if writeErr := atomicWrite(filepath.Join(repositoryDir, report.ID+".json"), append(encoded, '\n')); writeErr != nil {
		return fmt.Errorf("persisting verification report: %w", writeErr)
	}
	pointer, err := json.Marshal(latestVerification{ID: report.ID})
	if err != nil {
		return fmt.Errorf("encoding latest verification pointer: %w", err)
	}
	if writeErr := atomicWrite(filepath.Join(repositoryDir, "latest.json"), append(pointer, '\n')); writeErr != nil {
		return fmt.Errorf("persisting latest verification pointer: %w", writeErr)
	}
	return contextError(ctx)
}

// Current returns the latest validated private report for a repository.
func (s *VerificationStore) Current(ctx context.Context, repositoryID string) (verification.Report, error) {
	if err := contextError(ctx); err != nil {
		return verification.Report{}, err
	}
	if !validRepositoryID(repositoryID) {
		return verification.Report{}, ErrVerificationNotFound
	}
	repositoryDir := filepath.Join(s.root, strings.TrimPrefix(repositoryID, "sha256:"))
	pointerBytes, err := os.ReadFile(filepath.Join(repositoryDir, "latest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return verification.Report{}, ErrVerificationNotFound
		}
		return verification.Report{}, fmt.Errorf("reading latest verification pointer: %w", err)
	}
	var pointer latestVerification
	if decodeErr := json.Unmarshal(pointerBytes, &pointer); decodeErr != nil || !validVerificationID(pointer.ID) {
		return verification.Report{}, ErrVerificationCorrupt
	}
	reportBytes, err := os.ReadFile(filepath.Join(repositoryDir, pointer.ID+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return verification.Report{}, ErrVerificationCorrupt
		}
		return verification.Report{}, fmt.Errorf("reading verification report: %w", err)
	}
	var report verification.Report
	if decodeErr := json.Unmarshal(reportBytes, &report); decodeErr != nil || report.ID != pointer.ID || report.ValidateStoredID() != nil {
		return verification.Report{}, ErrVerificationCorrupt
	}
	return report, contextError(ctx)
}

func validVerificationID(id string) bool {
	return strings.HasPrefix(id, "verify_") && validHex(strings.TrimPrefix(id, "verify_"), 64)
}
