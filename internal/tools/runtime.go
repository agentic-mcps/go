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
	RegisterRaceReport(server, runtime)
	RegisterCoverageGaps(server, runtime)
}

func (r *Runtime) resolvePackage(ctx context.Context, pattern string) (string, error) {
	selection, err := r.resolvePackages(ctx, pattern)
	if err != nil {
		return "", err
	}
	return selection.Pattern, nil
}

type packageSelection struct {
	Pattern  string
	Packages []packageMatch
}

type packageMatch struct {
	ImportPath string
	Dir        string
}

func (r *Runtime) resolvePackages(ctx context.Context, pattern string) (packageSelection, error) {
	if pattern == "" {
		return packageSelection{}, fmt.Errorf("package is empty")
	}
	if strings.TrimSpace(pattern) != pattern {
		return packageSelection{}, fmt.Errorf("package must not have surrounding whitespace")
	}
	if strings.HasPrefix(pattern, "-") || strings.ContainsRune(pattern, '\x00') {
		return packageSelection{}, fmt.Errorf("package %q is invalid", pattern)
	}

	var stdout, stderr bytes.Buffer
	result, err := r.runner.Run(ctx, execution.Command{
		Name: "go",
		Args: []string{"list", "-json", "-mod=readonly", pattern},
		Env:  map[string]string{"GOWORK": "auto"},
	}, execution.Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return packageSelection{}, fmt.Errorf("listing package: %w", err)
	}
	if result.ExitCode != 0 {
		return packageSelection{}, fmt.Errorf("listing package: go list exited %d: %s", result.ExitCode, boundedMessage(stderr.String()))
	}

	decoder := json.NewDecoder(&stdout)
	selection := packageSelection{Pattern: pattern, Packages: make([]packageMatch, 0)}
	for {
		var listed struct {
			ImportPath string
			Dir        string
		}
		if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return packageSelection{}, fmt.Errorf("decoding go list output: %w", err)
		}
		if listed.ImportPath == "" || listed.Dir == "" {
			return packageSelection{}, fmt.Errorf("go list returned incomplete package metadata")
		}
		resolved, err := r.workspace.Resolve(listed.Dir)
		if err != nil {
			return packageSelection{}, fmt.Errorf("package %q is outside the configured workspace: %w", pattern, err)
		}
		selection.Packages = append(selection.Packages, packageMatch{ImportPath: listed.ImportPath, Dir: resolved})
	}
	if len(selection.Packages) == 0 {
		return packageSelection{}, fmt.Errorf("package %q matched no packages", pattern)
	}
	return selection, nil
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
