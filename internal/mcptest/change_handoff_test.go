package mcptest

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-mcps/go/internal/verification"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type handoffContract struct {
	ID             string `json:"id"`
	RepositoryID   string `json:"repository_id"`
	LatestSnapshot struct {
		ID string `json:"id"`
	} `json:"latest_snapshot"`
	LatestVerification  string            `json:"latest_verification"`
	Decisions           []json.RawMessage `json:"decisions"`
	UnresolvedQuestions []string          `json:"unresolved_questions"`
	FocusedPaths        []string          `json:"focused_paths"`
	FocusedPackages     []string          `json:"focused_packages"`
	FocusedSymbols      []string          `json:"focused_symbols"`
	AllowedPaths        []string          `json:"allowed_paths"`
	Checkpoints         []json.RawMessage `json:"checkpoints"`
}

func TestSubprocessChangeHandoff(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	if sidecar == "" {
		t.Skip("AGENTIC_GO_GOPLS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	repository := handoffRepository(ctx, t)
	binary := buildHandoffServer(ctx, t, sidecar)

	first := connectHandoffServer(ctx, t, binary, repository)
	begin := callHandoffTool(ctx, t, first, "go_begin_change", map[string]any{
		"base": "HEAD", "goal": "change Value API", "package": "./...",
	})
	if begin.IsError {
		t.Fatalf("begin error: %#v", begin.Content)
	}
	contract := decodeHandoffContract(t, begin.StructuredContent)
	if contract.ID == "" || contract.RepositoryID == "" || contract.LatestSnapshot.ID == "" {
		t.Fatalf("incomplete begin contract: %s", mustJSON(t, begin.StructuredContent))
	}
	assertContractCollections(t, contract)
	cleanupHandoffContract(t, contract)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repository, "value.go"), []byte("package handoff\n\nfunc Value(s string) string { return s }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := connectHandoffServer(ctx, t, binary, repository)
	current := readHandoffContract(ctx, t, second)
	if current.ID != contract.ID || current.RepositoryID != contract.RepositoryID || current.LatestSnapshot.ID != contract.LatestSnapshot.ID {
		t.Fatalf("fresh process did not resume exact lineage: initial=%#v current=%#v", contract, current)
	}
	checkpoint := callHandoffTool(ctx, t, second, "go_checkpoint_change", map[string]any{
		"contract_id": current.ID, "expected_snapshot_id": current.LatestSnapshot.ID,
		"decisions": []string{"API change reviewed"}, "unresolved_questions": []string{},
	})
	if checkpoint.IsError {
		t.Fatalf("checkpoint error: %#v", checkpoint.Content)
	}
	checkpointJSON := mustJSON(t, checkpoint.StructuredContent)
	if !strings.Contains(string(checkpointJSON), "exported_api_change") {
		t.Fatalf("checkpoint omitted exported API evidence: %s", checkpointJSON)
	}
	stale := callHandoffTool(ctx, t, second, "go_checkpoint_change", map[string]any{
		"contract_id": current.ID, "expected_snapshot_id": current.LatestSnapshot.ID,
	})
	if !stale.IsError {
		t.Fatal("stale checkpoint unexpectedly succeeded")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	third := connectHandoffServer(ctx, t, binary, repository)
	handoff := readHandoffContract(ctx, t, third)
	if handoff.ID != contract.ID || handoff.LatestSnapshot.ID == contract.LatestSnapshot.ID || len(handoff.Checkpoints) != 1 || len(handoff.Decisions) != 1 {
		t.Fatalf("checkpoint lineage was not persisted across processes: %#v", handoff)
	}
	assertContractCollections(t, handoff)
	verified := callHandoffTool(ctx, t, third, "go_verify_change", map[string]any{
		"base": "HEAD", "package": "./...", "fail_on": "none",
		"contract_id": handoff.ID, "expected_snapshot_id": handoff.LatestSnapshot.ID,
	})
	if verified.IsError {
		t.Fatalf("unified verification error: %#v", verified.Content)
	}
	var report verification.Report
	if err := json.Unmarshal(mustJSON(t, verified.StructuredContent), &report); err != nil {
		t.Fatal(err)
	}
	cleanupHandoffVerification(t, handoff.RepositoryID, report.ID)
	if report.ID == "" || report.Snapshot.CurrentID != handoff.LatestSnapshot.ID || report.Result.Status != verification.ResultPass {
		t.Fatalf("unified verification = %#v", report)
	}
	latest, err := third.ReadResource(ctx, &mcp.ReadResourceParams{URI: "agentic-go://verification/latest"})
	if err != nil || len(latest.Contents) != 1 {
		t.Fatalf("latest verification resource error=%v result=%#v", err, latest)
	}
	var latestReport verification.Report
	if decodeErr := json.Unmarshal([]byte(latest.Contents[0].Text), &latestReport); decodeErr != nil || latestReport.ID != report.ID {
		t.Fatalf("latest verification = %q, error %v", latestReport.ID, decodeErr)
	}
	linked := readHandoffContract(ctx, t, third)
	if linked.LatestVerification != report.ID {
		t.Fatalf("contract verification link = %q, want %q", linked.LatestVerification, report.ID)
	}
	if closeErr := third.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	status := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	status.Dir = repository
	output, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, output)
	}
	if string(output) != " M value.go\n" {
		t.Fatalf("unexpected worktree pollution: %q", output)
	}
}

func cleanupHandoffVerification(t *testing.T, repositoryID, reportID string) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	repositoryDirectory := filepath.Join(cache, "agentic-go", "verifications", strings.TrimPrefix(repositoryID, "sha256:"))
	t.Cleanup(func() {
		for _, name := range []string{reportID + ".json", "latest.json"} {
			if err := os.Remove(filepath.Join(repositoryDirectory, name)); err != nil && !os.IsNotExist(err) {
				t.Errorf("remove test verification %s: %v", name, err)
			}
		}
		if err := os.Remove(repositoryDirectory); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove empty test verification directory: %v", err)
		}
	})
}

func handoffRepository(ctx context.Context, t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/handoff\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "value.go"), []byte("package handoff\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", "."},
		{"-c", "commit.gpgsign=false", "commit", "-m", "initial"},
	}
	for _, arguments := range commands {
		command := exec.CommandContext(ctx, "git", arguments...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	return root
}

func buildHandoffServer(ctx context.Context, t *testing.T, sidecar string) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agentic-go")
	if err := os.Symlink(sidecar, filepath.Join(filepath.Dir(binary), "agentic-go-gopls")); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/agentic-go")
	build.Dir = repositoryRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build server: %v\n%s", err, output)
	}
	return binary
}

func connectHandoffServer(ctx context.Context, t *testing.T, binary, repository string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "handoff-e2e", Version: "test"}, nil)
	command := exec.CommandContext(ctx, binary, "--workspace", repository, "--max-tool-seconds", "30")
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func callHandoffTool(ctx context.Context, t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readHandoffContract(ctx context.Context, t *testing.T, session *mcp.ClientSession) handoffContract {
	t.Helper()
	resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "agentic-go://change-contract/current"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resource.Contents) != 1 {
		t.Fatalf("contract resource contents = %d", len(resource.Contents))
	}
	var contract handoffContract
	if err := json.Unmarshal([]byte(resource.Contents[0].Text), &contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

func decodeHandoffContract(t *testing.T, value any) handoffContract {
	t.Helper()
	data := mustJSON(t, value)
	var contract handoffContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

func assertContractCollections(t *testing.T, contract handoffContract) {
	t.Helper()
	if contract.Decisions == nil || contract.UnresolvedQuestions == nil || contract.FocusedPaths == nil || contract.FocusedPackages == nil || contract.FocusedSymbols == nil || contract.AllowedPaths == nil || contract.Checkpoints == nil {
		t.Fatalf("contract contains nil collections: %#v", contract)
	}
}

func cleanupHandoffContract(t *testing.T, contract handoffContract) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	repositoryDirectory := filepath.Join(cache, "agentic-go", "contracts", strings.TrimPrefix(contract.RepositoryID, "sha256:"))
	contractPath := filepath.Join(repositoryDirectory, contract.ID+".json")
	t.Cleanup(func() {
		if err := os.Remove(contractPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove test contract: %v", err)
		}
		if err := os.Remove(repositoryDirectory); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove empty test repository contract directory: %v", err)
		}
	})
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
