package agent

import (
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/live"
)

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
