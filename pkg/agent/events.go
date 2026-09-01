package agent

import (
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// EventType is a typed internal system event. It lets memory, evolution,
// notifications, automation, and observability react to meaningful occurrences
// without being coupled to each other. This is deliberately small and internal
// — it complements (does not replace) the channel-oriented message bus.
type EventType string

const (
	// Memory lifecycle.
	EventMemoryCreated    EventType = "memory.created"
	EventMemoryUpdated    EventType = "memory.updated"
	EventMemorySuperseded EventType = "memory.superseded"
	// Task / turn lifecycle.
	EventTaskStarted   EventType = "task.started"
	EventTaskCompleted EventType = "task.completed"
	EventTaskFailed    EventType = "task.failed"
	// Automation / schedule.
	EventAutomationTriggered EventType = "automation.triggered"
	// Device / pairing.
	EventDevicePaired  EventType = "device.paired"
	EventDeviceRevoked EventType = "device.revoked"
	// Model health.
	EventModelFailed    EventType = "model.failed"
	EventModelRecovered EventType = "model.recovered"
	// Channel.
	EventChannelMessageReceived EventType = "channel.message_received"
	EventChannelMessageSent     EventType = "channel.message_sent"
	// Skills.
	EventSkillInstalled EventType = "skill.installed"
	EventSkillEnabled   EventType = "skill.enabled"
	EventSkillDisabled  EventType = "skill.disabled"
)

// Event is one typed occurrence. Subject is a small, consumer-friendly key
// (e.g. the predicate for a memory event, the tool/urn for a task); Data carries
// optional structured details.
type Event struct {
	Type    EventType
	At      time.Time
	Subject string
	Data    map[string]interface{}
}

// EventSink receives events.
type EventSink func(Event)

// EventBus is a tiny in-process pub/sub for typed internal events. It does not
// persist or queue; sinks that are slow or erroring are isolated so one bad
// subscriber can't break an unrelated subsystem.
type EventBus struct {
	mu    sync.RWMutex
	sinks map[int]EventSink
	next  int
}

// NewEventBus returns a ready-to-use zero-cost bus.
func NewEventBus() *EventBus {
	return &EventBus{sinks: make(map[int]EventSink)}
}

// Publish delivers an event to every subscriber, isolating panics/errors.
func (b *EventBus) Publish(ev Event) {
	if b == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	b.mu.RLock()
	sinks := make([]EventSink, 0, len(b.sinks))
	for _, s := range b.sinks {
		sinks = append(sinks, s)
	}
	b.mu.RUnlock()

	for _, s := range sinks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WarnCF("events", "event sink panicked", map[string]interface{}{"type": ev.Type})
				}
			}()
			s(ev)
		}()
	}
}

// Subscribe registers a sink and returns an unsubscribe function.
func (b *EventBus) Subscribe(sink EventSink) func() {
	if b == nil {
		return func() {}
	}
	b.mu.Lock()
	id := b.next
	b.next++
	b.sinks[id] = sink
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.sinks, id)
		b.mu.Unlock()
	}
}

// publish helper that is nil-safe and sets timestamp.
func (b *EventBus) emit(typ EventType, subject string, data map[string]interface{}) {
	if b == nil {
		return
	}
	b.Publish(Event{Type: typ, Subject: subject, Data: data})
}
