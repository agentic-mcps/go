package adoption

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-mcps/go/validation/internal/pilot"
)

func TestValidateMatrixRequiresExactThreeArms(t *testing.T) {
	ss := []Scenario{{ID: "a", TaskID: "ta"}, {ID: "b", TaskID: "tb"}}
	var rs []Run
	for _, s := range ss {
		for _, a := range []string{"baseline", "discoverability", "guidance"} {
			for i := 1; i <= 3; i++ {
				rs = append(rs, Run{ScenarioID: s.ID, TaskID: s.TaskID, Arm: a, Repetition: i, Model: "gpt-5.6-luna", Reasoning: "max"})
			}
		}
	}
	if err := ValidateMatrix(ss, rs); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMatrix(ss, rs[:17]); err == nil {
		t.Fatal("expected incomplete matrix")
	}
	rs[17] = rs[0]
	if err := ValidateMatrix(ss, rs); err == nil {
		t.Fatal("expected duplicate matrix")
	}
}

func TestValidateMatrixIgnoresIntegratedDiagnosticArm(t *testing.T) {
	ss := []Scenario{{ID: "a", TaskID: "ta"}, {ID: "b", TaskID: "tb"}}
	var rs []Run
	for _, s := range ss {
		for _, a := range MatrixArms() {
			for i := 1; i <= 3; i++ {
				rs = append(rs, Run{ScenarioID: s.ID, TaskID: s.TaskID, Arm: a, Repetition: i, Model: "gpt-5.6-luna", Reasoning: "max"})
			}
		}
	}
	rs = append(rs, Run{ScenarioID: "a", TaskID: "ta", Arm: ArmIntegrated, Repetition: 1, Model: "gpt-5.6-luna", Reasoning: "max"})
	if err := ValidateMatrix(ss, rs); err != nil {
		t.Fatal(err)
	}
}

func TestGuidanceIsStableAndDigestable(t *testing.T) {
	if Guidance == "" || DigestString(Guidance) == "" {
		t.Fatal("guidance digest missing")
	}
}

func TestLoadRunRecomputesStaleFocusMetrics(t *testing.T) {
	r := Run{SchemaVersion: Schema, ScenarioID: "s", TaskID: "t", Arm: "guidance", Repetition: 1, Model: "gpt-5.6-luna", Reasoning: "max", Transcript: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","result":{"pack_id":"p"},"arguments":{"previous_pack_id":"old"}}}`),
	}}
	r.FocusToolCalls, r.FirstFocusCallPosition = 99, 99
	r.RefreshUse, r.FocusEvidenceUse = false, false
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "run.json")
	if err = os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRun(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.FocusToolCalls != 1 || got.FirstFocusCallPosition != 1 || !got.RefreshUse || got.FocusEvidenceUse {
		t.Fatalf("stale metrics not recomputed: %+v", got)
	}
}

func TestLoadRunRecordsObjectiveFocusWorkflowMetrics(t *testing.T) {
	r := Run{SchemaVersion: Schema, ScenarioID: "s", TaskID: "t", Arm: ArmGuidance, Repetition: 1, Model: "gpt-5.6-luna", Reasoning: "max", Transcript: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","result":{"structured_content":{"pack_id":"pack-1"}}}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","arguments":{"previous_pack_id":"pack-1"},"result":{"structured_content":{"pack_id":"pack-2"}}}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"failed","result":{"content":[{"type":"text","text":"building change context: stale snapshot"}]}}}`),
	}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "run.json")
	if err = os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRun(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.FocusToolCalls != 2 || got.FocusFailedCalls != 1 || !got.FocusResultFollowedByEdit || !got.RefreshCompleted || !got.FocusEvidenceUse || got.FocusErrorCategories["stale_snapshot"] != 1 {
		t.Fatalf("objective focus metrics = %+v", got)
	}
}

func TestLoadRunRecomputesSkillDiscovery(t *testing.T) {
	r := Run{SchemaVersion: Schema, ScenarioID: "s", TaskID: "t", Arm: ArmIntegrated, Repetition: 1, Model: "gpt-5.6-luna", Reasoning: "max", SkillSHA256: DigestString("skill"), EffectiveInstructionSurface: IntegratedInstructionSurface, Transcript: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"command_execution","command":"sed -n '1,120p' /tmp/skills/agentic-go-context/SKILL.md"}}`),
	}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "run.json")
	if err = os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRun(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SkillDiscovered {
		t.Fatal("skill discovery was not recomputed from the transcript")
	}
}

func TestLoadRunRequiresIntegratedSurfaceMetadata(t *testing.T) {
	r := Run{SchemaVersion: Schema, ScenarioID: "s", TaskID: "t", Arm: ArmIntegrated, Repetition: 1, Model: "gpt-5.6-luna", Reasoning: "max"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "run.json")
	if err = os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadRun(p); err == nil {
		t.Fatal("integrated run without skill metadata was accepted")
	}
}

func TestSanitizeRunRehashesPrivateProcessEvidence(t *testing.T) {
	r := Run{SchemaVersion: Schema, ScenarioID: "s", TaskID: "t", Arm: ArmIntegrated, Repetition: 1, Model: "gpt-5.6-luna", Reasoning: "max", Prompt: "task", Transcript: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"command_execution","command":"sed /private/var/folders/a/b/T/skill/SKILL.md"}}`),
	}, Patch: "/private/var/folders/a/b/T/patch", PatchSHA256: DigestString("/private/var/folders/a/b/T/patch"), Uncertainty: []string{"failure /private/var/folders/a/b/T/run"}}
	normalized := SanitizeRun(r)
	data, err := json.Marshal(normalized.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/private/var/folders/") || strings.Contains(normalized.Patch, "/private/var/folders/") || strings.Contains(normalized.Uncertainty[0], "/private/var/folders/") {
		t.Fatalf("private process path survived sanitization: %+v", normalized)
	}
	if normalized.TranscriptSHA256 != DigestString(string(data)) || normalized.PatchSHA256 != DigestString(normalized.Patch) {
		t.Fatalf("sanitized hashes were not recomputed: %+v", normalized)
	}
}

func TestLoadSkillValidatesAndInstallsOnlyShippedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "openai.yaml"), []byte("yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := LoadSkill(root)
	if err != nil {
		t.Fatal(err)
	}
	if skill.Digest == "" || len(skill.Files) != 2 {
		t.Fatalf("skill = %#v", skill)
	}
	home := t.TempDir()
	installed, err := skill.Install(home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(installed, home) || installed == root {
		t.Fatalf("installed path = %q", installed)
	}
	if _, statErr := os.Stat(filepath.Join(installed, "SKILL.md")); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(filepath.Join(installed, "agents", "openai.yaml")); statErr != nil {
		t.Fatal(statErr)
	}
	entries, err := os.ReadDir(installed)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("installed entries = %d, want skill and agents", len(entries))
	}
}

func TestLoadSkillRejectsUnexpectedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml"), "README.md"} {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadSkill(root); err == nil {
		t.Fatal("unexpected skill file was accepted")
	}
}

func TestSkillInstallDoesNotContaminateCandidateWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := pilot.WorkspaceDigest(workspace)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err = os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "agents", "openai.yaml"), []byte("yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, err := LoadSkill(root)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	installed, err := skill.Install(home)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(workspace, installed)
	if err != nil || rel == "." || rel == "" || !strings.HasPrefix(rel, "..") {
		t.Fatalf("skill installed inside candidate workspace: path=%q rel=%q err=%v", installed, rel, err)
	}
	after, err := pilot.WorkspaceDigest(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("candidate workspace digest changed: before=%s after=%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("skill installation contaminated candidate workspace: err=%v", err)
	}
}
