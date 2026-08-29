// Package verification defines and assembles the portable change-verification
// report shared by the CLI, GitHub Action, and MCP adapters.
package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	// SchemaVersion identifies the frozen unified verification contract.
	SchemaVersion = "agentic.verify/v1"
	// ArchivedBetaSchemaVersion identifies reports emitted before the v1 freeze.
	ArchivedBetaSchemaVersion = "agentic.verify/v1beta1"
)

const (
	maxVisibleChangedFiles         = 15
	maxVisibleChangedRanges        = 5
	maxVisibleChangedDeclarations  = 20
	maxVisibleImpactedPackages     = 20
	maxVisibleCheckTargets         = 20
	maxVisibleTestPackages         = 20
	maxVisibleNonpassingTests      = 20
	maxVisibleFindings             = 50
	maxVisibleCoverageRanges       = 20
	maxVisibleDiagnostics          = 50
	maxVisibleContractViolations   = 50
	maxVisibleRiskLocations        = 5
	maxVisibleUncertaintyLocations = 5
)

// Severity is the portable importance of a finding.
type Severity string

// Finding severities are ordered from advisory information to blocking errors.
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// ResultStatus is the automation state of a completed report.
type ResultStatus string

// Report result states distinguish complete evidence from findings and gaps.
const (
	ResultPass       ResultStatus = "pass"
	ResultFindings   ResultStatus = "findings"
	ResultIncomplete ResultStatus = "incomplete"
)

// EvidenceStatus records whether a planned check completed and what it found.
type EvidenceStatus string

// Evidence states distinguish executed outcomes from unavailable checks.
const (
	EvidencePassed  EvidenceStatus = "passed"
	EvidenceFailed  EvidenceStatus = "failed"
	EvidenceSkipped EvidenceStatus = "skipped"
	EvidenceError   EvidenceStatus = "error"
)

// CheckKind identifies a semantic verification check independently of an
// adapter or command name.
type CheckKind string

// Check kinds identify the language-native evidence shipped in v0.2.
const (
	CheckTests       CheckKind = "go.test"
	CheckCoverage    CheckKind = "go.coverage"
	CheckRace        CheckKind = "go.race"
	CheckConcurrency CheckKind = "go.analysis.concurrency"
	CheckErrors      CheckKind = "go.analysis.errors"
	CheckDiagnostics CheckKind = "go.diagnostics"
	CheckContract    CheckKind = "go.contract"
)

// BaselineState describes how an analyzer diagnostic compares with the base.
type BaselineState string

// Baseline states classify diagnostics relative to the merge-base snapshot.
const (
	BaselineIntroduced BaselineState = "introduced"
	BaselineExisting   BaselineState = "existing"
	BaselineResolved   BaselineState = "resolved"
	BaselineUnknown    BaselineState = "unknown"
)

// ChangeKind describes a final-snapshot file or declaration change.
type ChangeKind string

// Change kinds describe final-worktree paths and declarations.
const (
	ChangeAdded     ChangeKind = "added"
	ChangeModified  ChangeKind = "modified"
	ChangeDeleted   ChangeKind = "deleted"
	ChangeRenamed   ChangeKind = "renamed"
	ChangeUntracked ChangeKind = "untracked"
)

// Location is a workspace-relative source position.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col,omitempty"`
}

// LineRange is an inclusive source-line range.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Provider identifies the implementation that produced a report.
type Provider struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ProviderCapability records one effective implementation and its normalized
// portable capabilities.
type ProviderCapability struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// SnapshotTransition records one exact immutable checkpoint edge.
type SnapshotTransition struct {
	CheckpointID string `json:"checkpoint_id"`
	PreviousID   string `json:"previous_id"`
	CurrentID    string `json:"current_id"`
}

// SnapshotLineage binds verification evidence to the semantic snapshot and
// optional Change Contract lineage observed by the intelligence service.
type SnapshotLineage struct {
	CurrentID       string               `json:"current_id"`
	ExpectedID      string               `json:"expected_id,omitempty"`
	ContractInitial string               `json:"contract_initial,omitempty"`
	ContractLatest  string               `json:"contract_latest,omitempty"`
	Transitions     []SnapshotTransition `json:"transitions"`
}

// ProvenanceReference records a source-grounded context or deterministic
// refactor operation that preceded verification in this service process.
type ProvenanceReference struct {
	Kind             string `json:"kind"`
	Operation        string `json:"operation"`
	Reference        string `json:"reference,omitempty"`
	InputSnapshotID  string `json:"input_snapshot_id"`
	OutputSnapshotID string `json:"output_snapshot_id"`
	Applied          bool   `json:"applied,omitempty"`
}

// Provenance retains bounded context and refactor lineage without source
// contents, prompts, goals, or absolute paths.
type Provenance struct {
	Context   []ProvenanceReference `json:"context"`
	Refactors []ProvenanceReference `json:"refactors"`
}

// Repository identifies the compared repository state without absolute paths.
type Repository struct {
	RequestedBase   string `json:"requested_base"`
	BaseCommit      string `json:"base_commit"`
	MergeBaseCommit string `json:"merge_base_commit"`
	HeadCommit      string `json:"head_commit"`
	SnapshotID      string `json:"snapshot_id"`
	Workspace       string `json:"workspace"`
	Dirty           bool   `json:"dirty"`
}

// ChangedFile is one path in the final change snapshot.
//
//nolint:govet // Preserve the canonical JSON field order.
type ChangedFile struct {
	Path                   string      `json:"path"`
	PreviousPath           string      `json:"previous_path,omitempty"`
	Change                 ChangeKind  `json:"change"`
	BaseRanges             []LineRange `json:"base_ranges"`
	BaseRangesTotal        int         `json:"base_ranges_total"`
	BaseRangesTruncated    bool        `json:"base_ranges_truncated"`
	CurrentRanges          []LineRange `json:"current_ranges"`
	CurrentRangesTotal     int         `json:"current_ranges_total"`
	CurrentRangesTruncated bool        `json:"current_ranges_truncated"`
}

// ChangedDeclaration is one source declaration intersecting a changed range.
//
//nolint:govet // Preserve the canonical JSON field order.
type ChangedDeclaration struct {
	Kind            string     `json:"kind"`
	Package         string     `json:"package"`
	Name            string     `json:"name"`
	Change          ChangeKind `json:"change"`
	BaseLocation    *Location  `json:"base_location,omitempty"`
	CurrentLocation *Location  `json:"current_location,omitempty"`
}

// Change contains the source facts in a change snapshot.
//
//nolint:govet // Preserve the canonical JSON field order.
type Change struct {
	Files                 []ChangedFile        `json:"files"`
	FilesTotal            int                  `json:"files_total"`
	FilesTruncated        bool                 `json:"files_truncated"`
	Declarations          []ChangedDeclaration `json:"declarations"`
	DeclarationsTotal     int                  `json:"declarations_total"`
	DeclarationsTruncated bool                 `json:"declarations_truncated"`
}

// ImpactedPackage is one directly changed or reverse-dependent Go package.
//
//nolint:govet // Preserve the canonical JSON field order.
type ImpactedPackage struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Distance int      `json:"distance"`
	Reasons  []string `json:"reasons"`
}

// Impact contains the conservative package closure for a change.
//
//nolint:govet // Preserve the canonical JSON field order.
type Impact struct {
	Packages          []ImpactedPackage `json:"packages"`
	PackagesTotal     int               `json:"packages_total"`
	PackagesTruncated bool              `json:"packages_truncated"`
}

// Check is one semantic operation in a verification plan.
//
//nolint:govet // Preserve the canonical JSON field order.
type Check struct {
	ID               string    `json:"id"`
	Kind             CheckKind `json:"kind"`
	Required         bool      `json:"required"`
	Targets          []string  `json:"targets"`
	TargetsTotal     int       `json:"targets_total"`
	TargetsTruncated bool      `json:"targets_truncated"`
	Reason           string    `json:"reason"`
}

// TestPackageSummary aggregates terminal test outcomes for one package.
//
//nolint:govet // Preserve the canonical JSON field order.
type TestPackageSummary struct {
	Package string `json:"package"`
	Status  string `json:"status"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	Output  string `json:"output,omitempty"`
}

// TestCaseSummary is a retained failed or skipped test case.
//
//nolint:govet // Preserve the canonical JSON field order.
type TestCaseSummary struct {
	Package  string  `json:"package"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	ElapsedS float64 `json:"elapsed_s"`
	Output   string  `json:"output,omitempty"`
}

// TestSummary contains bounded test evidence without passing test records.
//
//nolint:govet // Preserve the canonical JSON field order.
type TestSummary struct {
	Passed              int                  `json:"passed"`
	Failed              int                  `json:"failed"`
	Skipped             int                  `json:"skipped"`
	Packages            []TestPackageSummary `json:"packages"`
	PackagesTotal       int                  `json:"packages_total"`
	PackagesTruncated   bool                 `json:"packages_truncated"`
	Nonpassing          []TestCaseSummary    `json:"nonpassing"`
	NonpassingTotal     int                  `json:"nonpassing_total"`
	NonpassingTruncated bool                 `json:"nonpassing_truncated"`
}

// SourceRange is a workspace-relative source range with a statement count.
type SourceRange struct {
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	StartCol   int    `json:"start_col,omitempty"`
	EndLine    int    `json:"end_line"`
	EndCol     int    `json:"end_col,omitempty"`
	Statements int    `json:"statements"`
}

// CoverageSummary contains statement-weighted coverage of changed code.
//
//nolint:govet // Preserve the canonical JSON field order.
type CoverageSummary struct {
	TotalStatements    int           `json:"total_statements"`
	CoveredStatements  int           `json:"covered_statements"`
	Percent            float64       `json:"percent"`
	Uncovered          []SourceRange `json:"uncovered"`
	UncoveredTotal     int           `json:"uncovered_total"`
	UncoveredTruncated bool          `json:"uncovered_truncated"`
}

// AnalysisSummary contains base/current analyzer comparison counts.
type AnalysisSummary struct {
	Base       int `json:"base"`
	Current    int `json:"current"`
	Introduced int `json:"introduced"`
	Existing   int `json:"existing"`
	Resolved   int `json:"resolved"`
	Unknown    int `json:"unknown"`
}

// RaceSummary contains bounded race-detector evidence.
type RaceSummary struct {
	Conflicts int `json:"conflicts"`
}

// Diagnostic records one normalized compiler or semantic-provider
// observation at a workspace-relative location.
type Diagnostic struct {
	Source   string   `json:"source"`
	Code     string   `json:"code,omitempty"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
}

// DiagnosticSummary contains bounded current-snapshot semantic evidence.
// Diagnostics are evidence only until a base comparison can classify them.
type DiagnosticSummary struct {
	Items     []Diagnostic `json:"items"`
	Total     int          `json:"total"`
	Errors    int          `json:"errors"`
	Warnings  int          `json:"warnings"`
	Truncated bool         `json:"truncated"`
}

// ContractViolation is one normalized machine-checkable contract deviation.
type ContractViolation struct {
	Code               string     `json:"code"`
	Policy             string     `json:"policy"`
	Message            string     `json:"message"`
	Locations          []Location `json:"locations"`
	LocationsTotal     int        `json:"locations_total"`
	LocationsTruncated bool       `json:"locations_truncated"`
}

// ContractSummary records optional Change Contract compliance evidence.
type ContractSummary struct {
	ContractID          string              `json:"contract_id"`
	Violations          []ContractViolation `json:"violations"`
	ViolationsTotal     int                 `json:"violations_total"`
	ViolationsTruncated bool                `json:"violations_truncated"`
	Forbidden           int                 `json:"forbidden"`
	Warnings            int                 `json:"warnings"`
}

// Evidence records the outcome of one planned check.
//
//nolint:govet // Preserve the canonical JSON field order.
type Evidence struct {
	CheckID     string             `json:"check_id"`
	Kind        CheckKind          `json:"kind"`
	Status      EvidenceStatus     `json:"status"`
	DurationMS  int64              `json:"duration_ms"`
	Summary     string             `json:"summary"`
	Error       string             `json:"error,omitempty"`
	Tests       *TestSummary       `json:"tests,omitempty"`
	Coverage    *CoverageSummary   `json:"coverage,omitempty"`
	Analysis    *AnalysisSummary   `json:"analysis,omitempty"`
	Race        *RaceSummary       `json:"race,omitempty"`
	Diagnostics *DiagnosticSummary `json:"diagnostics,omitempty"`
	Contract    *ContractSummary   `json:"contract,omitempty"`
}

// Finding is an observed issue produced by executed evidence.
type Finding struct {
	Kind       string        `json:"kind"`
	Rule       string        `json:"rule,omitempty"`
	Severity   Severity      `json:"severity"`
	Message    string        `json:"message"`
	Suggestion string        `json:"suggestion,omitempty"`
	Location   *Location     `json:"location,omitempty"`
	CheckID    string        `json:"check_id,omitempty"`
	Baseline   BaselineState `json:"baseline,omitempty"`
}

// RiskArea is a change-grounded reason for focused review or another check.
//
//nolint:govet // Preserve the canonical JSON field order.
type RiskArea struct {
	Code               string     `json:"code"`
	Reason             string     `json:"reason"`
	Guidance           string     `json:"guidance"`
	Locations          []Location `json:"locations"`
	LocationsTotal     int        `json:"locations_total"`
	LocationsTruncated bool       `json:"locations_truncated"`
}

// Uncertainty is a known limit on a report's conclusion.
//
//nolint:govet // Preserve the canonical JSON field order.
type Uncertainty struct {
	Code               string     `json:"code"`
	Message            string     `json:"message"`
	CheckID            string     `json:"check_id,omitempty"`
	Locations          []Location `json:"locations"`
	LocationsTotal     int        `json:"locations_total"`
	LocationsTruncated bool       `json:"locations_truncated"`
}

// PolicyResult is the report's automation result, not a safety verdict.
//
//nolint:govet // Preserve the canonical JSON field order.
type PolicyResult struct {
	Status           ResultStatus `json:"status"`
	ExitCode         int          `json:"exit_code"`
	BlockingFindings int          `json:"blocking_findings"`
	IncompleteChecks int          `json:"incomplete_checks"`
	Summary          string       `json:"summary"`
}

// Report is the portable result shared by every delivery adapter.
//
//nolint:govet // Preserve the canonical JSON field order.
type Report struct {
	SchemaVersion     string               `json:"schema_version"`
	ID                string               `json:"id"`
	Provider          Provider             `json:"provider"`
	Providers         []ProviderCapability `json:"providers"`
	Repository        Repository           `json:"repository"`
	Snapshot          SnapshotLineage      `json:"snapshot"`
	Provenance        Provenance           `json:"provenance"`
	Change            Change               `json:"change"`
	Impact            Impact               `json:"impact"`
	Plan              []Check              `json:"plan"`
	Evidence          []Evidence           `json:"evidence"`
	Findings          []Finding            `json:"findings"`
	FindingsTotal     int                  `json:"findings_total"`
	FindingsTruncated bool                 `json:"findings_truncated"`
	Risks             []RiskArea           `json:"risks"`
	Uncertainties     []Uncertainty        `json:"uncertainties"`
	Result            PolicyResult         `json:"result"`
}

// FailOn controls which introduced analyzer severities block policy.
type FailOn string

// Fail-on thresholds control which introduced severities block policy.
const (
	FailOnError   FailOn = "error"
	FailOnWarning FailOn = "warning"
	FailOnInfo    FailOn = "info"
	FailOnNone    FailOn = "none"
)

// Policy controls report evaluation without changing collected evidence.
type Policy struct {
	MinChangedCoverage *float64
	FailOn             FailOn
}

// NewReport initializes the canonical schema and every collection.
func NewReport(providerVersion string, repository Repository) Report {
	return Report{
		SchemaVersion: SchemaVersion,
		Provider:      Provider{Name: "agentic-go", Version: providerVersion},
		Providers:     make([]ProviderCapability, 0),
		Repository:    repository,
		Snapshot:      SnapshotLineage{Transitions: make([]SnapshotTransition, 0)},
		Provenance: Provenance{
			Context: make([]ProvenanceReference, 0), Refactors: make([]ProvenanceReference, 0),
		},
		Change: Change{
			Files:        make([]ChangedFile, 0),
			Declarations: make([]ChangedDeclaration, 0),
		},
		Impact:        Impact{Packages: make([]ImpactedPackage, 0)},
		Plan:          make([]Check, 0),
		Evidence:      make([]Evidence, 0),
		Findings:      make([]Finding, 0),
		Risks:         make([]RiskArea, 0),
		Uncertainties: make([]Uncertainty, 0),
		Result:        PolicyResult{Status: ResultPass, Summary: "requested verification completed without blocking findings"},
	}
}

// Finalize normalizes ordering and applies policy to a complete report.
func (r *Report) Finalize(policy Policy) error {
	if r == nil {
		return fmt.Errorf("report is nil")
	}
	if err := policy.normalize(); err != nil {
		return err
	}
	r.initializeCollections()
	r.aggregateLocalizedFacts()
	r.sortCollections()

	evidence := make(map[string]Evidence, len(r.Evidence))
	for _, item := range r.Evidence {
		evidence[item.CheckID] = item
	}
	incomplete := 0
	for _, check := range r.Plan {
		if !check.Required {
			continue
		}
		item, ok := evidence[check.ID]
		if !ok || item.Status == EvidenceError {
			incomplete++
		}
	}

	blocking := 0
	for _, item := range r.Findings {
		if alwaysBlocks(item.Kind) || policy.blocks(item) {
			blocking++
		}
	}

	r.Result.IncompleteChecks = incomplete
	r.Result.BlockingFindings = blocking
	switch {
	case incomplete > 0:
		r.Result.Status = ResultIncomplete
		r.Result.ExitCode = 2
		r.Result.Summary = fmt.Sprintf("verification incomplete: %d required checks unavailable", incomplete)
	case blocking > 0:
		r.Result.Status = ResultFindings
		r.Result.ExitCode = 1
		r.Result.Summary = fmt.Sprintf("verification completed with %d blocking findings", blocking)
	default:
		r.Result.Status = ResultPass
		r.Result.ExitCode = 0
		r.Result.Summary = "requested verification completed without blocking findings"
	}
	r.truncateDetails()
	if err := r.assignID(); err != nil {
		return err
	}
	return nil
}

func (r *Report) assignID() error {
	r.ID = ""
	encoded, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encoding verification identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	r.ID = "verify_" + hex.EncodeToString(digest[:])
	return nil
}

// ValidateID verifies that a report carries the exact content address assigned
// by Finalize without mutating the caller's value.
func (r Report) ValidateID() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("verification report identity is invalid")
	}
	return r.validateContentID()
}

// ValidateStoredID accepts the current schema and the archived beta schema
// while still verifying the exact content address.
func (r Report) ValidateStoredID() error {
	if r.SchemaVersion != SchemaVersion && r.SchemaVersion != ArchivedBetaSchemaVersion {
		return fmt.Errorf("verification report identity is invalid")
	}
	return r.validateContentID()
}

func (r Report) validateContentID() error {
	if !strings.HasPrefix(r.ID, "verify_") || len(r.ID) != len("verify_")+sha256.Size*2 {
		return fmt.Errorf("verification report identity is invalid")
	}
	want := r.ID
	if err := r.assignID(); err != nil {
		return err
	}
	if r.ID != want {
		return fmt.Errorf("verification report identity does not match its content")
	}
	return nil
}

func (p *Policy) normalize() error {
	if p.FailOn == "" {
		p.FailOn = FailOnError
	}
	switch p.FailOn {
	case FailOnError, FailOnWarning, FailOnInfo, FailOnNone:
	default:
		return fmt.Errorf("invalid fail-on threshold %q", p.FailOn)
	}
	if p.MinChangedCoverage != nil && (*p.MinChangedCoverage < 0 || *p.MinChangedCoverage > 100) {
		return fmt.Errorf("minimum changed coverage must be between 0 and 100")
	}
	return nil
}

func (p Policy) blocks(item Finding) bool {
	if item.Kind == string(CheckContract) {
		return item.Severity == SeverityError
	}
	if item.Baseline != "" && item.Baseline != BaselineIntroduced {
		return false
	}
	threshold := severityRank(Severity(p.FailOn))
	return p.FailOn != FailOnNone && severityRank(item.Severity) >= threshold
}

func alwaysBlocks(kind string) bool {
	return kind == "test.failure" || kind == "go.race" || kind == "coverage.policy"
}

func severityRank(value Severity) int {
	switch value {
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

func (r *Report) initializeCollections() {
	if r.Providers == nil {
		r.Providers = make([]ProviderCapability, 0)
	}
	for index := range r.Providers {
		if r.Providers[index].Capabilities == nil {
			r.Providers[index].Capabilities = make([]string, 0)
		}
	}
	if r.Snapshot.Transitions == nil {
		r.Snapshot.Transitions = make([]SnapshotTransition, 0)
	}
	if r.Provenance.Context == nil {
		r.Provenance.Context = make([]ProvenanceReference, 0)
	}
	if r.Provenance.Refactors == nil {
		r.Provenance.Refactors = make([]ProvenanceReference, 0)
	}
	if r.Change.Files == nil {
		r.Change.Files = make([]ChangedFile, 0)
	}
	if r.Change.Declarations == nil {
		r.Change.Declarations = make([]ChangedDeclaration, 0)
	}
	if r.Impact.Packages == nil {
		r.Impact.Packages = make([]ImpactedPackage, 0)
	}
	if r.Plan == nil {
		r.Plan = make([]Check, 0)
	}
	if r.Evidence == nil {
		r.Evidence = make([]Evidence, 0)
	}
	if r.Findings == nil {
		r.Findings = make([]Finding, 0)
	}
	if r.Risks == nil {
		r.Risks = make([]RiskArea, 0)
	}
	if r.Uncertainties == nil {
		r.Uncertainties = make([]Uncertainty, 0)
	}
	for index := range r.Change.Files {
		if r.Change.Files[index].BaseRanges == nil {
			r.Change.Files[index].BaseRanges = make([]LineRange, 0)
		}
		if r.Change.Files[index].CurrentRanges == nil {
			r.Change.Files[index].CurrentRanges = make([]LineRange, 0)
		}
	}
	for index := range r.Impact.Packages {
		if r.Impact.Packages[index].Reasons == nil {
			r.Impact.Packages[index].Reasons = make([]string, 0)
		}
	}
	for index := range r.Plan {
		if r.Plan[index].Targets == nil {
			r.Plan[index].Targets = make([]string, 0)
		}
	}
	for index := range r.Evidence {
		if diagnostics := r.Evidence[index].Diagnostics; diagnostics != nil && diagnostics.Items == nil {
			diagnostics.Items = make([]Diagnostic, 0)
		}
		if contract := r.Evidence[index].Contract; contract != nil {
			if contract.Violations == nil {
				contract.Violations = make([]ContractViolation, 0)
			}
			for violationIndex := range contract.Violations {
				if contract.Violations[violationIndex].Locations == nil {
					contract.Violations[violationIndex].Locations = make([]Location, 0)
				}
			}
		}
		if tests := r.Evidence[index].Tests; tests != nil {
			if tests.Packages == nil {
				tests.Packages = make([]TestPackageSummary, 0)
			}
			if tests.Nonpassing == nil {
				tests.Nonpassing = make([]TestCaseSummary, 0)
			}
		}
		if coverage := r.Evidence[index].Coverage; coverage != nil && coverage.Uncovered == nil {
			coverage.Uncovered = make([]SourceRange, 0)
		}
	}
	for index := range r.Risks {
		if r.Risks[index].Locations == nil {
			r.Risks[index].Locations = make([]Location, 0)
		}
	}
	for index := range r.Uncertainties {
		if r.Uncertainties[index].Locations == nil {
			r.Uncertainties[index].Locations = make([]Location, 0)
		}
	}
}

func (r *Report) sortCollections() {
	sort.Slice(r.Providers, func(i, j int) bool {
		return r.Providers[i].Name+"\x00"+r.Providers[i].Version < r.Providers[j].Name+"\x00"+r.Providers[j].Version
	})
	for index := range r.Providers {
		sort.Strings(r.Providers[index].Capabilities)
		r.Providers[index].Capabilities = dedupeSortedStrings(r.Providers[index].Capabilities)
	}
	sort.Slice(r.Snapshot.Transitions, func(i, j int) bool {
		return snapshotTransitionSortKey(r.Snapshot.Transitions[i]) < snapshotTransitionSortKey(r.Snapshot.Transitions[j])
	})
	sort.Slice(r.Provenance.Context, func(i, j int) bool {
		return provenanceSortKey(r.Provenance.Context[i]) < provenanceSortKey(r.Provenance.Context[j])
	})
	sort.Slice(r.Provenance.Refactors, func(i, j int) bool {
		return provenanceSortKey(r.Provenance.Refactors[i]) < provenanceSortKey(r.Provenance.Refactors[j])
	})
	sort.Slice(r.Change.Files, func(i, j int) bool { return r.Change.Files[i].Path < r.Change.Files[j].Path })
	for index := range r.Change.Files {
		sort.Slice(r.Change.Files[index].BaseRanges, func(i, j int) bool {
			return lineRangeSortKey(r.Change.Files[index].BaseRanges[i]) < lineRangeSortKey(r.Change.Files[index].BaseRanges[j])
		})
		sort.Slice(r.Change.Files[index].CurrentRanges, func(i, j int) bool {
			return lineRangeSortKey(r.Change.Files[index].CurrentRanges[i]) < lineRangeSortKey(r.Change.Files[index].CurrentRanges[j])
		})
	}
	sort.Slice(r.Change.Declarations, func(i, j int) bool {
		left := r.Change.Declarations[i].Package + "\x00" + r.Change.Declarations[i].Name
		right := r.Change.Declarations[j].Package + "\x00" + r.Change.Declarations[j].Name
		return left < right
	})
	sort.Slice(r.Impact.Packages, func(i, j int) bool {
		if r.Impact.Packages[i].Distance != r.Impact.Packages[j].Distance {
			return r.Impact.Packages[i].Distance < r.Impact.Packages[j].Distance
		}
		return r.Impact.Packages[i].ID < r.Impact.Packages[j].ID
	})
	for index := range r.Impact.Packages {
		sort.Strings(r.Impact.Packages[index].Reasons)
	}
	sort.Slice(r.Plan, func(i, j int) bool { return r.Plan[i].ID < r.Plan[j].ID })
	for index := range r.Plan {
		sort.Strings(r.Plan[index].Targets)
	}
	sort.Slice(r.Evidence, func(i, j int) bool { return r.Evidence[i].CheckID < r.Evidence[j].CheckID })
	for index := range r.Evidence {
		if diagnostics := r.Evidence[index].Diagnostics; diagnostics != nil {
			sort.Slice(diagnostics.Items, func(i, j int) bool {
				return diagnosticSortKey(diagnostics.Items[i]) < diagnosticSortKey(diagnostics.Items[j])
			})
		}
		if contract := r.Evidence[index].Contract; contract != nil {
			for violationIndex := range contract.Violations {
				contract.Violations[violationIndex].Locations = sortAndDeduplicateLocations(contract.Violations[violationIndex].Locations)
			}
			sort.Slice(contract.Violations, func(i, j int) bool {
				return contractViolationSortKey(contract.Violations[i]) < contractViolationSortKey(contract.Violations[j])
			})
		}
		if tests := r.Evidence[index].Tests; tests != nil {
			sort.Slice(tests.Packages, func(i, j int) bool { return tests.Packages[i].Package < tests.Packages[j].Package })
			sort.Slice(tests.Nonpassing, func(i, j int) bool {
				left := tests.Nonpassing[i].Package + "\x00" + tests.Nonpassing[i].Name
				right := tests.Nonpassing[j].Package + "\x00" + tests.Nonpassing[j].Name
				return left < right
			})
		}
		if coverage := r.Evidence[index].Coverage; coverage != nil {
			sort.Slice(coverage.Uncovered, func(i, j int) bool {
				return sourceRangeSortKey(coverage.Uncovered[i]) < sourceRangeSortKey(coverage.Uncovered[j])
			})
		}
	}
	sort.Slice(r.Findings, func(i, j int) bool {
		left := findingSortKey(r.Findings[i])
		right := findingSortKey(r.Findings[j])
		return left < right
	})
	for index := range r.Risks {
		r.Risks[index].Locations = sortAndDeduplicateLocations(r.Risks[index].Locations)
	}
	sort.Slice(r.Risks, func(i, j int) bool { return riskSortKey(r.Risks[i]) < riskSortKey(r.Risks[j]) })
	for index := range r.Uncertainties {
		r.Uncertainties[index].Locations = sortAndDeduplicateLocations(r.Uncertainties[index].Locations)
	}
	sort.Slice(r.Uncertainties, func(i, j int) bool {
		return uncertaintySortKey(r.Uncertainties[i]) < uncertaintySortKey(r.Uncertainties[j])
	})
}

func (r *Report) aggregateLocalizedFacts() {
	risks := make([]RiskArea, 0, len(r.Risks))
	riskIndexes := make(map[string]int, len(r.Risks))
	for _, item := range r.Risks {
		key := strings.Join([]string{item.Code, item.Reason, item.Guidance}, "\x00")
		index, exists := riskIndexes[key]
		if !exists {
			item.Locations = append(make([]Location, 0, len(item.Locations)), item.Locations...)
			riskIndexes[key] = len(risks)
			risks = append(risks, item)
			continue
		}
		risks[index].Locations = append(risks[index].Locations, item.Locations...)
	}
	r.Risks = risks

	uncertainties := make([]Uncertainty, 0, len(r.Uncertainties))
	uncertaintyIndexes := make(map[string]int, len(r.Uncertainties))
	for _, item := range r.Uncertainties {
		key := strings.Join([]string{item.Code, item.CheckID, item.Message}, "\x00")
		index, exists := uncertaintyIndexes[key]
		if !exists {
			item.Locations = append(make([]Location, 0, len(item.Locations)), item.Locations...)
			uncertaintyIndexes[key] = len(uncertainties)
			uncertainties = append(uncertainties, item)
			continue
		}
		uncertainties[index].Locations = append(uncertainties[index].Locations, item.Locations...)
	}
	r.Uncertainties = uncertainties
}

func (r *Report) truncateDetails() {
	for index := range r.Change.Files {
		file := &r.Change.Files[index]
		file.BaseRanges, file.BaseRangesTotal, file.BaseRangesTruncated = boundDetails(file.BaseRanges, maxVisibleChangedRanges)
		file.CurrentRanges, file.CurrentRangesTotal, file.CurrentRangesTruncated = boundDetails(file.CurrentRanges, maxVisibleChangedRanges)
	}
	r.Change.Files, r.Change.FilesTotal, r.Change.FilesTruncated = boundDetails(r.Change.Files, maxVisibleChangedFiles)
	r.Change.Declarations, r.Change.DeclarationsTotal, r.Change.DeclarationsTruncated = boundDetails(r.Change.Declarations, maxVisibleChangedDeclarations)
	r.Impact.Packages, r.Impact.PackagesTotal, r.Impact.PackagesTruncated = boundDetails(r.Impact.Packages, maxVisibleImpactedPackages)

	for index := range r.Plan {
		check := &r.Plan[index]
		check.Targets, check.TargetsTotal, check.TargetsTruncated = boundDetails(check.Targets, maxVisibleCheckTargets)
	}
	for index := range r.Evidence {
		if diagnostics := r.Evidence[index].Diagnostics; diagnostics != nil {
			diagnostics.Items, diagnostics.Total, diagnostics.Truncated = boundDetails(diagnostics.Items, maxVisibleDiagnostics)
		}
		if contract := r.Evidence[index].Contract; contract != nil {
			for violationIndex := range contract.Violations {
				violation := &contract.Violations[violationIndex]
				violation.Locations, violation.LocationsTotal, violation.LocationsTruncated = boundDetails(violation.Locations, maxVisibleUncertaintyLocations)
			}
			contract.Violations, contract.ViolationsTotal, contract.ViolationsTruncated = boundDetails(contract.Violations, maxVisibleContractViolations)
		}
		if tests := r.Evidence[index].Tests; tests != nil {
			tests.Packages, tests.PackagesTotal, tests.PackagesTruncated = boundDetails(tests.Packages, maxVisibleTestPackages)
			tests.Nonpassing, tests.NonpassingTotal, tests.NonpassingTruncated = boundDetails(tests.Nonpassing, maxVisibleNonpassingTests)
		}
		if coverage := r.Evidence[index].Coverage; coverage != nil {
			coverage.Uncovered, coverage.UncoveredTotal, coverage.UncoveredTruncated = boundDetails(coverage.Uncovered, maxVisibleCoverageRanges)
		}
	}
	r.Findings, r.FindingsTotal, r.FindingsTruncated = boundDetails(r.Findings, maxVisibleFindings)
	for index := range r.Risks {
		risk := &r.Risks[index]
		risk.Locations, risk.LocationsTotal, risk.LocationsTruncated = boundDetails(risk.Locations, maxVisibleRiskLocations)
	}
	for index := range r.Uncertainties {
		uncertainty := &r.Uncertainties[index]
		uncertainty.Locations, uncertainty.LocationsTotal, uncertainty.LocationsTruncated = boundDetails(uncertainty.Locations, maxVisibleUncertaintyLocations)
	}
}

func snapshotTransitionSortKey(item SnapshotTransition) string {
	return strings.Join([]string{item.CheckpointID, item.PreviousID, item.CurrentID}, "\x00")
}

func provenanceSortKey(item ProvenanceReference) string {
	return strings.Join([]string{item.Kind, item.Operation, item.Reference, item.InputSnapshotID, item.OutputSnapshotID, fmt.Sprint(item.Applied)}, "\x00")
}

func diagnosticSortKey(item Diagnostic) string {
	return strings.Join([]string{item.Location.File, fmt.Sprintf("%010d", item.Location.Line), fmt.Sprintf("%010d", item.Location.Col), item.Severity, item.Source, item.Code, item.Message}, "\x00")
}

func contractViolationSortKey(item ContractViolation) string {
	location := ""
	if len(item.Locations) > 0 {
		location = locationSortKey(item.Locations[0])
	}
	return strings.Join([]string{item.Code, item.Policy, item.Message, location}, "\x00")
}

func dedupeSortedStrings(items []string) []string {
	result := items[:0]
	for _, item := range items {
		if len(result) == 0 || result[len(result)-1] != item {
			result = append(result, item)
		}
	}
	return result
}

func boundDetails[T any](items []T, limit int) ([]T, int, bool) {
	total := len(items)
	if total <= limit {
		return items, total, false
	}
	visible := make([]T, limit)
	copy(visible, items[:limit])
	return visible, total, true
}

func sortAndDeduplicateLocations(locations []Location) []Location {
	sort.Slice(locations, func(i, j int) bool { return locationSortKey(locations[i]) < locationSortKey(locations[j]) })
	if len(locations) < 2 {
		return locations
	}
	result := locations[:1]
	for _, item := range locations[1:] {
		if item != result[len(result)-1] {
			result = append(result, item)
		}
	}
	return result
}

func findingSortKey(item Finding) string {
	location := ""
	if item.Location != nil {
		location = fmt.Sprintf("%s:%09d:%09d", item.Location.File, item.Location.Line, item.Location.Col)
	}
	return strings.Join([]string{location, item.Kind, item.Rule, item.Message}, "\x00")
}

func riskSortKey(item RiskArea) string {
	location := ""
	if len(item.Locations) > 0 {
		location = fmt.Sprintf("%s:%09d:%09d", item.Locations[0].File, item.Locations[0].Line, item.Locations[0].Col)
	}
	return item.Code + "\x00" + location
}

func uncertaintySortKey(item Uncertainty) string {
	location := ""
	if len(item.Locations) > 0 {
		location = fmt.Sprintf("%s:%09d:%09d", item.Locations[0].File, item.Locations[0].Line, item.Locations[0].Col)
	}
	return item.Code + "\x00" + location + "\x00" + item.Message
}

func sourceRangeSortKey(item SourceRange) string {
	return fmt.Sprintf("%s:%09d:%09d:%09d:%09d", item.File, item.StartLine, item.StartCol, item.EndLine, item.EndCol)
}

func lineRangeSortKey(item LineRange) string {
	return fmt.Sprintf("%09d:%09d", item.Start, item.End)
}

func locationSortKey(item Location) string {
	return fmt.Sprintf("%s:%09d:%09d", item.File, item.Line, item.Col)
}
