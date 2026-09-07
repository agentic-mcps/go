package pilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Schema identifies the pilot record contract.
const Schema = "agentic.eval.agent/v1alpha1"

// Scenario describes one evaluated repository task.
type Scenario struct {
	SchemaVersion string   `json:"schema_version"`
	ID            string   `json:"id"`
	TaskID        string   `json:"task_id"`
	Prompt        string   `json:"prompt"`
	Obligations   []string `json:"obligations"`
}

// Run records one isolated agent evaluation.
//
//nolint:govet // JSON record layout is kept grouped by contract field category.
type Run struct {
	SchemaVersion             string             `json:"schema_version"`
	ScenarioID                string             `json:"scenario_id"`
	TaskID                    string             `json:"task_id"`
	Condition                 string             `json:"condition"`
	Repetition                int                `json:"repetition"`
	Model                     string             `json:"model"`
	Reasoning                 string             `json:"reasoning"`
	SourceSHA256              string             `json:"source_sha256"`
	BinarySHA256              string             `json:"binary_sha256"`
	WorkspaceSHA256           string             `json:"workspace_sha256"`
	Prompt                    string             `json:"prompt"`
	Transcript                []json.RawMessage  `json:"transcript"`
	Patch                     string             `json:"patch"`
	PatchSHA256               string             `json:"patch_sha256"`
	Acceptance                string             `json:"acceptance"`
	ProcessError              string             `json:"process_error,omitempty"`
	Stderr                    string             `json:"stderr,omitempty"`
	AcceptanceEvidenceSHA256  string             `json:"acceptance_evidence_sha256"`
	ScopeViolations           []string           `json:"scope_violations"`
	EvidenceBytes             int64              `json:"evidence_bytes"`
	ToolCalls                 int                `json:"tool_calls"`
	FocusToolCalls            int                `json:"focus_tool_calls"`
	FocusFailedCalls          int                `json:"focus_failed_calls"`
	FocusErrorCategories      map[string]int     `json:"focus_error_categories"`
	FirstFocusCallPosition    int                `json:"first_focus_call_position"`
	RefreshUse                bool               `json:"refresh_use"`
	FocusEvidenceUse          bool               `json:"focus_evidence_use"`
	FocusResultFollowedByEdit bool               `json:"focus_result_followed_by_edit"`
	RefreshCompleted          bool               `json:"refresh_completed"`
	FocusDelivery             string             `json:"focus_delivery"`
	DurationMS                int64              `json:"duration_ms"`
	OperatorIntervention      bool               `json:"operator_intervention"`
	Uncertainty               []string           `json:"uncertainty"`
	Qualifying                bool               `json:"qualifying"`
	DecisionObligations       []ObligationResult `json:"decision_obligations"`
	TranscriptSHA256          string             `json:"transcript_sha256"`
}

// ObligationResult records evidence for one scenario obligation.
type ObligationResult struct {
	Obligation string `json:"obligation"`
	Status     string `json:"status"`
	Evidence   string `json:"evidence"`
}

// Summary aggregates pilot runs.
//
//nolint:govet // JSON summary layout keeps the public fields in contract order.
type Summary struct {
	SchemaVersion string                      `json:"schema_version"`
	Runs          int                         `json:"runs"`
	ByCondition   map[string]ConditionSummary `json:"by_condition"`
}

// ConditionSummary aggregates runs for one condition.
//
//nolint:govet // JSON summary layout keeps the public fields in contract order.
type ConditionSummary struct {
	Runs                  int     `json:"runs"`
	Qualifying            int     `json:"qualifying"`
	ObligationCoverage    int     `json:"obligation_coverage"`
	ObligationTotal       int     `json:"obligation_total"`
	EvidenceBytes         []int64 `json:"evidence_bytes"`
	ToolCalls             []int   `json:"tool_calls"`
	DurationMS            []int64 `json:"duration_ms"`
	OperatorInterventions int     `json:"operator_interventions"`
}

func decode(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err = d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("input must contain one JSON value")
	}
	return nil
}

// LoadScenario decodes and validates a scenario manifest.
func LoadScenario(path string) (Scenario, error) {
	var s Scenario
	if err := decode(path, &s); err != nil {
		return s, err
	}
	if s.SchemaVersion != Schema || s.ID == "" || s.TaskID == "" || strings.TrimSpace(s.Prompt) == "" || len(s.Obligations) == 0 {
		return s, fmt.Errorf("invalid scenario %s", path)
	}
	return s, nil
}

// LoadScenarios loads the required two scenario manifests.
func LoadScenarios(root string) ([]Scenario, error) {
	paths, _ := filepath.Glob(filepath.Join(root, "*.json"))
	sort.Strings(paths)
	if len(paths) != 2 {
		return nil, fmt.Errorf("pilot requires exactly 2 scenarios, found %d", len(paths))
	}
	out := make([]Scenario, 0, 2)
	seen := map[string]bool{}
	for _, p := range paths {
		s, e := LoadScenario(p)
		if e != nil {
			return nil, e
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("duplicate scenario %q", s.ID)
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out, nil
}

// LoadRun decodes and validates one pilot record.
func LoadRun(path string) (Run, error) {
	var r Run
	if err := decode(path, &r); err != nil {
		return r, err
	}
	if r.SchemaVersion != Schema || r.Condition != "baseline" && r.Condition != "focus" || r.Repetition < 1 || r.Repetition > 5 || r.Model != "gpt-5.6-luna" || r.Prompt == "" {
		return r, fmt.Errorf("invalid pilot run %s", path)
	}
	if len(r.ScopeViolations) > 0 {
		sort.Strings(r.ScopeViolations)
	}
	data, _ := json.Marshal(r.Transcript)
	if containsPrivatePath(string(data)) || containsPrivatePath(r.Patch) {
		return r, errors.New("pilot run contains an unsanitized absolute path")
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if r.TranscriptSHA256 != "" && r.TranscriptSHA256 != got {
		return r, errors.New("transcript hash mismatch")
	}
	if r.PatchSHA256 != "" && r.PatchSHA256 != DigestString(r.Patch) {
		return r, errors.New("patch hash mismatch")
	}
	r.TranscriptSHA256 = got
	focus := FocusMetrics(Events{Raw: r.Transcript})
	r.FocusToolCalls = focus.Calls
	r.FocusFailedCalls = focus.FailedCalls
	r.FocusErrorCategories = focus.ErrorCategories
	r.FirstFocusCallPosition = focus.FirstPosition
	r.RefreshUse = focus.Refresh
	r.FocusEvidenceUse = focus.Evidence
	r.FocusResultFollowedByEdit = focus.FocusResultFollowedByEdit
	r.RefreshCompleted = focus.RefreshCompleted
	return r, nil
}

// ValidateMatrix enforces the complete paired pilot design before aggregation.
func ValidateMatrix(scenarios []Scenario, runs []Run) error {
	if len(scenarios) != 2 {
		return fmt.Errorf("pilot requires exactly 2 scenarios, found %d", len(scenarios))
	}
	known := make(map[string]Scenario, len(scenarios))
	for _, s := range scenarios {
		if _, exists := known[s.ID]; exists {
			return fmt.Errorf("duplicate scenario %q", s.ID)
		}
		known[s.ID] = s
	}
	if len(runs) != 20 {
		return fmt.Errorf("pilot requires exactly 20 runs, found %d", len(runs))
	}
	seen := make(map[string]bool, len(runs))
	for _, r := range runs {
		s, ok := known[r.ScenarioID]
		if !ok {
			return fmt.Errorf("run references unknown scenario %q", r.ScenarioID)
		}
		if r.TaskID != s.TaskID {
			return fmt.Errorf("run %q task %q does not match scenario task %q", r.ScenarioID, r.TaskID, s.TaskID)
		}
		if r.Condition != "baseline" && r.Condition != "focus" {
			return fmt.Errorf("invalid condition %q", r.Condition)
		}
		if r.Repetition < 1 || r.Repetition > 5 {
			return fmt.Errorf("invalid repetition %d", r.Repetition)
		}
		if r.Model != "gpt-5.6-luna" || r.Reasoning != "max" {
			return fmt.Errorf("run %s/%s/r%d must use gpt-5.6-luna/max", r.ScenarioID, r.Condition, r.Repetition)
		}
		key := fmt.Sprintf("%s\x00%s\x00%d", r.ScenarioID, r.Condition, r.Repetition)
		if seen[key] {
			return fmt.Errorf("duplicate run tuple %s/%s/r%d", r.ScenarioID, r.Condition, r.Repetition)
		}
		seen[key] = true
	}
	for _, s := range scenarios {
		for _, condition := range []string{"baseline", "focus"} {
			for repetition := 1; repetition <= 5; repetition++ {
				key := fmt.Sprintf("%s\x00%s\x00%d", s.ID, condition, repetition)
				if !seen[key] {
					return fmt.Errorf("missing run tuple %s/%s/r%d", s.ID, condition, repetition)
				}
			}
		}
	}
	return nil
}

// DigestString returns the SHA-256 digest of value.
func DigestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SanitizeText removes private filesystem roots from evidence.
func SanitizeText(value string) string {
	for _, replacement := range privatePathReplacements() {
		value = replaceAbsoluteRoot(value, replacement.root, replacement.placeholder)
	}
	return value
}

type pathReplacement struct {
	root        string
	placeholder string
}

func privatePathReplacements() []pathReplacement {
	replacements := []pathReplacement{
		{root: "/Users", placeholder: "<user>"},
		{root: "/private/var/folders", placeholder: "<tmp>"},
		{root: "/var/folders", placeholder: "<tmp>"},
		{root: "/private/var/tmp", placeholder: "<tmp>"},
		{root: "/var/tmp", placeholder: "<tmp>"},
		{root: "/private/tmp", placeholder: "<tmp>"},
		{root: "/tmp", placeholder: "<tmp>"},
	}
	if configured := filepath.Clean(os.TempDir()); filepath.IsAbs(configured) && configured != "." {
		replacements = append(replacements, pathReplacement{root: configured, placeholder: "<tmp>"})
		if strings.HasPrefix(configured, "/var/") {
			replacements = append(replacements, pathReplacement{root: "/private" + configured, placeholder: "<tmp>"})
		}
	}
	seen := make(map[string]bool, len(replacements))
	unique := replacements[:0]
	for _, replacement := range replacements {
		if replacement.root == "" || seen[replacement.root] {
			continue
		}
		seen[replacement.root] = true
		unique = append(unique, replacement)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		return len(unique[i].root) > len(unique[j].root)
	})
	return unique
}

func replaceAbsoluteRoot(value, root, placeholder string) string {
	root = strings.TrimRight(filepath.ToSlash(root), "/")
	if root == "" {
		return value
	}
	var out strings.Builder
	start := 0
	for start < len(value) {
		relative := strings.Index(value[start:], root)
		if relative < 0 {
			break
		}
		at := start + relative
		end := at + len(root)
		if absolutePathBoundary(value, at, end) {
			out.WriteString(value[start:at])
			out.WriteString(placeholder)
			start = end
			continue
		}
		out.WriteString(value[start : at+1])
		start = at + 1
	}
	if start == 0 {
		return value
	}
	out.WriteString(value[start:])
	return out.String()
}

func containsPrivatePath(value string) bool {
	for _, replacement := range privatePathReplacements() {
		root := strings.TrimRight(filepath.ToSlash(replacement.root), "/")
		start := 0
		for start < len(value) {
			relative := strings.Index(value[start:], root)
			if relative < 0 {
				break
			}
			at := start + relative
			if absolutePathBoundary(value, at, at+len(root)) {
				return true
			}
			start = at + 1
		}
	}
	return false
}

func absolutePathBoundary(value string, start, end int) bool {
	if start > 0 && isPathToken(value[start-1]) {
		return false
	}
	if end < len(value) && isPathToken(value[end]) && value[end] != '/' {
		return false
	}
	return true
}

func isPathToken(value byte) bool {
	return value == '/' || value == '_' || value == '-' || value == '.' || value == '>' ||
		value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

// SanitizeTranscript sanitizes each transcript event.
func SanitizeTranscript(in []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(in))
	for i, raw := range in {
		out[i] = json.RawMessage(SanitizeText(string(raw)))
	}
	return out
}

// WorkspaceDigest hashes the sorted relative paths and contents, excluding
// Git metadata and private pilot output. It is stable across checkout roots.
func WorkspaceDigest(root string) (string, error) {
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() && (rel == ".git" || rel == ".agentic-go-eval" || strings.HasPrefix(rel, ".agentic-go-eval/")) {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		data, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if e != nil {
			return "", e
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		_, _ = h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Summarize aggregates the supplied pilot runs.
func Summarize(runs []Run) Summary {
	s := Summary{SchemaVersion: Schema, ByCondition: map[string]ConditionSummary{}}
	for _, r := range runs {
		c := s.ByCondition[r.Condition]
		c.Runs++
		if r.Qualifying {
			c.Qualifying++
		}
		for _, obligation := range r.DecisionObligations {
			c.ObligationTotal++
			if obligation.Status == "satisfied" {
				c.ObligationCoverage++
			}
		}
		c.EvidenceBytes = append(c.EvidenceBytes, r.EvidenceBytes)
		c.ToolCalls = append(c.ToolCalls, r.ToolCalls)
		c.DurationMS = append(c.DurationMS, r.DurationMS)
		if r.OperatorIntervention {
			c.OperatorInterventions++
		}
		s.Runs++
		s.ByCondition[r.Condition] = c
	}
	return s
}
