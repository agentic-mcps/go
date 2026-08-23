// Package finding defines shared structured analyzer output types.
package finding

// Location is a slash-separated source position relative to the workspace.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col,omitempty"`
}

// Severity is the stable importance assigned to a finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is one source-located analyzer diagnostic.
type Finding struct {
	Rule       string   `json:"rule"`
	RuleName   string   `json:"rule_name,omitempty"`
	Severity   Severity `json:"severity"`
	Location   Location `json:"location"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// AuditResult is the bounded, aggregate result of an analyzer run.
type AuditResult struct {
	Findings         []Finding        `json:"findings"`
	Total            int              `json:"total"`
	Truncated        bool             `json:"truncated"`
	CountsBySeverity map[Severity]int `json:"counts_by_severity"`
	FilesScanned     int              `json:"files_scanned"`
	DurationMS       int64            `json:"duration_ms"`
}
