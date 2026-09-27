package pilot

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestBuildCommandConditions(t *testing.T) {
	b, err := BuildCommand(CommandSpec{Workspace: "/w", Prompt: "do task"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArg(b.Args, "--ignore-user-config") {
		t.Fatal("baseline inherits user config")
	}
	for _, a := range b.Args {
		if a == "go_context" {
			t.Fatal("baseline exposes focus tool")
		}
	}
	f, err := BuildCommand(CommandSpec{Workspace: "/w", Prompt: "do task", Focus: true, Binary: "/bin/agentic-go"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range f.Args {
		if strings.HasPrefix(a, "mcp_servers.agentic-go.command=") {
			found = true
		}
	}
	if !found {
		t.Fatal("focus MCP override missing")
	}
	if !containsArg(f.Args, "--ignore-user-config") {
		t.Fatal("focus inherits user config")
	}
	if strings.Contains(strings.Join(f.Args, " "), "mcp_servers.agentic-go.args") {
		t.Fatalf("focus command must use agentic-go's stdio default entrypoint: %v", f.Args)
	}
	integrated, err := BuildCommand(CommandSpec{Workspace: "/w", Prompt: "do task", Focus: true, Binary: "/bin/agentic-go", CodexHome: "/tmp/codex-home"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsEnv(integrated.Env, "CODEX_HOME=/tmp/codex-home") {
		t.Fatalf("integrated command missing isolated CODEX_HOME: %v", integrated.Env)
	}
}

func TestFocusToolCalls(t *testing.T) {
	e := Events{Raw: []json.RawMessage{
		json.RawMessage(`{"type":"mcp_tool_call","name":"go_context"}`),
		json.RawMessage(`{"type":"mcp_tool_call","name":"go_audit"}`),
	}}
	if got := FocusToolCalls(e); got != 1 {
		t.Fatalf("focus calls = %d, want 1", got)
	}
}

func TestFocusMetricsIgnoresProseAndFailedCalls(t *testing.T) {
	e := Events{Raw: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"agent_message","text":"refresh go_context later"}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"failed","result":null,"arguments":{"previous_pack_id":"old"}}}`),
	}}
	if got := FocusMetrics(e); got.Calls != 0 || got.FailedCalls != 1 || got.Refresh || got.Evidence || len(got.ErrorCategories) != 1 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestFocusMetricsDerivesWorkflowSignalsFromEventOrder(t *testing.T) {
	e := Events{Raw: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","result":{"structured_content":{"pack_id":"pack-1"}}}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"agent_message","text":"the result is useful"}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","arguments":{"previous_pack_id":"pack-1"},"result":{"structured_content":{"pack_id":"pack-2","refresh":{"status":"replaced"}}}}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"failed","result":{"content":[{"type":"text","text":"building change context: provide at most one focus selection"}]}}}`),
	}}
	got := FocusMetrics(e)
	if got.Calls != 2 || got.FailedCalls != 1 || got.FirstPosition != 1 || !got.Refresh || !got.Evidence || !got.FocusResultFollowedByEdit || !got.RefreshCompleted {
		t.Fatalf("workflow metrics = %+v", got)
	}
	if got.ErrorCategories["invalid_input"] != 1 {
		t.Fatalf("error categories = %+v", got.ErrorCategories)
	}
}

func TestFocusMetricsDoesNotInferRefreshOrEditFromProse(t *testing.T) {
	e := Events{Raw: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","result":{"structured_content":{"pack_id":"pack-1"}}}}`),
		json.RawMessage(`{"type":"item.completed","item":{"type":"agent_message","text":"refresh with the focus pack after editing"}}`),
	}}
	got := FocusMetrics(e)
	if got.Evidence || got.FocusResultFollowedByEdit || got.RefreshCompleted {
		t.Fatalf("prose was treated as workflow evidence: %+v", got)
	}
}

func TestFocusMetricsFlagsOnlyRedundantRefreshes(t *testing.T) {
	got := FocusMetrics(Events{Raw: []json.RawMessage{
		focusRefreshSuccessEvent(`{"query":"Worker","max_bytes":12000}`, "pack-1", "snapshot-1", "./..."),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-1","max_bytes":12000}`, "pack-2", "snapshot-2", "./..."),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-2","max_bytes":12000}`, "pack-3", "snapshot-2", "./..."),
	}})
	if got.RedundantRefreshes != 1 {
		t.Fatalf("redundant refreshes = %d, want 1", got.RedundantRefreshes)
	}

	got = FocusMetrics(Events{Raw: []json.RawMessage{
		focusRefreshSuccessEvent(`{"query":"Worker","max_bytes":12000}`, "pack-1", "snapshot-1", "./..."),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-1","max_bytes":12000}`, "pack-2", "snapshot-2", "./..."),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-2","max_bytes":12000}`, "pack-3", "snapshot-3", "./..."),
	}})
	if got.RedundantRefreshes != 0 {
		t.Fatalf("refresh after edit was flagged: %d", got.RedundantRefreshes)
	}

	got = FocusMetrics(Events{Raw: []json.RawMessage{
		focusRefreshSuccessEvent(`{"query":"Worker","max_bytes":12000}`, "pack-1", "snapshot-1", "./..."),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-1","max_bytes":12000}`, "pack-2", "snapshot-2", "./..."),
		focusFailureEvent(`{"previous_pack_id":"pack-2"}`, "building change context: previous pack ID is invalid"),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-2","max_bytes":12000}`, "pack-3", "snapshot-2", "./..."),
	}})
	if got.RedundantRefreshes != 0 {
		t.Fatalf("refresh after stale rejection was flagged: %d", got.RedundantRefreshes)
	}

	got = FocusMetrics(Events{Raw: []json.RawMessage{
		focusRefreshSuccessEvent(`{"query":"Worker","max_bytes":12000}`, "pack-1", "snapshot-1", "./..."),
		json.RawMessage(`{"type":"item.completed","item":{"type":"file_change","status":"completed"}}`),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-1","max_bytes":12000}`, "pack-2", "snapshot-2", "./..."),
		focusRefreshSuccessEvent(`{"previous_pack_id":"pack-2","max_bytes":24000}`, "pack-3", "snapshot-2", "./..."),
	}})
	if got.RedundantRefreshes != 0 {
		t.Fatalf("refresh with changed evidence requirements was flagged: %d", got.RedundantRefreshes)
	}
}

func TestFocusFailureClassificationAndRecovery(t *testing.T) {
	e := Events{Raw: []json.RawMessage{
		focusSuccessEvent(`{"query":"Worker"}`, "pack-1"),
		focusFailureEvent(`{"symbol_ref":"mutated-ref"}`, "invalid Symbol Ref"),
		focusSuccessEvent(`{"file":"internal/worker.go","line":20,"column":6}`, "pack-2"),
		focusFailureEvent(`{"file":"internal/worker.go","line":171,"column":1}`, "gopls RPC 0: no identifier found"),
		focusSuccessEvent(`{"file":"internal/worker.go","line":171,"column":6}`, "pack-3"),
		focusFailureEvent(`{"file":"internal/worker.go","line":172,"column":6}`, "gopls RPC 0: provider exploded"),
	}}
	got := FocusMetrics(e)
	if got.Calls != 3 || got.FailedCalls != 3 || got.FailureCauses[FocusFailureSelectorMisuse] != 2 || got.FailureCauses[FocusFailureProvider] != 1 {
		t.Fatalf("failure metrics = %+v", got)
	}
	if len(got.FailureRecords) != 3 {
		t.Fatalf("failure records = %+v", got.FailureRecords)
	}
	if got.FailureRecords[0].Cause != FocusFailureSelectorMisuse || got.FailureRecords[0].Selector != "symbol_ref" || got.FailureRecords[0].Reason != "invalid_symbol_ref" || !got.FailureRecords[0].Recovered {
		t.Fatalf("symbol ref record = %+v", got.FailureRecords[0])
	}
	if got.FailureRecords[1].Cause != FocusFailureSelectorMisuse || got.FailureRecords[1].Reason != "invalid_declaration_coordinate" || !got.FailureRecords[1].Recovered {
		t.Fatalf("coordinate record = %+v", got.FailureRecords[1])
	}
	if got.FailureRecords[2].Cause != FocusFailureProvider || got.FailureRecords[2].Reason != "provider_error" || got.FailureRecords[2].Recovered {
		t.Fatalf("provider record = %+v", got.FailureRecords[2])
	}
	encoded, err := json.Marshal(got.FailureRecords)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "mutated-ref") || strings.Contains(string(encoded), "internal/worker.go") || strings.Contains(string(encoded), "provider exploded") {
		t.Fatalf("raw selector or provider data leaked: %s", encoded)
	}
}

func TestFocusFailureReplaysRejectEvidenceAndDetectRepeatedSelector(t *testing.T) {
	e := Events{Raw: []json.RawMessage{
		focusFailureEvent(`{"file":"internal/worker.go","line":171,"column":1}`, "gopls RPC 0: no identifier found"),
		focusFailureEvent(`{"file":"internal/worker.go","line":171,"column":1}`, "gopls RPC 0: no identifier found"),
		focusSuccessEvent(`{"file":"internal/worker.go","line":171,"column":6}`, "pack-1"),
	}}
	got := FocusMetrics(e)
	if got.Calls != 1 || got.FailedCalls != 2 || got.Evidence || got.FailureCauses[FocusFailureSelectorMisuse] != 2 {
		t.Fatalf("replay metrics = %+v", got)
	}
	if len(got.FailureRecords) != 2 || !got.FailureRecords[0].Repeated || got.FailureRecords[0].Selector != "file" || got.FailureRecords[0].Reason != "invalid_declaration_coordinate" {
		t.Fatalf("repeated selector records = %+v", got.FailureRecords)
	}
	if !got.FailureRecords[0].Recovered || !got.FailureRecords[1].Recovered {
		t.Fatalf("fresh-selector recovery not recorded: %+v", got.FailureRecords)
	}
}

func TestFocusFailureClassifiesRejectedPreviousPackAsStale(t *testing.T) {
	got := FocusMetrics(Events{Raw: []json.RawMessage{
		focusFailureEvent(`{"previous_pack_id":"emitted-pack"}`, "building change context: previous pack ID is invalid"),
	}})
	if got.ErrorCategories[focusErrorInvalidInput] != 1 {
		t.Fatalf("historical category changed: %+v", got.ErrorCategories)
	}
	if got.FailureCauses[FocusFailureStaleSnapshot] != 1 || len(got.FailureRecords) != 1 {
		t.Fatalf("stale lineage classification = %+v", got)
	}
	record := got.FailureRecords[0]
	if record.Cause != FocusFailureStaleSnapshot || record.Selector != "refresh" || record.Reason != "stale_snapshot" {
		t.Fatalf("stale lineage record = %+v", record)
	}
}

func focusSuccessEvent(arguments, packID string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","arguments":%s,"result":{"structured_content":{"pack_id":"%s"}}}}`, arguments, packID))
}

func focusRefreshSuccessEvent(arguments, packID, snapshotID, scope string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"completed","arguments":%s,"result":{"structured_content":{"pack_id":"%s","snapshot":{"id":"%s","scope":"%s","workspace":".","base_commit":"base"}}}}}`, arguments, packID, snapshotID, scope))
}

func focusFailureEvent(arguments, message string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"go_context","status":"failed","arguments":%s,"result":{"content":[{"type":"text","text":"%s"}]}}}`, arguments, message))
}

func TestSkillDiscoveryEvidenceRequiresSkillFileMarker(t *testing.T) {
	if SkillDiscoveryEvidence(Events{Raw: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"agent_message","text":"agentic-go-context is useful"}}`),
	}}, "agentic-go-context") {
		t.Fatal("prose was counted as skill discovery")
	}
	if !SkillDiscoveryEvidence(Events{Raw: []json.RawMessage{
		json.RawMessage(`{"type":"item.completed","item":{"type":"command_execution","command":"sed -n '1,120p' /tmp/skills/agentic-go-context/SKILL.md"}}`),
	}}, "agentic-go-context") {
		t.Fatal("skill file read was not counted")
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsEnv(env []string, want string) bool {
	for _, value := range env {
		if value == want {
			return true
		}
	}
	return false
}

func TestParseJSONLCountsToolsAndUsage(t *testing.T) {
	e, err := ParseJSONL([]byte("{\"type\":\"message\"}\n{\"type\":\"tool_call\",\"usage\":{\"input_tokens\":3}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Raw) != 2 || e.ToolCalls != 1 || e.Usage["input_tokens"] != float64(3) {
		t.Fatalf("%+v", e)
	}
}

func TestParseJSONLCountsCodexCLICompletedOperations(t *testing.T) {
	data, err := os.ReadFile("testdata/codex-cli-0.149.1-events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	e, err := ParseJSONL(data)
	if err != nil {
		t.Fatal(err)
	}
	if e.ToolCalls != 3 {
		t.Fatalf("tool calls = %d, want 3", e.ToolCalls)
	}
}
