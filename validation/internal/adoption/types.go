// Package adoption defines the private discoverability experiment contract.
package adoption

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentic-mcps/go/validation/internal/pilot"
)

// Schema identifies the adoption record contract.
const Schema = "agentic.eval.adoption/v1alpha1"

const (
	// ArmBaseline is the no-focus control condition.
	ArmBaseline = "baseline"
	// ArmDiscoverability exposes focus through MCP only.
	ArmDiscoverability = "discoverability"
	// ArmGuidance adds generic prompt guidance to the focus condition.
	ArmGuidance = "guidance"
	// ArmIntegrated is a diagnostic condition with the shipped skill injected
	// into an isolated Codex home. It is not part of the canonical matrix.
	ArmIntegrated = "integrated"
	// IntegratedInstructionSurface identifies the complete diagnostic surface.
	IntegratedInstructionSurface = "mcp-server-instructions+agentic-go-context-skill"
)

// Guidance is the generic treatment instruction.
const Guidance = "For an unfamiliar Go change, call go_context with base and one selector: query, symbol_ref, or file with line and column. After editing, refresh with base and previous_pack_id only; stale selectors and refs are expected to be rejected."

// Scenario describes an adoption task.
type Scenario struct {
	SchemaVersion string   `json:"schema_version"`
	ID            string   `json:"id"`
	TaskID        string   `json:"task_id"`
	Prompt        string   `json:"prompt"`
	Obligations   []string `json:"obligations"`
}

// Run records one adoption experiment cell.
//
//nolint:govet // JSON contract groups related evidence fields.
type Run struct {
	SchemaVersion               string            `json:"schema_version"`
	ScenarioID                  string            `json:"scenario_id"`
	TaskID                      string            `json:"task_id"`
	Arm                         string            `json:"arm"`
	Repetition                  int               `json:"repetition"`
	Model                       string            `json:"model"`
	Reasoning                   string            `json:"reasoning"`
	Prompt                      string            `json:"prompt"`
	PromptSHA256                string            `json:"prompt_sha256"`
	MCPDescriptionSHA256        string            `json:"mcp_description_sha256"`
	SkillSHA256                 string            `json:"skill_sha256,omitempty"`
	EffectiveInstructionSurface string            `json:"effective_instruction_surface,omitempty"`
	SourceSHA256                string            `json:"source_sha256"`
	BinarySHA256                string            `json:"binary_sha256"`
	InitialWorkspaceSHA256      string            `json:"initial_workspace_sha256"`
	PostWorkspaceSHA256         string            `json:"post_workspace_sha256"`
	Transcript                  []json.RawMessage `json:"transcript"`
	TranscriptSHA256            string            `json:"transcript_sha256"`
	Patch                       string            `json:"patch"`
	PatchSHA256                 string            `json:"patch_sha256"`
	Stderr                      string            `json:"stderr,omitempty"`
	ProcessError                string            `json:"process_error,omitempty"`
	FocusDelivery               string            `json:"focus_delivery"`
	WorkspaceSHA256             string            `json:"workspace_sha256"`
	DecisionObligations         []string          `json:"decision_obligations"`
	AcceptanceEvidenceSHA256    string            `json:"acceptance_evidence_sha256"`
	Acceptance                  string            `json:"acceptance"`
	ScopeViolations             []string          `json:"scope_violations"`
	EvidenceBytes               int64             `json:"evidence_bytes"`
	ToolCalls                   int               `json:"tool_calls"`
	FocusToolCalls              int               `json:"focus_tool_calls"`
	FocusFailedCalls            int               `json:"focus_failed_calls"`
	FocusErrorCategories        map[string]int    `json:"focus_error_categories"`
	FirstFocusCallPosition      int               `json:"first_focus_call_position"`
	RefreshUse                  bool              `json:"refresh_use"`
	FocusEvidenceUse            bool              `json:"focus_evidence_use"`
	FocusResultFollowedByEdit   bool              `json:"focus_result_followed_by_edit"`
	RefreshCompleted            bool              `json:"refresh_completed"`
	SkillDiscovered             bool              `json:"skill_discovered"`
	DurationMS                  int64             `json:"duration_ms"`
	OperatorIntervention        bool              `json:"operator_intervention"`
	Uncertainty                 []string          `json:"uncertainty"`
	Qualifying                  bool              `json:"qualifying"`
}

// LoadRun loads and canonicalizes one adoption run.
func LoadRun(path string) (Run, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Run{}, err
	}
	var r Run
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&r); err != nil {
		return r, err
	}
	if r.SchemaVersion != Schema || r.ScenarioID == "" || r.TaskID == "" || !ValidArm(r.Arm) || r.Repetition < 1 || r.Repetition > 3 || r.Model != "gpt-5.6-luna" || r.Reasoning != "max" {
		return r, fmt.Errorf("invalid adoption run %s", path)
	}
	if r.PromptSHA256 != "" && r.PromptSHA256 != DigestString(r.Prompt) {
		return r, fmt.Errorf("prompt hash mismatch")
	}
	if r.Arm == ArmIntegrated {
		if !isSHA256(r.SkillSHA256) {
			return r, fmt.Errorf("integrated run has invalid skill hash")
		}
		if r.EffectiveInstructionSurface != IntegratedInstructionSurface {
			return r, fmt.Errorf("integrated run has invalid instruction surface")
		}
	}
	data, _ := json.Marshal(r.Transcript)
	if r.TranscriptSHA256 != "" && r.TranscriptSHA256 != DigestString(string(data)) {
		return r, fmt.Errorf("transcript hash mismatch")
	}
	r.TranscriptSHA256 = DigestString(string(data))
	focus := pilot.FocusMetrics(pilot.Events{Raw: r.Transcript})
	r.FocusToolCalls = focus.Calls
	r.FocusFailedCalls = focus.FailedCalls
	r.FocusErrorCategories = focus.ErrorCategories
	r.FirstFocusCallPosition = focus.FirstPosition
	r.RefreshUse = focus.Refresh
	r.FocusEvidenceUse = focus.Evidence
	r.FocusResultFollowedByEdit = focus.FocusResultFollowedByEdit
	r.RefreshCompleted = focus.RefreshCompleted
	r.SkillDiscovered = pilot.SkillDiscoveryEvidence(pilot.Events{Raw: r.Transcript}, SkillName)
	if r.PatchSHA256 != "" && r.PatchSHA256 != DigestString(r.Patch) {
		return r, fmt.Errorf("patch hash mismatch")
	}
	return r, nil
}

// SanitizeRun normalizes private process evidence before it is re-ingested.
// Hashes are recomputed because sanitization intentionally changes text.
func SanitizeRun(r Run) Run {
	r.Prompt = pilot.SanitizeText(r.Prompt)
	r.PromptSHA256 = DigestString(r.Prompt)
	r.Transcript = pilot.SanitizeTranscript(r.Transcript)
	transcript, _ := json.Marshal(r.Transcript)
	r.TranscriptSHA256 = DigestString(string(transcript))
	r.Patch = pilot.SanitizeText(r.Patch)
	if r.PatchSHA256 != "" {
		r.PatchSHA256 = DigestString(r.Patch)
	}
	r.Stderr = pilot.SanitizeText(r.Stderr)
	r.ProcessError = pilot.SanitizeText(r.ProcessError)
	for i := range r.Uncertainty {
		r.Uncertainty[i] = pilot.SanitizeText(r.Uncertainty[i])
	}
	for i := range r.ScopeViolations {
		r.ScopeViolations[i] = pilot.SanitizeText(r.ScopeViolations[i])
	}
	for i := range r.DecisionObligations {
		r.DecisionObligations[i] = pilot.SanitizeText(r.DecisionObligations[i])
	}
	return r
}

// LoadRuns loads and canonicalizes an ingested adoption result set.
func LoadRuns(path string) ([]Run, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var r []Run
	if e = json.Unmarshal(b, &r); e != nil {
		return nil, e
	}
	for i := range r {
		focus := pilot.FocusMetrics(pilot.Events{Raw: r[i].Transcript})
		r[i].FocusToolCalls = focus.Calls
		r[i].FocusFailedCalls = focus.FailedCalls
		r[i].FocusErrorCategories = focus.ErrorCategories
		r[i].FirstFocusCallPosition = focus.FirstPosition
		r[i].RefreshUse = focus.Refresh
		r[i].FocusEvidenceUse = focus.Evidence
		r[i].FocusResultFollowedByEdit = focus.FocusResultFollowedByEdit
		r[i].RefreshCompleted = focus.RefreshCompleted
		r[i].SkillDiscovered = pilot.SkillDiscoveryEvidence(pilot.Events{Raw: r[i].Transcript}, SkillName)
	}
	return r, nil
}

// Aggregate summarizes adoption runs by arm.
func Aggregate(rs []Run) map[string]any {
	out := map[string]any{"schema_version": Schema, "runs": len(rs), "arms": map[string]any{}}
	arms := out["arms"].(map[string]any)
	for _, a := range []string{ArmBaseline, ArmDiscoverability, ArmGuidance, ArmIntegrated} {
		var n, q, c, failed, followedByEdit, refreshCompleted int
		var bytes, calls, durations []int64
		categories := map[string]int{}
		for _, r := range rs {
			if r.Arm != a {
				continue
			}
			n++
			if r.Qualifying {
				q++
			}
			c += r.FocusEvidenceUseInt()
			failed += r.FocusFailedCalls
			if r.FocusResultFollowedByEdit {
				followedByEdit++
			}
			if r.RefreshCompleted {
				refreshCompleted++
			}
			for category, count := range r.FocusErrorCategories {
				categories[category] += count
			}
			bytes = append(bytes, r.EvidenceBytes)
			calls = append(calls, int64(r.ToolCalls))
			durations = append(durations, r.DurationMS)
		}
		arms[a] = map[string]any{"runs": n, "qualifying": q, "focus_evidence_use": c, "focus_failed_calls": failed, "focus_result_followed_by_edit": followedByEdit, "refresh_completed": refreshCompleted, "focus_error_categories": categories, "evidence_bytes_median": median(bytes), "tool_calls_median": median(calls), "duration_ms_median": median(durations)}
	}
	return out
}

// FocusEvidenceUseInt converts evidence use to an integer count.
func (r Run) FocusEvidenceUseInt() int {
	if r.FocusEvidenceUse {
		return 1
	}
	return 0
}

// ValidArm reports whether arm is a canonical matrix condition or the
// separate integrated diagnostic condition.
func ValidArm(arm string) bool {
	return arm == ArmBaseline || arm == ArmDiscoverability || arm == ArmGuidance || arm == ArmIntegrated
}

func isSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// MatrixArms returns the conditions required by the canonical matrix.
func MatrixArms() []string {
	return []string{ArmBaseline, ArmDiscoverability, ArmGuidance}
}

func median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[len(v)/2]
}

// DigestString returns the SHA-256 digest of s.
func DigestString(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// LoadScenarios loads the two adoption scenarios from root.
func LoadScenarios(root string) ([]Scenario, error) {
	ps, _ := filepath.Glob(filepath.Join(root, "*.json"))
	sort.Strings(ps)
	if len(ps) != 2 {
		return nil, fmt.Errorf("adoption requires exactly 2 scenarios, found %d", len(ps))
	}
	out := make([]Scenario, 0, 2)
	seen := map[string]bool{}
	for _, p := range ps {
		b, e := os.ReadFile(p)
		if e != nil {
			return nil, e
		}
		var s Scenario
		d := json.NewDecoder(strings.NewReader(string(b)))
		d.DisallowUnknownFields()
		if e = d.Decode(&s); e != nil || s.SchemaVersion == "" || s.ID == "" || s.TaskID == "" || s.Prompt == "" || len(s.Obligations) == 0 {
			return nil, fmt.Errorf("invalid scenario %s", p)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("duplicate scenario %q", s.ID)
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out, nil
}

// ValidateMatrix verifies the complete adoption matrix.
func ValidateMatrix(ss []Scenario, rs []Run) error {
	if len(ss) != 2 {
		return fmt.Errorf("adoption requires exactly 2 scenarios")
	}
	known := map[string]string{}
	for _, s := range ss {
		known[s.ID] = s.TaskID
	}
	matrixRuns := make([]Run, 0, len(rs))
	for _, r := range rs {
		taskID, knownScenario := known[r.ScenarioID]
		if !knownScenario || r.TaskID == "" || r.TaskID != taskID {
			return fmt.Errorf("unknown scenario or task in %s", r.ScenarioID)
		}
		if !ValidArm(r.Arm) {
			return fmt.Errorf("invalid arm %q", r.Arm)
		}
		if r.Repetition < 1 || r.Repetition > 3 {
			return fmt.Errorf("invalid repetition")
		}
		if r.Model != "gpt-5.6-luna" || r.Reasoning != "max" {
			return fmt.Errorf("run must use gpt-5.6-luna/max")
		}
		if r.Arm != ArmIntegrated {
			matrixRuns = append(matrixRuns, r)
		}
	}
	if len(matrixRuns) != 18 {
		return fmt.Errorf("adoption requires exactly 18 matrix runs, found %d", len(matrixRuns))
	}
	seen := map[string]bool{}
	for _, r := range matrixRuns {
		k := fmt.Sprintf("%s/%s/%d", r.ScenarioID, r.Arm, r.Repetition)
		if seen[k] {
			return fmt.Errorf("duplicate run %s", k)
		}
		seen[k] = true
	}
	for _, s := range ss {
		for _, a := range MatrixArms() {
			for i := 1; i <= 3; i++ {
				if !seen[fmt.Sprintf("%s/%s/%d", s.ID, a, i)] {
					return fmt.Errorf("missing run %s/%s/%d", s.ID, a, i)
				}
			}
		}
	}
	return nil
}
