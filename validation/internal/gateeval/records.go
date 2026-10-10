// Package gateeval implements the pre-registered gate evaluation in
// validation/gate/README.md: commit selection, variant generation, arm
// execution, and scoring.
package gateeval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Class identifies a variant stratum from the protocol.
type Class string

// Variant classes. True patches must pass; every other class is a flaw an arm
// should block.
const (
	ClassTrue           Class = "T"
	ClassDestructive    Class = "DT"
	ClassMutant         Class = "M"
	ClassDeleteTests    Class = "C1"
	ClassSkipTests      Class = "C2"
	ClassLogAssertions  Class = "C3"
	ClassRevertTests    Class = "C4"
	ClassEnvGuardedSkip Class = "D1"
	ClassEarlyReturn    Class = "D2"
	ClassBuildTag       Class = "D3"
	ClassLowercaseName  Class = "D4"
	ClassHelperSkip     Class = "D5"
	ClassHeldout6       Class = "D6"
	ClassHeldout7       Class = "D7"
	ClassHeldout8       Class = "D8"
	ClassHeldout9       Class = "D9"
	ClassHeldout10      Class = "D10"
	ClassStub           Class = "S1"
	ClassConsumerBreak  Class = "S2"
	ClassDroppedErr     Class = "S3"
)

// IsFlaw reports whether arms are expected to block the class.
func (c Class) IsFlaw() bool {
	return c != ClassTrue && c != ClassDestructive
}

// Arm identifies one gate under comparison.
type Arm string

// Arms from the protocol. B2Star ignores test failures that also occur at base.
const (
	ArmB0     Arm = "B0"
	ArmB1     Arm = "B1"
	ArmB2     Arm = "B2"
	ArmB2Star Arm = "B2s"
	ArmB3     Arm = "B3"
	ArmGCI    Arm = "Gci"
	ArmGHook  Arm = "Ghook"
)

// Project is one corpus repository.
type Project struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Pinned string `json:"pinned"`
}

// TestRef names one top-level test. File is repository-relative, slash form.
type TestRef struct {
	Package string `json:"package"`
	File    string `json:"file"`
	Name    string `json:"name"`
}

// Selection is one mined commit and the strata it belongs to.
//
//nolint:govet // Keep JSON field order readable.
type Selection struct {
	Project        string   `json:"project"`
	Commit         string   `json:"commit"`
	Base           string   `json:"base"`
	Rank           string   `json:"rank"`
	DirectPackages []string `json:"direct_packages"`
	TruePatch      bool     `json:"true_patch"`
	FlawSeed       bool     `json:"flaw_seed"`
	Destructive    bool     `json:"destructive"`
	ChangedLines   int      `json:"changed_lines"`
}

// Variant is one tree an arm is run against. Branch is a local branch in the
// project clone whose tip is the variant tree; Base is the comparison base.
//
//nolint:govet // Keep JSON field order readable.
type Variant struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Commit    string    `json:"commit"`
	Base      string    `json:"base"`
	Class     Class     `json:"class"`
	Branch    string    `json:"branch"`
	Oracle    []TestRef `json:"oracle"`
	Effective bool      `json:"effective"`
	Operator  string    `json:"operator,omitempty"`
	Notes     string    `json:"notes,omitempty"`
}

// Exclusion records why a commit or variant left the evaluation.
type Exclusion struct {
	Project string `json:"project"`
	Commit  string `json:"commit"`
	Class   Class  `json:"class,omitempty"`
	Reason  string `json:"reason"`
}

// Run is one arm execution against one variant.
//
//nolint:govet // Keep JSON field order readable.
type Run struct {
	VariantID   string `json:"variant_id"`
	Arm         Arm    `json:"arm"`
	Attempt     int    `json:"attempt"`
	Blocked     bool   `json:"blocked"`
	Warned      bool   `json:"warned"`
	Unknown     bool   `json:"unknown"`
	ExitCode    int    `json:"exit_code"`
	DurationMS  int64  `json:"duration_ms"`
	OutputBytes int    `json:"output_bytes"`
	TextBytes   int    `json:"text_bytes,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ReadJSONL decodes one JSON value per line into a slice.
func ReadJSONL[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	return DecodeJSONL[T](file)
}

// DecodeJSONL decodes one JSON value per non-empty line.
func DecodeJSONL[T any](reader io.Reader) ([]T, error) {
	values := make([]T, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var value T
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		values = append(values, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading JSON lines: %w", err)
	}
	return values, nil
}

// AppendJSONL appends one value as a JSON line, creating the file if needed.
func AppendJSONL(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return file.Close()
}
