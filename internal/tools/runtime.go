package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/trace"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Runtime owns the shared, process-wide dependencies used by MCP tools.
type Runtime struct {
	workspace *workspace.Workspace
	runner    *execution.Runner
	tracer    *trace.Tracer
}

// NewRuntime validates the dependencies shared by every tool registration.
func NewRuntime(ws *workspace.Workspace, runner *execution.Runner, tracer *trace.Tracer) (*Runtime, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("execution runner is nil")
	}
	if tracer == nil {
		return nil, fmt.Errorf("tracer is nil")
	}
	return &Runtime{workspace: ws, runner: runner, tracer: tracer}, nil
}

// RegisterAll is the single deterministic v0.1 tool registration list.
func RegisterAll(server *mcp.Server, runtime *Runtime) {
	RegisterTestStructured(server, runtime)
}

func (r *Runtime) resolvePackage(ctx context.Context, pattern string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("package is empty")
	}
	if strings.TrimSpace(pattern) != pattern {
		return "", fmt.Errorf("package must not have surrounding whitespace")
	}
	if strings.HasPrefix(pattern, "-") || strings.ContainsRune(pattern, '\x00') {
		return "", fmt.Errorf("package %q is invalid", pattern)
	}

	var stdout, stderr bytes.Buffer
	result, err := r.runner.Run(ctx, execution.Command{
		Name: "go",
		Args: []string{"list", "-json", "-mod=readonly", pattern},
		Env:  map[string]string{"GOWORK": "auto"},
	}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return "", fmt.Errorf("listing package: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("listing package: go list exited %d: %s", result.ExitCode, boundedMessage(stderr.String()))
	}

	decoder := json.NewDecoder(&stdout)
	packages := 0
	for {
		var listed struct {
			Dir string
		}
		if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return "", fmt.Errorf("decoding go list output: %w", err)
		}
		if listed.Dir == "" {
			return "", fmt.Errorf("go list returned a package without a directory")
		}
		if _, err := r.workspace.Resolve(listed.Dir); err != nil {
			return "", fmt.Errorf("package %q is outside the configured workspace: %w", pattern, err)
		}
		packages++
	}
	if packages == 0 {
		return "", fmt.Errorf("package %q matched no packages", pattern)
	}
	return pattern, nil
}

func boundedMessage(value string) string {
	value = strings.TrimSpace(value)
	const limit = 512
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func boolPtr(value bool) *bool { return &value }
