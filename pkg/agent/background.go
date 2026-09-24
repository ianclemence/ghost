package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/tools"
)

// maxBackgroundNoteChars bounds the completion text persisted into session
// history: enough for the model to continue the work, never a full dump.
const maxBackgroundNoteChars = 2000

// PollBackground returns the session's running detached tasks and drains
// its completions for presentation. History notes are written by the event
// sink at completion time (once, surface-independent), never here —
// draining twice or from two surfaces can never duplicate model input.
func (al *AgentLoop) PollBackground(sessionKey string) ([]tools.BackgroundTask, []tools.BackgroundDone) {
	if al == nil || al.tools == nil {
		return nil, nil
	}
	return al.tools.BackgroundRunning(sessionKey), al.tools.DrainBackgroundDone(sessionKey)
}

// publishBackgroundEvent forwards one detached-task lifecycle observation
// to surfaces over the bus. The WS bridge allowlists these types alongside
// progress events; the app filters by session_id. Content carries the
// human-readable payload (label on start, findings on finish, already
// length-capped by the log); metadata carries routing. Nil bus = drop.
func publishBackgroundEvent(b *bus.MessageBus, ev tools.BackgroundEvent) {
	if b == nil {
		return
	}
	content := ev.Label
	if ev.Type == tools.BackgroundEventDone {
		content = ev.Result
		if strings.TrimSpace(content) == "" {
			content = ev.Label
		}
	}
	b.PublishOutbound(bus.OutboundMessage{
		Channel: "system",
		Content: content,
		Metadata: map[string]interface{}{
			"type":       ev.Type,
			"session_id": ev.Session,
			"label":      ev.Label,
			"tool":       ev.Tool,
			"ok":         ev.OK,
			"elapsed_ms": ev.ElapsedMs,
		},
	})
}

// noteBackgroundDone records one completion as session history. Failures
// are stated plainly so the next turn recovers instead of assuming.
func (al *AgentLoop) noteBackgroundDone(sessionKey string, d tools.BackgroundDone) {
	if al.sessions == nil {
		return
	}
	result := strings.TrimSpace(d.Result)
	if len(result) > maxBackgroundNoteChars {
		result = result[:maxBackgroundNoteChars] + "…"
	}
	elapsed := d.Elapsed.Round(time.Second)
	var note string
	if d.OK {
		note = fmt.Sprintf("Background task '%s' finished in %s: %s", d.Label, elapsed, result)
	} else {
		if result == "" {
			result = "no result reported"
		}
		note = fmt.Sprintf("Background task '%s' failed after %s: %s", d.Label, elapsed, result)
	}
	al.sessions.AddMessage(sessionKey, "system", note)
	al.sessions.Save(sessionKey)
}
