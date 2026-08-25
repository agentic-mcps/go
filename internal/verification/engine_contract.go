package verification

import "context"

// ChangeOptions bounds adapter-independent change discovery.
type ChangeOptions struct {
	Base        string
	Package     string
	MaxPackages int
}

// SourceFile retains the content used to derive one changed-file record. It is
// an internal handoff from language-specific discovery to verification checks.
type SourceFile struct {
	Change         ChangedFile
	BaseContent    []byte
	CurrentContent []byte
	Edits          []LineEdit
}

// LineEdit retains the paired zero-context hunk coordinates required to map
// unchanged base locations into the final snapshot.
type LineEdit struct {
	BaseStart    int
	BaseCount    int
	CurrentStart int
	CurrentCount int
}

// ExecutionTarget is one language-native unit that can be verified. Absolute
// directories are internal infrastructure and never enter the report.
type ExecutionTarget struct {
	ID               string
	Dir              string
	ModulePath       string
	ModuleDir        string
	Distance         int
	Reasons          []string
	Cgo              bool
	BuildConstrained bool
}

// ChangeAnalysis is the complete discovery handoff consumed by Engine.
type ChangeAnalysis struct {
	Repository       Repository
	Change           Change
	Impact           Impact
	Files            []SourceFile
	Packages         []ExecutionTarget
	Uncertainties    []Uncertainty
	Risks            []RiskArea
	Complete         bool
	ObservedPackages int
}

// ChangeAnalyzer discovers the final source snapshot and affected unit closure.
// The interface keeps Git and Go package mechanics outside the verification
// engine without introducing a general plugin abstraction.
type ChangeAnalyzer interface {
	Analyze(context.Context, ChangeOptions) (ChangeAnalysis, error)
	MaterializeBase(context.Context, Repository, string) (string, error)
}
