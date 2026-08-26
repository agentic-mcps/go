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
	"sort"
	"strings"
	"sync"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/gopls"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

const (
	maximumContextBytes  = 1 << 20
	briefDiagnosticFiles = 32
)

type verifier interface {
	Collect(context.Context, verification.Request) (verification.Collection, error)
}

type storedSearch struct {
	Matches []SymbolMatch `json:"matches"`
	Omitted int           `json:"omitted"`
}

// Core assembles adapter-independent intelligence from snapshot, semantic,
// change-discovery, artifact, and verification infrastructure.
type Core struct {
	workspace     *workspace.Workspace
	runner        *execution.Runner
	snapshots     *Snapshotter
	semantic      semanticProvider
	mutator       semanticMutator
	artifacts     *ArtifactStore
	contracts     *ContractStore
	refactors     *RefactorStore
	verifications *VerificationStore
	changes       verification.ChangeAnalyzer
	verifier      verifier
	refactorWrite func(string, []byte, os.FileMode) error
	provenanceMu  sync.Mutex
	provenance    []verification.ProvenanceReference
}

// NewCore constructs the supported pinned-gopls intelligence service. The
// sidecar manager remains infrastructure and no LSP type crosses this seam.
func NewCore(
	ws *workspace.Workspace,
	runner *execution.Runner,
	manager *gopls.Manager,
	changes verification.ChangeAnalyzer,
	verify *verification.Engine,
) (*Core, error) {
	snapshots, err := NewSnapshotter(ws, runner)
	if err != nil {
		return nil, err
	}
	semantic, err := newGoplsProvider(manager, ws, snapshots)
	if err != nil {
		return nil, err
	}
	artifacts, err := NewArtifactStore("")
	if err != nil {
		return nil, err
	}
	contracts, err := NewContractStore("")
	if err != nil {
		return nil, err
	}
	refactors, err := NewRefactorStore("")
	if err != nil {
		return nil, err
	}
	verifications, err := NewVerificationStore("")
	if err != nil {
		return nil, err
	}
	return newCore(ws, runner, snapshots, semantic, artifacts, contracts, refactors, verifications, changes, verify)
}

func newCore(
	ws *workspace.Workspace,
	runner *execution.Runner,
	snapshots *Snapshotter,
	semantic semanticProvider,
	artifacts *ArtifactStore,
	contracts *ContractStore,
	refactors *RefactorStore,
	verifications *VerificationStore,
	changes verification.ChangeAnalyzer,
	verify verifier,
) (*Core, error) {
	switch {
	case ws == nil:
		return nil, fmt.Errorf("workspace is nil")
	case runner == nil:
		return nil, fmt.Errorf("runner is nil")
	case snapshots == nil:
		return nil, fmt.Errorf("snapshotter is nil")
	case semantic == nil:
		return nil, fmt.Errorf("semantic provider is nil")
	case artifacts == nil:
		return nil, fmt.Errorf("artifact store is nil")
	case contracts == nil:
		return nil, fmt.Errorf("contract store is nil")
	case refactors == nil:
		return nil, fmt.Errorf("refactor store is nil")
	case verifications == nil:
		return nil, fmt.Errorf("verification store is nil")
	case changes == nil:
		return nil, fmt.Errorf("change analyzer is nil")
	case verify == nil:
		return nil, fmt.Errorf("verification engine is nil")
	}
	mutator, ok := semantic.(semanticMutator)
	if !ok {
		return nil, fmt.Errorf("semantic provider does not support guarded refactoring")
	}
	return &Core{
		workspace: ws, runner: runner, snapshots: snapshots, semantic: semantic,
		mutator: mutator, artifacts: artifacts, contracts: contracts, refactors: refactors, verifications: verifications,
		changes: changes, verifier: verify, refactorWrite: atomicReplace,
	}, nil
}

// Search returns one deterministic page of snapshot-bound workspace symbols.
func (c *Core) Search(ctx context.Context, request SearchRequest) (SearchResult, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if strings.TrimSpace(request.Query) == "" {
		return SearchResult{}, fmt.Errorf("search query is required")
	}
	if request.Scope == "" {
		request.Scope = "./..."
	}
	if request.Limit == 0 {
		request.Limit = DefaultSearchLimit
	}
	if request.Limit < 1 || request.Limit > MaximumSearchLimit {
		return SearchResult{}, fmt.Errorf("search limit must be between 1 and %d", MaximumSearchLimit)
	}
	snapshot, err := c.capture(ctx, "", request.Scope, request.ExpectedSnapshotID)
	if err != nil {
		return SearchResult{}, err
	}
	if !snapshot.Capabilities.WorkspaceSymbol {
		return SearchResult{}, fmt.Errorf("the active semantic provider does not support workspace symbols")
	}
	key := artifactKey("search", request.Scope, request.Query)
	matches := []SymbolMatch{}
	omitted := 0
	offset := int64(0)
	artifactID := ""
	if request.Cursor != "" {
		cursor, cursorErr := DecodeArtifactCursor(request.Cursor, "")
		if cursorErr != nil {
			return SearchResult{}, cursorErr
		}
		artifact, artifactErr := c.artifacts.Get(cursor.ID, snapshot.ID, key)
		if artifactErr != nil {
			if errors.Is(artifactErr, ErrArtifactMismatch) {
				return SearchResult{}, fmt.Errorf("%w: search continuation belongs to another workspace snapshot", ErrSnapshotChanged)
			}
			return SearchResult{}, artifactErr
		}
		var stored storedSearch
		if decodeErr := json.Unmarshal(artifact.Payload, &stored); decodeErr != nil {
			return SearchResult{}, fmt.Errorf("decoding search artifact: %w", decodeErr)
		}
		matches, omitted = stored.Matches, stored.Omitted
		offset, artifactID = cursor.Offset, artifact.ID
	} else {
		err = c.semantic.Read(ctx, snapshot, func(reader semanticReader) error {
			result, searchErr := reader.Search(ctx, request.Query)
			matches, omitted = result.Items, result.Omitted
			return searchErr
		})
		if err != nil {
			return SearchResult{}, err
		}
		matches, err = normalizeSymbolMatches(snapshot, matches)
		if err != nil {
			return SearchResult{}, err
		}
		payload, marshalErr := json.Marshal(storedSearch{Matches: matches, Omitted: omitted})
		if marshalErr != nil {
			return SearchResult{}, marshalErr
		}
		artifact, artifactErr := c.artifacts.Put(snapshot.ID, key, payload)
		if artifactErr != nil {
			return SearchResult{}, artifactErr
		}
		artifactID = artifact.ID
	}
	if offset > int64(len(matches)) {
		return SearchResult{}, ErrCursorInvalid
	}
	end := int(offset) + request.Limit
	if end > len(matches) {
		end = len(matches)
	}
	page := append([]SymbolMatch(nil), matches[offset:end]...)
	next := ""
	if end < len(matches) {
		next, err = EncodeArtifactCursor(artifactID, int64(end))
		if err != nil {
			return SearchResult{}, err
		}
	}
	uncertainties := []Uncertainty{}
	if omitted > 0 {
		uncertainties = append(uncertainties, Uncertainty{
			Code:    "semantic.external_locations",
			Message: fmt.Sprintf("%d semantic matches outside the configured workspace were omitted", omitted), Locations: []Location{},
		})
	}
	if _, err := c.snapshots.Validate(ctx, snapshot); err != nil {
		return SearchResult{}, err
	}
	result := SearchResult{
		SchemaVersion: ContextSchemaVersion, Provider: c.provider(), Snapshot: snapshot,
		Matches: page, Total: len(matches), Truncated: end < len(matches), NextCursor: next,
		Uncertainties: uncertainties,
	}
	c.recordContextProvenance("search", result.Snapshot)
	return result, nil
}

// Symbol returns default semantic facets for a stable ref or compatibility
// source position, rejecting any stale snapshot identity.
func (c *Core) Symbol(ctx context.Context, request SymbolRequest) (SymbolContext, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if (request.Ref == "") == (request.Position == nil) {
		return SymbolContext{}, fmt.Errorf("provide exactly one symbol reference or source position")
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = DefaultSymbolBytes
	}
	if err := validContextBudget(request.MaxBytes); err != nil {
		return SymbolContext{}, err
	}
	var snapshot SnapshotRef
	var file string
	var position Position
	var err error
	if request.Ref != "" {
		identity, decodeErr := decodeSymbolRef(request.Ref)
		if decodeErr != nil {
			return SymbolContext{}, decodeErr
		}
		if request.ExpectedSnapshotID != "" && request.ExpectedSnapshotID != identity.SnapshotID {
			return SymbolContext{}, fmt.Errorf("%w: expected %s, symbol belongs to %s", ErrSnapshotChanged, request.ExpectedSnapshotID, identity.SnapshotID)
		}
		snapshot, err = c.capture(ctx, identity.Base, identity.Scope, identity.SnapshotID)
		file, position = identity.Path, identity.Position
	} else {
		snapshot, err = c.capture(ctx, "", "./...", request.ExpectedSnapshotID)
		if err == nil {
			var absolute string
			absolute, err = c.workspace.Resolve(request.Position.File)
			if err == nil {
				file, err = c.workspace.Relative(absolute)
			}
			if err == nil {
				position, err = sourcePosition(absolute, *request.Position)
			}
		}
	}
	if err != nil {
		return SymbolContext{}, err
	}
	if !snapshot.Capabilities.DocumentSymbol {
		return SymbolContext{}, fmt.Errorf("the active semantic provider does not support document symbols")
	}
	result := emptySymbolContext(snapshot, c.provider())
	omitted := make(map[string]int)
	err = c.semantic.Read(ctx, snapshot, func(reader semanticReader) error {
		result.Symbol, err = reader.SymbolAt(ctx, file, position)
		if err != nil {
			return err
		}
		result.Symbol, err = normalizeSymbolMatch(snapshot, result.Symbol)
		if err != nil {
			return err
		}
		if snapshot.Capabilities.Hover {
			result.Hover, err = reader.Hover(ctx, file, position)
			if err != nil {
				return err
			}
		}
		if snapshot.Capabilities.Definition {
			locations, readErr := reader.Definition(ctx, file, position)
			if readErr != nil {
				return readErr
			}
			result.Definitions = locationSet(locations)
			omitted["definitions"] = locations.Omitted
		}
		if request.Facets.TypeDefinition && snapshot.Capabilities.TypeDefinition {
			locations, readErr := reader.TypeDefinition(ctx, file, position)
			if readErr != nil {
				return readErr
			}
			result.TypeDefinitions = locationSet(locations)
			omitted["type definitions"] = locations.Omitted
		}
		if snapshot.Capabilities.References {
			locations, readErr := reader.References(ctx, file, position)
			if readErr != nil {
				return readErr
			}
			result.References = locationSet(locations)
			omitted["references"] = locations.Omitted
			for _, location := range locations.Items {
				if strings.HasSuffix(location.File, "_test.go") {
					result.RelatedTests.Items = append(result.RelatedTests.Items, location)
				}
			}
			result.RelatedTests.Total = len(result.RelatedTests.Items)
		}
		if snapshot.Capabilities.Implementation && (result.Symbol.Kind == "go.type" || result.Symbol.Kind == "go.method") {
			symbols, readErr := reader.Implementations(ctx, file, position)
			if readErr != nil {
				return readErr
			}
			symbols.Items, err = normalizeSymbolMatches(snapshot, symbols.Items)
			if err != nil {
				return err
			}
			result.Implementations = symbolSet(symbols)
			omitted["implementations"] = symbols.Omitted
		}
		if snapshot.Capabilities.Diagnostics {
			result.Diagnostics, err = reader.Diagnostics(ctx, file)
			result.DiagnosticsTotal = len(result.Diagnostics)
			if err != nil {
				return err
			}
		}
		if request.Facets.CallHierarchy && snapshot.Capabilities.CallHierarchy {
			calls, readErr := reader.Calls(ctx, file, position)
			if readErr != nil {
				return readErr
			}
			result.Calls = callSet(calls)
			omitted["call relationships"] = calls.Omitted
		}
		return nil
	})
	if err != nil {
		return SymbolContext{}, err
	}
	for facet, count := range omitted {
		if count > 0 {
			result.Uncertainties = append(result.Uncertainties, Uncertainty{
				Code:    "semantic.external_locations",
				Message: fmt.Sprintf("%d %s outside the configured workspace were omitted", count, facet), Locations: []Location{},
			})
		}
	}
	if request.Facets.CallHierarchy && !snapshot.Capabilities.CallHierarchy {
		result.Uncertainties = append(result.Uncertainties, Uncertainty{
			Code:    "semantic.unsupported_capability",
			Message: "the active semantic provider does not support call hierarchy", Locations: []Location{},
		})
	}
	if request.Facets.TypeDefinition && !snapshot.Capabilities.TypeDefinition {
		result.Uncertainties = append(result.Uncertainties, Uncertainty{
			Code:    "semantic.unsupported_capability",
			Message: "the active semantic provider does not support type definitions", Locations: []Location{},
		})
	}
	result.Uncertainties = append(result.Uncertainties, fileSemanticUncertainties(c.workspace, file, request.Facets.CallHierarchy)...)
	sort.Slice(result.Uncertainties, func(i, j int) bool {
		if result.Uncertainties[i].Code != result.Uncertainties[j].Code {
			return result.Uncertainties[i].Code < result.Uncertainties[j].Code
		}
		return result.Uncertainties[i].Message < result.Uncertainties[j].Message
	})
	result, err = c.boundSymbolContext(result, request.MaxBytes)
	if err != nil {
		return SymbolContext{}, err
	}
	if _, err := c.snapshots.Validate(ctx, snapshot); err != nil {
		return SymbolContext{}, err
	}
	c.recordContextProvenance("symbol", result.Snapshot)
	return result, nil
}

// Brief assembles a compact workspace/package overview with optional change
// impact when a local base is supplied.
func (c *Core) Brief(ctx context.Context, request BriefRequest) (ContextPack, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if request.Scope == "" {
		request.Scope = "./..."
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = DefaultBriefBytes
	}
	if err := validContextBudget(request.MaxBytes); err != nil {
		return ContextPack{}, err
	}
	snapshot, err := c.capture(ctx, request.Base, request.Scope, request.ExpectedSnapshotID)
	if err != nil {
		return ContextPack{}, err
	}
	packages, err := inventoryPackages(ctx, c.workspace, c.runner, request.Scope)
	if err != nil {
		return ContextPack{}, err
	}
	packageSummaries, modules, err := summarizeInventory(ctx, c.workspace, packages)
	if err != nil {
		return ContextPack{}, err
	}
	guidance, err := inventoryGuidance(ctx, c.workspace)
	if err != nil {
		return ContextPack{}, err
	}
	result := ContextPack{
		SchemaVersion: ContextSchemaVersion, Provider: c.provider(), Snapshot: snapshot,
		Modules: modules, Packages: packageSummaries, Symbols: []SymbolMatch{}, Diagnostics: []Diagnostic{},
		Guidance: guidance, Risks: []RiskArea{}, Uncertainties: []Uncertainty{},
	}
	result.Uncertainties = append(result.Uncertainties, Uncertainty{
		Code:    "go.external_consumers",
		Message: "packages and consumers outside the configured workspace are not modeled", Locations: []Location{},
	})
	if snapshot.Capabilities.Diagnostics {
		files := inventoryGoFiles(c.workspace, packages)
		limit := len(files)
		if limit > briefDiagnosticFiles {
			limit = briefDiagnosticFiles
			result.Uncertainties = append(result.Uncertainties, Uncertainty{
				Code:    "semantic.diagnostics_bounded",
				Message: fmt.Sprintf("workspace brief diagnostics sampled %d of %d active Go files", limit, len(files)), Locations: []Location{},
			})
		}
		err = c.semantic.Read(ctx, snapshot, func(reader semanticReader) error {
			for _, file := range files[:limit] {
				if contextErr := ctx.Err(); contextErr != nil {
					return contextErr
				}
				diagnostics, readErr := reader.Diagnostics(ctx, file)
				if readErr != nil {
					return readErr
				}
				result.Diagnostics = append(result.Diagnostics, diagnostics...)
			}
			return nil
		})
		if err != nil {
			return ContextPack{}, err
		}
	}
	for _, pkg := range packageSummaries {
		if pkg.Generated {
			result.Uncertainties = append(result.Uncertainties, Uncertainty{
				Code:    "go.generated_code",
				Message: "active generated Go files are analyzed, but their generator inputs may not be modeled", Locations: []Location{},
			})
		}
		if pkg.Constrained {
			result.Uncertainties = append(result.Uncertainties, Uncertainty{
				Code:    "go.build_constraints",
				Message: "package selection reflects only the recorded active build configuration", Locations: []Location{},
			})
		}
		if pkg.Cgo {
			result.Uncertainties = append(result.Uncertainties, Uncertainty{
				Code:    "go.cgo",
				Message: "cgo semantics depend on the local C toolchain and build environment", Locations: []Location{},
			})
		}
	}
	if request.Base != "" {
		analysis, analysisErr := c.changes.Analyze(ctx, verification.ChangeOptions{Base: request.Base, Package: request.Scope, MaxPackages: 200})
		if analysisErr != nil {
			return ContextPack{}, analysisErr
		}
		result.Change = compactChange(analysis)
		result.Risks = append(result.Risks, intelligenceRisks(analysis.Risks)...)
		result.Uncertainties = append(result.Uncertainties, intelligenceUncertainties(analysis.Uncertainties)...)
	}
	sortDiagnostics(result.Diagnostics)
	sort.Slice(result.Uncertainties, func(i, j int) bool { return result.Uncertainties[i].Code < result.Uncertainties[j].Code })
	result.Totals = ContextTotals{
		Modules: len(result.Modules), Packages: len(result.Packages), Symbols: len(result.Symbols),
		Diagnostics: len(result.Diagnostics), Guidance: len(result.Guidance), Risks: len(result.Risks), Uncertainties: len(result.Uncertainties),
	}
	result, err = c.boundContextPack(result, request.MaxBytes, artifactKey("brief", request.Base, request.Scope))
	if err != nil {
		return ContextPack{}, err
	}
	if _, err := c.snapshots.Validate(ctx, snapshot); err != nil {
		return ContextPack{}, err
	}
	c.recordContextProvenance("brief", result.Snapshot)
	return result, nil
}

func (c *Core) capture(ctx context.Context, base, scope, expected string) (SnapshotRef, error) {
	snapshot, err := c.snapshots.Capture(ctx, SnapshotRequest{Base: base, Scope: scope, Semantic: c.semantic.Identity()})
	if err != nil {
		return SnapshotRef{}, err
	}
	if expected != "" && expected != snapshot.ID {
		return SnapshotRef{}, fmt.Errorf("%w: expected %s, observed %s", ErrSnapshotChanged, expected, snapshot.ID)
	}
	return snapshot, nil
}

func (c *Core) provider() Provider {
	return Provider{Name: "agentic-go-gopls", Version: c.semantic.Identity().Version}
}

// Capabilities returns the effective negotiated semantic manifest and compact
// response defaults without exposing the sidecar path or LSP wire types.
func (c *Core) Capabilities() Capabilities {
	identity := c.semantic.Identity()
	return Capabilities{
		Provider: c.provider(), Semantic: identity.Capabilities, ContextSchema: ContextSchemaVersion,
		BriefBytes: DefaultBriefBytes, SymbolBytes: DefaultSymbolBytes, SearchDefault: DefaultSearchLimit,
		SearchMaximum: MaximumSearchLimit, ArtifactMaximum: MaxArtifactChunkBytes,
	}
}

// ReadArtifact resolves an opaque Context Pack continuation cursor into one
// bounded resource chunk.
func (c *Core) ReadArtifact(ctx context.Context, cursor string, limit int64) (ArtifactChunk, error) {
	decoded, err := DecodeArtifactCursor(cursor, "")
	if err != nil {
		return ArtifactChunk{}, err
	}
	if limit == 0 {
		limit = MaxArtifactChunkBytes
	}
	return c.artifacts.ReadChunk(ctx, decoded.ID, cursor, decoded.Offset, limit)
}

func sourcePosition(path string, source SourcePosition) (Position, error) {
	if source.Line < 1 || source.Column < 1 {
		return Position{}, fmt.Errorf("source line and column must be positive")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Position{}, err
	}
	line, offset := 1, 0
	for line < source.Line && offset < len(contents) {
		if contents[offset] == '\n' {
			line++
		}
		offset++
	}
	if line != source.Line {
		return Position{}, fmt.Errorf("source line %d is outside %s", source.Line, filepath.Base(path))
	}
	lineEnd := offset
	for lineEnd < len(contents) && contents[lineEnd] != '\n' {
		lineEnd++
	}
	target := offset + source.Column - 1
	if target > lineEnd {
		return Position{}, fmt.Errorf("source column %d is outside line %d", source.Column, source.Line)
	}
	position, err := gopls.PositionForOffset(contents, target)
	if err != nil {
		return Position{}, err
	}
	return Position(position), nil
}

func normalizeSymbolMatches(snapshot SnapshotRef, matches []SymbolMatch) ([]SymbolMatch, error) {
	result := make([]SymbolMatch, 0, len(matches))
	seen := make(map[string]struct{})
	for _, match := range matches {
		normalized, err := normalizeSymbolMatch(snapshot, match)
		if err != nil {
			return nil, err
		}
		key := normalized.Kind + "\x00" + normalized.Qualified + "\x00" + normalized.Location.File + fmt.Sprintf("\x00%d\x00%d", normalized.Location.Line, normalized.Location.Column)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	sortSymbols(result)
	return result, nil
}

func normalizeSymbolMatch(snapshot SnapshotRef, match SymbolMatch) (SymbolMatch, error) {
	if match.Location.File == "" || match.Kind == "" || match.Name == "" || match.Qualified == "" || match.Location.Line < 1 || match.Location.Column < 1 {
		return SymbolMatch{}, fmt.Errorf("semantic provider returned an incomplete symbol")
	}
	if match.Ref == "" {
		ref, err := encodeSymbolRef(symbolIdentity{
			SnapshotID: snapshot.ID, Path: match.Location.File,
			Base: snapshot.RequestedBase, Scope: snapshot.Scope,
			Position: Position{Line: match.Location.Line - 1, Character: match.Location.Column - 1},
			Kind:     match.Kind, Package: match.Package, Qualified: match.Qualified,
		})
		if err != nil {
			return SymbolMatch{}, err
		}
		match.Ref = ref
	} else {
		identity, err := decodeSymbolRef(match.Ref)
		if err != nil {
			return SymbolMatch{}, err
		}
		if err := requireSymbolSnapshot(identity, snapshot); err != nil {
			return SymbolMatch{}, err
		}
	}
	return match, nil
}

func locationSet(result semanticLocations) LocationSet {
	return LocationSet{Items: append([]Location(nil), result.Items...), Total: len(result.Items) + result.Omitted, Truncated: result.Omitted > 0}
}

func symbolSet(result semanticSymbols) SymbolSet {
	return SymbolSet{Items: append([]SymbolMatch(nil), result.Items...), Total: len(result.Items) + result.Omitted, Truncated: result.Omitted > 0}
}

func callSet(result semanticCalls) CallSet {
	return CallSet{Items: append([]CallEdge(nil), result.Items...), Total: len(result.Items) + result.Omitted, Truncated: result.Omitted > 0}
}

func emptySymbolContext(snapshot SnapshotRef, provider Provider) SymbolContext {
	return SymbolContext{
		SchemaVersion: ContextSchemaVersion, Provider: provider, Snapshot: snapshot,
		Definitions: LocationSet{Items: []Location{}}, TypeDefinitions: LocationSet{Items: []Location{}},
		References: LocationSet{Items: []Location{}}, Implementations: SymbolSet{Items: []SymbolMatch{}},
		RelatedTests: LocationSet{Items: []Location{}}, Diagnostics: []Diagnostic{}, Calls: CallSet{Items: []CallEdge{}},
		Uncertainties: []Uncertainty{},
	}
}

func (c *Core) boundSymbolContext(full SymbolContext, maximum int) (SymbolContext, error) {
	payload, err := json.Marshal(full)
	if err != nil || len(payload) <= maximum {
		return full, err
	}
	artifact, err := c.artifacts.Put(full.Snapshot.ID, artifactKey("symbol", string(full.Symbol.Ref)), payload)
	if err != nil {
		return SymbolContext{}, err
	}
	bounded := full
	bounded.Truncated = true
	bounded.NextCursor, err = EncodeArtifactCursor(artifact.ID, 0)
	if err != nil {
		return SymbolContext{}, err
	}
	for encodedSize(bounded) > maximum {
		switch {
		case len(bounded.Calls.Items) > 0:
			bounded.Calls.Items = bounded.Calls.Items[:len(bounded.Calls.Items)-1]
			bounded.Calls.Truncated = true
		case len(bounded.References.Items) > 0:
			bounded.References.Items = bounded.References.Items[:len(bounded.References.Items)-1]
			bounded.References.Truncated = true
		case len(bounded.Implementations.Items) > 0:
			bounded.Implementations.Items = bounded.Implementations.Items[:len(bounded.Implementations.Items)-1]
			bounded.Implementations.Truncated = true
		case len(bounded.TypeDefinitions.Items) > 0:
			bounded.TypeDefinitions.Items = bounded.TypeDefinitions.Items[:len(bounded.TypeDefinitions.Items)-1]
			bounded.TypeDefinitions.Truncated = true
		case len(bounded.Definitions.Items) > 0:
			bounded.Definitions.Items = bounded.Definitions.Items[:len(bounded.Definitions.Items)-1]
			bounded.Definitions.Truncated = true
		case len(bounded.RelatedTests.Items) > 0:
			bounded.RelatedTests.Items = bounded.RelatedTests.Items[:len(bounded.RelatedTests.Items)-1]
			bounded.RelatedTests.Truncated = true
		case len(bounded.Diagnostics) > 0:
			bounded.Diagnostics = bounded.Diagnostics[:len(bounded.Diagnostics)-1]
		case len(bounded.Hover) > 0:
			bounded.Hover = ""
		case len(bounded.Uncertainties) > 0:
			bounded.Uncertainties = bounded.Uncertainties[:len(bounded.Uncertainties)-1]
		default:
			return SymbolContext{}, fmt.Errorf("symbol context metadata exceeds %d-byte budget", maximum)
		}
	}
	return bounded, nil
}

func (c *Core) boundContextPack(full ContextPack, maximum int, key string) (ContextPack, error) {
	payload, err := json.Marshal(full)
	if err != nil || len(payload) <= maximum {
		return full, err
	}
	artifact, err := c.artifacts.Put(full.Snapshot.ID, key, payload)
	if err != nil {
		return ContextPack{}, err
	}
	bounded := full
	bounded.Truncated = true
	bounded.NextCursor, err = EncodeArtifactCursor(artifact.ID, 0)
	if err != nil {
		return ContextPack{}, err
	}
	for encodedSize(bounded) > maximum {
		switch {
		case len(bounded.Diagnostics) > 0:
			bounded.Diagnostics = bounded.Diagnostics[:len(bounded.Diagnostics)-1]
		case bounded.Change != nil && len(bounded.Change.ReverseDependents) > 0:
			bounded.Change.ReverseDependents = bounded.Change.ReverseDependents[:len(bounded.Change.ReverseDependents)-1]
			bounded.Change.Truncated = true
		case bounded.Change != nil && len(bounded.Change.Declarations) > 0:
			bounded.Change.Declarations = bounded.Change.Declarations[:len(bounded.Change.Declarations)-1]
			bounded.Change.Truncated = true
		case bounded.Change != nil && len(bounded.Change.Files) > 0:
			bounded.Change.Files = bounded.Change.Files[:len(bounded.Change.Files)-1]
			bounded.Change.Truncated = true
		case bounded.Change != nil && len(bounded.Change.DirectUnits) > 0:
			bounded.Change.DirectUnits = bounded.Change.DirectUnits[:len(bounded.Change.DirectUnits)-1]
			bounded.Change.Truncated = true
		case packagesHaveExports(bounded.Packages):
			trimLastExport(bounded.Packages)
		case len(bounded.Packages) > 0:
			bounded.Packages = bounded.Packages[:len(bounded.Packages)-1]
		case len(bounded.Guidance) > 0:
			bounded.Guidance = bounded.Guidance[:len(bounded.Guidance)-1]
		case len(bounded.Risks) > 0:
			bounded.Risks = bounded.Risks[:len(bounded.Risks)-1]
		case len(bounded.Modules) > 0:
			bounded.Modules = bounded.Modules[:len(bounded.Modules)-1]
		case len(bounded.Uncertainties) > 0:
			bounded.Uncertainties = bounded.Uncertainties[:len(bounded.Uncertainties)-1]
		default:
			return ContextPack{}, fmt.Errorf("context metadata exceeds %d-byte budget", maximum)
		}
	}
	return bounded, nil
}

func validContextBudget(size int) error {
	if size < 1 || size > maximumContextBytes {
		return fmt.Errorf("context byte budget must be between 1 and %d", maximumContextBytes)
	}
	return nil
}

func encodedSize(value any) int {
	encoded, _ := json.Marshal(value)
	return len(encoded)
}

func packagesHaveExports(packages []PackageSummary) bool {
	for _, pkg := range packages {
		if len(pkg.Exported) > 0 {
			return true
		}
	}
	return false
}

func trimLastExport(packages []PackageSummary) {
	for index := len(packages) - 1; index >= 0; index-- {
		if len(packages[index].Exported) > 0 {
			packages[index].Exported = packages[index].Exported[:len(packages[index].Exported)-1]
			return
		}
	}
}

func artifactKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func inventoryGoFiles(ws *workspace.Workspace, packages []inventoryPackage) []string {
	set := make(map[string]struct{})
	for _, pkg := range packages {
		groups := [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles}
		for _, group := range groups {
			for _, name := range group {
				if relative, err := ws.Relative(filepath.Join(pkg.Dir, name)); err == nil {
					set[relative] = struct{}{}
				}
			}
		}
	}
	result := make([]string, 0, len(set))
	for file := range set {
		result = append(result, file)
	}
	sort.Strings(result)
	return result
}

func fileSemanticUncertainties(ws *workspace.Workspace, file string, callsRequested bool) []Uncertainty {
	result := []Uncertainty{{
		Code:    "go.external_consumers",
		Message: "references and implementations outside the configured workspace are not modeled", Locations: []Location{},
	}}
	absolute, err := ws.Resolve(file)
	if err != nil {
		return append(result, Uncertainty{Code: "source.unavailable", Message: err.Error(), Locations: []Location{}})
	}
	contents, err := os.ReadFile(absolute)
	if err != nil {
		return append(result, Uncertainty{Code: "source.unavailable", Message: "source metadata could not be inspected", Locations: []Location{}})
	}
	location := []Location{{File: file, Line: 1, Column: 1}}
	if strings.Contains(string(contents), "Code generated ") {
		result = append(result, Uncertainty{
			Code:    "go.generated_code",
			Message: "generated source is analyzed, but its generator inputs may not be modeled", Locations: location,
		})
	}
	if strings.Contains(string(contents), "//go:build") || strings.Contains(string(contents), "// +build") {
		result = append(result, Uncertainty{
			Code:    "go.build_constraints",
			Message: "symbol context reflects only the recorded active build configuration", Locations: location,
		})
	}
	if strings.Contains(string(contents), `import "C"`) {
		result = append(result, Uncertainty{
			Code:    "go.cgo",
			Message: "cgo semantics depend on the local C toolchain and build environment", Locations: location,
		})
	}
	if callsRequested {
		result = append(result, Uncertainty{
			Code:    "go.dynamic_calls",
			Message: "static call hierarchy may omit interface dispatch, reflection, and other dynamic calls", Locations: location,
		})
	}
	return result
}

func compactChange(analysis verification.ChangeAnalysis) *ChangeContext {
	result := &ChangeContext{
		Files: []string{}, Declarations: []string{}, DirectUnits: []string{}, ReverseDependents: []string{},
		ObservedUnits: analysis.ObservedPackages, Complete: analysis.Complete,
	}
	for _, file := range analysis.Change.Files {
		result.Files = append(result.Files, file.Path)
	}
	for _, declaration := range analysis.Change.Declarations {
		result.Declarations = append(result.Declarations, string(declaration.Kind)+":"+declaration.Package+"."+declaration.Name)
	}
	for _, pkg := range analysis.Impact.Packages {
		if pkg.Distance == 0 {
			result.DirectUnits = append(result.DirectUnits, pkg.ID)
		} else {
			result.ReverseDependents = append(result.ReverseDependents, pkg.ID)
		}
	}
	sort.Strings(result.Files)
	sort.Strings(result.Declarations)
	sort.Strings(result.DirectUnits)
	sort.Strings(result.ReverseDependents)
	result.FilesTotal = len(result.Files)
	result.DeclarationsTotal = len(result.Declarations)
	result.DirectUnitsTotal = len(result.DirectUnits)
	result.ReverseDependentsTotal = len(result.ReverseDependents)
	return result
}

func intelligenceRisks(source []verification.RiskArea) []RiskArea {
	result := make([]RiskArea, 0, len(source))
	for _, risk := range source {
		locations := make([]Location, 0, len(risk.Locations))
		for _, location := range risk.Locations {
			locations = append(locations, Location{File: location.File, Line: location.Line, Column: location.Col})
		}
		result = append(result, RiskArea{Code: risk.Code, Summary: risk.Reason, Guidance: risk.Guidance, Locations: locations})
	}
	return result
}

func intelligenceUncertainties(source []verification.Uncertainty) []Uncertainty {
	result := make([]Uncertainty, 0, len(source))
	for _, uncertainty := range source {
		locations := make([]Location, 0, len(uncertainty.Locations))
		for _, location := range uncertainty.Locations {
			locations = append(locations, Location{File: location.File, Line: location.Line, Column: location.Col})
		}
		result = append(result, Uncertainty{Code: uncertainty.Code, Message: uncertainty.Message, Locations: locations})
	}
	return result
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.Slice(diagnostics, func(i, j int) bool {
		return locationLess(diagnostics[i].Location, diagnostics[j].Location) ||
			(diagnostics[i].Location == diagnostics[j].Location && diagnostics[i].Message < diagnostics[j].Message)
	})
}
