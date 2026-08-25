package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunMCPConfigPrintsClientNativeShapes(t *testing.T) {
	dependencies := mcpConfigDependencies{
		executable:       func() (string, error) { return "/opt/agentic-go", nil },
		resolveWorkspace: func(string) (string, error) { return "/repo", nil },
	}
	for _, client := range []string{"generic", "claude"} {
		t.Run(client, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exit := runMCPConfig([]string{"--client", client, "--workspace", "/repo"}, &stdout, &stderr, dependencies); exit != 0 {
				t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("decode %s configuration: %v\n%s", client, err, stdout.String())
			}
			if !strings.Contains(stdout.String(), "/opt/agentic-go") || !strings.Contains(stdout.String(), "/repo") {
				t.Fatalf("configuration = %q", stdout.String())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	if exit := runMCPConfig([]string{"--client", "codex", "--workspace", "/repo"}, &stdout, &stderr, dependencies); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	for _, want := range []string{"[mcp_servers.agentic-go]", `command = "/opt/agentic-go"`, `args = ["--workspace", "/repo"]`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("Codex configuration %q does not contain %q", stdout.String(), want)
		}
	}
}

func TestRunMCPConfigRequiresKnownClient(t *testing.T) {
	dependencies := mcpConfigDependencies{
		executable:       func() (string, error) { return "/opt/agentic-go", nil },
		resolveWorkspace: func(string) (string, error) { return "/repo", nil },
	}
	for _, args := range [][]string{nil, {"--client", "cursor"}} {
		var stdout, stderr bytes.Buffer
		if exit := runMCPConfig(args, &stdout, &stderr, dependencies); exit != 2 {
			t.Fatalf("args=%v exit=%d, want 2", args, exit)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "--client") {
			t.Fatalf("args=%v stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}
