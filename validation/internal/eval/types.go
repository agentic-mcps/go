package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// TaskSchema identifies the task manifest contract.
	TaskSchema = "agentic.eval.task/v1alpha1"
	// ResultSchema identifies the candidate score contract.
	ResultSchema = "agentic.eval.result/v1alpha1"
	// TranscriptSchema identifies the MCP replay input contract.
	TranscriptSchema = "agentic.eval.transcript/v1alpha1"
	// ReplaySchema identifies the MCP replay result contract.
	ReplaySchema = "agentic.eval.replay/v1alpha1"
)

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Task defines one pinned historical change and its acceptance contract.
type Task struct {
	SchemaVersion string     `json:"schema_version"`
	ID            string     `json:"id"`
	Repository    Repository `json:"repository"`
	Prompt        string     `json:"prompt"`
	Scope         Scope      `json:"scope"`
	Oracle        Oracle     `json:"oracle"`
	Acceptance    []Command  `json:"acceptance"`
	Invariants    []string   `json:"invariants"`
	Exercises     []string   `json:"exercises"`
	Pilot         bool       `json:"pilot"`
}

// Repository identifies the adjacent upstream commits used by a task.
type Repository struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Base   string `json:"base"`
	Target string `json:"target"`
}

// Scope defines the candidate workspace, packages, and permitted paths.
type Scope struct {
	Workspace    string   `json:"workspace"`
	Packages     []string `json:"packages"`
	AllowedPaths []string `json:"allowed_paths"`
}

// Oracle defines upstream files overlaid only inside the scoring clone.
type Oracle struct {
	OverlayPaths []string `json:"overlay_paths"`
}

// Command defines one bounded argv-based acceptance command.
type Command struct {
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// LoadTask strictly decodes and validates one task manifest.
func LoadTask(path string) (Task, error) {
	var task Task
	if err := decodeStrict(path, &task); err != nil {
		return task, err
	}
	if err := task.Validate(); err != nil {
		return task, fmt.Errorf("%s: %w", path, err)
	}
	return task, nil
}

// LoadTasks validates the complete eight-task corpus and its transcripts.
func LoadTasks(root string) ([]Task, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "task.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no task manifests under %s", root)
	}
	sort.Strings(paths)
	tasks := make([]Task, 0, len(paths))
	ids := make(map[string]struct{}, len(paths))
	pilots := 0
	for _, path := range paths {
		task, loadErr := LoadTask(path)
		if loadErr != nil {
			return nil, loadErr
		}
		if _, duplicate := ids[task.ID]; duplicate {
			return nil, fmt.Errorf("duplicate task id %q", task.ID)
		}
		ids[task.ID] = struct{}{}
		if task.Pilot {
			pilots++
		}
		transcript, transcriptErr := LoadTranscript(filepath.Join(root, task.ID, "replay.json"))
		if transcriptErr != nil {
			return nil, fmt.Errorf("load replay transcript for %s: %w", task.ID, transcriptErr)
		}
		if transcript.TaskID != task.ID {
			return nil, fmt.Errorf("replay transcript task %q does not match manifest %q", transcript.TaskID, task.ID)
		}
		tasks = append(tasks, task)
	}
	if len(tasks) != 8 {
		return nil, fmt.Errorf("v0.8 corpus requires exactly 8 tasks, found %d", len(tasks))
	}
	if pilots != 2 {
		return nil, fmt.Errorf("v0.8 corpus requires exactly 2 pilot tasks, found %d", pilots)
	}
	return tasks, nil
}

// Validate checks one task's portable and safety-critical invariants.
func (t Task) Validate() error {
	if t.SchemaVersion != TaskSchema {
		return fmt.Errorf("schema_version must be %q", TaskSchema)
	}
	if !idPattern.MatchString(t.ID) {
		return fmt.Errorf("invalid task id %q", t.ID)
	}
	if t.Repository.Name == "" || filepath.Base(t.Repository.Name) != t.Repository.Name {
		return errors.New("repository name must be one path segment")
	}
	if !strings.HasPrefix(t.Repository.URL, "https://github.com/") {
		return errors.New("repository URL must use https://github.com/")
	}
	if !shaPattern.MatchString(t.Repository.Base) || !shaPattern.MatchString(t.Repository.Target) || t.Repository.Base == t.Repository.Target {
		return errors.New("repository base and target must be distinct full lowercase commit IDs")
	}
	if len(strings.TrimSpace(t.Prompt)) < 20 {
		return errors.New("prompt is too short")
	}
	if t.Scope.Workspace != "." {
		return errors.New("v0.8 task workspace must be current repository root")
	}
	if len(t.Scope.Packages) == 0 || len(t.Scope.AllowedPaths) == 0 {
		return errors.New("scope packages and allowed_paths must be non-empty")
	}
	if len(t.Oracle.OverlayPaths) == 0 || len(t.Acceptance) == 0 || len(t.Invariants) == 0 || len(t.Exercises) == 0 {
		return errors.New("oracle, acceptance, invariants, and exercises must be non-empty")
	}
	for _, path := range append(append([]string(nil), t.Scope.AllowedPaths...), t.Oracle.OverlayPaths...) {
		if err := validateRelativePath(path); err != nil {
			return err
		}
	}
	allowed := make(map[string]struct{}, len(t.Scope.AllowedPaths))
	for _, path := range t.Scope.AllowedPaths {
		if _, exists := allowed[path]; exists {
			return fmt.Errorf("duplicate allowed path %q", path)
		}
		allowed[path] = struct{}{}
	}
	for _, path := range t.Oracle.OverlayPaths {
		if _, ok := allowed[path]; !ok {
			return fmt.Errorf("oracle path %q is outside allowed paths", path)
		}
	}
	for _, command := range t.Acceptance {
		if len(command.Argv) == 0 || command.Argv[0] != "go" {
			return errors.New("acceptance commands must invoke go directly")
		}
		if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 900 {
			return errors.New("acceptance timeout must be between 1 and 900 seconds")
		}
		for _, arg := range command.Argv {
			if arg == "" || strings.ContainsRune(arg, '\x00') {
				return errors.New("acceptance argv contains an empty or invalid argument")
			}
		}
	}
	return nil
}

func validateRelativePath(path string) error {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || strings.HasPrefix(path, "../") || strings.Contains(path, `\`) {
		return fmt.Errorf("unsafe workspace-relative path %q", path)
	}
	return nil
}

func decodeStrict(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("manifest must contain one JSON value")
	}
	return nil
}
