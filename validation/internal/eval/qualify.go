package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const QualificationSchema = "agentic.eval.qualification/v1alpha1"

const scopeProbePath = ".agentic-go-eval-scope-probe"

type Qualification struct {
	SchemaVersion string   `json:"schema_version"`
	TaskID        string   `json:"task_id"`
	Status        string   `json:"status"`
	BundleSHA256  string   `json:"bundle_sha256"`
	NoOp          Result   `json:"no_op"`
	Reference     Result   `json:"reference"`
	ScopeProbe    Result   `json:"scope_probe"`
	DurationMS    int64    `json:"duration_ms"`
	Failures      []string `json:"failures"`
}

func Qualify(ctx context.Context, task Task, bundle, source string) (Qualification, error) {
	started := time.Now()
	qualification := Qualification{
		SchemaVersion: QualificationSchema, TaskID: task.ID, Status: "pass", Failures: []string{},
	}
	digest, err := fileSHA256(bundle)
	if err != nil {
		return qualification, err
	}
	qualification.BundleSHA256 = digest
	for _, allowed := range task.Scope.AllowedPaths {
		if allowed == scopeProbePath {
			return qualification, errors.New("task scope reserves the qualification probe path")
		}
	}
	root, err := os.MkdirTemp("", "agentic-go-eval-qualify-")
	if err != nil {
		return qualification, err
	}
	defer func() { _ = os.RemoveAll(root) }()
	workspace := filepath.Join(root, "workspace")
	if err := Setup(ctx, task, bundle, workspace); err != nil {
		return qualification, err
	}
	qualification.NoOp, err = Score(ctx, task, bundle, workspace)
	if err != nil {
		return qualification, err
	}
	if !oracleDiscriminates(qualification.NoOp) {
		qualification.Failures = append(qualification.Failures, "oracle does not fail on the fixture base")
	}
	if err := applyReference(ctx, task, source, workspace); err != nil {
		return qualification, err
	}
	qualification.Reference, err = Score(ctx, task, bundle, workspace)
	if err != nil {
		return qualification, err
	}
	if qualification.Reference.Status != "pass" {
		qualification.Failures = append(qualification.Failures, "historical reference change does not pass")
	}
	probe := filepath.Join(workspace, scopeProbePath)
	if err := os.WriteFile(probe, []byte("qualification scope probe\n"), 0o600); err != nil {
		return qualification, err
	}
	qualification.ScopeProbe, err = Score(ctx, task, bundle, workspace)
	if err != nil {
		return qualification, err
	}
	if !scopeProbeDiscriminates(qualification.ScopeProbe) {
		qualification.Failures = append(qualification.Failures, "scope probe did not fail independently of behavior")
	}
	if len(qualification.Failures) > 0 {
		qualification.Status = "fail"
	}
	qualification.DurationMS = time.Since(started).Milliseconds()
	return qualification, nil
}

func WriteQualification(path string, qualification Qualification) error {
	data, err := json.MarshalIndent(qualification, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o644)
}

func oracleDiscriminates(result Result) bool {
	if result.Status != "fail" || len(result.Commands) == 0 {
		return false
	}
	failed := false
	for _, command := range result.Commands {
		if command.Status == "incomplete" {
			return false
		}
		if command.Status == "fail" {
			failed = true
		}
	}
	return failed
}

func scopeProbeDiscriminates(result Result) bool {
	if result.Status != "fail" || len(result.UnexpectedPaths) != 1 || result.UnexpectedPaths[0] != scopeProbePath {
		return false
	}
	for _, command := range result.Commands {
		if command.Status != "pass" {
			return false
		}
	}
	return true
}

func applyReference(ctx context.Context, task Task, source, workspace string) error {
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	parent, err := commandText(ctx, "", nil, "git", "-C", root, "rev-parse", task.Repository.Target+"^")
	if err != nil || parent != task.Repository.Base {
		return errors.New("source repository does not contain the adjacent task commits")
	}
	args := []string{"diff", "--binary", task.Repository.Base, task.Repository.Target, "--", "."}
	for _, oracle := range task.Oracle.OverlayPaths {
		args = append(args, ":(exclude)"+oracle)
	}
	patch, err := commandBytes(ctx, root, "git", args...)
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return errors.New("historical reference has no non-oracle change")
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--binary", "--whitespace=nowarn", "-")
	cmd.Dir = workspace
	cmd.Stdin = bytes.NewReader(patch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("apply historical reference: %w: %s", err, output)
	}
	return nil
}
