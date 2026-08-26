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
)

type refactorSearchResult struct {
	Snapshot struct {
		ID           string `json:"id"`
		RepositoryID string `json:"repository_id"`
	} `json:"snapshot"`
	Matches []struct {
		Ref string `json:"ref"`
	} `json:"matches"`
}

type refactorToolResult struct {
	Snapshot struct {
		ID           string `json:"id"`
		RepositoryID string `json:"repository_id"`
	} `json:"snapshot"`
	PlanID        string   `json:"plan_id"`
	AffectedFiles []string `json:"affected_files"`
	Applied       bool     `json:"applied"`
}

func TestSubprocessGuardedRefactor(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	if sidecar == "" {
		t.Skip("AGENTIC_GO_GOPLS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	repository := refactorRepository(ctx, t)
	binary := buildHandoffServer(ctx, t, sidecar)
	session := connectHandoffServer(ctx, t, binary, repository)
	defer func() { _ = session.Close() }()

	searched := callHandoffTool(ctx, t, session, "go_search", map[string]any{"query": "OldName", "limit": 5})
	if searched.IsError {
		t.Fatalf("search error: %#v", searched.Content)
	}
	var search refactorSearchResult
	decodeToolResult(t, searched.StructuredContent, &search)
	if search.Snapshot.ID == "" || search.Snapshot.RepositoryID == "" || len(search.Matches) != 1 || search.Matches[0].Ref == "" {
		t.Fatalf("incomplete search result: %s", mustJSON(t, searched.StructuredContent))
	}

	previewed := callHandoffTool(ctx, t, session, "go_refactor", map[string]any{
		"operation": "rename", "symbol_ref": search.Matches[0].Ref, "new_name": "NewName",
		"expected_snapshot_id": search.Snapshot.ID,
	})
	if previewed.IsError {
		t.Fatalf("preview error: %#v", previewed.Content)
	}
	var preview refactorToolResult
	decodeToolResult(t, previewed.StructuredContent, &preview)
	cleanupRefactorPlan(t, search.Snapshot.RepositoryID, preview.PlanID)
	if preview.PlanID == "" || preview.Applied || len(preview.AffectedFiles) != 1 || preview.AffectedFiles[0] != "value.go" {
		t.Fatalf("unexpected preview: %s", mustJSON(t, previewed.StructuredContent))
	}
	assertRefactorSource(t, repository, true)

	rejected := callHandoffTool(ctx, t, session, "go_refactor", map[string]any{
		"apply": true, "plan_id": preview.PlanID, "expected_snapshot_id": search.Snapshot.ID, "operation": "rename",
	})
	if !rejected.IsError {
		t.Fatal("apply with preview arguments unexpectedly succeeded")
	}
	assertRefactorSource(t, repository, true)

	appliedResult := callHandoffTool(ctx, t, session, "go_refactor", map[string]any{
		"apply": true, "plan_id": preview.PlanID, "expected_snapshot_id": search.Snapshot.ID,
	})
	if appliedResult.IsError {
		t.Fatalf("apply error: %#v", appliedResult.Content)
	}
	var applied refactorToolResult
	decodeToolResult(t, appliedResult.StructuredContent, &applied)
	if !applied.Applied || applied.PlanID != preview.PlanID || applied.Snapshot.ID == search.Snapshot.ID || applied.Snapshot.RepositoryID != search.Snapshot.RepositoryID {
		t.Fatalf("unexpected apply: %s", mustJSON(t, appliedResult.StructuredContent))
	}
	assertRefactorSource(t, repository, false)

	testCommand := exec.CommandContext(ctx, "go", "test", "./...")
	testCommand.Dir = repository
	testCommand.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if output, err := testCommand.CombinedOutput(); err != nil {
		t.Fatalf("renamed fixture tests: %v\n%s", err, output)
	}
	status := exec.CommandContext(ctx, "git", "status", "--porcelain=v1")
	status.Dir = repository
	output, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, output)
	}
	if string(output) != " M value.go\n" {
		t.Fatalf("unexpected worktree mutation: %q", output)
	}
}

func refactorRepository(ctx context.Context, t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":        "module example.com/refactorfixture\n\ngo 1.25\n",
		"value.go":      "package refactorfixture\n\nfunc OldName() int { return 1 }\nfunc Use() int { return OldName() }\n",
		"value_test.go": "package refactorfixture\n\nimport \"testing\"\n\nfunc TestUse(t *testing.T) { if Use() != 1 { t.Fatal(Use()) } }\n",
	}
	for path, contents := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
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

func decodeToolResult(t *testing.T, value any, target any) {
	t.Helper()
	if err := json.Unmarshal(mustJSON(t, value), target); err != nil {
		t.Fatal(err)
	}
}

func assertRefactorSource(t *testing.T, repository string, oldName bool) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repository, "value.go"))
	if err != nil {
		t.Fatal(err)
	}
	hasOld := strings.Contains(string(contents), "OldName")
	hasNew := strings.Contains(string(contents), "NewName")
	if hasOld != oldName || hasNew == oldName {
		t.Fatalf("unexpected refactor source: %q", contents)
	}
}

func cleanupRefactorPlan(t *testing.T, repositoryID, planID string) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	repositoryDirectory := filepath.Join(cache, "agentic-go", "refactors", "plans", strings.TrimPrefix(repositoryID, "sha256:"))
	planPath := filepath.Join(repositoryDirectory, planID+".json")
	t.Cleanup(func() {
		if err := os.Remove(planPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove test refactor plan: %v", err)
		}
		if err := os.Remove(repositoryDirectory); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove empty test refactor directory: %v", err)
		}
	})
}
