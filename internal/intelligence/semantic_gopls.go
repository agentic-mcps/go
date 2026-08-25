package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/ashwingopalsamy/agentic-go/internal/gopls"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

const (
	maxImplementationResolution = 100
	maxCallHierarchyRoots       = 20
)

type goplsRPC interface {
	Request(context.Context, string, any, any, bool) error
	Notify(string, any) error
	Restart(context.Context) error
	Capabilities() (gopls.Capabilities, error)
}

// goplsProvider serializes request-boundary synchronization with semantic
// reads so every result describes exactly its declared workspace snapshot.
//
//nolint:govet // lifecycle state stays grouped; one provider exists per workspace.
type goplsProvider struct {
	manager   goplsRPC
	workspace *workspace.Workspace
	snapshots *Snapshotter
	root      string
	records   []contentRecord
	last      SnapshotRef
	mu        sync.Mutex
}

func newGoplsProvider(manager goplsRPC, ws *workspace.Workspace, snapshots *Snapshotter) (*goplsProvider, error) {
	if manager == nil {
		return nil, fmt.Errorf("gopls manager is nil")
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if snapshots == nil {
		return nil, fmt.Errorf("snapshotter is nil")
	}
	return &goplsProvider{manager: manager, workspace: ws, snapshots: snapshots, root: ws.Root()}, nil
}

func (p *goplsProvider) Identity() SemanticIdentity {
	capabilities, _ := p.manager.Capabilities()
	return SemanticIdentity{Version: gopls.SupportedVersion, Capabilities: CapabilityManifest{
		WorkspaceSymbol: capabilities.WorkspaceSymbol, Hover: capabilities.Hover,
		Definition: capabilities.Definition, TypeDefinition: capabilities.TypeDefinition,
		References: capabilities.References, Implementation: capabilities.Implementation,
		DocumentSymbol: capabilities.DocumentSymbol, CallHierarchy: capabilities.CallHierarchy,
		Diagnostics: capabilities.Diagnostics, Rename: capabilities.Rename,
		Formatting: capabilities.Formatting, CodeAction: capabilities.CodeAction,
	}}
}

func (p *goplsProvider) Read(ctx context.Context, snapshot SnapshotRef, fn func(semanticReader) error) error {
	if fn == nil {
		return fmt.Errorf("semantic read callback is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	records, found := p.snapshots.manifest(snapshot.ID)
	if !found {
		return fmt.Errorf("%w: snapshot manifest %s is unavailable", ErrSnapshotChanged, snapshot.ID)
	}
	restarted := false
	if p.last.ID != "" && mustRestartGopls(p.last, snapshot, p.records, records) {
		if err := p.manager.Restart(ctx); err != nil {
			return fmt.Errorf("restarting gopls for snapshot change: %w", err)
		}
		restarted = true
	}
	if p.last.ID != "" && !restarted {
		changes := watchedFileChanges(p.root, p.records, records)
		if len(changes) > 0 {
			if err := p.manager.Notify("workspace/didChangeWatchedFiles", map[string]any{"changes": changes}); err != nil {
				return fmt.Errorf("synchronizing gopls files: %w", err)
			}
		}
	}
	if err := fn(&goplsReader{p: p, snapshot: snapshot}); err != nil {
		return err
	}
	p.last = snapshot
	p.records = append([]contentRecord(nil), records...)
	return nil
}

func mustRestartGopls(previous, current SnapshotRef, oldRecords, newRecords []contentRecord) bool {
	if previous.HeadCommit != current.HeadCommit || previous.GoplsVersion != current.GoplsVersion ||
		!reflect.DeepEqual(previous.Capabilities, current.Capabilities) ||
		!reflect.DeepEqual(previous.Build, current.Build) {
		return true
	}
	return !reflect.DeepEqual(configDigests(oldRecords), configDigests(newRecords))
}

func configDigests(records []contentRecord) map[string]string {
	result := make(map[string]string)
	for _, record := range records {
		switch filepath.Base(record.Path) {
		case "go.mod", "go.sum", "go.work", "go.work.sum":
			result[record.Path] = record.Digest
		}
	}
	return result
}

type watchedFileChange struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

func watchedFileChanges(root string, oldRecords, newRecords []contentRecord) []watchedFileChange {
	old := make(map[string]string, len(oldRecords))
	current := make(map[string]string, len(newRecords))
	for _, record := range oldRecords {
		old[record.Path] = record.Digest
	}
	for _, record := range newRecords {
		current[record.Path] = record.Digest
	}
	paths := make(map[string]struct{}, len(old)+len(current))
	for path := range old {
		paths[path] = struct{}{}
	}
	for path := range current {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := make([]watchedFileChange, 0, len(ordered))
	for _, path := range ordered {
		oldDigest, existed := old[path]
		newDigest, exists := current[path]
		changeType := 0
		switch {
		case !existed && exists:
			changeType = 1
		case existed && exists && oldDigest != newDigest:
			changeType = 2
		case existed && !exists:
			changeType = 3
		}
		if changeType != 0 {
			changes = append(changes, watchedFileChange{URI: fileURI(root, path), Type: changeType})
		}
	}
	return changes
}

type goplsReader struct {
	p        *goplsProvider
	snapshot SnapshotRef
}

type rpcPos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rpcRange struct {
	Start rpcPos `json:"start"`
	End   rpcPos `json:"end"`
}

type rpcLocation struct {
	URI                  string   `json:"uri"`
	TargetURI            string   `json:"targetUri"`
	Range                rpcRange `json:"range"`
	TargetSelectionRange rpcRange `json:"targetSelectionRange"`
}

type rpcDocumentSymbol struct {
	Name           string              `json:"name"`
	Children       []rpcDocumentSymbol `json:"children"`
	Range          rpcRange            `json:"range"`
	SelectionRange rpcRange            `json:"selectionRange"`
	Kind           int                 `json:"kind"`
}

type rpcWorkspaceSymbol struct {
	Name          string      `json:"name"`
	ContainerName string      `json:"containerName"`
	Location      rpcLocation `json:"location"`
	Kind          int         `json:"kind"`
}

type rpcCallItem struct {
	Name           string   `json:"name"`
	Detail         string   `json:"detail"`
	URI            string   `json:"uri"`
	Range          rpcRange `json:"range"`
	SelectionRange rpcRange `json:"selectionRange"`
	Kind           int      `json:"kind"`
}

func (r *goplsReader) req(ctx context.Context, method string, params, out any) error {
	return r.p.manager.Request(ctx, method, params, out, true)
}

func (r *goplsReader) file(uri string) (string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" || (parsed.Host != "" && parsed.Host != "localhost") {
		return "", false
	}
	path := filepath.Clean(filepath.FromSlash(parsed.Path))
	relative, err := r.p.workspace.Relative(path)
	if err != nil {
		return "", false
	}
	return relative, true
}

func (r *goplsReader) location(raw rpcLocation) (Location, bool, error) {
	uri, sourceRange := raw.URI, raw.Range
	if raw.TargetURI != "" {
		uri, sourceRange = raw.TargetURI, raw.TargetSelectionRange
	}
	file, ok := r.file(uri)
	if !ok {
		return Location{}, false, nil
	}
	location, err := r.sourceLocation(file, sourceRange)
	return location, true, err
}

func (r *goplsReader) sourceLocation(file string, sourceRange rpcRange) (Location, error) {
	absolute, err := r.p.workspace.Resolve(file)
	if err != nil {
		return Location{}, fmt.Errorf("containing semantic location %s: %w", file, err)
	}
	contents, err := os.ReadFile(absolute)
	if err != nil {
		return Location{}, fmt.Errorf("reading semantic location %s: %w", file, err)
	}
	start, err := gopls.OffsetForPosition(contents, gopls.Position(sourceRange.Start))
	if err != nil {
		return Location{}, fmt.Errorf("converting semantic location start for %s: %w", file, err)
	}
	end, err := gopls.OffsetForPosition(contents, gopls.Position(sourceRange.End))
	if err != nil {
		return Location{}, fmt.Errorf("converting semantic location end for %s: %w", file, err)
	}
	line, column := byteLineColumn(contents, start)
	endLine, endColumn := byteLineColumn(contents, end)
	return Location{File: file, Line: line, Column: column, EndLine: endLine, EndColumn: endColumn}, nil
}

func byteLineColumn(contents []byte, offset int) (int, int) {
	line, lineStart := 1, 0
	for index, value := range contents[:offset] {
		if value == '\n' {
			line++
			lineStart = index + 1
		}
	}
	return line, offset - lineStart + 1
}

func (r *goplsReader) symbol(name string, kind int, packageName, file string, sourceRange rpcRange) (SymbolMatch, error) {
	qualified := name
	if packageName != "" {
		qualified = packageName + "." + name
	}
	ref, err := encodeSymbolRef(symbolIdentity{
		SnapshotID: r.snapshot.ID, Path: file,
		Base: r.snapshot.RequestedBase, Scope: r.snapshot.Scope,
		Position: Position{Line: sourceRange.Start.Line, Character: sourceRange.Start.Character},
		Kind:     normalizedSymbolKind(kind), Package: packageName, Qualified: qualified,
	})
	if err != nil {
		return SymbolMatch{}, err
	}
	location, err := r.sourceLocation(file, sourceRange)
	if err != nil {
		return SymbolMatch{}, err
	}
	return SymbolMatch{
		Ref: ref, Kind: normalizedSymbolKind(kind), Name: name, Qualified: qualified,
		Package: packageName, Location: location,
	}, nil
}

func (r *goplsReader) Search(ctx context.Context, query string) (semanticSymbols, error) {
	var raw []rpcWorkspaceSymbol
	if err := r.req(ctx, "workspace/symbol", map[string]any{"query": query}, &raw); err != nil {
		return semanticSymbols{}, err
	}
	result := semanticSymbols{Items: []SymbolMatch{}}
	for _, candidate := range raw {
		uri, sourceRange := candidate.Location.URI, candidate.Location.Range
		if candidate.Location.TargetURI != "" {
			uri, sourceRange = candidate.Location.TargetURI, candidate.Location.TargetSelectionRange
		}
		file, ok := r.file(uri)
		if !ok {
			result.Omitted++
			continue
		}
		match, err := r.symbol(candidate.Name, candidate.Kind, candidate.ContainerName, file, sourceRange)
		if err != nil {
			return semanticSymbols{}, err
		}
		result.Items = append(result.Items, match)
	}
	sortSymbols(result.Items)
	return result, nil
}

func (r *goplsReader) SymbolAt(ctx context.Context, file string, position Position) (SymbolMatch, error) {
	var raw []rpcDocumentSymbol
	if err := r.req(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": textDocument(r.p.root, file)}, &raw); err != nil {
		return SymbolMatch{}, err
	}
	selected, found := smallestContainingSymbol(raw, rpcPos(position))
	if !found {
		return SymbolMatch{}, fmt.Errorf("symbol not found at %s:%d:%d", file, position.Line+1, position.Character+1)
	}
	packageName := packageForFile(filepath.Join(r.p.root, filepath.FromSlash(file)))
	return r.symbol(selected.Name, selected.Kind, packageName, file, selected.SelectionRange)
}

func smallestContainingSymbol(symbols []rpcDocumentSymbol, position rpcPos) (rpcDocumentSymbol, bool) {
	var selected rpcDocumentSymbol
	found := false
	var visit func([]rpcDocumentSymbol)
	visit = func(candidates []rpcDocumentSymbol) {
		for _, candidate := range candidates {
			if !rangeContains(candidate.Range, position) {
				continue
			}
			selected, found = candidate, true
			visit(candidate.Children)
		}
	}
	visit(symbols)
	return selected, found
}

func rangeContains(sourceRange rpcRange, position rpcPos) bool {
	return comparePosition(sourceRange.Start, position) <= 0 && comparePosition(position, sourceRange.End) <= 0
}

func comparePosition(left, right rpcPos) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Character - right.Character
}

func packageForFile(path string) string {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
	if err != nil || parsed.Name == nil {
		return ""
	}
	return parsed.Name.Name
}

func (r *goplsReader) Hover(ctx context.Context, file string, position Position) (string, error) {
	var raw json.RawMessage
	if err := r.req(ctx, "textDocument/hover", documentPosition(r.p.root, file, position), &raw); err != nil {
		return "", err
	}
	return parseHover(raw), nil
}

func (r *goplsReader) locations(ctx context.Context, method, file string, position Position, extra map[string]any) (semanticLocations, error) {
	params := documentPosition(r.p.root, file, position)
	for key, value := range extra {
		params[key] = value
	}
	var raw json.RawMessage
	if err := r.req(ctx, method, params, &raw); err != nil {
		return semanticLocations{}, err
	}
	locations, err := decodeLocations(raw)
	if err != nil {
		return semanticLocations{}, fmt.Errorf("decoding %s response: %w", method, err)
	}
	result := semanticLocations{Items: []Location{}}
	for _, candidate := range locations {
		location, ok, locationErr := r.location(candidate)
		if locationErr != nil {
			return semanticLocations{}, locationErr
		}
		if !ok {
			result.Omitted++
			continue
		}
		result.Items = append(result.Items, location)
	}
	sortLocations(result.Items)
	return result, nil
}

func decodeLocations(raw json.RawMessage) ([]rpcLocation, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []rpcLocation{}, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var locations []rpcLocation
		if err := json.Unmarshal(raw, &locations); err != nil {
			return nil, err
		}
		return locations, nil
	}
	var location rpcLocation
	if err := json.Unmarshal(raw, &location); err != nil {
		return nil, err
	}
	return []rpcLocation{location}, nil
}

func (r *goplsReader) Definition(ctx context.Context, file string, position Position) (semanticLocations, error) {
	return r.locations(ctx, "textDocument/definition", file, position, nil)
}

func (r *goplsReader) TypeDefinition(ctx context.Context, file string, position Position) (semanticLocations, error) {
	return r.locations(ctx, "textDocument/typeDefinition", file, position, nil)
}

func (r *goplsReader) References(ctx context.Context, file string, position Position) (semanticLocations, error) {
	return r.locations(ctx, "textDocument/references", file, position, map[string]any{"context": map[string]bool{"includeDeclaration": true}})
}

func (r *goplsReader) Implementations(ctx context.Context, file string, position Position) (semanticSymbols, error) {
	locations, err := r.locations(ctx, "textDocument/implementation", file, position, nil)
	if err != nil {
		return semanticSymbols{}, err
	}
	result := semanticSymbols{Items: []SymbolMatch{}, Omitted: locations.Omitted}
	limit := len(locations.Items)
	if limit > maxImplementationResolution {
		result.Omitted += limit - maxImplementationResolution
		limit = maxImplementationResolution
	}
	for _, location := range locations.Items[:limit] {
		match, symbolErr := r.SymbolAt(ctx, location.File, Position{Line: location.Line - 1, Character: location.Column - 1})
		if symbolErr != nil {
			result.Omitted++
			continue
		}
		result.Items = append(result.Items, match)
	}
	sortSymbols(result.Items)
	return result, nil
}

func (r *goplsReader) Diagnostics(ctx context.Context, file string) ([]Diagnostic, error) {
	var raw struct {
		Items []struct {
			Message  string          `json:"message"`
			Source   string          `json:"source"`
			Code     json.RawMessage `json:"code"`
			Range    rpcRange        `json:"range"`
			Severity int             `json:"severity"`
		} `json:"items"`
	}
	if err := r.req(ctx, "textDocument/diagnostic", map[string]any{"textDocument": textDocument(r.p.root, file)}, &raw); err != nil {
		return nil, err
	}
	result := make([]Diagnostic, 0, len(raw.Items))
	for _, item := range raw.Items {
		source := item.Source
		if source == "" {
			source = "gopls"
		}
		location, locationErr := r.sourceLocation(file, item.Range)
		if locationErr != nil {
			return nil, locationErr
		}
		result = append(result, Diagnostic{
			Source: source, Code: diagnosticCode(item.Code),
			Severity: diagnosticSeverity(item.Severity), Message: item.Message,
			Location: location,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		return locationLess(left.Location, right.Location) || (left.Location == right.Location && left.Message < right.Message)
	})
	return result, nil
}

func diagnosticCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return strings.Trim(string(raw), `"`)
}

func diagnosticSeverity(value int) string {
	switch value {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}

func (r *goplsReader) Calls(ctx context.Context, file string, position Position) (semanticCalls, error) {
	var prepared []rpcCallItem
	if err := r.req(ctx, "textDocument/prepareCallHierarchy", documentPosition(r.p.root, file, position), &prepared); err != nil {
		return semanticCalls{}, err
	}
	result := semanticCalls{Items: []CallEdge{}}
	limit := len(prepared)
	if limit > maxCallHierarchyRoots {
		result.Omitted += limit - maxCallHierarchyRoots
		limit = maxCallHierarchyRoots
	}
	for _, item := range prepared[:limit] {
		var incoming []struct {
			From rpcCallItem `json:"from"`
		}
		if err := r.req(ctx, "callHierarchy/incomingCalls", map[string]any{"item": item}, &incoming); err != nil {
			return semanticCalls{}, err
		}
		for _, call := range incoming {
			r.appendCall(&result, "incoming", call.From)
		}
		var outgoing []struct {
			To rpcCallItem `json:"to"`
		}
		if err := r.req(ctx, "callHierarchy/outgoingCalls", map[string]any{"item": item}, &outgoing); err != nil {
			return semanticCalls{}, err
		}
		for _, call := range outgoing {
			r.appendCall(&result, "outgoing", call.To)
		}
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].Direction != result.Items[j].Direction {
			return result.Items[i].Direction < result.Items[j].Direction
		}
		return symbolLess(result.Items[i].Symbol, result.Items[j].Symbol)
	})
	return result, nil
}

func (r *goplsReader) appendCall(result *semanticCalls, direction string, item rpcCallItem) {
	file, ok := r.file(item.URI)
	if !ok {
		result.Omitted++
		return
	}
	match, err := r.symbol(item.Name, item.Kind, item.Detail, file, item.SelectionRange)
	if err != nil {
		result.Omitted++
		return
	}
	result.Items = append(result.Items, CallEdge{Direction: direction, Symbol: match})
}

func textDocument(root, file string) map[string]string {
	return map[string]string{"uri": fileURI(root, file)}
}

func documentPosition(root, file string, position Position) map[string]any {
	return map[string]any{"textDocument": textDocument(root, file), "position": rpcPos(position)}
}

func fileURI(root, path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.Join(root, filepath.FromSlash(path))}).String()
}

func normalizedSymbolKind(kind int) string {
	switch kind {
	case 4:
		return "go.package"
	case 5, 11, 23, 26:
		return "go.type"
	case 6:
		return "go.method"
	case 7, 8:
		return "go.field"
	case 12:
		return "go.function"
	case 13:
		return "go.variable"
	case 14:
		return "go.constant"
	default:
		return "go.symbol"
	}
}

func parseHover(raw json.RawMessage) string {
	var response struct {
		Contents json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Contents) == 0 {
		return ""
	}
	return hoverContents(response.Contents)
}

func hoverContents(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var marked struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &marked) == nil && marked.Value != "" {
		return marked.Value
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if value := hoverContents(part); value != "" {
				values = append(values, value)
			}
		}
		return strings.Join(values, "\n")
	}
	return ""
}

func sortLocations(locations []Location) {
	sort.Slice(locations, func(i, j int) bool { return locationLess(locations[i], locations[j]) })
}

func locationLess(left, right Location) bool {
	if left.File != right.File {
		return left.File < right.File
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	if left.Column != right.Column {
		return left.Column < right.Column
	}
	if left.EndLine != right.EndLine {
		return left.EndLine < right.EndLine
	}
	return left.EndColumn < right.EndColumn
}

func sortSymbols(symbols []SymbolMatch) {
	sort.Slice(symbols, func(i, j int) bool { return symbolLess(symbols[i], symbols[j]) })
}

func symbolLess(left, right SymbolMatch) bool {
	if left.Qualified != right.Qualified {
		return left.Qualified < right.Qualified
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return locationLess(left.Location, right.Location)
}
