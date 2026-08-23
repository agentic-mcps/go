package tools

import (
	"context"
	"fmt"
	"io"

	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/parser"
)

func (r *Runtime) runTestJSON(ctx context.Context, arguments []string, consume func(parser.TestEvent) error) (execution.Result, error) {
	reader, writer := io.Pipe()
	parsed := make(chan error, 1)
	go func() {
		defer func() { _ = reader.Close() }()
		_, parseErr := parser.DecodeTestJSON(reader, consume)
		parsed <- parseErr
	}()

	result, runErr := r.runner.Run(ctx, execution.Command{
		Name: "go",
		Args: arguments,
		Env:  map[string]string{"GOWORK": "auto"},
	}, execution.Streams{Stdout: writer})
	_ = writer.CloseWithError(runErr)
	parseErr := <-parsed

	if runErr != nil {
		return execution.Result{}, fmt.Errorf("running go test -json: %w", runErr)
	}
	if parseErr != nil {
		return execution.Result{}, fmt.Errorf("parsing go test -json output (exit %d): %w", result.ExitCode, parseErr)
	}
	return result, nil
}
