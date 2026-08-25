package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type mcpConfigDependencies struct {
	executable       func() (string, error)
	resolveWorkspace func(string) (string, error)
}

func defaultMCPConfigDependencies() mcpConfigDependencies {
	return mcpConfigDependencies{
		executable: func() (string, error) {
			path, err := os.Executable()
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(path)
		},
		resolveWorkspace: resolveConfigWorkspace,
	}
}

func runMCPConfig(args []string, stdout, stderr io.Writer, dependencies mcpConfigDependencies) int {
	flags := flag.NewFlagSet("agentic-go mcp-config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	client := flags.String("client", "", "MCP client: generic, codex, or claude")
	workspacePath := flags.String("workspace", ".", "Go workspace root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "agentic-go mcp-config: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if *client != "generic" && *client != "codex" && *client != "claude" {
		_, _ = fmt.Fprintf(stderr, "agentic-go mcp-config: invalid --client %q (want generic, codex, or claude)\n", *client)
		return 2
	}
	executable, err := dependencies.executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go mcp-config: locating executable: %v\n", err)
		return 1
	}
	workspaceRoot, err := dependencies.resolveWorkspace(*workspacePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go mcp-config: resolving workspace: %v\n", err)
		return 1
	}
	arguments := []string{"--workspace", workspaceRoot}

	switch *client {
	case "codex":
		_, err = fmt.Fprintf(stdout, "[mcp_servers.agentic-go]\nenabled = true\ncommand = %s\nargs = [%s, %s]\nstartup_timeout_sec = 30\n",
			strconv.Quote(executable), strconv.Quote(arguments[0]), strconv.Quote(arguments[1]))
	case "generic":
		err = writeMCPConfigJSON(stdout, map[string]any{
			"name": "agentic-go", "transport": "stdio", "command": executable, "args": arguments,
		})
	case "claude":
		err = writeMCPConfigJSON(stdout, map[string]any{
			"mcpServers": map[string]any{
				"agentic-go": map[string]any{"command": executable, "args": arguments},
			},
		})
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agentic-go mcp-config: writing configuration: %v\n", err)
		return 1
	}
	return 0
}

func resolveConfigWorkspace(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}
	return resolved, nil
}

func writeMCPConfigJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
