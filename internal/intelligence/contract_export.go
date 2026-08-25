package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

// ContractExportRequest selects one explicit workspace-contained handoff copy.
// An empty contract ID selects the current active contract for the repository.
type ContractExportRequest struct {
	ContractID  string
	Destination string
}

// ContractExport identifies an explicit handoff without exposing an absolute
// workspace or private cache path.
type ContractExport struct {
	ContractID string `json:"contract_id"`
	SnapshotID string `json:"snapshot_id"`
	Path       string `json:"path"`
	Digest     string `json:"digest"`
}

// ExportChangeContract writes a caller-requested, private copy into an existing
// contained workspace directory. It never overwrites an existing path.
func ExportChangeContract(
	ctx context.Context,
	ws *workspace.Workspace,
	runner *execution.Runner,
	store *ContractStore,
	request ContractExportRequest,
) (ContractExport, error) {
	if err := contextError(ctx); err != nil {
		return ContractExport{}, err
	}
	if ws == nil || runner == nil || store == nil {
		return ContractExport{}, fmt.Errorf("change contract export dependencies are incomplete")
	}
	destination, relative, err := contractExportDestination(ws, request.Destination)
	if err != nil {
		return ContractExport{}, err
	}
	snapshots, err := NewSnapshotter(ws, runner)
	if err != nil {
		return ContractExport{}, err
	}
	observed, err := snapshots.Capture(ctx, SnapshotRequest{Scope: "./...", Semantic: SemanticIdentity{Version: "contract-export"}})
	if err != nil {
		return ContractExport{}, fmt.Errorf("identifying contract repository: %w", err)
	}
	var contract ChangeContract
	if request.ContractID == "" {
		contract, err = store.Current(ctx, observed.RepositoryID)
	} else {
		contract, err = store.Load(ctx, observed.RepositoryID, request.ContractID)
	}
	if err != nil {
		return ContractExport{}, err
	}
	encoded, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return ContractExport{}, fmt.Errorf("encoding change contract export: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := contextError(ctx); err != nil {
		return ContractExport{}, err
	}
	if err := atomicCreatePrivate(destination, encoded); err != nil {
		return ContractExport{}, fmt.Errorf("exporting change contract: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return ContractExport{
		ContractID: contract.ID,
		SnapshotID: contract.LatestSnapshot.ID,
		Path:       relative,
		Digest:     fmt.Sprintf("sha256:%x", digest[:]),
	}, nil
}

func contractExportDestination(ws *workspace.Workspace, destination string) (string, string, error) {
	if strings.TrimSpace(destination) == "" || filepath.IsAbs(destination) || strings.IndexFunc(destination, unicode.IsControl) >= 0 {
		return "", "", fmt.Errorf("export destination must be a workspace-relative file path")
	}
	clean := filepath.Clean(destination)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("export destination must stay within the workspace")
	}
	for _, component := range strings.Split(filepath.ToSlash(clean), "/") {
		if component == ".git" {
			return "", "", fmt.Errorf("export destination must not enter Git metadata")
		}
	}
	parent, err := ws.Resolve(filepath.Dir(clean))
	if err != nil {
		return "", "", fmt.Errorf("resolving export directory: %w", err)
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("export directory is not an existing contained directory")
	}
	target := filepath.Join(parent, filepath.Base(clean))
	if _, statErr := os.Lstat(target); statErr == nil {
		return "", "", fmt.Errorf("export destination already exists")
	} else if !os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("inspecting export destination: %w", statErr)
	}
	relative, err := filepath.Rel(ws.Root(), target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("export destination resolves outside the workspace")
	}
	return target, filepath.ToSlash(relative), nil
}

func atomicCreatePrivate(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".change-contract-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if linkErr := os.Link(temporaryPath, path); linkErr != nil {
		return linkErr
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = directory.Sync()
	if closeErr := directory.Close(); err == nil {
		err = closeErr
	}
	return err
}
