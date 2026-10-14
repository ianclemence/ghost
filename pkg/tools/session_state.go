package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type SessionStateTool struct {
	mu        sync.RWMutex
	pending   func(session string) []string
	artifacts func(session string) []string
	schedules func() []string
}

func NewSessionStateTool() *SessionStateTool { return &SessionStateTool{} }

func (t *SessionStateTool) SetSources(pending, artifacts func(session string) []string, schedules func() []string) {
	t.mu.Lock()
	t.pending, t.artifacts, t.schedules = pending, artifacts, schedules
	t.mu.Unlock()
}

func (t *SessionStateTool) Name() string { return "session_state" }

func (t *SessionStateTool) Description() string {
	return "What is true right now, read from Ghost's own records and not from the conversation: approvals waiting for the owner, pages and files built in this conversation, and open reminders and routines. " +
		"Call it before you say that something was or was not built, is or is not waiting, was or was not scheduled, or before you describe where things stand. Never answer those from memory of the chat."
}

func (t *SessionStateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func section(b *strings.Builder, title string, lines []string, none string) {
	if len(lines) == 0 {
		fmt.Fprintf(b, "%s: %s\n", title, none)
		return
	}
	fmt.Fprintf(b, "%s (%d):\n", title, len(lines))
	for _, l := range lines {
		fmt.Fprintf(b, "- %s\n", l)
	}
}

func (t *SessionStateTool) Execute(ctx context.Context, _ map[string]interface{}) *ToolResult {
	t.mu.RLock()
	pending, artifacts, schedules := t.pending, t.artifacts, t.schedules
	t.mu.RUnlock()
	session := SessionKeyFromContext(ctx)
	var b strings.Builder
	if pending != nil {
		section(&b, "Waiting for the owner's approval", pending(session), "nothing")
	}
	if artifacts != nil {
		section(&b, "Built in this conversation", artifacts(session), "nothing")
	}
	if schedules != nil {
		section(&b, "Open reminders and routines", schedules(), "none")
	}
	if b.Len() == 0 {
		return ErrorResult("session state is not available right now")
	}
	return &ToolResult{ForLLM: strings.TrimSpace(b.String()), Silent: true}
}
