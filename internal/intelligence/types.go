package intelligence

import (
	"context"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/verification"
)

const (
	// ContextSchemaVersion identifies the pre-freeze Context Pack contract.
	ContextSchemaVersion = "agentic.context/v1alpha1"
	// ChangeSchemaVersion identifies the pre-freeze Change Contract contract.
	ChangeSchemaVersion = "agentic.change/v1alpha1"
	// DefaultBriefBytes is the compact workspace-brief response budget.
	DefaultBriefBytes = 8 << 10
	// DefaultSymbolBytes is the compact symbol-context response budget.
	DefaultSymbolBytes = 16 << 10
	// DefaultSearchLimit is the ordinary workspace-symbol result count.
	DefaultSearchLimit = 20
	// MaximumSearchLimit bounds one workspace-symbol response.
	MaximumSearchLimit = 100
)

// Service is the sole semantic product seam. Its domain types are independent
// of MCP, LSP, gopls, Git, and subprocess protocols.
type Service interface {
	Brief(context.Context, BriefRequest) (ContextPack, error)
	Search(context.Context, SearchRequest) (SearchResult, error)
	Symbol(context.Context, SymbolRequest) (SymbolContext, error)
	Begin(context.Context, BeginRequest) (ChangeContract, error)
	Checkpoint(context.Context, CheckpointRequest) (Checkpoint, error)
	Refactor(context.Context, RefactorRequest) (RefactorResult, error)
	Verify(context.Context, verification.Request) (verification.Report, error)
}

// Provider identifies the implementation that produced intelligence.
type Provider struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities describes the effective semantic and compact-context contract.
//
//nolint:govet // field order is the public JSON schema order.
type Capabilities struct {
	Provider        Provider           `json:"provider"`
	Semantic        CapabilityManifest `json:"semantic"`
	ContextSchema   string             `json:"context_schema"`
	BriefBytes      int                `json:"brief_bytes"`
	SymbolBytes     int                `json:"symbol_bytes"`
	SearchDefault   int                `json:"search_default"`
	SearchMaximum   int                `json:"search_maximum"`
	ArtifactMaximum int64              `json:"artifact_maximum_bytes"`
}

// Location is a workspace-relative, one-based UTF-8 byte source range.
type Location struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line,omitempty"`
	EndColumn int    `json:"end_column,omitempty"`
}

// Diagnostic is one normalized compiler or semantic-provider observation.
type Diagnostic struct {
	Source   string   `json:"source"`
	Code     string   `json:"code,omitempty"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
}

// Uncertainty states an analytical limit without inferring safety.
type Uncertainty struct {
	Code      string     `json:"code"`
	Message   string     `json:"message"`
	Locations []Location `json:"locations"`
}

// RiskArea identifies a source-grounded review lens, not a diagnosed defect.
type RiskArea struct {
	Code      string     `json:"code"`
	Summary   string     `json:"summary"`
	Guidance  string     `json:"guidance"`
	Locations []Location `json:"locations"`
}

// ModuleSummary describes one active Go module without an absolute path.
type ModuleSummary struct {
	Path      string `json:"path"`
	GoVersion string `json:"go_version"`
	Workspace string `json:"workspace"`
}

// PackageSummary describes one workspace package and compact API facts.
//
//nolint:govet // field order is the public JSON schema order.
type PackageSummary struct {
	Kind        string   `json:"kind"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Directory   string   `json:"directory"`
	Module      string   `json:"module"`
	Imports     int      `json:"imports"`
	Tests       int      `json:"tests"`
	Exported    []string `json:"exported"`
	Cgo         bool     `json:"cgo"`
	Generated   bool     `json:"generated"`
	Constrained bool     `json:"constrained"`
}

// GuidanceRef identifies applicable repository guidance by location and hash.
type GuidanceRef struct {
	File   string `json:"file"`
	Digest string `json:"digest"`
}

// ChangeContext is a compact, language-neutral view of local change impact.
//
//nolint:govet // field order is the public JSON schema order.
type ChangeContext struct {
	Files                  []string `json:"files"`
	FilesTotal             int      `json:"files_total"`
	Declarations           []string `json:"declarations"`
	DeclarationsTotal      int      `json:"declarations_total"`
	DirectUnits            []string `json:"direct_units"`
	DirectUnitsTotal       int      `json:"direct_units_total"`
	ReverseDependents      []string `json:"reverse_dependents"`
	ReverseDependentsTotal int      `json:"reverse_dependents_total"`
	ObservedUnits          int      `json:"observed_units"`
	Truncated              bool     `json:"truncated"`
	Complete               bool     `json:"complete"`
}

// SymbolRef is an opaque snapshot-bound Go symbol identity.
type SymbolRef string

// SymbolMatch is one normalized workspace symbol with source provenance.
type SymbolMatch struct {
	Ref       SymbolRef `json:"ref"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Qualified string    `json:"qualified"`
	Package   string    `json:"package"`
	Location  Location  `json:"location"`
}

// ContextTotals retains complete counts before response-budget truncation.
type ContextTotals struct {
	Modules       int `json:"modules"`
	Packages      int `json:"packages"`
	Symbols       int `json:"symbols"`
	Diagnostics   int `json:"diagnostics"`
	Guidance      int `json:"guidance"`
	Risks         int `json:"risks"`
	Uncertainties int `json:"uncertainties"`
}

// ContextPack is the durable, compact workspace-or-symbol context boundary.
//
//nolint:govet // field order is the public JSON schema order.
type ContextPack struct {
	SchemaVersion string           `json:"schema_version"`
	Provider      Provider         `json:"provider"`
	Snapshot      SnapshotRef      `json:"snapshot"`
	Modules       []ModuleSummary  `json:"modules"`
	Packages      []PackageSummary `json:"packages"`
	Symbols       []SymbolMatch    `json:"symbols"`
	Diagnostics   []Diagnostic     `json:"diagnostics"`
	Guidance      []GuidanceRef    `json:"guidance"`
	Change        *ChangeContext   `json:"change,omitempty"`
	Risks         []RiskArea       `json:"risks"`
	Uncertainties []Uncertainty    `json:"uncertainties"`
	Totals        ContextTotals    `json:"totals"`
	Truncated     bool             `json:"truncated"`
	NextCursor    string           `json:"next_cursor,omitempty"`
}

// BriefRequest selects one compact workspace overview.
type BriefRequest struct {
	Base               string
	Scope              string
	ExpectedSnapshotID string
	MaxBytes           int
}

// SearchRequest selects a deterministic page of workspace symbols.
//
//nolint:govet // request order follows the public operation contract.
type SearchRequest struct {
	Query              string
	Scope              string
	ExpectedSnapshotID string
	Limit              int
	Cursor             string
}

// SearchResult is one snapshot-bound, deterministically ordered search page.
//
//nolint:govet // field order is the public JSON schema order.
type SearchResult struct {
	SchemaVersion string        `json:"schema_version"`
	Provider      Provider      `json:"provider"`
	Snapshot      SnapshotRef   `json:"snapshot"`
	Matches       []SymbolMatch `json:"matches"`
	Total         int           `json:"total"`
	Truncated     bool          `json:"truncated"`
	NextCursor    string        `json:"next_cursor,omitempty"`
	Uncertainties []Uncertainty `json:"uncertainties"`
}

// SourcePosition is a workspace-relative one-based UTF-8 byte position.
type SourcePosition struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// SymbolFacets selects optional expensive symbol relationships.
type SymbolFacets struct {
	CallHierarchy  bool `json:"call_hierarchy"`
	TypeDefinition bool `json:"type_definition"`
}

// SymbolRequest resolves a stable ref or a compatibility source position.
type SymbolRequest struct {
	Ref                SymbolRef
	Position           *SourcePosition
	ExpectedSnapshotID string
	Facets             SymbolFacets
	MaxBytes           int
}

// LocationSet retains complete counts for a bounded location facet.
type LocationSet struct {
	Items     []Location `json:"items"`
	Total     int        `json:"total"`
	Truncated bool       `json:"truncated"`
}

// SymbolSet retains complete counts for a bounded symbol facet.
type SymbolSet struct {
	Items     []SymbolMatch `json:"items"`
	Total     int           `json:"total"`
	Truncated bool          `json:"truncated"`
}

// CallEdge is one bounded static call-hierarchy relationship.
type CallEdge struct {
	Direction string      `json:"direction"`
	Symbol    SymbolMatch `json:"symbol"`
}

// CallSet retains complete counts for a bounded call-hierarchy facet.
type CallSet struct {
	Items     []CallEdge `json:"items"`
	Total     int        `json:"total"`
	Truncated bool       `json:"truncated"`
}

// SymbolContext contains default source-grounded facets for one Go symbol.
//
//nolint:govet // field order is the public JSON schema order.
type SymbolContext struct {
	SchemaVersion    string        `json:"schema_version"`
	Provider         Provider      `json:"provider"`
	Snapshot         SnapshotRef   `json:"snapshot"`
	Symbol           SymbolMatch   `json:"symbol"`
	Hover            string        `json:"hover"`
	Definitions      LocationSet   `json:"definitions"`
	TypeDefinitions  LocationSet   `json:"type_definitions"`
	References       LocationSet   `json:"references"`
	Implementations  SymbolSet     `json:"implementations"`
	RelatedTests     LocationSet   `json:"related_tests"`
	Diagnostics      []Diagnostic  `json:"diagnostics"`
	DiagnosticsTotal int           `json:"diagnostics_total"`
	Calls            CallSet       `json:"calls"`
	Uncertainties    []Uncertainty `json:"uncertainties"`
	Truncated        bool          `json:"truncated"`
	NextCursor       string        `json:"next_cursor,omitempty"`
}

// PolicyMode is the structural response to one machine-checkable change.
type PolicyMode string

const (
	// PolicyAllow records a structural condition without a violation.
	PolicyAllow PolicyMode = "allow"
	// PolicyWarn records a non-blocking structural violation.
	PolicyWarn PolicyMode = "warn"
	// PolicyForbid records a blocking structural violation.
	PolicyForbid PolicyMode = "forbid"
)

// StructuralPolicies configures machine-checkable Change Contract boundaries.
type StructuralPolicies struct {
	OutsideAllowedPaths PolicyMode `json:"outside_allowed_paths"`
	OutsideFocus        PolicyMode `json:"outside_focus"`
	ExportedAPI         PolicyMode `json:"exported_api"`
	Dependency          PolicyMode `json:"dependency"`
	CrossModule         PolicyMode `json:"cross_module"`
	GeneratedFile       PolicyMode `json:"generated_file"`
	TestDeletion        PolicyMode `json:"test_deletion"`
}

// Decision is caller-authored handoff context and is never semantically enforced.
//
//nolint:govet // field order is the public JSON schema order.
type Decision struct {
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// CheckpointRef is one immutable snapshot transition in a Change Contract.
//
//nolint:govet // field order is the public JSON schema order.
type CheckpointRef struct {
	ID                 string    `json:"id"`
	PreviousSnapshotID string    `json:"previous_snapshot_id"`
	CurrentSnapshotID  string    `json:"current_snapshot_id"`
	RecordedAt         time.Time `json:"recorded_at"`
}

// BeginRequest creates one persistent Change Contract.
//
//nolint:govet // request order follows the public operation contract.
type BeginRequest struct {
	Base            string
	Goal            string
	Scope           string
	FocusedPaths    []string
	FocusedPackages []string
	FocusedSymbols  []SymbolRef
	AllowedPaths    []string
	Policies        StructuralPolicies
}

// ChangeContract is one persistent structural continuity record.
//
//nolint:govet // field order is the public JSON schema order.
type ChangeContract struct {
	SchemaVersion       string             `json:"schema_version"`
	ID                  string             `json:"id"`
	RepositoryID        string             `json:"repository_id"`
	Goal                string             `json:"goal"`
	Base                string             `json:"base"`
	Scope               string             `json:"scope"`
	InitialSnapshot     SnapshotRef        `json:"initial_snapshot"`
	LatestSnapshot      SnapshotRef        `json:"latest_snapshot"`
	FocusedPaths        []string           `json:"focused_paths"`
	FocusedPackages     []string           `json:"focused_packages"`
	FocusedSymbols      []SymbolRef        `json:"focused_symbols"`
	AllowedPaths        []string           `json:"allowed_paths"`
	Policies            StructuralPolicies `json:"policies"`
	Decisions           []Decision         `json:"decisions"`
	UnresolvedQuestions []string           `json:"unresolved_questions"`
	Checkpoints         []CheckpointRef    `json:"checkpoints"`
	LatestVerification  string             `json:"latest_verification,omitempty"`
	Active              bool               `json:"active"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

// CheckpointRequest records structural drift and caller-authored handoff state.
type CheckpointRequest struct {
	ContractID          string
	ExpectedSnapshot    string
	Decisions           []string
	UnresolvedQuestions []string
}

// PolicyViolation is one machine-checkable Change Contract deviation.
type PolicyViolation struct {
	Code      string     `json:"code"`
	Policy    PolicyMode `json:"policy"`
	Message   string     `json:"message"`
	Locations []Location `json:"locations"`
}

// Checkpoint is one snapshot transition and its structural evidence.
//
//nolint:govet // field order is the public JSON schema order.
type Checkpoint struct {
	ContractID       string            `json:"contract_id"`
	Previous         SnapshotRef       `json:"previous"`
	Current          SnapshotRef       `json:"current"`
	AffectedPackages []string          `json:"affected_packages"`
	Diagnostics      []Diagnostic      `json:"diagnostics"`
	Violations       []PolicyViolation `json:"violations"`
	Uncertainties    []Uncertainty     `json:"uncertainties"`
	RecordedAt       time.Time         `json:"recorded_at"`
}

// RefactorRequest previews or applies one deterministic semantic operation.
//
//nolint:govet // request order follows the public operation contract.
type RefactorRequest struct {
	Operation          string
	Ref                SymbolRef
	NewName            string
	Files              []string
	PlanID             string
	ExpectedSnapshotID string
	Apply              bool
}

// RefactorResult is one content-addressed preview or guarded apply outcome.
//
//nolint:govet // field order is the public JSON schema order.
type RefactorResult struct {
	PlanID        string        `json:"plan_id"`
	Operation     string        `json:"operation"`
	Snapshot      SnapshotRef   `json:"snapshot"`
	Applied       bool          `json:"applied"`
	Diff          string        `json:"diff"`
	AffectedFiles []string      `json:"affected_files"`
	Risks         []RiskArea    `json:"risks"`
	Uncertainties []Uncertainty `json:"uncertainties"`
}
