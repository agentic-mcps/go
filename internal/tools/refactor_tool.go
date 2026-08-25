package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/ashwingopalsamy/agentic-go/internal/intelligence"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RefactorInput previews or explicitly applies one guarded refactor plan.
type RefactorInput struct { //nolint:govet // Field order follows the public request contract.
	Operation          string   `json:"operation,omitempty" jsonschema:"preview operation: rename, format, organize_imports, or fix_all"`
	SymbolRef          string   `json:"symbol_ref,omitempty" jsonschema:"snapshot-bound symbol reference required for rename preview"`
	NewName            string   `json:"new_name,omitempty" jsonschema:"new Go identifier required for rename preview"`
	Files              []string `json:"files,omitempty" jsonschema:"existing workspace-relative files for formatting or allowed fix actions"`
	PlanID             string   `json:"plan_id,omitempty" jsonschema:"content-addressed preview plan required for apply"`
	ExpectedSnapshotID string   `json:"expected_snapshot_id" jsonschema:"required exact snapshot used for preview or apply"`
	Apply              bool     `json:"apply,omitempty" jsonschema:"apply the exact stored plan after explicit client approval"`
}

// RegisterRefactor registers guarded preview and apply through one plan-bound
// operation. Static annotations describe the apply-capable trust boundary.
func RegisterRefactor(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "go_refactor",
		Description: "Previews deterministic gopls refactors and applies only an explicitly approved, snapshot-bound plan to existing contained non-generated files. It does not stage, commit, or change Git history.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false, OpenWorldHint: boolPtr(false),
		},
	}, runtime.refactor)
}

func (r *Runtime) refactor(ctx context.Context, _ *mcp.CallToolRequest, input RefactorInput) (*mcp.CallToolResult, intelligence.RefactorResult, error) {
	if err := validateRefactorInput(input); err != nil {
		return nil, intelligence.RefactorResult{}, err
	}
	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.RefactorResult{}, err
	}
	result, err := service.Refactor(ctx, intelligence.RefactorRequest{
		Operation: input.Operation, Ref: intelligence.SymbolRef(input.SymbolRef), NewName: input.NewName,
		Files: input.Files, PlanID: input.PlanID, ExpectedSnapshotID: input.ExpectedSnapshotID, Apply: input.Apply,
	})
	if err != nil {
		return nil, intelligence.RefactorResult{}, fmt.Errorf("guarded refactor: %w", err)
	}
	state := "previewed"
	if result.Applied {
		state = "applied"
	}
	text := fmt.Sprintf("guarded refactor %s: %d affected files at snapshot %s; canonical plan evidence is in structuredContent", state, len(result.AffectedFiles), result.Snapshot.ID)
	if result.PlanID == "" {
		text = fmt.Sprintf("guarded refactor preview found no source edits at snapshot %s", result.Snapshot.ID)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, result, nil
}

func validateRefactorInput(input RefactorInput) error {
	if strings.TrimSpace(input.ExpectedSnapshotID) == "" || hasRefactorControl(input.ExpectedSnapshotID) {
		return fmt.Errorf("expected_snapshot_id is required as one token")
	}
	if input.Apply {
		if strings.TrimSpace(input.PlanID) == "" || hasRefactorControl(input.PlanID) {
			return fmt.Errorf("plan_id is required as one token for apply")
		}
		if input.Operation != "" || input.SymbolRef != "" || input.NewName != "" || len(input.Files) != 0 {
			return fmt.Errorf("apply accepts only plan_id and expected_snapshot_id")
		}
		return nil
	}
	if input.PlanID != "" {
		return fmt.Errorf("plan_id is valid only when apply is true")
	}
	switch input.Operation {
	case intelligence.RefactorRename:
		if input.SymbolRef == "" || strings.TrimSpace(input.NewName) == "" || len(input.Files) != 0 {
			return fmt.Errorf("rename preview requires symbol_ref and new_name, without files")
		}
	case intelligence.RefactorFormat, intelligence.RefactorOrganizeImports, intelligence.RefactorFixAll:
		if len(input.Files) == 0 || input.SymbolRef != "" || input.NewName != "" {
			return fmt.Errorf("%s preview requires files, without symbol_ref or new_name", input.Operation)
		}
	default:
		return fmt.Errorf("operation must be rename, format, organize_imports, or fix_all")
	}
	for _, file := range input.Files {
		if strings.TrimSpace(file) == "" || hasRefactorControl(file) {
			return fmt.Errorf("files must contain non-empty workspace-relative paths")
		}
	}
	return nil
}

func hasRefactorControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
}
