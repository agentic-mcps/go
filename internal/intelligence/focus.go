package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/agentic-mcps/go/internal/verification"
)

// FocusSchemaVersion identifies the additive focus evidence contract.
const FocusSchemaVersion = "agentic.focus/v1"

const (
	maxFocusCandidates   = 20
	maxFocusTestSymbols  = 20
	maxFocusExcerpts     = 24
	maxFocusExcerptBytes = 4096
)

// FocusRequest selects change evidence and at most one focused anchor.
//
//nolint:govet // Field order follows request semantics.
type FocusRequest struct {
	MinChangedCoverage *float64
	Base               string
	Scope              string
	ExpectedSnapshotID string
	FailOn             verification.FailOn
	MaxPackages        int
	Race               bool
	Query              string
	SymbolRef          SymbolRef
	Position           *SourcePosition
	MaxBytes           int
	PreviousPackID     string
	FocusFile          string
	FocusPackage       string
}

// VerificationApplicability separates report outcome from current applicability.
//
//nolint:govet // Field order is the public JSON schema order.
type VerificationApplicability struct {
	ReportID   string                    `json:"report_id,omitempty"`
	Outcome    verification.ResultStatus `json:"outcome,omitempty"`
	Reasons    []string                  `json:"reasons"`
	NextAction string                    `json:"next_action"`
	Present    bool                      `json:"present"`
	Applicable bool                      `json:"applicable"`
}

// FocusResult binds change, focused source, and verification evidence to one snapshot.
//
//nolint:govet // Field order is the public JSON schema order.
type FocusResult struct {
	SchemaVersion    string                     `json:"schema_version"`
	Provider         Provider                   `json:"provider"`
	Snapshot         SnapshotRef                `json:"snapshot"`
	Change           verification.Change        `json:"change"`
	Impact           verification.Impact        `json:"impact"`
	Risks            []verification.RiskArea    `json:"risks"`
	Uncertainties    []verification.Uncertainty `json:"uncertainties"`
	Verification     VerificationApplicability  `json:"verification"`
	ObservedPackages int                        `json:"observed_packages"`
	Complete         bool                       `json:"complete"`
	Context          *FocusContext              `json:"context,omitempty"`
	PackID           string                     `json:"pack_id,omitempty"`
	Refresh          *FocusRefresh              `json:"refresh,omitempty"`
}

// FocusRefresh reports how a previous delivered pack was resolved.
type FocusRefresh struct {
	PreviousPackID string `json:"previous_pack_id"`
	Status         string `json:"status"`
	Message        string `json:"message"`
}

// FocusContext is the optional declaration-focused portion of a change
// context. A query with multiple matches returns Candidates and leaves Symbol
// unset so callers must disambiguate explicitly.
//
//nolint:govet // Field order is the public JSON schema order.
type FocusContext struct {
	Selection        FocusSelection  `json:"selection"`
	Candidates       []SymbolMatch   `json:"candidates"`
	Symbol           *SymbolContext  `json:"symbol,omitempty"`
	TestDeclarations []SymbolMatch   `json:"test_declarations"`
	Excerpts         []SourceExcerpt `json:"excerpts"`
	CallSites        []FocusCallSite `json:"call_sites"`
	Reasons          []string        `json:"reasons"`
	Uncertainties    []Uncertainty   `json:"uncertainties"`
	EvidenceStates   []EvidenceState `json:"evidence_states"`
	TypedEvidence    *TypedEvidence  `json:"typed_evidence,omitempty"`
	Truncated        bool            `json:"truncated"`
}

// EvidenceState distinguishes evidence that was inspected from evidence that
// was unavailable or omitted by a bound.
type EvidenceState struct {
	Facet  string `json:"facet"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// FocusCallSite records one source-attributed incoming call.
//
//nolint:govet // Field order is the public JSON schema order.
type FocusCallSite struct {
	Caller   SymbolMatch `json:"caller"`
	Location Location    `json:"location"`
	Reason   string      `json:"reason"`
}

// FocusSelection records the explicit anchor used to produce a focused
// context. At most one anchor is populated for a request.
type FocusSelection struct {
	Query     string          `json:"query,omitempty"`
	SymbolRef SymbolRef       `json:"symbol_ref,omitempty"`
	Position  *SourcePosition `json:"position,omitempty"`
	File      string          `json:"file,omitempty"`
	Package   string          `json:"package,omitempty"`
}

// SourceExcerpt is a bounded source slice supporting a selected declaration
// or relationship. Text is always sourced from the observed snapshot.
//
//nolint:govet // Field order is the public JSON schema order.
type SourceExcerpt struct {
	Location Location `json:"location"`
	Text     string   `json:"text"`
}

type verificationFocusMetadata struct {
	Snapshot SnapshotRef         `json:"snapshot"`
	Request  focusPolicyIdentity `json:"request"`
}

type focusPolicyIdentity struct {
	MinChangedCoverage *float64            `json:"min_changed_coverage,omitempty"`
	Base               string              `json:"base"`
	Scope              string              `json:"scope"`
	FailOn             verification.FailOn `json:"fail_on"`
	MaxPackages        int                 `json:"max_packages"`
	Race               bool                `json:"race"`
}

//nolint:govet // Field order keeps persisted metadata readable.
type storedFocusPack struct {
	Version      int                 `json:"version"`
	RepositoryID string              `json:"repository_id"`
	Request      storedFocusRequest  `json:"request"`
	Selected     *storedFocusSymbol  `json:"selected,omitempty"`
	Delivered    []deliveredEvidence `json:"delivered"`
}

//nolint:govet // Field order keeps persisted selection readable.
type storedFocusRequest struct {
	Query    string          `json:"query,omitempty"`
	Position *SourcePosition `json:"position,omitempty"`
	MaxBytes int             `json:"max_bytes"`
	File     string          `json:"file,omitempty"`
	Package  string          `json:"package,omitempty"`
}

type storedFocusSymbol struct {
	Name      string `json:"name"`
	Qualified string `json:"qualified"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
}

//nolint:govet // Field order keeps persisted evidence readable.
type deliveredEvidence struct {
	Kind     string   `json:"kind"`
	Location Location `json:"location"`
	Digest   string   `json:"digest"`
}

// Focus returns current change and optional declaration evidence without running verification.
func (c *Core) Focus(ctx context.Context, request FocusRequest) (FocusResult, error) {
	ctx, cancel := c.runner.Deadline(ctx)
	defer cancel()
	if err := normalizeFocusRequest(&request); err != nil {
		return FocusResult{}, err
	}
	var previous *storedFocusPack
	var err error
	if request.PreviousPackID != "" {
		previous, err = c.loadFocusPack(request.PreviousPackID)
		if err != nil {
			return FocusResult{}, err
		}
	}
	observation, err := c.observe(ctx, request.Base, request.Scope, request.ExpectedSnapshotID)
	if err != nil {
		return FocusResult{}, err
	}
	defer observation.release()
	refresh := (*FocusRefresh)(nil)
	refreshCandidates := []SymbolMatch{}
	if previous != nil {
		if previous.RepositoryID != observation.snapshot.RepositoryID {
			return FocusResult{}, fmt.Errorf("previous focus pack belongs to another repository")
		}
		request.Query, request.Position, request.SymbolRef = previous.Request.Query, previous.Request.Position, ""
		request.FocusFile, request.FocusPackage = previous.Request.File, previous.Request.Package
		request.MaxBytes = previous.Request.MaxBytes
		refresh = &FocusRefresh{PreviousPackID: request.PreviousPackID, Status: "replaced", Message: "previous evidence was reselected against the current observation"}
		if previous.Selected != nil {
			current, resolveErr := c.resolveRefreshSymbol(ctx, &observation, *previous.Selected)
			if resolveErr != nil {
				return FocusResult{}, resolveErr
			}
			switch len(current) {
			case 1:
				request.Query, request.Position, request.SymbolRef = "", nil, current[0].Ref
			case 0:
				refresh.Status = "selection_required"
				refresh.Message = "the previous declaration was not found; deletion is unconfirmed and a fresh selection is required"
			default:
				refreshCandidates = current
				refresh.Status = "selection_required"
				refresh.Message = "the previous declaration resolves ambiguously after a move or rename; select a current candidate"
			}
		}
	}
	focused, err := c.focusContext(ctx, &observation, request)
	if err != nil {
		return FocusResult{}, err
	}
	analysis, err := c.changes.Analyze(ctx, verification.ChangeOptions{Base: request.Base, Package: request.Scope, MaxPackages: request.MaxPackages})
	if err != nil {
		return FocusResult{}, err
	}
	if analysis.Repository.BaseCommit != observation.snapshot.BaseCommit || analysis.Repository.MergeBaseCommit != observation.snapshot.MergeBaseCommit || analysis.Repository.HeadCommit != observation.snapshot.HeadCommit {
		return FocusResult{}, fmt.Errorf("%w while analyzing change consequences", ErrSnapshotChanged)
	}
	applicability, err := c.verificationApplicability(ctx, observation.snapshot, focusIdentity(request))
	if err != nil {
		return FocusResult{}, err
	}
	if _, validationErr := c.snapshots.Validate(ctx, observation.snapshot); validationErr != nil {
		return FocusResult{}, validationErr
	}
	if refresh != nil && refresh.Status == "selection_required" {
		focused = unresolvedRefreshContext(previous, refresh.Message, refreshCandidates)
	}
	result := FocusResult{
		SchemaVersion: FocusSchemaVersion, Provider: c.provider(), Snapshot: observation.snapshot,
		Change: analysis.Change, Impact: analysis.Impact, Risks: nonNilRisks(analysis.Risks),
		Uncertainties: nonNilVerificationUncertainties(analysis.Uncertainties), Verification: applicability,
		ObservedPackages: analysis.ObservedPackages, Complete: analysis.Complete, Context: focused, Refresh: refresh,
	}
	if focused != nil {
		selection := focused.Selection
		if previous != nil {
			selection = FocusSelection{Query: previous.Request.Query, Position: previous.Request.Position, File: previous.Request.File, Package: previous.Request.Package}
		}
		result.PackID, err = c.saveFocusPack(observation.snapshot, request, selection, focused)
		if err != nil {
			return FocusResult{}, err
		}
	}
	return result, nil
}

func (c *Core) resolveRefreshSymbol(ctx context.Context, observation *snapshotObservation, selected storedFocusSymbol) ([]SymbolMatch, error) {
	matches := []SymbolMatch{}
	err := c.readObservation(ctx, observation, func(reader semanticReader) error {
		found, err := reader.Search(ctx, selected.Name)
		if err != nil {
			return err
		}
		normalized, err := normalizeSymbolMatches(observation.snapshot, found.Items)
		if err != nil {
			return err
		}
		exact := []SymbolMatch{}
		candidates := []SymbolMatch{}
		for _, match := range normalized {
			if match.Kind == selected.Kind && match.Name == selected.Name {
				candidates = append(candidates, match)
			}
			if match.Qualified == selected.Qualified && match.Kind == selected.Kind {
				exact = append(exact, match)
			}
		}
		if len(exact) == 1 {
			matches = exact
		} else {
			matches = candidates
		}
		return nil
	})
	return matches, err
}

func unresolvedRefreshContext(previous *storedFocusPack, message string, candidates []SymbolMatch) *FocusContext {
	if candidates == nil {
		candidates = []SymbolMatch{}
	}
	return &FocusContext{Selection: FocusSelection{Query: previous.Request.Query, Position: previous.Request.Position, File: previous.Request.File, Package: previous.Request.Package}, Candidates: candidates, TestDeclarations: []SymbolMatch{}, Excerpts: []SourceExcerpt{}, CallSites: []FocusCallSite{}, Reasons: []string{message}, Uncertainties: []Uncertainty{{Code: "semantic.refresh_unresolved", Message: message, Locations: []Location{}}}, EvidenceStates: []EvidenceState{{Facet: "previous_declaration", State: "unavailable", Reason: "resolution did not confirm deletion"}}}
}

func (c *Core) saveFocusPack(snapshot SnapshotRef, request FocusRequest, selection FocusSelection, focused *FocusContext) (string, error) {
	pack := storedFocusPack{Version: 1, RepositoryID: snapshot.RepositoryID, Request: storedFocusRequest{Query: selection.Query, Position: selection.Position, MaxBytes: request.MaxBytes, File: selection.File, Package: selection.Package}, Delivered: focusEvidenceManifest(focused)}
	if focused.Symbol != nil {
		match := focused.Symbol.Symbol
		pack.Selected = &storedFocusSymbol{Name: match.Name, Qualified: match.Qualified, Kind: match.Kind, File: match.Location.File}
	}
	payload, err := json.Marshal(pack)
	if err != nil {
		return "", err
	}
	artifact, err := c.artifacts.Put(snapshot.ID, "focus-pack/v1", payload)
	if err != nil {
		return "", err
	}
	if err := c.artifacts.pruneFocusPacks(); err != nil {
		return "", err
	}
	return artifact.ID, nil
}

func (c *Core) loadFocusPack(id string) (*storedFocusPack, error) {
	artifact, err := c.artifacts.getFocusPack(id)
	if err != nil {
		return nil, err
	}
	var pack storedFocusPack
	if json.Unmarshal(artifact.Payload, &pack) != nil || pack.Version != 1 || pack.RepositoryID == "" || pack.Request.MaxBytes == 0 || pack.Delivered == nil {
		return nil, ErrArtifactCorrupt
	}
	return &pack, nil
}

func focusEvidenceManifest(context *FocusContext) []deliveredEvidence {
	result := []deliveredEvidence{}
	appendEvidence := func(kind string, location Location, value string) {
		digest := sha256.Sum256([]byte(value))
		result = append(result, deliveredEvidence{Kind: kind, Location: location, Digest: hex.EncodeToString(digest[:])})
	}
	if context.Symbol != nil {
		appendEvidence("declaration", context.Symbol.Symbol.Location, string(context.Symbol.Symbol.Ref))
	}
	for _, excerpt := range context.Excerpts {
		appendEvidence("excerpt", excerpt.Location, excerpt.Text)
	}
	for _, site := range context.CallSites {
		appendEvidence("call_site", site.Location, string(site.Caller.Ref))
	}
	for _, test := range context.TestDeclarations {
		appendEvidence("test_declaration", test.Location, string(test.Ref))
	}
	if context.TypedEvidence != nil {
		for _, relationship := range context.TypedEvidence.Relationships {
			appendEvidence("typed_relationship", relationship.Location, relationship.Kind+"\x00"+relationship.Subject+"\x00"+relationship.Object+"\x00"+relationship.Evidence)
		}
	}
	return result
}

func (c *Core) focusContext(ctx context.Context, observation *snapshotObservation, request FocusRequest) (*FocusContext, error) {
	if strings.TrimSpace(request.Query) == "" && request.SymbolRef == "" && request.Position == nil && request.FocusFile == "" && request.FocusPackage == "" {
		return nil, nil
	}
	if request.FocusFile != "" || request.FocusPackage != "" {
		return c.focusFileOrPackage(ctx, observation, request)
	}
	result := &FocusContext{
		Selection:  FocusSelection{Query: request.Query, SymbolRef: request.SymbolRef, Position: request.Position},
		Candidates: []SymbolMatch{}, TestDeclarations: []SymbolMatch{}, Excerpts: []SourceExcerpt{},
		CallSites: []FocusCallSite{},
		Reasons:   []string{}, Uncertainties: []Uncertainty{}, EvidenceStates: []EvidenceState{},
	}
	var selectedFile string
	var selectedPosition Position
	var selected SymbolMatch
	var selectedSet bool
	err := c.readObservation(ctx, observation, func(reader semanticReader) error {
		switch {
		case request.Query != "":
			if !observation.snapshot.Capabilities.WorkspaceSymbol {
				result.Uncertainties = append(result.Uncertainties, Uncertainty{Code: "semantic.unsupported_capability", Message: "the active semantic provider does not support workspace symbol queries", Locations: []Location{}})
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "declarations", State: "unavailable", Reason: "workspace symbol queries are unsupported"})
				result.Reasons = append(result.Reasons, "the requested query could not be resolved by the active semantic provider")
				return nil
			}
			matches, searchErr := reader.Search(ctx, request.Query)
			if searchErr != nil {
				return searchErr
			}
			normalized, normalizeErr := normalizeSymbolMatches(observation.snapshot, matches.Items)
			if normalizeErr != nil {
				return normalizeErr
			}
			if len(normalized) > maxFocusCandidates {
				result.Candidates = append(result.Candidates, normalized[:maxFocusCandidates]...)
				result.Uncertainties = append(result.Uncertainties, Uncertainty{Code: "semantic.query_bounded", Message: fmt.Sprintf("query returned more than %d candidates", maxFocusCandidates), Locations: []Location{}})
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "declaration_candidates", State: "gathered_but_omitted", Reason: "candidate bound exceeded"})
			} else {
				result.Candidates = append(result.Candidates, normalized...)
			}
			switch len(normalized) {
			case 0:
				result.Reasons = append(result.Reasons, "query returned no declarations")
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "declarations", State: "examined_and_absent"}, EvidenceState{Facet: "relationships", State: "unexamined", Reason: "no declaration was selected"})
			case 1:
				selected = normalized[0]
				selectedSet = true
				result.Reasons = append(result.Reasons, "query resolved to its only workspace declaration")
			default:
				result.Reasons = append(result.Reasons, "query is ambiguous; select one current Symbol Ref")
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "relationships", State: "unexamined", Reason: "query is ambiguous"})
			}
		case request.SymbolRef != "":
			identity, decodeErr := decodeSymbolRef(request.SymbolRef)
			if decodeErr != nil {
				return decodeErr
			}
			if identity.SnapshotID != observation.snapshot.ID {
				return fmt.Errorf("%w: symbol belongs to %s, observed %s", ErrSnapshotChanged, identity.SnapshotID, observation.snapshot.ID)
			}
			selectedFile, selectedPosition = identity.Path, identity.Position
			result.Reasons = append(result.Reasons, "selected the supplied current Symbol Ref")
			selectedSet = true
		case request.Position != nil:
			absolute, resolveErr := c.workspace.Resolve(request.Position.File)
			if resolveErr != nil {
				return resolveErr
			}
			selectedFile, resolveErr = c.workspace.Relative(absolute)
			if resolveErr != nil {
				return resolveErr
			}
			contents, sourceErr := observation.source(selectedFile)
			if sourceErr != nil {
				return sourceErr
			}
			selectedPosition, resolveErr = sourcePositionFromContents(filepath.Base(absolute), contents, *request.Position)
			if resolveErr != nil {
				return resolveErr
			}
			result.Reasons = append(result.Reasons, "selected the declaration enclosing the supplied source position")
			selectedSet = true
		}
		if request.Query != "" && selectedSet {
			selectedFile = selected.Location.File
			contents, sourceErr := observation.source(selectedFile)
			if sourceErr != nil {
				return sourceErr
			}
			selectedPosition, sourceErr = sourcePositionFromContents(filepath.Base(selectedFile), contents, SourcePosition{File: selectedFile, Line: selected.Location.Line, Column: selected.Location.Column})
			if sourceErr != nil {
				return sourceErr
			}
		}
		if !selectedSet {
			return nil
		}
		if selectedFile == "" {
			selectedFile = selected.Location.File
		}
		focused, tests, excerpts, uncertainties, collectErr := c.focusSymbol(ctx, reader, observation, selectedFile, selectedPosition, request.MaxBytes)
		if collectErr != nil {
			return collectErr
		}
		result.Symbol = focused
		if focused != nil {
			typed := c.typedEvidence(ctx, observation, focused, tests)
			result.TypedEvidence = &typed
			result.Uncertainties = append(result.Uncertainties, typed.Uncertainties...)
		} else {
			result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "declaration_relationships", State: "unavailable", Reason: "document symbols are unsupported"})
			return nil
		}
		for _, call := range focused.Calls.Items {
			if call.Direction != "incoming" {
				continue
			}
			for _, site := range call.CallSites {
				result.CallSites = append(result.CallSites, FocusCallSite{Caller: call.Symbol, Location: site, Reason: "direct incoming call site"})
			}
		}
		result.TestDeclarations = tests
		result.Excerpts = excerpts
		result.Uncertainties = append(result.Uncertainties, uncertainties...)
		if len(result.CallSites) == 0 {
			switch {
			case !observation.snapshot.Capabilities.CallHierarchy:
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "direct_callers", State: "unavailable", Reason: "the active semantic provider does not support call hierarchy"})
			case !isCallableSymbol(focused.Symbol.Kind):
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "direct_callers", State: "unexamined", Reason: "call hierarchy expansion is limited to function and method declarations"})
			case focused.Calls.Truncated || focused.Calls.Total > len(focused.Calls.Items) || hasIncomingCallWithoutSites(focused.Calls.Items):
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "direct_callers", State: "gathered_but_omitted", Reason: "call hierarchy evidence was incomplete or lacked source call sites"})
			default:
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "direct_callers", State: "examined_and_absent"})
			}
		}
		if isCallableSymbol(focused.Symbol.Kind) {
			result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "type_definition", State: "unexamined", Reason: "type definitions are not applicable to function and method declarations"})
		} else {
			switch {
			case !observation.snapshot.Capabilities.TypeDefinition:
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "type_definition", State: "unavailable", Reason: "the active semantic provider does not support type definitions"})
			case focused.TypeDefinitions.Truncated || focused.TypeDefinitions.Total > len(focused.TypeDefinitions.Items):
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "type_definition", State: "gathered_but_omitted", Reason: "type-definition evidence was bounded or omitted"})
			case focused.TypeDefinitions.Total == 0:
				result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "type_definition", State: "examined_and_absent"})
			}
		}
		if len(tests) == 0 {
			result.EvidenceStates = append(result.EvidenceStates, EvidenceState{Facet: "related_tests", State: "examined_and_absent"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(result.Reasons)
	sort.Slice(result.Uncertainties, func(i, j int) bool {
		if result.Uncertainties[i].Code != result.Uncertainties[j].Code {
			return result.Uncertainties[i].Code < result.Uncertainties[j].Code
		}
		return result.Uncertainties[i].Message < result.Uncertainties[j].Message
	})
	bounded, err := c.boundFocusContext(*result, request.MaxBytes)
	if err != nil {
		return nil, err
	}
	return &bounded, nil
}

func (c *Core) focusSymbol(ctx context.Context, reader semanticReader, observation *snapshotObservation, file string, position Position, maximum int) (*SymbolContext, []SymbolMatch, []SourceExcerpt, []Uncertainty, error) {
	if !observation.snapshot.Capabilities.DocumentSymbol {
		return nil, []SymbolMatch{}, []SourceExcerpt{}, []Uncertainty{{Code: "semantic.unsupported_capability", Message: "the active semantic provider does not support document symbols", Locations: []Location{}}}, nil
	}
	result := emptySymbolContext(observation.snapshot, c.provider())
	omitted := make(map[string]int)
	uncertainties := []Uncertainty{}
	selected, err := reader.SymbolAt(ctx, file, position)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	result.Symbol, err = normalizeSymbolMatch(observation.snapshot, selected)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if observation.snapshot.Capabilities.Hover {
		result.Hover, err = reader.Hover(ctx, file, position)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	if observation.snapshot.Capabilities.Definition {
		locations, readErr := reader.Definition(ctx, file, position)
		if readErr != nil {
			return nil, nil, nil, nil, readErr
		}
		result.Definitions = locationSet(locations)
		omitted["definitions"] = locations.Omitted
	}
	if observation.snapshot.Capabilities.TypeDefinition && !isCallableSymbol(result.Symbol.Kind) {
		locations, readErr := reader.TypeDefinition(ctx, file, position)
		if readErr != nil {
			return nil, nil, nil, nil, readErr
		}
		result.TypeDefinitions = locationSet(locations)
		omitted["type definitions"] = locations.Omitted
	}
	if observation.snapshot.Capabilities.References {
		locations, readErr := reader.References(ctx, file, position)
		if readErr != nil {
			return nil, nil, nil, nil, readErr
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
	if observation.snapshot.Capabilities.Implementation && (result.Symbol.Kind == "go.type" || result.Symbol.Kind == "go.method") {
		symbols, readErr := reader.Implementations(ctx, file, position)
		if readErr != nil {
			return nil, nil, nil, nil, readErr
		}
		symbols.Items, err = normalizeSymbolMatches(observation.snapshot, symbols.Items)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		result.Implementations = symbolSet(symbols)
		omitted["implementations"] = symbols.Omitted
	}
	if observation.snapshot.Capabilities.Diagnostics {
		result.Diagnostics, err = reader.Diagnostics(ctx, file)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		result.DiagnosticsTotal = len(result.Diagnostics)
	}
	if observation.snapshot.Capabilities.CallHierarchy && isCallableSymbol(result.Symbol.Kind) {
		calls, readErr := reader.Calls(ctx, file, position)
		if readErr != nil {
			return nil, nil, nil, nil, readErr
		}
		result.Calls = callSet(calls)
		omitted["call relationships"] = calls.Omitted
	}
	for facet, count := range omitted {
		if count > 0 {
			uncertainties = append(uncertainties, Uncertainty{Code: "semantic.external_locations", Message: fmt.Sprintf("%d %s outside the configured workspace were omitted", count, facet), Locations: []Location{}})
		}
	}
	if !observation.snapshot.Capabilities.CallHierarchy {
		uncertainties = append(uncertainties, Uncertainty{Code: "semantic.unsupported_capability", Message: "the active semantic provider does not support call hierarchy", Locations: []Location{}})
	}
	uncertainties = append(uncertainties, fileSemanticUncertaintiesObserved(observation, file, true)...)
	result.Uncertainties = append(result.Uncertainties, uncertainties...)
	sort.Slice(result.Uncertainties, func(i, j int) bool {
		if result.Uncertainties[i].Code != result.Uncertainties[j].Code {
			return result.Uncertainties[i].Code < result.Uncertainties[j].Code
		}
		return result.Uncertainties[i].Message < result.Uncertainties[j].Message
	})
	result, err = c.boundSymbolContext(result, maximum)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tests, testUncertainties := c.focusTestDeclarations(ctx, reader, observation, result.RelatedTests.Items)
	uncertainties = append(uncertainties, testUncertainties...)
	excerpts := c.focusExcerpts(observation, result, tests)
	return &result, tests, excerpts, uncertainties, nil
}

func (c *Core) focusTestDeclarations(ctx context.Context, reader semanticReader, observation *snapshotObservation, references []Location) ([]SymbolMatch, []Uncertainty) {
	declarations := []SymbolMatch{}
	uncertainties := []Uncertainty{}
	for _, reference := range references {
		if len(declarations) >= maxFocusTestSymbols {
			uncertainties = append(uncertainties, Uncertainty{Code: "semantic.related_tests_bounded", Message: fmt.Sprintf("related test declarations are bounded to %d items", maxFocusTestSymbols), Locations: []Location{}})
			break
		}
		contents, sourceErr := observation.source(reference.File)
		if sourceErr != nil {
			uncertainties = append(uncertainties, Uncertainty{Code: "semantic.related_test_unavailable", Message: sourceErr.Error(), Locations: []Location{reference}})
			continue
		}
		position, positionErr := sourcePositionFromContents(filepath.Base(reference.File), contents, SourcePosition{File: reference.File, Line: reference.Line, Column: reference.Column})
		if positionErr != nil {
			uncertainties = append(uncertainties, Uncertainty{Code: "semantic.related_test_unavailable", Message: positionErr.Error(), Locations: []Location{reference}})
			continue
		}
		match, err := reader.SymbolAt(ctx, reference.File, position)
		if err != nil {
			uncertainties = append(uncertainties, Uncertainty{Code: "semantic.related_test_unavailable", Message: fmt.Sprintf("enclosing test declaration could not be resolved at %s:%d", reference.File, reference.Line), Locations: []Location{reference}})
			continue
		}
		match, err = normalizeSymbolMatch(observation.snapshot, match)
		if err != nil {
			uncertainties = append(uncertainties, Uncertainty{Code: "semantic.related_test_unavailable", Message: err.Error(), Locations: []Location{reference}})
			continue
		}
		if !isGoTestEntry(match.Name) {
			continue
		}
		declarations = append(declarations, match)
	}
	sortSymbols(declarations)
	return declarations, uncertainties
}

func isGoTestEntry(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isCallableSymbol(kind string) bool {
	return kind == "go.function" || kind == "go.method"
}

func hasIncomingCallWithoutSites(calls []CallEdge) bool {
	for _, call := range calls {
		if call.Direction == "incoming" && len(call.CallSites) == 0 {
			return true
		}
	}
	return false
}

func (c *Core) focusExcerpts(observation *snapshotObservation, symbol SymbolContext, tests []SymbolMatch) []SourceExcerpt {
	excerpts := []SourceExcerpt{}
	seen := make(map[string]struct{})
	appendExcerpt := func(location Location) {
		if len(excerpts) >= maxFocusExcerpts || location.File == "" {
			return
		}
		key := fmt.Sprintf("%s:%d:%d:%d:%d", location.File, location.Line, location.Column, location.EndLine, location.EndColumn)
		if _, exists := seen[key]; exists {
			return
		}
		contents, err := observation.source(location.File)
		if err != nil {
			return
		}
		start, end := sourceLines(contents, location.Line, location.EndLine)
		if start < 0 || end <= start {
			return
		}
		text := string(contents[start:end])
		if len(text) > maxFocusExcerptBytes {
			text = text[:maxFocusExcerptBytes]
			for len(text) > 0 && (text[len(text)-1]&0xc0) == 0x80 {
				text = text[:len(text)-1]
			}
		}
		seen[key] = struct{}{}
		excerpts = append(excerpts, SourceExcerpt{Location: location, Text: text})
	}
	appendExcerpt(symbol.Symbol.Location)
	for _, match := range tests {
		appendExcerpt(match.Location)
	}
	for _, call := range symbol.Calls.Items {
		for _, location := range call.CallSites {
			appendExcerpt(location)
		}
	}
	sort.Slice(excerpts, func(i, j int) bool { return locationLess(excerpts[i].Location, excerpts[j].Location) })
	return excerpts
}

func sourceLines(contents []byte, first, last int) (int, int) {
	if first < 1 {
		return -1, -1
	}
	if last < first {
		last = first
	}
	line, start := 1, 0
	for index, value := range contents {
		if line == first {
			start = index
			break
		}
		if value == '\n' {
			line++
		}
	}
	if line != first {
		if first == line+1 && len(contents) == 0 {
			return 0, 0
		}
		return -1, -1
	}
	end := len(contents)
	line = first
	for index := start; index < len(contents); index++ {
		if contents[index] != '\n' {
			continue
		}
		if line == last {
			end = index + 1
			break
		}
		line++
	}
	return start, end
}

func (c *Core) boundFocusContext(full FocusContext, maximum int) (FocusContext, error) {
	if encodedSize(full) <= maximum {
		return full, nil
	}
	bounded := full
	bounded.Truncated = true
	bounded.EvidenceStates = append(bounded.EvidenceStates, EvidenceState{Facet: "budgeted_evidence", State: "gathered_but_omitted", Reason: "focused context exceeded the requested byte budget"})
	for encodedSize(bounded) > maximum {
		switch {
		case len(bounded.Excerpts) > 0:
			bounded.Excerpts = bounded.Excerpts[:len(bounded.Excerpts)-1]
		case bounded.TypedEvidence != nil && len(bounded.TypedEvidence.Relationships) > 0:
			bounded.TypedEvidence.Relationships = bounded.TypedEvidence.Relationships[:len(bounded.TypedEvidence.Relationships)-1]
			bounded.TypedEvidence.Truncated = true
		case len(bounded.CallSites) > 0:
			bounded.CallSites = bounded.CallSites[:len(bounded.CallSites)-1]
		case bounded.Symbol != nil && len(bounded.Symbol.Calls.Items) > 0:
			bounded.Symbol.Calls.Items = bounded.Symbol.Calls.Items[:len(bounded.Symbol.Calls.Items)-1]
			bounded.Symbol.Calls.Truncated = true
		case bounded.Symbol != nil && len(bounded.Symbol.References.Items) > 0:
			bounded.Symbol.References.Items = bounded.Symbol.References.Items[:len(bounded.Symbol.References.Items)-1]
			bounded.Symbol.References.Truncated = true
		case bounded.Symbol != nil && len(bounded.Symbol.Implementations.Items) > 0:
			bounded.Symbol.Implementations.Items = bounded.Symbol.Implementations.Items[:len(bounded.Symbol.Implementations.Items)-1]
			bounded.Symbol.Implementations.Truncated = true
		case len(bounded.TestDeclarations) > 0:
			bounded.TestDeclarations = bounded.TestDeclarations[:len(bounded.TestDeclarations)-1]
		case len(bounded.Candidates) > 2:
			bounded.Candidates = bounded.Candidates[:len(bounded.Candidates)-1]
		default:
			return FocusContext{}, fmt.Errorf("focused context metadata exceeds %d-byte budget", maximum)
		}
	}
	return bounded, nil
}

func normalizeFocusRequest(request *FocusRequest) error {
	if request == nil || strings.TrimSpace(request.Base) == "" || invalidFocusArgument(request.Base) {
		return fmt.Errorf("base is required and must be one local ref")
	}
	if request.Scope == "" {
		request.Scope = "./..."
	}
	if invalidFocusArgument(request.Scope) {
		return fmt.Errorf("scope is invalid")
	}
	if request.MaxPackages == 0 {
		request.MaxPackages = 200
	}
	if request.MaxPackages < 1 || request.MaxPackages > 500 {
		return fmt.Errorf("max packages must be between 1 and 500")
	}
	if request.FailOn == "" {
		request.FailOn = verification.FailOnError
	}
	switch request.FailOn {
	case verification.FailOnError, verification.FailOnWarning, verification.FailOnInfo, verification.FailOnNone:
	default:
		return fmt.Errorf("fail_on is invalid")
	}
	if request.MinChangedCoverage != nil && (*request.MinChangedCoverage < 0 || *request.MinChangedCoverage > 100) {
		return fmt.Errorf("min changed coverage must be between 0 and 100")
	}
	anchors := 0
	if strings.TrimSpace(request.Query) != "" {
		anchors++
		request.Query = strings.TrimSpace(request.Query)
	}
	if request.SymbolRef != "" {
		anchors++
	}
	if request.Position != nil {
		anchors++
		if request.Position.File == "" || invalidFocusArgument(request.Position.File) || request.Position.Line < 1 || request.Position.Column < 1 {
			return fmt.Errorf("position must contain a contained file and positive line and column")
		}
	}
	if request.FocusFile != "" {
		anchors++
		if invalidFocusArgument(request.FocusFile) {
			return fmt.Errorf("focus file is invalid")
		}
	}
	if request.FocusPackage != "" {
		anchors++
		if invalidFocusArgument(request.FocusPackage) {
			return fmt.Errorf("focus package is invalid")
		}
	}
	if request.PreviousPackID != "" {
		if !validID(request.PreviousPackID) {
			return fmt.Errorf("previous pack ID is invalid")
		}
		anchors++
	}
	if anchors > 1 {
		return fmt.Errorf("provide at most one focus selection")
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = DefaultBriefBytes
	}
	if err := validContextBudget(request.MaxBytes); err != nil {
		return err
	}
	return nil
}

func invalidFocusArgument(value string) bool {
	return strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") || strings.ContainsRune(value, 0)
}

func focusIdentity(request FocusRequest) focusPolicyIdentity {
	return focusPolicyIdentity{MinChangedCoverage: request.MinChangedCoverage, Base: request.Base, Scope: request.Scope, FailOn: request.FailOn, MaxPackages: request.MaxPackages, Race: request.Race}
}

func (c *Core) verificationApplicability(ctx context.Context, snapshot SnapshotRef, requested focusPolicyIdentity) (VerificationApplicability, error) {
	report, metadata, err := c.verifications.currentFocus(ctx, snapshot.RepositoryID)
	if errors.Is(err, ErrVerificationNotFound) {
		return VerificationApplicability{Reasons: []string{"no stored verification report exists for this repository"}, NextAction: "request verification for the current snapshot and policy"}, nil
	}
	if err != nil {
		return VerificationApplicability{}, err
	}
	reasons := make([]string, 0)
	if metadata == nil {
		reasons = append(reasons, "stored report has no applicability metadata")
	} else {
		if report.Repository.RequestedBase != requested.Base || report.Repository.BaseCommit != snapshot.BaseCommit || report.Repository.MergeBaseCommit != snapshot.MergeBaseCommit || report.Repository.HeadCommit != snapshot.HeadCommit {
			reasons = append(reasons, "base or resolved commit identity differs")
		}
		if report.Snapshot.CurrentID != snapshot.ID {
			reasons = append(reasons, "workspace snapshot differs")
		}
		if metadata.Request.Scope != requested.Scope {
			reasons = append(reasons, "package scope differs")
		}
		if !reflect.DeepEqual(metadata.Snapshot.Build, snapshot.Build) {
			reasons = append(reasons, "build configuration differs")
		}
		if metadata.Snapshot.GoplsVersion != snapshot.GoplsVersion || !reflect.DeepEqual(metadata.Snapshot.Capabilities, snapshot.Capabilities) {
			reasons = append(reasons, "semantic provider context differs")
		}
		if !reflect.DeepEqual(metadata.Request, requested) {
			reasons = append(reasons, "requested verification check policy differs")
		}
	}
	applicable := len(reasons) == 0
	next := "inspect the applicable report outcome and evidence"
	if !applicable {
		next = "request verification for the current snapshot and policy"
	}
	return VerificationApplicability{ReportID: report.ID, Outcome: report.Result.Status, Reasons: reasons, NextAction: next, Present: true, Applicable: applicable}, nil
}

func nonNilRisks(items []verification.RiskArea) []verification.RiskArea {
	if items == nil {
		return []verification.RiskArea{}
	}
	return items
}

func nonNilVerificationUncertainties(items []verification.Uncertainty) []verification.Uncertainty {
	if items == nil {
		return []verification.Uncertainty{}
	}
	return items
}

// FocusSummary renders the shared compact CLI and MCP description.
func FocusSummary(result FocusResult) string {
	selected := "no declaration selected"
	if result.Context != nil && result.Context.Symbol != nil {
		selected = "selected " + result.Context.Symbol.Symbol.Qualified
	}
	if result.Context != nil && result.Context.Selection.Query != "" && len(result.Context.Candidates) > 1 {
		selected = fmt.Sprintf("%d ambiguous candidates", len(result.Context.Candidates))
	}
	if result.Context != nil && (result.Context.Selection.File != "" || result.Context.Selection.Package != "") {
		selected = fmt.Sprintf("%d active type declarations", len(result.Context.Candidates))
	}
	summary := fmt.Sprintf("change context at snapshot %s: %d changed files, %d affected packages, %s; verification applicable: %t", result.Snapshot.ID, result.Change.FilesTotal, result.Impact.PackagesTotal, selected, result.Verification.Applicable)
	if result.PackID != "" {
		summary += "; focus pack: " + result.PackID
	}
	if result.Refresh != nil {
		summary += "; refresh: " + result.Refresh.Status
	}
	return summary
}
