package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const (
	refactorPlanSchemaVersion     = "agentic.refactor.plan/v1alpha1"
	refactorRecoverySchemaVersion = "agentic.refactor.recovery/v1alpha1"

	// RefactorRename identifies a guarded symbol rename.
	RefactorRename = "rename"
	// RefactorFormat identifies guarded document formatting.
	RefactorFormat = "format"
	// RefactorOrganizeImports identifies a guarded import organization action.
	RefactorOrganizeImports = "organize_imports"
	// RefactorFixAll identifies guarded source.fixAll actions.
	RefactorFixAll = "fix_all"
)

var (
	errRefactorPlanNotFound     = errors.New("refactor plan not found")
	errRefactorPlanCorrupt      = errors.New("refactor plan is corrupt")
	errRefactorRecoveryRequired = errors.New("refactor recovery is required")
	errRefactorRecoveryNotFound = errors.New("refactor recovery not found")
	errRefactorRecoveryDiverged = errors.New("refactor recovery target diverged")
)

type refactorFileEdit struct {
	Path            string `json:"path"`
	PreimageDigest  string `json:"preimage_digest"`
	PostimageDigest string `json:"postimage_digest"`
	Preimage        []byte `json:"preimage"`
	Postimage       []byte `json:"postimage"`
	Mode            uint32 `json:"mode"`
}

type refactorPlan struct {
	Snapshot      SnapshotRef        `json:"snapshot"`
	SchemaVersion string             `json:"schema_version"`
	ID            string             `json:"id"`
	RepositoryID  string             `json:"repository_id"`
	Operation     string             `json:"operation"`
	Diff          string             `json:"diff"`
	Files         []refactorFileEdit `json:"files"`
	DiffTruncated bool               `json:"diff_truncated"`
}

type refactorRecovery struct {
	SchemaVersion string             `json:"schema_version"`
	PlanID        string             `json:"plan_id"`
	RepositoryID  string             `json:"repository_id"`
	Files         []refactorFileEdit `json:"files"`
}

// RefactorStore persists private immutable plans and one recovery journal per
// repository outside target worktrees.
type RefactorStore struct {
	root string
}

// NewRefactorStore creates private plan and recovery storage. An empty root
// uses the platform user cache directory.
func NewRefactorStore(root string) (*RefactorStore, error) {
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("locating user cache: %w", err)
		}
		root = filepath.Join(cache, "agentic-go", "refactors")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving refactor store: %w", err)
	}
	for _, directory := range []string{absolute, filepath.Join(absolute, "plans"), filepath.Join(absolute, "recovery")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("creating refactor store: %w", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return nil, fmt.Errorf("securing refactor store: %w", err)
		}
	}
	return &RefactorStore{root: absolute}, nil
}

func (s *RefactorStore) savePlan(ctx context.Context, plan refactorPlan) (refactorPlan, error) {
	if err := contextError(ctx); err != nil {
		return refactorPlan{}, err
	}
	plan.ID = ""
	plan.Files = append([]refactorFileEdit(nil), plan.Files...)
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	if validationErr := validateRefactorPlan(plan, false); validationErr != nil {
		return refactorPlan{}, validationErr
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return refactorPlan{}, fmt.Errorf("encoding refactor plan identity: %w", err)
	}
	plan.ID = "rfp_" + hex.EncodeToString(hashBytes(encoded))
	if validationErr := validateRefactorPlan(plan, true); validationErr != nil {
		return refactorPlan{}, validationErr
	}
	repositoryDir := filepath.Join(s.root, "plans", strings.TrimPrefix(plan.RepositoryID, "sha256:"))
	if mkdirErr := os.MkdirAll(repositoryDir, 0o700); mkdirErr != nil {
		return refactorPlan{}, fmt.Errorf("creating refactor plan directory: %w", mkdirErr)
	}
	if chmodErr := os.Chmod(repositoryDir, 0o700); chmodErr != nil {
		return refactorPlan{}, fmt.Errorf("securing refactor plan directory: %w", chmodErr)
	}
	encoded, err = json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return refactorPlan{}, fmt.Errorf("encoding refactor plan: %w", err)
	}
	if err := atomicWrite(s.planPath(plan.RepositoryID, plan.ID), append(encoded, '\n')); err != nil {
		return refactorPlan{}, fmt.Errorf("persisting refactor plan: %w", err)
	}
	return plan, contextError(ctx)
}

func (s *RefactorStore) loadPlan(ctx context.Context, repositoryID, planID string) (refactorPlan, error) {
	if err := contextError(ctx); err != nil {
		return refactorPlan{}, err
	}
	if !validRepositoryID(repositoryID) || !validRefactorPlanID(planID) {
		return refactorPlan{}, errRefactorPlanNotFound
	}
	encoded, err := os.ReadFile(s.planPath(repositoryID, planID))
	if err != nil {
		if os.IsNotExist(err) {
			return refactorPlan{}, errRefactorPlanNotFound
		}
		return refactorPlan{}, fmt.Errorf("reading refactor plan: %w", err)
	}
	var plan refactorPlan
	if decodeErr := json.Unmarshal(encoded, &plan); decodeErr != nil {
		return refactorPlan{}, fmt.Errorf("%w: decoding JSON", errRefactorPlanCorrupt)
	}
	if plan.ID != planID || plan.RepositoryID != repositoryID || validateRefactorPlan(plan, true) != nil {
		return refactorPlan{}, errRefactorPlanCorrupt
	}
	want := plan.ID
	plan.ID = ""
	identity, err := json.Marshal(plan)
	if err != nil || want != "rfp_"+hex.EncodeToString(hashBytes(identity)) {
		return refactorPlan{}, errRefactorPlanCorrupt
	}
	plan.ID = want
	return plan, contextError(ctx)
}

func (s *RefactorStore) beginRecovery(ctx context.Context, plan refactorPlan) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if validationErr := validateRefactorPlan(plan, true); validationErr != nil {
		return errRefactorPlanCorrupt
	}
	if _, err := s.pending(ctx, plan.RepositoryID); err == nil {
		return errRefactorRecoveryRequired
	} else if !errors.Is(err, errRefactorRecoveryNotFound) {
		return err
	}
	journal := refactorRecovery{
		SchemaVersion: refactorRecoverySchemaVersion,
		PlanID:        plan.ID, RepositoryID: plan.RepositoryID,
		Files: append([]refactorFileEdit(nil), plan.Files...),
	}
	encoded, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding refactor recovery journal: %w", err)
	}
	if err := atomicCreateExclusive(s.recoveryPath(plan.RepositoryID), append(encoded, '\n')); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errRefactorRecoveryRequired
		}
		return fmt.Errorf("persisting refactor recovery journal: %w", err)
	}
	return contextError(ctx)
}

func (s *RefactorStore) pending(ctx context.Context, repositoryID string) (refactorRecovery, error) {
	if err := contextError(ctx); err != nil {
		return refactorRecovery{}, err
	}
	if !validRepositoryID(repositoryID) {
		return refactorRecovery{}, errRefactorRecoveryNotFound
	}
	encoded, err := os.ReadFile(s.recoveryPath(repositoryID))
	if err != nil {
		if os.IsNotExist(err) {
			return refactorRecovery{}, errRefactorRecoveryNotFound
		}
		return refactorRecovery{}, fmt.Errorf("reading refactor recovery journal: %w", err)
	}
	var journal refactorRecovery
	if err := json.Unmarshal(encoded, &journal); err != nil || journal.SchemaVersion != refactorRecoverySchemaVersion ||
		journal.RepositoryID != repositoryID || !validRefactorPlanID(journal.PlanID) || len(journal.Files) == 0 {
		return refactorRecovery{}, errRefactorPlanCorrupt
	}
	for _, file := range journal.Files {
		if err := validateRefactorFile(file); err != nil {
			return refactorRecovery{}, errRefactorPlanCorrupt
		}
	}
	for index := 1; index < len(journal.Files); index++ {
		if journal.Files[index-1].Path >= journal.Files[index].Path {
			return refactorRecovery{}, errRefactorPlanCorrupt
		}
	}
	return journal, contextError(ctx)
}

func (s *RefactorStore) completeRecovery(ctx context.Context, repositoryID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	err := os.Remove(s.recoveryPath(repositoryID))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing refactor recovery journal: %w", err)
	}
	return syncDirectory(filepath.Join(s.root, "recovery"))
}

func (s *RefactorStore) recover(ctx context.Context, repositoryID, root string) (int, error) {
	journal, err := s.pending(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	plan, err := s.loadPlan(ctx, repositoryID, journal.PlanID)
	if err != nil || !reflect.DeepEqual(plan.Files, journal.Files) {
		return 0, errRefactorPlanCorrupt
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return 0, fmt.Errorf("resolving recovery workspace: %w", err)
	}
	type recoveryTarget struct {
		path  string
		state string
		file  refactorFileEdit
	}
	targets := make([]recoveryTarget, 0, len(journal.Files))
	for _, file := range journal.Files {
		if err := contextError(ctx); err != nil {
			return 0, err
		}
		path, err := containedExistingPath(root, file.Path)
		if err != nil {
			return 0, fmt.Errorf("%w: %s", errRefactorRecoveryDiverged, file.Path)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return 0, fmt.Errorf("reading recovery target %s: %w", file.Path, err)
		}
		digest := digestBytes(contents)
		state := ""
		switch digest {
		case file.PreimageDigest:
			state = "preimage"
		case file.PostimageDigest:
			state = "postimage"
		default:
			return 0, fmt.Errorf("%w: %s no longer matches its guarded preimage or postimage", errRefactorRecoveryDiverged, file.Path)
		}
		targets = append(targets, recoveryTarget{path: path, file: file, state: state})
	}
	recovered := 0
	for _, target := range targets {
		if target.state != "postimage" {
			continue
		}
		if err := contextError(ctx); err != nil {
			return recovered, err
		}
		if err := atomicReplace(target.path, target.file.Preimage, os.FileMode(target.file.Mode)); err != nil {
			return recovered, fmt.Errorf("restoring %s: %w", target.file.Path, err)
		}
		recovered++
	}
	if err := s.completeRecovery(ctx, repositoryID); err != nil {
		return recovered, err
	}
	return recovered, nil
}

func (s *RefactorStore) planPath(repositoryID, planID string) string {
	return filepath.Join(s.root, "plans", strings.TrimPrefix(repositoryID, "sha256:"), planID+".json")
}

func (s *RefactorStore) recoveryPath(repositoryID string) string {
	return filepath.Join(s.root, "recovery", strings.TrimPrefix(repositoryID, "sha256:")+".json")
}

func validateRefactorPlan(plan refactorPlan, requireID bool) error {
	if plan.SchemaVersion != refactorPlanSchemaVersion || !validRepositoryID(plan.RepositoryID) ||
		plan.Snapshot.ID == "" || plan.Snapshot.RepositoryID != plan.RepositoryID || len(plan.Files) == 0 || plan.Files == nil {
		return errRefactorPlanCorrupt
	}
	if requireID && !validRefactorPlanID(plan.ID) {
		return errRefactorPlanCorrupt
	}
	switch plan.Operation {
	case RefactorRename, RefactorFormat, RefactorOrganizeImports, RefactorFixAll:
	default:
		return errRefactorPlanCorrupt
	}
	previous := ""
	for _, file := range plan.Files {
		if err := validateRefactorFile(file); err != nil || (previous != "" && file.Path <= previous) {
			return errRefactorPlanCorrupt
		}
		previous = file.Path
	}
	return nil
}

func validateRefactorFile(file refactorFileEdit) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path)))
	if file.Path == "" || filepath.IsAbs(file.Path) || clean != file.Path || clean == ".." || strings.HasPrefix(clean, "../") {
		return errRefactorPlanCorrupt
	}
	if file.PreimageDigest != digestBytes(file.Preimage) || file.PostimageDigest != digestBytes(file.Postimage) || file.PreimageDigest == file.PostimageDigest {
		return errRefactorPlanCorrupt
	}
	return nil
}

func validRefactorPlanID(id string) bool {
	return strings.HasPrefix(id, "rfp_") && validHex(strings.TrimPrefix(id, "rfp_"), 64)
}

func digestBytes(contents []byte) string {
	return "sha256:" + hex.EncodeToString(hashBytes(contents))
}

func hashBytes(contents []byte) []byte {
	hash := sha256.Sum256(contents)
	return hash[:]
}

func containedExistingPath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if relative == "" || filepath.IsAbs(relative) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid contained path")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return path, nil
}

func atomicReplace(path string, contents []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentic-go-refactor-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = tmp.Chmod(mode.Perm()); err == nil {
		_, err = tmp.Write(contents)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func atomicCreateExclusive(path string, contents []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentic-go-journal-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(contents)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Link(tmpName, path); err != nil {
		return err
	}
	if err := os.Remove(tmpName); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	err = directory.Sync()
	if closeErr := directory.Close(); err == nil {
		err = closeErr
	}
	return err
}
