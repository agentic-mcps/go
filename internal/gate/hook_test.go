package gate

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseHookInput(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    HookEvent
		wantErr bool
	}{
		{
			name: "claude stop event with unknown fields",
			input: `{"session_id":"abc-123","transcript_path":"/tmp/t.jsonl","cwd":"/repo",` +
				`"hook_event_name":"Stop","permission_mode":"default","stop_hook_active":true,` +
				`"future_field":{"nested":[1,2]},"model":"x"}`,
			want: HookEvent{
				Name: "Stop", SessionID: "abc-123", TranscriptPath: "/tmp/t.jsonl", Cwd: "/repo",
				PermissionMode: "default", StopHookActive: true,
			},
		},
		{
			name:  "missing optional fields stay zero",
			input: `{"session_id":"s","hook_event_name":"Stop"}`,
			want:  HookEvent{Name: "Stop", SessionID: "s"},
		},
		{name: "empty input", input: "", wantErr: true},
		{name: "not json", input: "Stop", wantErr: true},
		{name: "json array", input: `[1]`, wantErr: true},
		{name: "json null", input: `null`, wantErr: true},
		{name: "json string", input: ` "Stop" `, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseHookInput(strings.NewReader(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseHookInput() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHookInput() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseHookInput() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecideStop(t *testing.T) {
	blockItems := []Item{
		{Severity: SeverityBlock, Code: CodeTestDeleted, File: "a_test.go", Line: 3, Message: "TestA was deleted."},
		{Severity: SeverityBlock, Code: CodeStub, File: "a.go", Line: 9, Message: "F is a stub."},
		{Severity: SeverityBlock, Code: CodeBuild, File: "b.go", Line: 1, Message: "build failed."},
		{Severity: SeverityBlock, Code: CodeSyntax, File: "c.go", Line: 2, Message: "syntax error."},
		{Severity: SeverityWarn, Code: CodeTestFlaky, Message: "TestB looks flaky."},
	}
	blocked := Result{Verdict: VerdictBlock, Items: blockItems, Notes: []string{}, Fingerprint: "fp-new"}
	//nolint:govet // Keep each case readable in input, then expectation order.
	cases := []struct {
		name        string
		result      Result
		session     Session
		maxBlocks   int
		wantBlock   bool
		wantMessage []string
		wantSession Session
	}{
		{
			name:        "pass allows and resets",
			result:      Result{Verdict: VerdictPass, Items: []Item{}, Notes: []string{}, Fingerprint: "fp"},
			session:     Session{ID: "s", Blocks: 2, LastBlockedFingerprint: "old"},
			wantSession: Session{ID: "s"},
		},
		{
			name:        "first block blocks and records the fingerprint",
			result:      blocked,
			session:     Session{ID: "s"},
			wantBlock:   true,
			wantSession: Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp-new"},
		},
		{
			name:        "different fingerprint below the limit blocks again",
			result:      blocked,
			session:     Session{ID: "s", Blocks: 2, LastBlockedFingerprint: "fp-old"},
			maxBlocks:   3,
			wantBlock:   true,
			wantSession: Session{ID: "s", Blocks: 3, LastBlockedFingerprint: "fp-new"},
		},
		{
			name:    "same fingerprint allows and discloses",
			result:  blocked,
			session: Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp-new"},
			wantMessage: []string{
				"agentic-go check: the agent stopped again without changes; unresolved: ",
				"a_test.go:3 TestA was deleted.", "a.go:9 F is a stub.", "b.go:1 build failed.", "+1 more",
			},
			wantSession: Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp-new"},
		},
		{
			name:        "block limit reached allows and discloses",
			result:      blocked,
			session:     Session{ID: "s", Blocks: 3, LastBlockedFingerprint: "fp-old"},
			wantMessage: []string{"agentic-go check: block limit reached; unresolved: ", "a_test.go:3 TestA was deleted."},
			wantSession: Session{ID: "s", Blocks: 3, LastBlockedFingerprint: "fp-old"},
		},
		{
			name:        "custom limit applies",
			result:      blocked,
			session:     Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp-old"},
			maxBlocks:   1,
			wantMessage: []string{"block limit reached"},
			wantSession: Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp-old"},
		},
		{
			name:        "empty fingerprint never counts as unchanged",
			result:      Result{Verdict: VerdictBlock, Items: blockItems[:1], Notes: []string{}},
			session:     Session{ID: "s"},
			wantBlock:   true,
			wantSession: Session{ID: "s", Blocks: 1},
		},
		{
			name:        "unknown allows with the first note",
			result:      Result{Verdict: VerdictUnknown, Items: []Item{}, Notes: []string{"time budget 2m0s ran out before tests finished", "second"}},
			session:     Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp"},
			wantMessage: []string{"agentic-go check could not verify this change: time budget 2m0s ran out before tests finished"},
			wantSession: Session{ID: "s", Blocks: 1, LastBlockedFingerprint: "fp"},
		},
		{
			name:        "unknown without notes still explains",
			result:      Result{Verdict: VerdictUnknown, Items: []Item{}, Notes: []string{}},
			session:     Session{ID: "s"},
			wantMessage: []string{"agentic-go check could not verify this change: evidence was incomplete"},
			wantSession: Session{ID: "s"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, session := DecideStop(tc.result, tc.session, tc.maxBlocks)
			if !reflect.DeepEqual(session, tc.wantSession) {
				t.Fatalf("session = %+v, want %+v", session, tc.wantSession)
			}
			if tc.wantBlock {
				if out.Decision != "block" || out.SystemMessage != "" {
					t.Fatalf("output = %+v, want a block", out)
				}
				if want := Reason(tc.result, 2048); out.Reason != want || want == "" {
					t.Fatalf("reason = %q, want %q", out.Reason, want)
				}
				return
			}
			if out.Decision != "" || out.Reason != "" {
				t.Fatalf("output = %+v, want allow", out)
			}
			if len(tc.wantMessage) == 0 && out.SystemMessage != "" {
				t.Fatalf("system message = %q, want none", out.SystemMessage)
			}
			for _, want := range tc.wantMessage {
				if !strings.Contains(out.SystemMessage, want) {
					t.Fatalf("system message = %q, want it to contain %q", out.SystemMessage, want)
				}
			}
			if strings.Contains(out.SystemMessage, "c.go:2") {
				t.Fatalf("system message lists more than 3 items: %q", out.SystemMessage)
			}
		})
	}
}
