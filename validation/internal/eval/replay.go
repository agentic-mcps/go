package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Transcript defines a deterministic sequence of MCP tool calls.
type Transcript struct {
	SchemaVersion string      `json:"schema_version"`
	TaskID        string      `json:"task_id"`
	Operations    []Operation `json:"operations"`
}

// Operation defines one expected MCP tool call and transport outcome.
type Operation struct {
	Arguments   map[string]any `json:"arguments"`
	Tool        string         `json:"tool"`
	ExpectError bool           `json:"expect_error"`
}

// ReplayResult records a complete deterministic transcript replay.
type ReplayResult struct {
	SchemaVersion string       `json:"schema_version"`
	TaskID        string       `json:"task_id"`
	Status        string       `json:"status"`
	ServerSHA256  string       `json:"server_sha256"`
	Calls         []ReplayCall `json:"calls"`
	Uncertainties []string     `json:"uncertainties"`
	Measurements  Measurements `json:"measurements"`
}

// ReplayCall records one MCP operation and its bounded result artifact.
type ReplayCall struct {
	Tool         string `json:"tool"`
	Status       string `json:"status"`
	ResultSHA256 string `json:"result_sha256"`
	Artifact     string `json:"artifact,omitempty"`
	Error        string `json:"error,omitempty"`
	Sequence     int    `json:"sequence"`
	DurationMS   int64  `json:"duration_ms"`
	ResultBytes  int64  `json:"result_bytes"`
}

// LoadTranscript strictly decodes and validates one replay transcript.
func LoadTranscript(path string) (Transcript, error) {
	var transcript Transcript
	if err := decodeStrict(path, &transcript); err != nil {
		return transcript, err
	}
	if transcript.SchemaVersion != TranscriptSchema || !idPattern.MatchString(transcript.TaskID) || len(transcript.Operations) == 0 {
		return transcript, errors.New("invalid replay transcript header")
	}
	for i, operation := range transcript.Operations {
		if operation.Tool == "" || operation.Arguments == nil {
			return transcript, fmt.Errorf("operation %d has an empty tool or null arguments", i+1)
		}
	}
	return transcript, nil
}

// Replay runs one checked-in transcript against a stdio MCP server.
func Replay(ctx context.Context, transcript Transcript, server, workspace, artifacts string) (ReplayResult, error) {
	started := time.Now()
	result := ReplayResult{
		SchemaVersion: ReplaySchema, TaskID: transcript.TaskID, Status: "pass",
		Calls: []ReplayCall{}, Measurements: Measurements{}, Uncertainties: []string{},
	}
	serverPath, err := filepath.Abs(server)
	if err != nil {
		return result, err
	}
	serverPath, err = filepath.EvalSymlinks(serverPath)
	if err != nil {
		return result, err
	}
	result.ServerSHA256, err = fileSHA256(serverPath)
	if err != nil {
		return result, err
	}
	workspacePath, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return result, err
	}
	if mkdirErr := os.MkdirAll(artifacts, 0o755); mkdirErr != nil {
		return result, mkdirErr
	}
	command := exec.Command(serverPath)
	command.Dir = workspacePath
	goPath, err := exec.LookPath("go")
	if err != nil {
		return result, fmt.Errorf("resolve Go toolchain: %w", err)
	}
	command.Env = controlledEnvironment(goPath, os.Environ())
	client := mcp.NewClient(&mcp.Implementation{Name: "agentic-go-eval", Version: "v0.8"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		return result, fmt.Errorf("connect MCP server: %w", err)
	}
	defer func() { _ = session.Close() }()
	for i, operation := range transcript.Operations {
		call := ReplayCall{Sequence: i + 1, Tool: operation.Tool, Status: "pass"}
		callStarted := time.Now()
		response, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: operation.Tool, Arguments: operation.Arguments})
		call.DurationMS = time.Since(callStarted).Milliseconds()
		if callErr != nil {
			call.Error = sanitizeReplayError(callErr.Error(), workspacePath)
			if !operation.ExpectError {
				call.Status = "fail"
				result.Status = "fail"
			}
			result.Calls = append(result.Calls, call)
			if ctx.Err() != nil {
				result.Status = "incomplete"
				break
			}
			continue
		}
		if operation.ExpectError {
			call.Status = "fail"
			call.Error = "expected a protocol error but the call returned a result"
			result.Status = "fail"
		}
		data, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			return result, marshalErr
		}
		call.ResultBytes = int64(len(data))
		digest := sha256.Sum256(data)
		call.ResultSHA256 = hex.EncodeToString(digest[:])
		if bytesContainPath(data, workspacePath) {
			call.Status = "fail"
			call.Error = "MCP result contains the absolute workspace path"
			result.Status = "fail"
		} else {
			call.Artifact = call.ResultSHA256 + ".json"
			if writeErr := installArtifact(filepath.Join(artifacts, call.Artifact), data); writeErr != nil {
				return result, writeErr
			}
		}
		result.Measurements.ResultBytes += call.ResultBytes
		result.Calls = append(result.Calls, call)
	}
	result.Measurements.ToolCalls = len(result.Calls)
	result.Measurements.DurationMS = time.Since(started).Milliseconds()
	return result, nil
}

// WriteReplayResult atomically writes one replay summary.
func WriteReplayResult(path string, result ReplayResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o644)
}

func installArtifact(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != string(data) {
			return fmt.Errorf("content-addressed artifact collision: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

func bytesContainPath(data []byte, path string) bool {
	return strings.Contains(string(data), path) || strings.Contains(string(data), filepath.ToSlash(path))
}

func sanitizeReplayError(message, workspace string) string {
	return strings.ReplaceAll(message, workspace, "$WORKSPACE")
}
