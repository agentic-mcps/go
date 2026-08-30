package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/agentic-mcps/go/internal/intelligence"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// BeginChangeInput selects the initial private Change Contract boundary.
type BeginChangeInput struct { //nolint:govet // Field order follows the public request contract.
	Base            string                          `json:"base" jsonschema:"required local base ref"`
	Goal            string                          `json:"goal" jsonschema:"required human-written change goal"`
	Package         string                          `json:"package,omitempty"`
	FocusedPaths    []string                        `json:"focused_paths,omitempty"`
	FocusedPackages []string                        `json:"focused_packages,omitempty"`
	FocusedSymbols  []string                        `json:"focused_symbols,omitempty"`
	AllowedPaths    []string                        `json:"allowed_paths,omitempty"`
	Policies        intelligence.StructuralPolicies `json:"policies,omitempty"`
}

// CheckpointChangeInput selects one exact Change Contract lineage transition.
type CheckpointChangeInput struct {
	ContractID          string   `json:"contract_id" jsonschema:"required"`
	ExpectedSnapshotID  string   `json:"expected_snapshot_id" jsonschema:"required"`
	Decisions           []string `json:"decisions,omitempty"`
	UnresolvedQuestions []string `json:"unresolved_questions,omitempty"`
}

func changeAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false, OpenWorldHint: boolPtr(false)}
}

// RegisterChangeTools registers private Change Contract continuity operations.
func RegisterChangeTools(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_begin_change", Description: "Creates a private Change Contract for continuity; does not edit source.", Annotations: changeAnnotations()}, runtime.beginChange)
	mcp.AddTool(server, &mcp.Tool{Name: "go_checkpoint_change", Description: "Updates a private Change Contract with structural drift; does not edit source.", Annotations: changeAnnotations()}, runtime.checkpointChange)
}

func (r *Runtime) beginChange(ctx context.Context, _ *mcp.CallToolRequest, input BeginChangeInput) (*mcp.CallToolResult, intelligence.ChangeContract, error) {
	if err := validateChangeBaseGoal(input.Base, input.Goal); err != nil {
		return nil, intelligence.ChangeContract{}, err
	}
	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.ChangeContract{}, err
	}
	focusedSymbols := make([]intelligence.SymbolRef, len(input.FocusedSymbols))
	for i, ref := range input.FocusedSymbols {
		focusedSymbols[i] = intelligence.SymbolRef(ref)
	}
	contract, err := service.Begin(ctx, intelligence.BeginRequest{Base: input.Base, Goal: input.Goal, Scope: input.Package, FocusedPaths: input.FocusedPaths, FocusedPackages: input.FocusedPackages, FocusedSymbols: focusedSymbols, AllowedPaths: input.AllowedPaths, Policies: input.Policies})
	if err != nil {
		return nil, intelligence.ChangeContract{}, fmt.Errorf("beginning change: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("private Change Contract %s created; it does not edit source", contract.ID)}}}, contract, nil
}

func (r *Runtime) checkpointChange(ctx context.Context, _ *mcp.CallToolRequest, input CheckpointChangeInput) (*mcp.CallToolResult, intelligence.Checkpoint, error) {
	if strings.TrimSpace(input.ContractID) == "" || strings.TrimSpace(input.ExpectedSnapshotID) == "" {
		return nil, intelligence.Checkpoint{}, fmt.Errorf("contract_id and expected_snapshot_id are required")
	}
	if strings.IndexFunc(input.ContractID+input.ExpectedSnapshotID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return nil, intelligence.Checkpoint{}, fmt.Errorf("contract_id and expected_snapshot_id must be single tokens")
	}
	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.Checkpoint{}, err
	}
	checkpoint, err := service.Checkpoint(ctx, intelligence.CheckpointRequest{ContractID: input.ContractID, ExpectedSnapshot: input.ExpectedSnapshotID, Decisions: input.Decisions, UnresolvedQuestions: input.UnresolvedQuestions})
	if err != nil {
		return nil, intelligence.Checkpoint{}, fmt.Errorf("checkpointing change: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("private Change Contract %s checkpointed; it does not edit source", checkpoint.ContractID)}}}, checkpoint, nil
}

func validateChangeBaseGoal(base, goal string) error {
	if strings.TrimSpace(base) == "" || strings.HasPrefix(base, "-") || strings.IndexFunc(base, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("base is invalid")
	}
	if strings.TrimSpace(goal) == "" {
		return fmt.Errorf("goal is required")
	}
	return nil
}
