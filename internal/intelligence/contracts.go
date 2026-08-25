package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrContractNotFound means no contained private contract matches the ID.
	ErrContractNotFound = errors.New("change contract not found")
	// ErrContractCorrupt means persisted contract state violates its schema.
	ErrContractCorrupt = errors.New("change contract is corrupt")
)

// ContractStore persists private Change Contracts outside target worktrees.
type ContractStore struct {
	root string
}

// NewContractStore creates a private contract store. An empty root uses the
// platform user cache directory.
func NewContractStore(root string) (*ContractStore, error) {
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("locating user cache: %w", err)
		}
		root = filepath.Join(cache, "agentic-go", "contracts")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving contract store: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("creating contract store: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("securing contract store: %w", err)
	}
	return &ContractStore{root: absolute}, nil
}

// DefaultStructuralPolicies returns the v0.5 machine-checkable defaults.
func DefaultStructuralPolicies() StructuralPolicies {
	return StructuralPolicies{
		OutsideAllowedPaths: PolicyForbid,
		OutsideFocus:        PolicyWarn,
		ExportedAPI:         PolicyWarn,
		Dependency:          PolicyWarn,
		CrossModule:         PolicyWarn,
		GeneratedFile:       PolicyForbid,
		TestDeletion:        PolicyWarn,
	}
}

func normalizeStructuralPolicies(policies *StructuralPolicies) error {
	if policies == nil {
		return fmt.Errorf("policies are nil")
	}
	defaults := DefaultStructuralPolicies()
	values := []struct {
		name         string
		value        *PolicyMode
		defaultValue PolicyMode
	}{
		{"outside_allowed_paths", &policies.OutsideAllowedPaths, defaults.OutsideAllowedPaths},
		{"outside_focus", &policies.OutsideFocus, defaults.OutsideFocus},
		{"exported_api", &policies.ExportedAPI, defaults.ExportedAPI},
		{"dependency", &policies.Dependency, defaults.Dependency},
		{"cross_module", &policies.CrossModule, defaults.CrossModule},
		{"generated_file", &policies.GeneratedFile, defaults.GeneratedFile},
		{"test_deletion", &policies.TestDeletion, defaults.TestDeletion},
	}
	for _, item := range values {
		if *item.value == "" {
			*item.value = item.defaultValue
		}
		if *item.value != PolicyAllow && *item.value != PolicyWarn && *item.value != PolicyForbid {
			return fmt.Errorf("%s policy must be allow, warn, or forbid", item.name)
		}
	}
	return nil
}

// Save atomically writes one validated contract with private permissions.
func (s *ContractStore) Save(ctx context.Context, contract ChangeContract) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := validateContract(contract); err != nil {
		return err
	}
	repositoryDir, err := s.repositoryDir(contract.RepositoryID)
	if err != nil {
		return err
	}
	if mkdirErr := os.MkdirAll(repositoryDir, 0o700); mkdirErr != nil {
		return fmt.Errorf("creating contract repository directory: %w", mkdirErr)
	}
	if chmodErr := os.Chmod(repositoryDir, 0o700); chmodErr != nil {
		return fmt.Errorf("securing contract repository directory: %w", chmodErr)
	}
	encoded, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding change contract: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := atomicWrite(filepath.Join(repositoryDir, contract.ID+".json"), encoded); err != nil {
		return fmt.Errorf("persisting change contract: %w", err)
	}
	return contextError(ctx)
}

// Load reads one repository-bound contract without exposing its cache path.
func (s *ContractStore) Load(ctx context.Context, repositoryID, contractID string) (ChangeContract, error) {
	if err := contextError(ctx); err != nil {
		return ChangeContract{}, err
	}
	repositoryDir, err := s.repositoryDir(repositoryID)
	if err != nil || !validContractID(contractID) {
		return ChangeContract{}, ErrContractNotFound
	}
	encoded, err := os.ReadFile(filepath.Join(repositoryDir, contractID+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return ChangeContract{}, ErrContractNotFound
		}
		return ChangeContract{}, fmt.Errorf("reading change contract: %w", err)
	}
	var contract ChangeContract
	if err := json.Unmarshal(encoded, &contract); err != nil {
		return ChangeContract{}, fmt.Errorf("%w: decoding JSON", ErrContractCorrupt)
	}
	if contract.ID != contractID || contract.RepositoryID != repositoryID {
		return ChangeContract{}, fmt.Errorf("%w: repository or contract identity mismatch", ErrContractCorrupt)
	}
	if err := validateContract(contract); err != nil {
		return ChangeContract{}, fmt.Errorf("%w: %v", ErrContractCorrupt, err)
	}
	return contract, contextError(ctx)
}

// Current returns the most recently updated active contract for a repository.
func (s *ContractStore) Current(ctx context.Context, repositoryID string) (ChangeContract, error) {
	repositoryDir, err := s.repositoryDir(repositoryID)
	if err != nil {
		return ChangeContract{}, ErrContractNotFound
	}
	entries, err := os.ReadDir(repositoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return ChangeContract{}, ErrContractNotFound
		}
		return ChangeContract{}, fmt.Errorf("listing change contracts: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".json") {
			id := strings.TrimSuffix(entry.Name(), ".json")
			if validContractID(id) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	var current ChangeContract
	found := false
	for _, id := range ids {
		if err := contextError(ctx); err != nil {
			return ChangeContract{}, err
		}
		contract, loadErr := s.Load(ctx, repositoryID, id)
		if loadErr != nil {
			return ChangeContract{}, loadErr
		}
		if !contract.Active {
			continue
		}
		if !found || contract.UpdatedAt.After(current.UpdatedAt) || (contract.UpdatedAt.Equal(current.UpdatedAt) && contract.ID > current.ID) {
			current, found = contract, true
		}
	}
	if !found {
		return ChangeContract{}, ErrContractNotFound
	}
	return current, nil
}

func (s *ContractStore) repositoryDir(repositoryID string) (string, error) {
	if !validRepositoryID(repositoryID) {
		return "", ErrContractNotFound
	}
	return filepath.Join(s.root, strings.TrimPrefix(repositoryID, "sha256:")), nil
}

func validRepositoryID(id string) bool {
	return strings.HasPrefix(id, "sha256:") && validHex(strings.TrimPrefix(id, "sha256:"), 64)
}

func validContractID(id string) bool {
	return strings.HasPrefix(id, "chg_") && validHex(strings.TrimPrefix(id, "chg_"), 64)
}

func validHex(value string, size int) bool {
	return len(value) == size && strings.Trim(value, "0123456789abcdef") == ""
}

func validateContract(contract ChangeContract) error {
	switch {
	case contract.SchemaVersion != ChangeSchemaVersion:
		return fmt.Errorf("unsupported change contract schema %q", contract.SchemaVersion)
	case !validContractID(contract.ID):
		return fmt.Errorf("invalid change contract ID")
	case !validRepositoryID(contract.RepositoryID):
		return fmt.Errorf("invalid repository identity")
	case strings.TrimSpace(contract.Goal) == "":
		return fmt.Errorf("change goal is required")
	case strings.TrimSpace(contract.Base) == "":
		return fmt.Errorf("change base is required")
	case strings.TrimSpace(contract.Scope) == "":
		return fmt.Errorf("package scope is required")
	case contract.InitialSnapshot.RepositoryID != contract.RepositoryID || contract.LatestSnapshot.RepositoryID != contract.RepositoryID:
		return fmt.Errorf("snapshot repository identity mismatch")
	case contract.InitialSnapshot.ID == "" || contract.LatestSnapshot.ID == "":
		return fmt.Errorf("snapshot identity is required")
	case contract.FocusedPaths == nil || contract.FocusedPackages == nil || contract.FocusedSymbols == nil || contract.AllowedPaths == nil:
		return fmt.Errorf("focus and allowed collections must be non-null")
	case contract.Decisions == nil || contract.UnresolvedQuestions == nil || contract.Checkpoints == nil:
		return fmt.Errorf("handoff collections must be non-null")
	case contract.CreatedAt.IsZero() || contract.UpdatedAt.IsZero():
		return fmt.Errorf("contract timestamps are required")
	}
	policies := contract.Policies
	if err := normalizeStructuralPolicies(&policies); err != nil || policies != contract.Policies {
		if err != nil {
			return err
		}
		return fmt.Errorf("contract policies are not normalized")
	}
	return nil
}
