package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// hookDefaultMaxBlocks is how many times one session may be blocked.
	hookDefaultMaxBlocks = 3
	// hookUnresolvedItems is how many blocking items a disclosure lists.
	hookUnresolvedItems = 3
	// hookInputLimit bounds the hook payload read from stdin.
	hookInputLimit = 1 << 20
	// hookDecisionBlock is the Claude Code decision that keeps the agent working.
	hookDecisionBlock = "block"
)

// HookEvent is the part of an agent hook payload the gate uses.
type HookEvent struct {
	Name           string
	SessionID      string
	TranscriptPath string
	Cwd            string
	PermissionMode string
	StopHookActive bool
}

// HookOutput is the JSON a Stop hook prints. The zero value allows the stop
// silently.
type HookOutput struct {
	Decision      string `json:"decision,omitempty"`
	Reason        string `json:"reason,omitempty"`
	SystemMessage string `json:"systemMessage,omitempty"`
}

// hookPayload is the Claude Code hook input; unknown fields are ignored.
type hookPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	PermissionMode string `json:"permission_mode"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// ParseHookInput decodes a Claude Code hook payload. Unknown fields are
// ignored so newer agent versions keep working.
func ParseHookInput(r io.Reader) (HookEvent, error) {
	data, err := io.ReadAll(io.LimitReader(r, hookInputLimit))
	if err != nil {
		return HookEvent{}, fmt.Errorf("reading hook input: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return HookEvent{}, errors.New("reading hook input: input is empty")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return HookEvent{}, errors.New("decoding hook input: input is not a JSON object")
	}
	var payload hookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return HookEvent{}, fmt.Errorf("decoding hook input: %w", err)
	}
	return HookEvent{
		Name:           payload.HookEventName,
		SessionID:      payload.SessionID,
		TranscriptPath: payload.TranscriptPath,
		Cwd:            payload.Cwd,
		PermissionMode: payload.PermissionMode,
		StopHookActive: payload.StopHookActive,
	}, nil
}

// DecideStop applies the stop-hook policy to a gate result and returns the
// hook output with the updated session. It blocks only a blocking result whose
// fingerprint differs from the last blocked one, and at most maxBlocks times
// per session (zero selects 3). A repeated or over-limit block is allowed and
// disclosed to the user instead, so a stuck agent can never loop and gaming
// the gate becomes visible. stop_hook_active does not bypass the policy.
func DecideStop(result Result, session Session, maxBlocks int) (HookOutput, Session) {
	if maxBlocks <= 0 {
		maxBlocks = hookDefaultMaxBlocks
	}
	switch result.Verdict {
	case VerdictPass:
		session.Blocks = 0
		session.LastBlockedFingerprint = ""
		return HookOutput{}, session
	case VerdictBlock:
		return hookDecideBlock(result, session, maxBlocks)
	default:
		return HookOutput{SystemMessage: "agentic-go check could not verify this change: " + hookFirstNote(result)}, session
	}
}

func hookDecideBlock(result Result, session Session, maxBlocks int) (HookOutput, Session) {
	if result.Fingerprint != "" && result.Fingerprint == session.LastBlockedFingerprint {
		return HookOutput{SystemMessage: "agentic-go check: the agent stopped again without changes; unresolved: " + hookUnresolved(result.Items)}, session
	}
	if session.Blocks >= maxBlocks {
		return HookOutput{SystemMessage: "agentic-go check: block limit reached; unresolved: " + hookUnresolved(result.Items)}, session
	}
	session.Blocks++
	session.LastBlockedFingerprint = result.Fingerprint
	return HookOutput{Decision: hookDecisionBlock, Reason: Reason(result, formatDefaultLimit)}, session
}

// hookUnresolved lists up to three blocking items on one line.
func hookUnresolved(items []Item) string {
	blocking := formatBlockingItems(items)
	parts := make([]string, 0, hookUnresolvedItems+1)
	for i, item := range blocking {
		if i == hookUnresolvedItems {
			parts = append(parts, fmt.Sprintf("+%d more", len(blocking)-hookUnresolvedItems))
			break
		}
		line := formatCut(formatOneLine(item.Message), formatTextMaxBytes)
		if location := formatLocation(item); location != "" {
			line = location + " " + line
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "; ")
}

func hookFirstNote(result Result) string {
	for _, note := range result.Notes {
		if note = formatOneLine(note); note != "" {
			return note
		}
	}
	return "evidence was incomplete"
}
