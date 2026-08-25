// Package verification defines and assembles the portable change-verification
// report shared by the CLI, GitHub Action, and MCP adapters.
package verification

import (
	"fmt"
	"sort"
	"strings"
)

// SchemaVersion identifies the portable v0.2 report contract.
const SchemaVersion = "agentic.verify/v1alpha1"

// Severity is the portable importance of a finding.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// ResultStatus is the automation state of a completed report.
type ResultStatus string

const (
	ResultPass       ResultStatus = "pass"
	ResultFindings   ResultStatus = "findings"
	ResultIncomplete ResultStatus = "incomplete"
)

// EvidenceStatus records whether a planned check completed and what it found.
type EvidenceStatus string

const (
	EvidencePassed  EvidenceStatus = "passed"
	EvidenceFailed  EvidenceStatus = "failed"
	EvidenceSkipped EvidenceStatus = "skipped"
	EvidenceError   EvidenceStatus = "error"
)

// CheckKind identifies a semantic verification check independently of an
// adapter or command name.
type CheckKind string

const (
	CheckTests       CheckKind = "go.test"
	CheckCoverage    CheckKind = "go.coverage"
	CheckRace        CheckKind = "go.race"
	CheckConcurrency CheckKind = "go.analysis.concurrency"
	CheckErrors      CheckKind = "go.analysis.errors"
)

// BaselineState describes how an analyzer diagnostic compares with the base.
type BaselineState string

const (
	BaselineIntroduced BaselineState = "introduced"
	BaselineExisting   BaselineState = "existing"
	BaselineResolved   BaselineState = "resolved"
	BaselineUnknown    BaselineState = "unknown"
)

// ChangeKind describes a final-snapshot file or declaration change.
type ChangeKind string

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
type ChangedFile struct {
	Path          string      `json:"path"`
	PreviousPath  string      `json:"previous_path,omitempty"`
	Change        ChangeKind  `json:"change"`
	BaseRanges    []LineRange `json:"base_ranges"`
	CurrentRanges []LineRange `json:"current_ranges"`
}

// ChangedDeclaration is one source declaration intersecting a changed range.
type ChangedDeclaration struct {
	Kind            string     `json:"kind"`
	Package         string     `json:"package"`
	Name            string     `json:"name"`
	Change          ChangeKind `json:"change"`
	BaseLocation    *Location  `json:"base_location,omitempty"`
	CurrentLocation *Location  `json:"current_location,omitempty"`
}

// Change contains the source facts in a change snapshot.
type Change struct {
	Files        []ChangedFile        `json:"files"`
	Declarations []ChangedDeclaration `json:"declarations"`
}

// ImpactedPackage is one directly changed or reverse-dependent Go package.
type ImpactedPackage struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Distance int      `json:"distance"`
	Reasons  []string `json:"reasons"`
}

// Impact contains the conservative package closure for a change.
type Impact struct {
	Packages []ImpactedPackage `json:"packages"`
}

// Check is one semantic operation in a verification plan.
type Check struct {
	ID       string    `json:"id"`
	Kind     CheckKind `json:"kind"`
	Required bool      `json:"required"`
	Targets  []string  `json:"targets"`
	Reason   string    `json:"reason"`
}

// TestPackageSummary aggregates terminal test outcomes for one package.
type TestPackageSummary struct {
	Package string `json:"package"`
	Status  string `json:"status"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	Output  string `json:"output,omitempty"`
}

// TestCaseSummary is a retained failed or skipped test case.
type TestCaseSummary struct {
	Package  string  `json:"package"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	ElapsedS float64 `json:"elapsed_s"`
	Output   string  `json:"output,omitempty"`
}

// TestSummary contains bounded test evidence without passing test records.
type TestSummary struct {
	Passed     int                  `json:"passed"`
	Failed     int                  `json:"failed"`
	Skipped    int                  `json:"skipped"`
	Packages   []TestPackageSummary `json:"packages"`
	Nonpassing []TestCaseSummary    `json:"nonpassing"`
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

// Evidence records the outcome of one planned check.
type Evidence struct {
	CheckID    string           `json:"check_id"`
	Kind       CheckKind        `json:"kind"`
	Status     EvidenceStatus   `json:"status"`
	DurationMS int64            `json:"duration_ms"`
	Summary    string           `json:"summary"`
	Error      string           `json:"error,omitempty"`
	Tests      *TestSummary     `json:"tests,omitempty"`
	Coverage   *CoverageSummary `json:"coverage,omitempty"`
	Analysis   *AnalysisSummary `json:"analysis,omitempty"`
	Race       *RaceSummary     `json:"race,omitempty"`
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
type RiskArea struct {
	Code      string     `json:"code"`
	Reason    string     `json:"reason"`
	Guidance  string     `json:"guidance"`
	Locations []Location `json:"locations"`
}

// Uncertainty is a known limit on a report's conclusion.
type Uncertainty struct {
	Code      string     `json:"code"`
	Message   string     `json:"message"`
	CheckID   string     `json:"check_id,omitempty"`
	Locations []Location `json:"locations"`
}

// PolicyResult is the report's automation result, not a safety verdict.
type PolicyResult struct {
	Status           ResultStatus `json:"status"`
	ExitCode         int          `json:"exit_code"`
	BlockingFindings int          `json:"blocking_findings"`
	IncompleteChecks int          `json:"incomplete_checks"`
	Summary          string       `json:"summary"`
}

// Report is the portable result shared by every delivery adapter.
type Report struct {
	SchemaVersion string        `json:"schema_version"`
	Provider      Provider      `json:"provider"`
	Repository    Repository    `json:"repository"`
	Change        Change        `json:"change"`
	Impact        Impact        `json:"impact"`
	Plan          []Check       `json:"plan"`
	Evidence      []Evidence    `json:"evidence"`
	Findings      []Finding     `json:"findings"`
	Risks         []RiskArea    `json:"risks"`
	Uncertainties []Uncertainty `json:"uncertainties"`
	Result        PolicyResult  `json:"result"`
}

// FailOn controls which introduced analyzer severities block policy.
type FailOn string

const (
	FailOnError   FailOn = "error"
	FailOnWarning FailOn = "warning"
	FailOnInfo    FailOn = "info"
	FailOnNone    FailOn = "none"
)

// Policy controls report evaluation without changing collected evidence.
type Policy struct {
	FailOn             FailOn
	MinChangedCoverage *float64
}

// NewReport initializes the canonical schema and every collection.
func NewReport(providerVersion string, repository Repository) Report {
	return Report{
		SchemaVersion: SchemaVersion,
		Provider:      Provider{Name: "agentic-go", Version: providerVersion},
		Repository:    repository,
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
	sort.Slice(r.Change.Files, func(i, j int) bool { return r.Change.Files[i].Path < r.Change.Files[j].Path })
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
	sort.Slice(r.Risks, func(i, j int) bool { return riskSortKey(r.Risks[i]) < riskSortKey(r.Risks[j]) })
	sort.Slice(r.Uncertainties, func(i, j int) bool {
		return uncertaintySortKey(r.Uncertainties[i]) < uncertaintySortKey(r.Uncertainties[j])
	})
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
