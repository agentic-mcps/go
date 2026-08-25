package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/intelligence"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WorkspaceBriefInput selects the compact, source-grounded workspace overview.
type WorkspaceBriefInput struct {
	Base               string `json:"base,omitempty" jsonschema:"optional local base ref for change context"`
	Package            string `json:"package,omitempty" jsonschema:"optional Go package scope; default ./..."`
	ExpectedSnapshotID string `json:"expected_snapshot_id,omitempty" jsonschema:"reject the request unless this snapshot is still current"`
	MaxBytes           int    `json:"max_bytes,omitempty" jsonschema:"optional response budget; default 8192"`
}

// SearchInput selects a page of workspace symbols.
type SearchInput struct {
	Query              string `json:"query" jsonschema:"workspace symbol query"`
	Package            string `json:"package,omitempty" jsonschema:"optional Go package scope"`
	Limit              int    `json:"limit,omitempty" jsonschema:"maximum 100; default 20"`
	Cursor             string `json:"cursor,omitempty" jsonschema:"snapshot-bound continuation cursor"`
	ExpectedSnapshotID string `json:"expected_snapshot_id,omitempty" jsonschema:"reject the request unless this snapshot is still current"`
}

// SymbolContextInput selects a symbol by opaque reference or source position.
type SymbolContextInput struct {
	SymbolRef          string `json:"symbol_ref,omitempty" jsonschema:"opaque snapshot-bound symbol reference"`
	File               string `json:"file,omitempty" jsonschema:"workspace-relative Go file"`
	Line               int    `json:"line,omitempty" jsonschema:"one-based source line"`
	Column             int    `json:"column,omitempty" jsonschema:"one-based UTF-8 byte column"`
	CallHierarchy      bool   `json:"call_hierarchy,omitempty"`
	TypeDefinition     bool   `json:"type_definition,omitempty"`
	MaxBytes           int    `json:"max_bytes,omitempty" jsonschema:"optional response budget; default 16384"`
	ExpectedSnapshotID string `json:"expected_snapshot_id,omitempty" jsonschema:"reject the request unless this snapshot is still current"`
}

func intelligenceAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)}
}

// RegisterWorkspaceBrief registers the compact workspace brief adapter.
func RegisterWorkspaceBrief(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_workspace_brief", Description: "Returns a compact, source-grounded brief of the Go workspace.", Annotations: intelligenceAnnotations()}, runtime.workspaceBrief)
}

// RegisterSearch registers the workspace-symbol search adapter.
func RegisterSearch(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_search", Description: "Searches workspace symbols with snapshot-bound source provenance.", Annotations: intelligenceAnnotations()}, runtime.search)
}

// RegisterSymbolContext registers the symbol-context adapter.
func RegisterSymbolContext(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_symbol_context", Description: "Returns source-grounded context and relationships for one Go symbol.", Annotations: intelligenceAnnotations()}, runtime.symbolContext)
}

func (r *Runtime) requireIntelligence() (IntelligenceService, error) {
	if r == nil || r.intelligence == nil {
		return nil, fmt.Errorf("intelligence service is unavailable")
	}
	return r.intelligence, nil
}

func (r *Runtime) workspaceBrief(ctx context.Context, _ *mcp.CallToolRequest, input WorkspaceBriefInput) (*mcp.CallToolResult, intelligence.ContextPack, error) {
	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.ContextPack{}, err
	}
	if input.Base != "" && invalidSingleArgument(input.Base) {
		return nil, intelligence.ContextPack{}, fmt.Errorf("base is invalid")
	}
	if input.Package != "" && invalidSingleArgument(input.Package) {
		return nil, intelligence.ContextPack{}, fmt.Errorf("package is invalid")
	}
	pack, err := service.Brief(ctx, intelligence.BriefRequest{Base: input.Base, Scope: input.Package, ExpectedSnapshotID: input.ExpectedSnapshotID, MaxBytes: input.MaxBytes})
	if err != nil {
		return nil, intelligence.ContextPack{}, fmt.Errorf("workspace brief: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("workspace brief at snapshot %s: %d packages, %d diagnostics; canonical context is in structuredContent", pack.Snapshot.ID, len(pack.Packages), len(pack.Diagnostics))}}}, pack, nil
}

func (r *Runtime) search(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, intelligence.SearchResult, error) {
	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.SearchResult{}, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return nil, intelligence.SearchResult{}, fmt.Errorf("query is required")
	}
	if input.Package != "" && invalidSingleArgument(input.Package) {
		return nil, intelligence.SearchResult{}, fmt.Errorf("package is invalid")
	}
	result, err := service.Search(ctx, intelligence.SearchRequest{Query: input.Query, Scope: input.Package, ExpectedSnapshotID: input.ExpectedSnapshotID, Limit: input.Limit, Cursor: input.Cursor})
	if err != nil {
		return nil, intelligence.SearchResult{}, fmt.Errorf("searching symbols: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("workspace search found %d of %d symbols at snapshot %s; canonical results are in structuredContent", len(result.Matches), result.Total, result.Snapshot.ID)}}}, result, nil
}

func (r *Runtime) symbolContext(ctx context.Context, _ *mcp.CallToolRequest, input SymbolContextInput) (*mcp.CallToolResult, intelligence.SymbolContext, error) {
	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.SymbolContext{}, err
	}
	request := intelligence.SymbolRequest{Ref: intelligence.SymbolRef(input.SymbolRef), ExpectedSnapshotID: input.ExpectedSnapshotID, Facets: intelligence.SymbolFacets{CallHierarchy: input.CallHierarchy, TypeDefinition: input.TypeDefinition}, MaxBytes: input.MaxBytes}
	if input.SymbolRef == "" {
		if input.File == "" || input.Line < 1 || input.Column < 1 {
			return nil, intelligence.SymbolContext{}, fmt.Errorf("symbol_ref or positive file, line, and column is required")
		}
		request.Position = &intelligence.SourcePosition{File: input.File, Line: input.Line, Column: input.Column}
	} else if input.File != "" || input.Line != 0 || input.Column != 0 {
		return nil, intelligence.SymbolContext{}, fmt.Errorf("symbol_ref cannot be combined with file, line, or column")
	}
	result, err := service.Symbol(ctx, request)
	if err != nil {
		return nil, intelligence.SymbolContext{}, fmt.Errorf("resolving symbol context: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("symbol context for %s at snapshot %s; canonical context is in structuredContent", result.Symbol.Name, result.Snapshot.ID)}}}, result, nil
}
