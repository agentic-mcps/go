// Package gate decides whether a coding agent's Go change is ready to hand
// back. It reports only problems introduced by the change, in a compact form
// an agent can act on, and is designed to run automatically from agent stop
// hooks, pre-push hooks, and CI rather than as a tool an agent must choose.
package gate

import (
	"context"
	"sort"
	"time"
)

// SchemaVersion identifies the JSON form of Result.
const SchemaVersion = "agentic.check/v1"

// Severity orders items by how they affect the verdict.
type Severity string

const (
	// SeverityBlock items make the verdict block.
	SeverityBlock Severity = "block"
	// SeverityWarn items are shown but never block.
	SeverityWarn Severity = "warn"
	// SeverityInfo items are context only.
	SeverityInfo Severity = "info"
)

// Verdict is the gate's single automation outcome.
type Verdict string

const (
	// VerdictPass means no blocking item was observed in completed checks.
	VerdictPass Verdict = "pass"
	// VerdictBlock means at least one blocking item was observed.
	VerdictBlock Verdict = "block"
	// VerdictUnknown means required evidence was incomplete and nothing blocked.
	VerdictUnknown Verdict = "unknown"
)

// Item codes. Codes are stable identifiers for automation and allow trailers.
const (
	CodeSyntax              = "go.syntax"
	CodeBuild               = "go.build"
	CodeTestFailed          = "test.failed"
	CodeTestConsumerFailed  = "test.consumer_failed"
	CodeTestPreexisting     = "test.preexisting"
	CodeTestFlaky           = "test.flaky"
	CodeRace                = "test.race"
	CodeTestDeleted         = "test.deleted"
	CodeTestHidden          = "test.hidden"
	CodeTestSkipAdded       = "test.skip_added"
	CodeAssertionsRemoved   = "test.assertions_removed"
	CodeAssertionsReduced   = "test.assertions_reduced"
	CodeTestEmpty           = "test.empty"
	CodeGoldenModified      = "test.golden_modified"
	CodeStub                = "code.stub"
	CodeGutted              = "code.gutted"
	CodeConfigModified      = "config.modified"
	CodeAnalysisIntroduced  = "analysis.introduced"
	CodeCoverageUncovered   = "coverage.uncovered"
	CodeCoverageUnavailable = "coverage.unavailable"
)

// Item is one actionable observation. File is workspace-relative with forward
// slashes; Line is one-based. Detail carries bounded supporting output such as
// the first lines of a failing test.
//
//nolint:govet // Keep the JSON field order readable for humans and agents.
type Item struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// Base identifies what the change is compared against and how it was chosen.
type Base struct {
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	Source string `json:"source"`
}

// Stats summarizes the work the gate performed.
//
//nolint:govet // Keep the JSON field order readable for humans and agents.
type Stats struct {
	ChangedFiles     int   `json:"changed_files"`
	ChangedGoFiles   int   `json:"changed_go_files"`
	PackagesTested   int   `json:"packages_tested"`
	PackagesAffected int   `json:"packages_affected"`
	DurationMS       int64 `json:"duration_ms"`
	Cached           bool  `json:"cached"`
}

// Result is the complete gate outcome. Items are sorted with SortItems. Notes
// explain incomplete or trimmed evidence in one line each.
//
//nolint:govet // Keep the JSON field order readable for humans and agents.
type Result struct {
	SchemaVersion string   `json:"schema_version"`
	Verdict       Verdict  `json:"verdict"`
	Base          Base     `json:"base"`
	Items         []Item   `json:"items"`
	Notes         []string `json:"notes"`
	Stats         Stats    `json:"stats"`
	// Fingerprint identifies the exact inputs the result was computed from;
	// the stop hook uses it to tell an unchanged retry from new work.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Profile selects defaults for where the gate runs.
type Profile string

const (
	// ProfileLocal is an interactive developer run.
	ProfileLocal Profile = "local"
	// ProfileHook is an agent stop hook: fast, cache-friendly, never fails closed.
	ProfileHook Profile = "hook"
	// ProfileCI is a pull-request check: thorough, fails closed on unknown.
	ProfileCI Profile = "ci"
)

// Options configures one gate run. Zero values select profile defaults.
//
//nolint:govet // Keep option fields grouped by meaning.
type Options struct {
	// Base is an explicit base ref; empty means auto-detect.
	Base string
	// SessionBase is the commit recorded when an agent session started.
	SessionBase string
	Profile     Profile
	// Budget bounds the whole run; zero selects the profile default.
	Budget time.Duration
	// Race forces race detection on affected packages.
	Race bool
	// RequireCoverage makes uncovered changed lines block.
	RequireCoverage bool
	// Skip is passed to go test -skip.
	Skip string
	// MaxPackages caps the tested package closure; zero selects the default.
	MaxPackages int
	// NoCache disables the fingerprint result cache.
	NoCache bool
	// SessionConfigDigests are the gate-configuration digests recorded when an
	// agent session started (see ConfigDigests). In the hook profile a
	// configuration edit is reported only for files that changed since then;
	// nil means no record and reports every edit.
	SessionConfigDigests map[string]string
}

// GitFunc runs git with the given arguments in the workspace root and returns
// stdout. A non-zero exit is an error carrying git's stderr.
type GitFunc func(ctx context.Context, args ...string) ([]byte, error)

// ComputeVerdict derives the verdict from items and evidence completeness.
// Blocking items always block, even when other evidence is incomplete.
func ComputeVerdict(items []Item, complete bool) Verdict {
	for _, item := range items {
		if item.Severity == SeverityBlock {
			return VerdictBlock
		}
	}
	if !complete {
		return VerdictUnknown
	}
	return VerdictPass
}

// SortItems orders items by severity (block, warn, info), then file, line,
// and code, so output is deterministic and the most important lines come first.
func SortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Code < b.Code
	})
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityBlock:
		return 0
	case SeverityWarn:
		return 1
	default:
		return 2
	}
}
