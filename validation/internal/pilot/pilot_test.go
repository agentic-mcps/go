package pilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenariosAreExactlyTwoAndTaskLinked(t *testing.T) {
	root := filepath.Join("scenarios")
	ss, err := LoadScenarios(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("got %d scenarios", len(ss))
	}
	for _, s := range ss {
		if s.TaskID == "" || len(s.Obligations) < 2 {
			t.Fatalf("invalid %q", s.ID)
		}
	}
}

func TestRunRejectsUnsafeAndStaleTranscript(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "run.json")
	r := Run{SchemaVersion: Schema, ScenarioID: "x", TaskID: "x", Condition: "focus", Repetition: 1, Model: "gpt-5.6-luna", Prompt: "task", Transcript: []json.RawMessage{json.RawMessage(`{"path":"relative/file.go"}`)}}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRun(p); err != nil {
		t.Fatal(err)
	}
	r.Patch = "/tmp/private.patch"
	b, _ = json.Marshal(r)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRun(p); err == nil {
		t.Fatal("expected unsanitized path rejection")
	}
	b = append(b[:len(b)-2], []byte(`,"transcript_sha256":"stale"}`)...)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRun(p); err == nil {
		t.Fatal("expected stale hash rejection")
	}
}

func TestLoadRunRejectsTamperedPatch(t *testing.T) {
	d := t.TempDir()
	r := Run{SchemaVersion: Schema, ScenarioID: "x", TaskID: "x", Condition: "focus", Repetition: 1, Model: "gpt-5.6-luna", Prompt: "task", Transcript: []json.RawMessage{}, Patch: "diff", PatchSHA256: DigestString("diff")}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(d, "run.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	r.Patch = "tampered"
	b, _ = json.Marshal(r)
	if err := os.WriteFile(filepath.Join(d, "run.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRun(filepath.Join(d, "run.json")); err == nil {
		t.Fatal("expected patch hash mismatch")
	}
}

func TestWorkspaceDigestStableAcrossExcludedFiles(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "a.go"), []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := WorkspaceDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	if mkdirErr := os.MkdirAll(filepath.Join(d, ".git"), 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(d, ".git", "index"), []byte("ignored"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	b, err := WorkspaceDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("excluded metadata changed digest: %s != %s", a, b)
	}
}

func TestSanitizeTextRemovesPrivateRoots(t *testing.T) {
	got := SanitizeText("error /Users/ashwin/x /tmp/run")
	if strings.Contains(got, "/Users/") || strings.Contains(got, "/tmp/") {
		t.Fatalf("unsanitized: %q", got)
	}
}

func TestSanitizeTextRemovesMacOSTempRootsAndPreservesPlaceholders(t *testing.T) {
	tmpRoot := t.TempDir()
	configuredTemp := filepath.Join(tmpRoot, "configured-temp")
	if err := os.MkdirAll(configuredTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", configuredTemp)
	input := "workspace /workspace/pkg/file.go <workspace>/pkg/file.go <tmp>/existing " +
		"/private/var/folders/ab/cd/T/run /var/folders/ab/cd/T/run /private/tmp/run /var/tmp/run " + configuredTemp + "/run"
	got := SanitizeText(input)
	for _, raw := range []string{"/private/var/folders/", "/var/folders/", "/private/tmp/", "/var/tmp/", configuredTemp + "/"} {
		if strings.Contains(got, raw) {
			t.Fatalf("%q remained in sanitized text: %q", raw, got)
		}
	}
	for _, placeholder := range []string{"/workspace/pkg/file.go", "<workspace>/pkg/file.go", "<tmp>/existing"} {
		if !strings.Contains(got, placeholder) {
			t.Fatalf("workspace or existing placeholder %q was changed: %q", placeholder, got)
		}
	}
}

func TestLoadRunRejectsUnsanitizedMacOSTempRoots(t *testing.T) {
	recordsRoot := t.TempDir()
	configuredTemp := filepath.Join(recordsRoot, "configured-temp")
	if err := os.MkdirAll(configuredTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", configuredTemp)
	for _, raw := range []string{
		"/private/var/folders/ab/cd/T/run",
		"/var/folders/ab/cd/T/run",
		"/private/tmp/run",
		"/var/tmp/run",
		configuredTemp + "/run",
	} {
		r := Run{SchemaVersion: Schema, ScenarioID: "x", TaskID: "x", Condition: "focus", Repetition: 1, Model: "gpt-5.6-luna", Prompt: "task", Transcript: []json.RawMessage{json.RawMessage(`{"text":"` + raw + `"}`)}}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(recordsRoot, "run.json")
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRun(path); err == nil {
			t.Fatalf("unsanitized temp root was accepted: %q", raw)
		}
	}
}

func TestScoreObligationsUsesRecordedEvidence(t *testing.T) {
	s := Scenario{Obligations: []string{"identify RetryOnConflict declaration", "identify callers and tests", "interpret verification applicability after edit"}}
	r := Run{Condition: "baseline", Acceptance: "pass", Transcript: []json.RawMessage{json.RawMessage(`{"text":"RetryOnConflict declaration, callers and _test.go; verification applicability"}`)}, Patch: "retryonconflict_test.go"}
	got := ScoreObligations(s, r)
	if len(got) != 3 || got[0].Status != "satisfied" || got[1].Status != "satisfied" || got[2].Status != "satisfied" {
		t.Fatalf("obligations = %+v", got)
	}
}

func TestScoreObligationsDoesNotTrustSelfAssertion(t *testing.T) {
	s := Scenario{Obligations: []string{"identify callers and tests"}}
	r := Run{Condition: "baseline", Acceptance: "pass", Transcript: []json.RawMessage{json.RawMessage(`{"agent_claim":"done"}`)}}
	got := ScoreObligations(s, r)
	if got[0].Status != "uncertain" {
		t.Fatalf("status = %q, want uncertain", got[0].Status)
	}
}

func pilotMatrix() ([]Scenario, []Run) {
	scenarios := []Scenario{{ID: "a", TaskID: "ta"}, {ID: "b", TaskID: "tb"}}
	runs := make([]Run, 0, 20)
	for _, s := range scenarios {
		for _, condition := range []string{"baseline", "focus"} {
			for repetition := 1; repetition <= 5; repetition++ {
				runs = append(runs, Run{ScenarioID: s.ID, TaskID: s.TaskID, Condition: condition, Repetition: repetition, Model: "gpt-5.6-luna", Reasoning: "max"})
			}
		}
	}
	return scenarios, runs
}

func TestValidateMatrixAcceptsCompletePilot(t *testing.T) {
	scenarios, runs := pilotMatrix()
	if err := ValidateMatrix(scenarios, runs); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMatrixRejectsDuplicate(t *testing.T) {
	scenarios, runs := pilotMatrix()
	runs[19] = runs[0]
	if err := ValidateMatrix(scenarios, runs); err == nil {
		t.Fatal("expected duplicate tuple rejection")
	}
}

func TestValidateMatrixRejectsMissing(t *testing.T) {
	scenarios, runs := pilotMatrix()
	if err := ValidateMatrix(scenarios, runs[:19]); err == nil {
		t.Fatal("expected incomplete matrix rejection")
	}
}

func TestValidateMatrixRejectsExtraOrWrongCell(t *testing.T) {
	scenarios, runs := pilotMatrix()
	runs[0].Model = "gpt-5.6-sol"
	if err := ValidateMatrix(scenarios, runs); err == nil {
		t.Fatal("expected wrong model rejection")
	}
	runs[0].Model = "gpt-5.6-luna"
	runs[0].ScenarioID = "extra"
	if err := ValidateMatrix(scenarios, runs); err == nil {
		t.Fatal("expected extra cell rejection")
	}
}

func TestSummarizeCountsEachRunOnce(t *testing.T) {
	runs := []Run{{Condition: "baseline", Qualifying: true, DecisionObligations: []ObligationResult{{Status: "satisfied"}}}}
	s := Summarize(runs)
	if s.Runs != 1 || s.ByCondition["baseline"].Runs != 1 {
		t.Fatalf("summary = %+v", s)
	}
}
