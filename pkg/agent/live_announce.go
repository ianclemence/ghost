package agent

import (
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/live"
)

// SettleSessionSurfaces retires the live surfaces of one finished turn.
// Conversation outcome describes the request; surface lifecycle describes
// the execution surface. This is the documented relationship between the
// two: success completes the task's Ghost-owned surfaces, any other
// terminal outcome fails them, waiting leaves them alone (the work is
// parked, not finished). User-held and already-terminal surfaces are
// never touched (CompleteTask enforces).
func (al *AgentLoop) SettleSessionSurfaces(sessionKey, outcome string) int {
	if al == nil || al.livePlane == nil || sessionKey == "" {
		return 0
	}
	switch outcome {
	case "success":
		return al.completeSessionTask(sessionKey, false)
	case "waiting_for_user", "waiting_for_permission":
		return 0
	default:
		return al.completeSessionTask(sessionKey, true)
	}
}

func (al *AgentLoop) completeSessionTask(sessionKey string, failed bool) int {
	taskID, _, err := al.resolveBrowserTask(sessionKey)
	if err != nil || taskID == "" {
		return 0
	}
	n := al.livePlane.CompleteTask(taskID, failed)
	// The task is terminal: release its browser session now rather than
	// waiting for the TTL. A waiting task never reaches here (SettleSession
	// leaves it alone), so an active user-controlled session is not closed.
	if ledger, lerr := al.browserSessionLedger(); lerr == nil && ledger != nil {
		_, _ = ledger.CloseForTask(taskID)
	}
	return n
}

// announceSurface tells the owner's devices that a live surface changed.
// It rides the existing outbound bus (mobile channel), so the mobile
// WebSocket fan-out delivers it with no second event system. The payload
// carries identity only — clients fetch authoritative state via the live
// surface API. Nil-safe: loops without a bus stay silent.
func (al *AgentLoop) announceSurface(sessionKey, surfaceID string, kind live.Kind) {
	if al == nil || surfaceID == "" || sessionKey == "" {
		return
	}
	b := al.Bus()
	if b == nil {
		return
	}
	b.PublishOutbound(bus.OutboundMessage{
		Channel: "mobile",
		Content: "",
		Metadata: map[string]interface{}{
			"type":       "surface_update",
			"surface_id": surfaceID,
			"kind":       string(kind),
			"session_id": sessionKey,
		},
	})
}
