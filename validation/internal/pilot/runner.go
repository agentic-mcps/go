package pilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CommandSpec is the external-agent invocation contract. The runner never
// interprets or modifies the agent's patch; it only records its output.
//
//nolint:govet // Command strings and optional isolated home stay adjacent.
type CommandSpec struct {
	Binary    string
	Workspace string
	Prompt    string
	Focus     bool
	CodexHome string
}

// BuildCommand constructs the isolated Codex command.
func BuildCommand(s CommandSpec) (*exec.Cmd, error) {
	if s.Workspace == "" || s.Prompt == "" {
		return nil, fmt.Errorf("workspace and prompt are required")
	}
	// Ignore the operator's config so baseline cannot inherit agentic-go or any
	// other MCP server. Focus adds its sole server explicitly below.
	args := []string{"exec", "--ephemeral", "--json", "--ignore-user-config", "--model", "gpt-5.6-luna", "--config", "model_reasoning_effort=max", "--cd", s.Workspace, "--sandbox", "danger-full-access"}
	if s.Focus {
		if s.Binary == "" {
			return nil, fmt.Errorf("focus runs require agentic-go binary")
		}
		b, err := filepath.Abs(s.Binary)
		if err != nil {
			return nil, err
		}
		args = append(args, "--config", "mcp_servers.agentic-go.command="+b)
	}
	args = append(args, s.Prompt)
	cmd := exec.Command("codex", args...)
	if s.CodexHome != "" {
		cmd.Env = append(os.Environ(), "CODEX_HOME="+s.CodexHome)
	}
	return cmd, nil
}

// Events contains parsed agent events and aggregate tool counts.
type Events struct {
	Usage     map[string]any
	Raw       []json.RawMessage
	ToolCalls int
}

// ParseJSONL parses Codex JSONL events.
func ParseJSONL(stdout []byte) (Events, error) {
	out := Events{Raw: []json.RawMessage{}, Usage: map[string]any{}}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			return out, fmt.Errorf("invalid codex JSONL: %w", err)
		}
		out.Raw = append(out.Raw, append(json.RawMessage(nil), raw...))
		var e map[string]any
		_ = json.Unmarshal(line, &e)
		typ, _ := e["type"].(string)
		item, _ := e["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		// codex-cli emits one item.started and one item.completed event for
		// command/file/MCP operations. Count completed operations only.
		if (typ == "item.completed" && isToolItem(itemType)) ||
			(itemType == "" && strings.Contains(typ, "tool")) {
			out.ToolCalls++
		}
		if u, ok := e["usage"].(map[string]any); ok {
			for k, v := range u {
				out.Usage[k] = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func isToolItem(itemType string) bool {
	switch itemType {
	case "command_execution", "file_change", "mcp_tool_call", "web_search_call":
		return true
	default:
		return strings.Contains(itemType, "tool")
	}
}

// RunOnce executes one pilot command and captures its evidence.
func RunOnce(ctx context.Context, spec CommandSpec) (Events, string, string, int64, error) {
	prepared, err := BuildCommand(spec)
	if err != nil {
		return Events{}, "", "", 0, err
	}
	var stdout, stderr bytes.Buffer
	started := time.Now()
	cmd := exec.CommandContext(ctx, prepared.Path, prepared.Args[1:]...)
	cmd.Dir = spec.Workspace
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = prepared.Env
	err = cmd.Run()
	elapsed := time.Since(started).Milliseconds()
	events, parseErr := ParseJSONL(stdout.Bytes())
	if parseErr != nil && err == nil {
		err = parseErr
	}
	return events, stdout.String(), stderr.String(), elapsed, err
}

// PreflightFocus proves that the exact server configured for a focus run can
// initialize and advertises the focus tool before an agent is charged a run.
func PreflightFocus(ctx context.Context, binary, workspace string) error {
	if binary == "" || workspace == "" {
		return fmt.Errorf("focus preflight requires binary and workspace")
	}
	server, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	command := exec.Command(server)
	command.Dir = workspace
	client := mcp.NewClient(&mcp.Implementation{Name: "agentic-go-pilot-preflight", Version: "v1alpha1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		return fmt.Errorf("focus MCP initialize: %w", err)
	}
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("focus MCP tools/list: %w", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "go_context" {
			return nil
		}
	}
	return fmt.Errorf("focus MCP tools/list did not advertise go_context")
}

// FocusToolCalls returns successful focus-call count for terminal events. The
// legacy fallback preserves support for older bare tool-call event records.
func FocusToolCalls(events Events) int {
	m := FocusMetrics(events)
	if m.Calls > 0 || m.FailedCalls > 0 {
		return m.Calls
	}
	n := 0
	for _, raw := range events.Raw {
		if strings.Contains(string(raw), `"name":"go_context"`) {
			n++
		}
	}
	return n
}

// FocusMetric records terminal go_context outcomes and objective workflow
// signals. Calls counts successful completions; FailedCalls counts failed
// completions. The legacy Refresh and Evidence fields remain for compatible
// consumers and are derived without trusting agent prose.
type FocusMetric struct {
	ErrorCategories                   map[string]int
	Calls, FailedCalls, FirstPosition int
	Refresh, Evidence                 bool
	FocusResultFollowedByEdit         bool
	RefreshCompleted                  bool
}

// SkillDiscoveryEvidence reports whether the transcript contains a concrete
// read or load of the named skill's SKILL.md. Generic prose or a tool call is
// not sufficient evidence of model discovery.
func SkillDiscoveryEvidence(events Events, skillName string) bool {
	if strings.TrimSpace(skillName) == "" {
		return false
	}
	marker := skillName + "/SKILL.md"
	for _, raw := range events.Raw {
		var e struct {
			Type string `json:"type"`
			Item struct {
				Type             string `json:"type"`
				Command          string `json:"command"`
				AggregatedOutput string `json:"aggregated_output"`
				Text             string `json:"text"`
			} `json:"item"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Type != "item.completed" {
			continue
		}
		if strings.Contains(e.Item.Command, marker) || strings.Contains(e.Item.AggregatedOutput, marker) || strings.Contains(e.Item.Text, marker) {
			return true
		}
	}
	return false
}

// FocusMetrics returns strict focus adoption metrics from agent events. It
// counts only completed MCP events and recognizes edits through the explicit
// file_change event, not through message wording.
func FocusMetrics(events Events) FocusMetric {
	m := FocusMetric{ErrorCategories: map[string]int{}}
	calls := make([]focusCall, 0)
	editPositions := make([]int, 0)
	for i, raw := range events.Raw {
		if isCompletedFileChange(raw) {
			editPositions = append(editPositions, i)
		}
	}
	for i, raw := range events.Raw {
		call, ok := parseCompletedFocusCall(raw, i)
		if !ok {
			continue
		}
		calls = append(calls, call)
		if !call.Success {
			m.FailedCalls++
			m.ErrorCategories[call.ErrorCategory]++
			continue
		}
		m.Calls++
		if m.FirstPosition == 0 {
			m.FirstPosition = call.Position + 1
		}
		if call.Refresh {
			m.Refresh = true
		}
		if hasEditAfter(editPositions, call.Position) {
			m.FocusResultFollowedByEdit = true
		}
		if call.Refresh && refreshFollowsEdit(calls, call, editPositions) {
			m.RefreshCompleted = true
		}
	}
	m.Evidence = m.FocusResultFollowedByEdit || m.RefreshCompleted
	return m
}

type focusCall struct {
	PreviousPackID string
	PackID         string
	ErrorCategory  string
	Position       int
	Refresh        bool
	Success        bool
}

func parseCompletedFocusCall(raw json.RawMessage, position int) (focusCall, bool) {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil || event["type"] != "item.completed" {
		return focusCall{}, false
	}
	item, ok := event["item"].(map[string]any)
	if !ok || item["type"] != "mcp_tool_call" || stringValue(item["tool"]) != "go_context" {
		return focusCall{}, false
	}
	arguments, _ := item["arguments"].(map[string]any)
	previousPackID := stringValue(arguments["previous_pack_id"])
	result, hasResult := item["result"]
	status := stringValue(item["status"])
	success := status != "failed" && hasResult && result != nil
	call := focusCall{Position: position, PreviousPackID: previousPackID, Refresh: previousPackID != "", Success: success}
	if !success {
		call.ErrorCategory = focusErrorCategory(focusErrorText(item))
		return call, true
	}
	call.PackID = focusPackID(result)
	return call, true
}

func isCompletedFileChange(raw json.RawMessage) bool {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil || event["type"] != "item.completed" {
		return false
	}
	item, ok := event["item"].(map[string]any)
	return ok && item["type"] == "file_change" && stringValue(item["status"]) != "failed"
}

func hasEditAfter(positions []int, callPosition int) bool {
	for _, position := range positions {
		if position > callPosition {
			return true
		}
	}
	return false
}

func refreshFollowsEdit(calls []focusCall, refresh focusCall, editPositions []int) bool {
	for i := len(calls) - 1; i >= 0; i-- {
		previous := calls[i]
		if previous.Position >= refresh.Position || !previous.Success {
			continue
		}
		if refresh.PreviousPackID != "" && previous.PackID != "" && previous.PackID != refresh.PreviousPackID {
			continue
		}
		for _, editPosition := range editPositions {
			if editPosition > previous.Position && editPosition < refresh.Position {
				return true
			}
		}
	}
	return false
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func focusPackID(result any) string {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"structured_content", "structuredContent"} {
		structured, ok := resultMap[key].(map[string]any)
		if !ok {
			continue
		}
		if packID := stringValue(structured["pack_id"]); packID != "" {
			return packID
		}
	}
	return ""
}

func focusErrorText(item map[string]any) string {
	parts := make([]string, 0, 2)
	if errText := focusValueText(item["error"]); errText != "" {
		parts = append(parts, errText)
	}
	result, ok := item["result"].(map[string]any)
	if !ok {
		return strings.Join(parts, " ")
	}
	content, _ := result["content"].([]any)
	for _, value := range content {
		entry, _ := value.(map[string]any)
		if text := stringValue(entry["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func focusValueText(value any) string {
	if text := stringValue(value); text != "" {
		return text
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"message", "code", "type"} {
		if text := stringValue(object[key]); text != "" {
			return text
		}
	}
	return ""
}

const (
	focusErrorInvalidInput = "invalid_input"
	focusErrorStale        = "stale_snapshot"
	focusErrorProvider     = "provider"
	focusErrorTimeout      = "timeout"
	focusErrorTransport    = "transport"
	focusErrorUnknown      = "unknown"
)

func focusErrorCategory(text string) string {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "invalid"), strings.Contains(text, "required"), strings.Contains(text, "positive"), strings.Contains(text, "at most one"), strings.Contains(text, "must be"):
		return focusErrorInvalidInput
	case strings.Contains(text, "snapshot"), strings.Contains(text, "stale"), strings.Contains(text, "previous focus pack"):
		return focusErrorStale
	case strings.Contains(text, "deadline"), strings.Contains(text, "timeout"), strings.Contains(text, "timed out"):
		return focusErrorTimeout
	case strings.Contains(text, "transport"), strings.Contains(text, "connection"), strings.Contains(text, "initialize"):
		return focusErrorTransport
	case strings.Contains(text, "gopls"), strings.Contains(text, "rpc"), strings.Contains(text, "unsupported"), strings.Contains(text, "unavailable"):
		return focusErrorProvider
	default:
		return focusErrorUnknown
	}
}
