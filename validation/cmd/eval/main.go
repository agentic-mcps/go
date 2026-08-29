package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	validation "github.com/ashwingopalsamy/agentic-go/validation/internal/eval"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agentic-go-eval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("expected validate, prepare, setup, score, or replay")
	}
	switch args[0] {
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
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		records, err := validation.PrepareAll(ctx, tasks, *sources, *output)
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
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
