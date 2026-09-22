package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/intelligence"
	"github.com/agentic-mcps/go/internal/trace"
	"github.com/agentic-mcps/go/internal/verification"
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
	Cursor             string `json:"cursor,omitempty" jsonschema:"snapshot-bound continuation cursor"`
	ExpectedSnapshotID string `json:"expected_snapshot_id,omitempty" jsonschema:"reject the request unless this snapshot is still current"`
	Limit              int    `json:"limit,omitempty" jsonschema:"maximum 100; default 20"`
}

// SymbolContextInput selects a symbol by opaque reference or source position.
type SymbolContextInput struct {
	SymbolRef          string `json:"symbol_ref,omitempty" jsonschema:"opaque snapshot-bound symbol reference"`
	File               string `json:"file,omitempty" jsonschema:"workspace-relative Go file"`
	ExpectedSnapshotID string `json:"expected_snapshot_id,omitempty" jsonschema:"reject the request unless this snapshot is still current"`
	Line               int    `json:"line,omitempty" jsonschema:"one-based source line"`
	Column             int    `json:"column,omitempty" jsonschema:"one-based UTF-8 byte column"`
	MaxBytes           int    `json:"max_bytes,omitempty" jsonschema:"optional response budget; default 16384"`
	CallHierarchy      bool   `json:"call_hierarchy,omitempty"`
	TypeDefinition     bool   `json:"type_definition,omitempty"`
}

// ContextInput is the additive go_context MCP input contract.
//
//nolint:govet // Field order follows the public input contract.
type ContextInput struct {
	Base               string   `json:"base" jsonschema:"local commit or ref to compare with HEAD and the final worktree"`
	Package            string   `json:"package,omitempty" jsonschema:"Go package scope; default ./..."`
	ExpectedSnapshotID string   `json:"expected_snapshot_id,omitempty" jsonschema:"reject unless this snapshot is current"`
	FailOn             string   `json:"fail_on,omitempty" jsonschema:"verification severity policy: error, warning, info, or none"`
	MinChangedCoverage *float64 `json:"min_changed_coverage,omitempty" jsonschema:"verification coverage policy from 0 through 100"`
	MaxPackages        int      `json:"max_packages,omitempty" jsonschema:"maximum affected package closure; default 200"`
	Race               bool     `json:"race,omitempty" jsonschema:"require race evidence for applicability"`
	Query              string   `json:"query,omitempty" jsonschema:"one mutually exclusive focus selector; start with query, symbol_ref, file+line+column, focus_file, or focus_package"`
	SymbolRef          string   `json:"symbol_ref,omitempty" jsonschema:"one mutually exclusive current snapshot-bound focus selector; stale refs are rejected"`
	File               string   `json:"file,omitempty" jsonschema:"source-position focus selector; provide with line and column; mutually exclusive with query and symbol_ref"`
	Line               int      `json:"line,omitempty" jsonschema:"source-position selector line; use with file and column"`
	Column             int      `json:"column,omitempty" jsonschema:"source-position selector one-based UTF-8 byte column; use with file and line"`
	MaxBytes           int      `json:"max_bytes,omitempty" jsonschema:"focused evidence budget; default 8192"`
	PreviousPackID     string   `json:"previous_pack_id,omitempty" jsonschema:"full-replacement refresh selector; after edits send base and this only; stale selectors and refs are rejected"`
	FocusFile          string   `json:"focus_file,omitempty" jsonschema:"one mutually exclusive active Go file selector; start with one selector"`
	FocusPackage       string   `json:"focus_package,omitempty" jsonschema:"one mutually exclusive active Go package selector; start with one selector"`
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

// RegisterContext adds the post-v1 focus tool after the frozen registry.
func RegisterContext(server *mcp.Server, runtime *Runtime) {
	mcp.AddTool(server, &mcp.Tool{Name: "go_context", Description: "Before editing unfamiliar or cross-package Go code, call with base and one selector group (query, symbol_ref, file+line+column, or focus_file/focus_package) to map impact and verification applicability. After editing, refresh with base and previous_pack_id only; stale selectors and refs are expected to be rejected, so select current evidence.", Annotations: intelligenceAnnotations()}, runtime.context)
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

func (r *Runtime) context(ctx context.Context, _ *mcp.CallToolRequest, input ContextInput) (call *mcp.CallToolResult, result intelligence.FocusResult, returnErr error) {
	started := time.Now()
	var tracer *trace.Tracer
	if r != nil {
		tracer = r.tracer
	}
	defer func() {
		if tracer == nil {
			return
		}
		event := trace.Event{Tool: "go_context", Args: input, Duration: time.Since(started)}
		if returnErr != nil {
			event.ErrorKind = contextTraceErrorKind(returnErr)
		} else {
			event.ResultSummary = contextTraceSummary(result)
		}
		_ = tracer.Record(event)
	}()

	service, err := r.requireIntelligence()
	if err != nil {
		return nil, intelligence.FocusResult{}, err
	}
	request := intelligence.FocusRequest{
		Base: input.Base, Scope: input.Package, ExpectedSnapshotID: input.ExpectedSnapshotID,
		FailOn: verification.FailOn(input.FailOn), MinChangedCoverage: input.MinChangedCoverage,
		MaxPackages: input.MaxPackages, Race: input.Race, Query: input.Query,
		SymbolRef: intelligence.SymbolRef(input.SymbolRef), MaxBytes: input.MaxBytes,
		PreviousPackID: input.PreviousPackID,
		FocusFile:      input.FocusFile, FocusPackage: input.FocusPackage,
	}
	if input.File != "" || input.Line != 0 || input.Column != 0 {
		request.Position = &intelligence.SourcePosition{File: input.File, Line: input.Line, Column: input.Column}
	}
	result, err = service.Focus(ctx, request)
	if err != nil {
		return nil, intelligence.FocusResult{}, fmt.Errorf("building change context: %w", err)
	}
	text := intelligence.FocusSummary(result) + "; canonical evidence is in structuredContent"
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, result, nil
}

func contextTraceErrorKind(err error) trace.ErrorKind {
	switch {
	case errors.Is(err, context.Canceled):
		return trace.ErrorCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return trace.ErrorDeadline
	default:
		return trace.ErrorInternal
	}
}

func contextTraceSummary(result intelligence.FocusResult) string {
	truncated := false
	if result.Context != nil {
		truncated = result.Context.Truncated
	}
	refresh := "none"
	if result.Refresh != nil {
		switch result.Refresh.Status {
		case "replaced":
			refresh = "replaced"
		default:
			refresh = "other"
		}
	}
	return fmt.Sprintf("changed_files=%d; impacted_packages=%d; complete=%t; truncated=%t; verification_applicable=%t; refresh=%s", result.Change.FilesTotal, result.Impact.PackagesTotal, result.Complete, truncated, result.Verification.Applicable, refresh)
}
