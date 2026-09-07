package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentic-mcps/go/internal/tools"
	"github.com/agentic-mcps/go/validation/internal/adoption"
	validation "github.com/agentic-mcps/go/validation/internal/eval"
	"github.com/agentic-mcps/go/validation/internal/pilot"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agentic-go-eval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("expected a supported evaluation command")
	}
	switch args[0] {
	case "adoption-validate":
		fs := flag.NewFlagSet("adoption-validate", flag.ContinueOnError)
		root := fs.String("scenarios", "validation/internal/adoption/scenarios", "scenario directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ss, err := adoption.LoadScenarios(*root)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"schema_version": adoption.Schema, "scenarios": len(ss), "arms": 3, "repetitions": 3, "runs": 18, "status": "valid"})
	case "adoption-run":
		fs := flag.NewFlagSet("adoption-run", flag.ContinueOnError)
		scenario := fs.String("scenario", "", "scenario id")
		arm := fs.String("arm", adoption.ArmBaseline, "baseline, discoverability, guidance, or integrated")
		repetition := fs.Int("repetition", 1, "repetition 1-3")
		dry := fs.Bool("dry-run", false, "validate without running")
		sourceRoot := fs.String("source-root", "", "upstream clone root (required)")
		outputRoot := fs.String("output-root", ".agentic-go-eval/adoption", "private output root")
		timeout := fs.Duration("timeout", 30*time.Minute, "agent timeout")
		skillRoot := fs.String("skill-root", ".agents/skills/agentic-go-context", "shipped skill root for the integrated diagnostic")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*sourceRoot) == "" {
			return errors.New("-source-root is required")
		}
		if *scenario == "" {
			return errors.New("-scenario is required")
		}
		if !adoption.ValidArm(*arm) || *repetition < 1 || *repetition > 3 {
			return errors.New("invalid arm or repetition")
		}
		ss, err := adoption.LoadScenarios("validation/internal/adoption/scenarios")
		if err != nil {
			return err
		}
		var s adoption.Scenario
		for _, x := range ss {
			if x.ID == *scenario {
				s = x
			}
		}
		if s.ID == "" {
			return fmt.Errorf("unknown scenario %q", *scenario)
		}
		p := s.Prompt
		if *arm == "guidance" {
			p += " " + adoption.Guidance
		}
		if *dry {
			result := map[string]any{"schema_version": adoption.Schema, "scenario": s.ID, "arm": *arm, "repetition": *repetition, "prompt_sha256": adoption.DigestString(p), "instruction_surface": adoptionInstructionSurface(*arm), "status": "dry-run"}
			if *arm == adoption.ArmIntegrated {
				skill, skillErr := adoption.LoadSkill(*skillRoot)
				if skillErr != nil {
					return skillErr
				}
				result["skill_sha256"] = skill.Digest
			}
			return printJSON(result)
		}
		return runAdoption(ctx, s, *arm, *repetition, *sourceRoot, *outputRoot, *timeout, *skillRoot)
	case "adoption-report":
		fs := flag.NewFlagSet("adoption-report", flag.ContinueOnError)
		input := fs.String("input", ".agentic-go-eval/adoption/ingested.json", "input")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rs, e := adoption.LoadRuns(*input)
		if e != nil {
			return e
		}
		return printJSON(adoption.Aggregate(rs))
	case "adoption-ingest":
		fs := flag.NewFlagSet("adoption-ingest", flag.ContinueOnError)
		root := fs.String("runs", ".agentic-go-eval/adoption/runs", "run directory")
		out := fs.String("output", ".agentic-go-eval/adoption/ingested.json", "output")
		allowIncomplete := fs.Bool("allow-incomplete", false, "validate available runs without requiring the full matrix")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ps, _ := filepath.Glob(filepath.Join(*root, "*.json"))
		rs := make([]adoption.Run, 0, len(ps))
		for _, p := range ps {
			r, e := adoption.LoadRun(p)
			if e != nil {
				return e
			}
			r = adoption.SanitizeRun(r)
			fm := pilot.FocusMetrics(pilot.Events{Raw: r.Transcript})
			r.FocusToolCalls = fm.Calls
			r.FocusFailedCalls = fm.FailedCalls
			r.FocusErrorCategories = fm.ErrorCategories
			r.FirstFocusCallPosition = fm.FirstPosition
			r.RefreshUse = fm.Refresh
			r.FocusEvidenceUse = fm.Evidence
			r.FocusResultFollowedByEdit = fm.FocusResultFollowedByEdit
			r.RefreshCompleted = fm.RefreshCompleted
			rs = append(rs, r)
		}
		ss, e := adoption.LoadScenarios("validation/internal/adoption/scenarios")
		if e != nil {
			return e
		}
		if !*allowIncomplete {
			e = adoption.ValidateMatrix(ss, rs)
		} else if len(rs) == 0 {
			e = errors.New("no adoption runs")
		}
		if e != nil {
			return e
		}
		b, _ := json.MarshalIndent(rs, "", "  ")
		if e = os.MkdirAll(filepath.Dir(*out), 0o755); e != nil {
			return e
		}
		if e = os.WriteFile(*out, append(b, '\n'), 0o644); e != nil {
			return e
		}
		return printJSON(map[string]any{"runs": len(rs), "status": "ingested"})
	case "validate":
		fs := flag.NewFlagSet("validate", flag.ContinueOnError)
		tasksRoot := fs.String("tasks", "validation/v0.8.0/tasks", "task manifest root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		tasks, err := validation.LoadTasks(*tasksRoot)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"schema_version": validation.TaskSchema, "tasks": len(tasks), "status": "valid"})
	case "prepare":
		fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
		tasksRoot := fs.String("tasks", "validation/v0.8.0/tasks", "task manifest root")
		taskID := fs.String("id", "", "optional single task id")
		sources := fs.String("sources", "", "root containing named local clones")
		output := fs.String("output", "", "bundle output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		tasks, err := validation.LoadTasks(*tasksRoot)
		if err != nil {
			return err
		}
		if *taskID != "" {
			var selected []validation.Task
			for _, task := range tasks {
				if task.ID == *taskID {
					selected = append(selected, task)
				}
			}
			if len(selected) != 1 {
				return fmt.Errorf("unknown task id %q", *taskID)
			}
			tasks = selected
		}
		boundedCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		records, err := validation.PrepareAll(boundedCtx, tasks, *sources, *output)
		if err != nil {
			return err
		}
		return printJSON(records)
	case "setup":
		fs := flag.NewFlagSet("setup", flag.ContinueOnError)
		manifest := fs.String("task", "", "task manifest")
		bundle := fs.String("bundle", "", "prepared task bundle")
		workspace := fs.String("workspace", "", "new candidate workspace")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		task, err := validation.LoadTask(*manifest)
		if err != nil {
			return err
		}
		if *bundle == "" || *workspace == "" {
			return errors.New("-bundle and -workspace are required")
		}
		if err := validation.Setup(ctx, task, *bundle, *workspace); err != nil {
			return err
		}
		return printJSON(map[string]any{"task_id": task.ID, "workspace": filepath.Base(*workspace), "status": "ready"})
	case "score":
		fs := flag.NewFlagSet("score", flag.ContinueOnError)
		manifest := fs.String("task", "", "task manifest")
		bundle := fs.String("bundle", "", "prepared task bundle")
		workspace := fs.String("workspace", "", "candidate workspace")
		output := fs.String("output", "", "result JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		task, err := validation.LoadTask(*manifest)
		if err != nil {
			return err
		}
		if *bundle == "" || *workspace == "" || *output == "" {
			return errors.New("-bundle, -workspace, and -output are required")
		}
		result, err := validation.Score(ctx, task, *bundle, *workspace)
		if err != nil {
			return err
		}
		if err := validation.WriteResult(*output, result); err != nil {
			return err
		}
		if err := printJSON(result); err != nil {
			return err
		}
		if result.Status != "pass" {
			return fmt.Errorf("task result is %s", result.Status)
		}
		return nil
	case "qualify":
		fs := flag.NewFlagSet("qualify", flag.ContinueOnError)
		manifest := fs.String("task", "", "task manifest")
		bundle := fs.String("bundle", "", "prepared task bundle")
		source := fs.String("source", "", "local upstream repository root")
		output := fs.String("output", "", "qualification result JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *manifest == "" || *bundle == "" || *source == "" || *output == "" {
			return errors.New("all qualify flags are required")
		}
		task, err := validation.LoadTask(*manifest)
		if err != nil {
			return err
		}
		result, err := validation.Qualify(ctx, task, *bundle, *source)
		if err != nil {
			return err
		}
		if err := validation.WriteQualification(*output, result); err != nil {
			return err
		}
		if err := printJSON(result); err != nil {
			return err
		}
		if result.Status != "pass" {
			return fmt.Errorf("qualification result is %s", result.Status)
		}
		return nil
	case "replay":
		fs := flag.NewFlagSet("replay", flag.ContinueOnError)
		transcriptPath := fs.String("transcript", "", "MCP replay transcript")
		server := fs.String("server", "", "agentic-go server binary")
		workspace := fs.String("workspace", "", "task workspace")
		artifacts := fs.String("artifacts", "", "private response artifact directory")
		output := fs.String("output", "", "replay result JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *transcriptPath == "" || *server == "" || *workspace == "" || *artifacts == "" || *output == "" {
			return errors.New("all replay flags are required")
		}
		transcript, err := validation.LoadTranscript(*transcriptPath)
		if err != nil {
			return err
		}
		result, err := validation.Replay(ctx, transcript, *server, *workspace, *artifacts)
		if err != nil {
			return err
		}
		if err := validation.WriteReplayResult(*output, result); err != nil {
			return err
		}
		if err := printJSON(result); err != nil {
			return err
		}
		if result.Status != "pass" {
			return fmt.Errorf("replay result is %s", result.Status)
		}
		return nil
	case "pilot-validate":
		fs := flag.NewFlagSet("pilot-validate", flag.ContinueOnError)
		root := fs.String("scenarios", "validation/internal/pilot/scenarios", "scenario directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ss, err := pilot.LoadScenarios(*root)
		if err != nil {
			return err
		}
		tasks, err := validation.LoadTasks("validation/v0.8.0/tasks")
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, task := range tasks {
			known[task.ID] = true
		}
		for _, scenario := range ss {
			if !known[scenario.TaskID] {
				return fmt.Errorf("scenario %q references unknown task %q", scenario.ID, scenario.TaskID)
			}
		}
		return printJSON(map[string]any{"schema_version": pilot.Schema, "scenarios": len(ss), "runs": 20, "status": "valid"})
	case "pilot-ingest":
		fs := flag.NewFlagSet("pilot-ingest", flag.ContinueOnError)
		root := fs.String("runs", ".agentic-go-eval/pilot/runs", "run directory")
		out := fs.String("output", ".agentic-go-eval/pilot/ingested.json", "output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		paths, _ := filepath.Glob(filepath.Join(*root, "*.json"))
		sort.Strings(paths)
		scenarios, err := pilot.LoadScenarios("validation/internal/pilot/scenarios")
		if err != nil {
			return err
		}
		scenarioByID := make(map[string]pilot.Scenario, len(scenarios))
		for _, scenario := range scenarios {
			scenarioByID[scenario.ID] = scenario
		}
		runs := make([]pilot.Run, 0, len(paths))
		for _, p := range paths {
			r, e := pilot.LoadRun(p)
			if e != nil {
				return e
			}
			scenario, ok := scenarioByID[r.ScenarioID]
			if !ok {
				return fmt.Errorf("run references unknown scenario %q", r.ScenarioID)
			}
			// Recompute instead of trusting a candidate-provided obligation list.
			r.DecisionObligations = pilot.ScoreObligations(scenario, r)
			runs = append(runs, r)
		}
		if err := pilot.ValidateMatrix(scenarios, runs); err != nil {
			return err
		}
		data, _ := json.MarshalIndent(runs, "", "  ")
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
			return err
		}
		return printJSON(map[string]any{"runs": len(runs), "output": *out, "status": "ingested"})
	case "pilot-report":
		fs := flag.NewFlagSet("pilot-report", flag.ContinueOnError)
		input := fs.String("input", ".agentic-go-eval/pilot/ingested.json", "ingested run JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var runs []pilot.Run
		if err := decodePilot(*input, &runs); err != nil {
			return err
		}
		return printJSON(pilot.Summarize(runs))
	case "pilot-run":
		fs := flag.NewFlagSet("pilot-run", flag.ContinueOnError)
		scenarioID := fs.String("scenario", "", "scenario id")
		condition := fs.String("condition", "baseline", "baseline or focus")
		repetition := fs.Int("repetition", 1, "repetition 1-5")
		sourceRoot := fs.String("source-root", "", "upstream clone root (required)")
		outputRoot := fs.String("output-root", ".agentic-go-eval/pilot", "private output root")
		timeout := fs.Duration("timeout", 30*time.Minute, "agent timeout")
		dryRun := fs.Bool("dry-run", false, "validate and print without running Codex")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*sourceRoot) == "" {
			return errors.New("-source-root is required")
		}
		if *scenarioID == "" {
			return errors.New("-scenario is required")
		}
		ss, err := pilot.LoadScenarios("validation/internal/pilot/scenarios")
		if err != nil {
			return err
		}
		var scenario pilot.Scenario
		for _, s := range ss {
			if s.ID == *scenarioID {
				scenario = s
			}
		}
		if scenario.ID == "" {
			return fmt.Errorf("unknown scenario %q", *scenarioID)
		}
		tasks, err := validation.LoadTasks("validation/v0.8.0/tasks")
		if err != nil {
			return err
		}
		var task validation.Task
		for _, t := range tasks {
			if t.ID == scenario.TaskID {
				task = t
			}
		}
		if task.ID == "" {
			return fmt.Errorf("scenario task %q not found", scenario.TaskID)
		}
		if *condition != "baseline" && *condition != "focus" || *repetition < 1 || *repetition > 5 {
			return errors.New("invalid condition or repetition")
		}
		bundleDir := filepath.Join(*outputRoot, "bundles")
		if mkdirErr := os.MkdirAll(bundleDir, 0o755); mkdirErr != nil {
			return mkdirErr
		}
		bundleGlob, _ := filepath.Glob(filepath.Join(bundleDir, task.ID+"-*.bundle"))
		if len(bundleGlob) == 0 {
			rec, e := validation.Prepare(context.WithoutCancel(ctx), task, filepath.Join(*sourceRoot, task.Repository.Name), bundleDir)
			if e != nil {
				return e
			}
			bundleGlob = []string{filepath.Join(bundleDir, rec.Bundle)}
		}
		if len(bundleGlob) != 1 {
			return fmt.Errorf("expected one bundle, found %d", len(bundleGlob))
		}
		bundlePath, err := filepath.Abs(bundleGlob[0])
		if err != nil {
			return err
		}
		binary := filepath.Join(*outputRoot, "agentic-go")
		if mkdirErr := os.MkdirAll(*outputRoot, 0o755); mkdirErr != nil {
			return mkdirErr
		}
		build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/agentic-go")
		build.Dir = "."
		if out, e := build.CombinedOutput(); e != nil {
			return fmt.Errorf("build agentic-go: %w: %s", e, out)
		}
		binaryHash, err := fileDigest(binary)
		if err != nil {
			return err
		}
		sourceHash, err := commandText(ctx, filepath.Join(*sourceRoot, task.Repository.Name), "git", "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		workspace := filepath.Join(*outputRoot, "checkouts", fmt.Sprintf("%s-%s-r%d", scenario.ID, *condition, *repetition))
		cmdSpec := pilot.CommandSpec{Workspace: workspace, Prompt: scenario.Prompt, Focus: *condition == "focus", Binary: binary}
		if *dryRun {
			cmd, e := pilot.BuildCommand(cmdSpec)
			if e != nil {
				return e
			}
			return printJSON(map[string]any{"scenario": scenario.ID, "condition": *condition, "repetition": *repetition, "workspace": workspace, "command": cmd.Args, "status": "dry-run"})
		}
		if err := validation.Setup(ctx, task, bundlePath, workspace); err != nil {
			return err
		}
		if *condition == "focus" {
			preflightCtx, preflightCancel := context.WithTimeout(ctx, 30*time.Second)
			preflightErr := pilot.PreflightFocus(preflightCtx, binary, workspace)
			preflightCancel()
			if preflightErr != nil {
				return fmt.Errorf("focus capability delivery failed: %w", preflightErr)
			}
		}
		runCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		events, stdout, stderr, duration, e := pilot.RunOnce(runCtx, cmdSpec)
		result, e2 := validation.Score(ctx, task, bundlePath, workspace)
		if e == nil {
			e = e2
		}
		data, _ := json.Marshal(events.Raw)
		fm := pilot.FocusMetrics(events)
		focusCalls := fm.Calls
		delivery := ""
		if *condition == "focus" {
			delivery = "healthy"
			if focusCalls == 0 {
				result.Uncertainties = append(result.Uncertainties, "focus capability delivered but agent made zero go_context calls")
			}
		}
		r := pilot.Run{SchemaVersion: pilot.Schema, ScenarioID: scenario.ID, TaskID: task.ID, Condition: *condition, Repetition: *repetition, Model: "gpt-5.6-luna", Reasoning: "max", Prompt: scenario.Prompt, Transcript: pilot.SanitizeTranscript(events.Raw), ToolCalls: events.ToolCalls, FocusToolCalls: focusCalls, FocusFailedCalls: fm.FailedCalls, FocusErrorCategories: fm.ErrorCategories, FirstFocusCallPosition: fm.FirstPosition, RefreshUse: fm.Refresh, FocusEvidenceUse: fm.Evidence, FocusResultFollowedByEdit: fm.FocusResultFollowedByEdit, RefreshCompleted: fm.RefreshCompleted, FocusDelivery: delivery, DurationMS: duration, EvidenceBytes: int64(len(data)), Acceptance: result.Status, Qualifying: result.Status == "pass", ScopeViolations: result.UnexpectedPaths, Uncertainty: result.Uncertainties}
		data, _ = json.Marshal(r.Transcript)
		r.SourceSHA256 = sourceHash
		r.BinarySHA256 = binaryHash
		qualification, qerr := validation.Qualify(ctx, task, bundlePath, filepath.Join(*sourceRoot, task.Repository.Name))
		if qerr != nil {
			r.Qualifying = false
			r.Uncertainty = append(r.Uncertainty, qerr.Error())
		} else if qualification.Status != "pass" {
			r.Qualifying = false
			r.Uncertainty = append(r.Uncertainty, qualification.Failures...)
		}
		r.Patch, _ = commandText(ctx, workspace, "git", "diff", "HEAD")
		r.Stderr = pilot.SanitizeText(stderr)
		if e != nil {
			r.ProcessError = pilot.SanitizeText(e.Error())
			r.Uncertainty = append(r.Uncertainty, r.ProcessError)
		}
		r.PatchSHA256 = pilot.DigestString(r.Patch)
		r.WorkspaceSHA256, _ = pilot.WorkspaceDigest(workspace)
		acceptanceEvidence, _ := json.Marshal(result)
		r.AcceptanceEvidenceSHA256 = pilot.DigestString(string(acceptanceEvidence))
		r.DecisionObligations = pilot.ScoreObligations(scenario, r)
		r.TranscriptSHA256 = sha256Bytes(data)
		out := filepath.Join(*outputRoot, "runs", fmt.Sprintf("%s-%s-r%d.json", scenario.ID, *condition, *repetition))
		if err := writePilotRun(out, r); err != nil {
			return err
		}
		_ = stdout
		if e != nil {
			return e
		}
		return printJSON(r)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func runAdoption(ctx context.Context, s adoption.Scenario, arm string, repetition int, sourceRoot, outputRoot string, timeout time.Duration, skillRoot string) error {
	sourceRoot, outputRoot, err := resolveAdoptionRoots(sourceRoot, outputRoot)
	if err != nil {
		return err
	}
	var skill adoption.Skill
	if arm == adoption.ArmIntegrated {
		skill, err = adoption.LoadSkill(skillRoot)
		if err != nil {
			return err
		}
	}
	tasks, err := validation.LoadTasks("validation/v0.8.0/tasks")
	if err != nil {
		return err
	}
	var task validation.Task
	for _, t := range tasks {
		if t.ID == s.TaskID {
			task = t
		}
	}
	if task.ID == "" {
		return fmt.Errorf("task %q not found", s.TaskID)
	}
	if mkErr := os.MkdirAll(filepath.Join(outputRoot, "bundles"), 0o755); mkErr != nil {
		return mkErr
	}
	bundleDir := filepath.Join(outputRoot, "bundles")
	paths, _ := filepath.Glob(filepath.Join(bundleDir, task.ID+"-*.bundle"))
	var bundle string
	if len(paths) == 1 {
		bundle = paths[0]
	} else {
		rec, e := validation.Prepare(context.WithoutCancel(ctx), task, filepath.Join(sourceRoot, task.Repository.Name), bundleDir)
		if e != nil {
			return e
		}
		bundle = filepath.Join(bundleDir, rec.Bundle)
	}
	binary := filepath.Join(outputRoot, "agentic-go")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/agentic-go")
	build.Dir = "."
	if out, e := build.CombinedOutput(); e != nil {
		return fmt.Errorf("build agentic-go: %w: %s", e, out)
	}
	bh, err := fileDigest(binary)
	if err != nil {
		return err
	}
	sh, err := commandText(ctx, filepath.Join(sourceRoot, task.Repository.Name), "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	workspace := filepath.Join(outputRoot, "checkouts", fmt.Sprintf("%s-%s-r%d", s.ID, arm, repetition))
	if err = validation.Setup(ctx, task, bundle, workspace); err != nil {
		return err
	}
	initial, err := adoptionWorkspaceDigest(workspace)
	if err != nil {
		return err
	}
	p := s.Prompt
	if arm == adoption.ArmGuidance {
		p += " " + adoption.Guidance
	}
	focus := arm != "baseline"
	if focus {
		pc, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = pilot.PreflightFocus(pc, binary, workspace)
		cancel()
		if err != nil {
			return err
		}
	}
	codexHome := ""
	if arm == adoption.ArmIntegrated {
		codexHome, err = os.MkdirTemp("", "agentic-go-adoption-codex-home-")
		if err != nil {
			return fmt.Errorf("create isolated Codex home: %w", err)
		}
		defer func() { _ = os.RemoveAll(codexHome) }()
		if _, err = skill.Install(codexHome); err != nil {
			return err
		}
		if err = copyCodexAuth(defaultCodexHome(), codexHome); err != nil {
			return err
		}
	}
	start := time.Now()
	runCtx, runCancel := contextWithTimeout(ctx, timeout)
	events, stdout, stderr, dur, runErr := pilot.RunOnce(runCtx, pilot.CommandSpec{Binary: binary, Workspace: workspace, Prompt: p, Focus: focus, CodexHome: codexHome})
	runCancel()
	_ = stdout
	result, scoreErr := validation.Score(ctx, task, bundle, workspace)
	if runErr == nil {
		runErr = scoreErr
	}
	tr := pilot.SanitizeTranscript(events.Raw)
	raw, _ := json.Marshal(tr)
	patch, _ := commandText(ctx, workspace, "git", "diff", "HEAD")
	post, _ := adoptionWorkspaceDigest(workspace)
	fm := pilot.FocusMetrics(events)
	focusCalls := fm.Calls
	skillDiscovered := pilot.SkillDiscoveryEvidence(events, adoption.SkillName)
	unc := append([]string{}, result.Uncertainties...)
	if focus && focusCalls == 0 {
		unc = append(unc, "focus capability delivered but agent made zero go_context calls")
	}
	if arm == adoption.ArmIntegrated && !skillDiscovered {
		unc = append(unc, "integrated skill was injected but transcript contains no skill discovery evidence")
	}
	if runErr != nil {
		unc = append(unc, runErr.Error())
	}
	acc, _ := json.Marshal(result)
	mcpInstructionsDigest := ""
	if focus {
		mcpInstructionsDigest = adoption.DigestString(tools.ServerInstructions)
	}
	r := adoption.Run{SchemaVersion: adoption.Schema, ScenarioID: s.ID, TaskID: task.ID, Arm: arm, Repetition: repetition, Model: "gpt-5.6-luna", Reasoning: "max", Prompt: p, PromptSHA256: adoption.DigestString(p), MCPDescriptionSHA256: mcpInstructionsDigest, SkillSHA256: skill.Digest, EffectiveInstructionSurface: adoptionInstructionSurface(arm), SourceSHA256: sh, BinarySHA256: bh, InitialWorkspaceSHA256: initial, PostWorkspaceSHA256: post, WorkspaceSHA256: post, Transcript: tr, TranscriptSHA256: adoption.DigestString(string(raw)), Patch: patch, PatchSHA256: adoption.DigestString(patch), AcceptanceEvidenceSHA256: adoption.DigestString(string(acc)), Acceptance: result.Status, ScopeViolations: result.UnexpectedPaths, EvidenceBytes: int64(len(raw)), ToolCalls: events.ToolCalls, FocusToolCalls: focusCalls, FocusFailedCalls: fm.FailedCalls, FocusErrorCategories: fm.ErrorCategories, FirstFocusCallPosition: fm.FirstPosition, RefreshUse: fm.Refresh, FocusEvidenceUse: fm.Evidence, FocusResultFollowedByEdit: fm.FocusResultFollowedByEdit, RefreshCompleted: fm.RefreshCompleted, SkillDiscovered: skillDiscovered, DurationMS: dur, OperatorIntervention: false, Uncertainty: unc, Qualifying: result.Status == "pass", FocusDelivery: map[bool]string{true: "healthy", false: ""}[focus]}
	r.Stderr = pilot.SanitizeText(stderr)
	if q, e := validation.Qualify(ctx, task, bundle, filepath.Join(sourceRoot, task.Repository.Name)); e != nil {
		r.Qualifying = false
		r.Uncertainty = append(r.Uncertainty, e.Error())
	} else if q.Status != "pass" {
		r.Qualifying = false
		r.Uncertainty = append(r.Uncertainty, q.Failures...)
	}
	_ = start
	out := filepath.Join(outputRoot, "runs", fmt.Sprintf("%s-%s-r%d.json", s.ID, arm, repetition))
	data, _ := json.MarshalIndent(r, "", "  ")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	if arm == adoption.ArmIntegrated && !skillDiscovered {
		return errors.New("integrated adoption run did not produce skill discovery evidence")
	}
	return printJSON(r)
}

func adoptionInstructionSurface(arm string) string {
	switch arm {
	case adoption.ArmIntegrated:
		return adoption.IntegratedInstructionSurface
	case adoption.ArmDiscoverability:
		return "mcp-server-instructions"
	case adoption.ArmGuidance:
		return "mcp-server-instructions+prompt-guidance"
	default:
		return "none"
	}
}

func defaultCodexHome() string {
	if configured := os.Getenv("CODEX_HOME"); configured != "" {
		return configured
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func copyCodexAuth(sourceHome, destinationHome string) error {
	if sourceHome == "" || destinationHome == "" {
		return nil
	}
	source, err := os.ReadFile(filepath.Join(sourceHome, "auth.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Codex auth: %w", err)
	}
	if err := os.WriteFile(filepath.Join(destinationHome, "auth.json"), source, 0o600); err != nil {
		return fmt.Errorf("copy Codex auth: %w", err)
	}
	return nil
}

func resolveAdoptionRoots(sourceRoot, outputRoot string) (string, string, error) {
	source, err := filepath.Abs(sourceRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve source root: %w", err)
	}
	output, err := filepath.Abs(outputRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve output root: %w", err)
	}
	return source, output, nil
}

func contextWithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
func adoptionWorkspaceDigest(root string) (string, error) { return pilot.WorkspaceDigest(root) }

func decodePilot(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

func sha256Bytes(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
func fileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Bytes(b), nil
}

func writePilotRun(path string, r pilot.Run) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func commandText(ctx context.Context, dir string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	return strings.TrimSpace(string(b)), e
}
