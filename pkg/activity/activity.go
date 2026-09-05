// Package activity projects canonical events into the human-facing
// activity narrative: "What is Ghost doing for me?"
//
// Borrowed pattern (OpenMausBot): streaming replies with tool-run
// activity chips — compact cards (◌ running / ✓ done / ! waiting /
// × failed) that expand for detail. Ghost derives chips from SAFE
// canonical events only: raw tool names, manifests, schemas, prompts,
// and secrets can never reach a chip (structural, not prompt-based).
//
// Three detail layers:
//  1. Chip:  "Checked the weather"
//  2. Expanded: "Weather · Open-Meteo · updated 2 minutes ago"
//  3. Diagnostics (explicit opt-in only): provider, latency, request_id
package activity

import (
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/utils"
)

// State is the chip lifecycle.
type State string

const (
	StateRunning   State = "running"
	StateWaiting   State = "waiting"
	StateSuccess   State = "success"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
	StatePaused    State = "paused"
)

// Chip is the UI-safe activity card.
type Chip struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	State     State     `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Summary   string    `json:"summary,omitempty"`
	Detail    string    `json:"detail,omitempty"` // layer 2: safe expanded text
}

// humanTitles maps event types to product-narrative titles. Unknown types
// deliberately yield "" (no chip) rather than leaking the raw type name.
var humanTitles = map[cevents.Type]string{
	cevents.MessageReceived:         "New message",
	cevents.MessageCreated:          "Reply sent",
	cevents.AgentStarted:            "Working on it",
	cevents.AgentProgress:           "Working on it",
	cevents.AgentWaiting:            "Waiting for you",
	cevents.AgentCompleted:          "Done",
	cevents.AgentFailed:             "Couldn't finish that",
	cevents.CapabilityStarted:       "Starting",
	cevents.CapabilityCompleted:     "Done",
	cevents.CapabilityFailed:        "Unavailable right now",
	cevents.ToolCompleted:           "Done",
	cevents.ToolFailed:              "Step failed",
	cevents.PermissionRequested:     "Waiting for approval",
	cevents.PermissionApproved:      "Approved",
	cevents.PermissionDenied:        "Declined",
	cevents.PermissionExpired:       "Approval expired",
	cevents.MemoryCreated:           "Remembered",
	cevents.MemoryUpdated:           "Memory updated",
	cevents.MemoryDeleted:           "Memory removed",
	cevents.IntegrationConnected:    "Connected",
	cevents.IntegrationDisconnected: "Disconnected",
	cevents.IntegrationExpired:      "Needs reconnecting",
	cevents.IntegrationFailed:       "Connection failed",
	cevents.RoutineCreated:          "Routine scheduled",
	cevents.RoutineStarted:          "Routine running",
	cevents.RoutineWaiting:          "Routine waiting",
	cevents.RoutineCompleted:        "Routine done",
	cevents.RoutineFailed:           "Routine failed",
	cevents.GhostReady:              "Ghost is ready",
	cevents.GhostDegraded:           "Running with limits",
	cevents.GhostOffline:            "Ghost is offline",
	cevents.GhostRecovering:         "Ghost is recovering",
	cevents.OperationFailed:         "Couldn't complete that",
}

// capabilityTitles refines capability chips from payload context.
func capabilityTitle(e *cevents.Event, base string) string {
	cap, _ := e.Payload["capability"].(string)
	if cap == "" {
		cap, _ = e.Payload["capability_id"].(string)
	}
	switch {
	case strings.Contains(cap, "weather"):
		if e.Type == cevents.CapabilityStarted {
			return "Checking the weather"
		}
		if e.Type == cevents.CapabilityFailed {
			return "Weather unavailable"
		}
		return "Weather checked"
	case strings.Contains(cap, "calendar"):
		if e.Type == cevents.CapabilityStarted {
			return "Checking your calendar"
		}
		if e.Type == cevents.CapabilityFailed {
			return "Calendar unavailable"
		}
		return "Calendar checked"
	case strings.Contains(cap, "memory"):
		if e.Type == cevents.CapabilityStarted {
			return "Searching your memory"
		}
		return "Memory searched"
	case strings.Contains(cap, "reminder"):
		if e.Type == cevents.CapabilityCompleted {
			return "Reminder created"
		}
		return base
	case strings.Contains(cap, "flight"):
		if e.Type == cevents.CapabilityFailed {
			return "Flight data unavailable"
		}
		return base
	case cap != "":
		return base
	default:
		return base
	}
}

// stateFor maps event types to chip states.
func stateFor(t cevents.Type, status string) State {
	switch t {
	case cevents.AgentWaiting, cevents.PermissionRequested, cevents.RoutineWaiting:
		return StateWaiting
	case cevents.AgentCompleted, cevents.MessageCreated, cevents.CapabilityCompleted,
		cevents.ToolCompleted, cevents.PermissionApproved, cevents.MemoryCreated,
		cevents.MemoryUpdated, cevents.IntegrationConnected, cevents.RoutineCreated,
		cevents.RoutineCompleted, cevents.GhostReady:
		return StateSuccess
	case cevents.AgentFailed, cevents.CapabilityFailed, cevents.ToolFailed,
		cevents.PermissionDenied, cevents.IntegrationFailed, cevents.RoutineFailed,
		cevents.OperationFailed, cevents.GhostOffline:
		return StateFailed
	case cevents.PermissionExpired, cevents.IntegrationExpired:
		return StateWaiting
	case cevents.MessageReceived, cevents.AgentStarted, cevents.CapabilityStarted,
		cevents.RoutineStarted, cevents.GhostRecovering, cevents.GhostStarted:
		return StateRunning
	default:
		return StateRunning
	}
}

// Project converts one canonical event to a chip. It returns ok=false for
// events with no human narrative (raw tool internals, debug traces) —
// those never reach the activity UI.
func Project(e *cevents.Event) (*Chip, bool) {
	if e == nil || !e.Visibility.UserVisible() {
		return nil, false
	}
	title, ok := humanTitles[e.Type]
	if !ok || title == "" {
		return nil, false
	}
	switch e.Type {
	case cevents.CapabilityStarted, cevents.CapabilityCompleted, cevents.CapabilityFailed:
		title = capabilityTitle(e, title)
	case cevents.PermissionRequested:
		if target, _ := e.Payload["target"].(string); target != "" {
			title = "Waiting for approval"
		}
	case cevents.MemoryCreated:
		if summary, _ := e.Payload["title"].(string); summary != "" {
			title = "Remembered: " + truncate(summary, 80)
		}
	case cevents.IntegrationConnected:
		if name, _ := e.Payload["integration"].(string); name != "" {
			title = humanizeIntegration(name) + " connected"
		}
	}
	chip := &Chip{
		ID: e.ID, EventID: e.ID, Title: title,
		Kind: string(e.Type), State: stateFor(e.Type, e.Status),
		Timestamp: e.Timestamp,
	}
	if s, _ := e.Payload["summary"].(string); s != "" {
		chip.Summary = truncate(s, 160)
	}
	chip.Detail = expandDetail(e)
	return chip, true
}

// expandDetail builds layer-2 text: safe provenance, never internals.
func expandDetail(e *cevents.Event) string {
	parts := []string{}
	if prov, _ := e.Payload["provider"].(string); prov != "" {
		parts = append(parts, utils.Prettify(prov))
	}
	if cap, _ := e.Payload["capability"].(string); cap != "" && !strings.Contains(strings.ToLower(strings.Join(parts, "")), strings.ToLower(cap)) {
		parts = append(parts, utils.Prettify(cap))
	}
	if e.Status != "" {
		parts = append(parts, e.Status)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func humanizeIntegration(name string) string {
	switch strings.ToLower(name) {
	case "calendar":
		return "Calendar"
	case "flight":
		return "Flight tracking"
	case "telegram":
		return "Telegram"
	default:
		if name == "" {
			return "Integration"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}


func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Diagnostics renders the layer-3 technical projection for explicit
// diagnostics views. Safe fields only; secrets were redacted at publish.
func Diagnostics(e *cevents.Event) map[string]interface{} {
	out := map[string]interface{}{
		"event_id": e.ID, "type": string(e.Type),
		"request_id": e.RequestID, "timestamp": e.Timestamp.Format(time.RFC3339),
	}
	if v, ok := e.Payload["provider"]; ok {
		out["provider"] = v
	}
	if v, ok := e.Payload["duration_ms"]; ok {
		out["duration_ms"] = v
	}
	if v, ok := e.Payload["attempt"]; ok {
		out["attempt"] = v
	}
	if e.Status != "" {
		out["status"] = e.Status
	}
	return out
}
