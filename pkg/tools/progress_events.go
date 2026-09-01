package tools

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
)

type ProgressEvent struct {
	Type       string `json:"type"`
	SubagentID string `json:"subagent_id"`
	Label      string `json:"label"`
	Detail     string `json:"detail,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

type ProgressTracker struct {
	events []ProgressEvent
	bus    *bus.MessageBus
	mu     sync.Mutex
}

func NewProgressTracker(bus *bus.MessageBus) *ProgressTracker {
	return &ProgressTracker{
		events: make([]ProgressEvent, 0),
		bus:    bus,
	}
}

func (p *ProgressTracker) Emit(event ProgressEvent) {
	event.Timestamp = time.Now().UnixMilli()

	p.mu.Lock()
	p.events = append(p.events, event)
	if len(p.events) > 100 {
		p.events = p.events[len(p.events)-100:]
	}
	p.mu.Unlock()

	if p.bus != nil {
		eventJSON, _ := json.Marshal(event)
		p.bus.PublishOutbound(bus.OutboundMessage{
			Channel: "system",
			Content: string(eventJSON),
			Metadata: map[string]interface{}{
				"type":        "progress_event",
				"event_type":  event.Type,
				"subagent_id": event.SubagentID,
			},
		})
	}
}

func (p *ProgressTracker) Start(subagentID, label string) {
	p.Emit(ProgressEvent{
		Type:       "started",
		SubagentID: subagentID,
		Label:      label,
	})
}

func (p *ProgressTracker) Thinking(subagentID, detail string) {
	p.Emit(ProgressEvent{
		Type:       "thinking",
		SubagentID: subagentID,
		Detail:     detail,
	})
}

func (p *ProgressTracker) ToolCall(subagentID, toolName string) {
	p.Emit(ProgressEvent{
		Type:       "tool_call",
		SubagentID: subagentID,
		Detail:     toolName,
	})
}

func (p *ProgressTracker) Complete(subagentID, status string, duration float64) {
	p.Emit(ProgressEvent{
		Type:       "completed",
		SubagentID: subagentID,
		Detail:     status,
	})
}

func (p *ProgressTracker) Error(subagentID, errMsg string) {
	p.Emit(ProgressEvent{
		Type:       "error",
		SubagentID: subagentID,
		Detail:     errMsg,
	})
}

func (p *ProgressTracker) GetRecent(count int) []ProgressEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 {
		count = 10
	}

	if count > len(p.events) {
		count = len(p.events)
	}

	result := make([]ProgressEvent, count)
	copy(result, p.events[len(p.events)-count:])
	return result
}

func (p *ProgressTracker) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

func (p *ProgressTracker) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}
