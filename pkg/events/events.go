package events

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// EventKind represents the type of runtime event.
type EventKind string

const (
	// Agent lifecycle events
	KindAgentTurnStart   EventKind = "agent.turn.start"
	KindAgentTurnEnd     EventKind = "agent.turn.end"
	KindAgentTurnError   EventKind = "agent.turn.error"
	KindAgentMessageIn   EventKind = "agent.message.in"
	KindAgentMessageOut  EventKind = "agent.message.out"

	// Tool events
	KindToolBefore       EventKind = "tool.before"
	KindToolAfter        EventKind = "tool.after"
	KindToolError        EventKind = "tool.error"
	KindToolDenied       EventKind = "tool.denied"

	// Channel events
	KindChannelMessageIn  EventKind = "channel.message.in"
	KindChannelMessageOut EventKind = "channel.message.out"
	KindChannelError      EventKind = "channel.error"
	KindChannelStateChange EventKind = "channel.state.change"

	// Session events
	KindSessionCreated   EventKind = "session.created"
	KindSessionSummarize EventKind = "session.summarize"
	KindSessionClear     EventKind = "session.clear"

	// LLM events
	KindLLMRequest       EventKind = "llm.request"
	KindLLMResponse      EventKind = "llm.response"
	KindLLMError         EventKind = "llm.error"
	KindLLMFallback      EventKind = "llm.fallback"

	// Skill events
	KindSkillInstalled    EventKind = "skill.installed"
	KindSkillRemoved      EventKind = "skill.removed"
	KindSkillError        EventKind = "skill.error"

	// Heartbeat events
	KindHeartbeatStart   EventKind = "heartbeat.start"
	KindHeartbeatEnd     EventKind = "heartbeat.end"
	KindHeartbeatError   EventKind = "heartbeat.error"

	// Bus events
	KindBusPublish       EventKind = "bus.publish"
	KindBusDrop          EventKind = "bus.drop"
)

// Event is a runtime event envelope.
type Event struct {
	ID          string                 `json:"id"`
	Kind        EventKind              `json:"kind"`
	Time        time.Time              `json:"time"`
	Source      string                 `json:"source"`
	Scope       map[string]string      `json:"scope,omitempty"`
	Payload     interface{}            `json:"payload,omitempty"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Correlation string                 `json:"correlation,omitempty"`
	Severity    Severity               `json:"severity"`
}

// Severity represents event severity.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

// String returns the string representation of severity.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarn:
		return "warn"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// NewEvent creates a new event with an auto-generated ID.
func NewEvent(kind EventKind, source string) Event {
	return Event{
		ID:     generateEventID(),
		Kind:   kind,
		Time:   time.Now(),
		Source: source,
		Attrs:  make(map[string]interface{}),
	}
}

// BackpressurePolicy controls how the subscription handles a full buffer.
type BackpressurePolicy int

const (
	// DropNewest drops the newest event when the buffer is full.
	DropNewest BackpressurePolicy = iota
	// DropOldest drops the oldest event when the buffer is full.
	DropOldest
	// Block blocks until space is available.
	Block
)

// SubscriptionOptions configures a subscription.
type SubscriptionOptions struct {
	Name          string
	BufferSize    int
	Priority      int
	Backpressure  BackpressurePolicy
	Timeout       time.Duration
}

// subscription represents an active event subscription.
type subscription struct {
	name          string
	channel       chan Event
	filter        EventFilter
	priority      int
	backpressure  BackpressurePolicy
	timeout       time.Duration
	received      int64
	handled       int64
	failed        int64
	dropped       int64
	stop          chan struct{}
}

// Stats returns subscription statistics.
func (s *subscription) Stats() SubscriptionStats {
	return SubscriptionStats{
		Name:     s.name,
		Received: atomic.LoadInt64(&s.received),
		Handled:  atomic.LoadInt64(&s.handled),
		Failed:   atomic.LoadInt64(&s.failed),
		Dropped:  atomic.LoadInt64(&s.dropped),
	}
}

// SubscriptionStats holds subscription metrics.
type SubscriptionStats struct {
	Name     string `json:"name"`
	Received int64  `json:"received"`
	Handled  int64  `json:"handled"`
	Failed   int64  `json:"failed"`
	Dropped  int64  `json:"dropped"`
}

// EventBus is a typed event bus with filter composition.
type EventBus struct {
	subs          []*subscription
	subMu         sync.RWMutex
	nextID        int64
	totalReceived int64
	totalDropped  int64
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subs: make([]*subscription, 0),
	}
}

// Publish sends an event to all matching subscribers.
func (eb *EventBus) Publish(ctx context.Context, evt Event) {
	atomic.AddInt64(&eb.totalReceived, 1)

	eb.subMu.RLock()
	subs := make([]*subscription, len(eb.subs))
	copy(subs, eb.subs)
	eb.subMu.RUnlock()

	for _, sub := range subs {
		if sub.filter != nil && !sub.filter.Matches(evt) {
			continue
		}

		atomic.AddInt64(&sub.received, 1)

		switch sub.backpressure {
		case Block:
			select {
			case sub.channel <- evt:
				atomic.AddInt64(&sub.handled, 1)
			case <-ctx.Done():
				atomic.AddInt64(&sub.dropped, 1)
			case <-sub.stop:
				atomic.AddInt64(&sub.dropped, 1)
			}
		default: // DropNewest
			select {
			case sub.channel <- evt:
				atomic.AddInt64(&sub.handled, 1)
			default:
				if sub.backpressure == DropOldest {
					select {
					case <-sub.channel:
						sub.channel <- evt
						atomic.AddInt64(&sub.handled, 1)
					default:
						atomic.AddInt64(&sub.dropped, 1)
					}
				} else {
					atomic.AddInt64(&sub.dropped, 1)
					atomic.AddInt64(&eb.totalDropped, 1)
				}
			}
		}
	}
}

// PublishNonBlocking sends an event without blocking. Drops if all subscribers are full.
func (eb *EventBus) PublishNonBlocking(evt Event) {
	atomic.AddInt64(&eb.totalReceived, 1)

	eb.subMu.RLock()
	subs := make([]*subscription, len(eb.subs))
	copy(subs, eb.subs)
	eb.subMu.RUnlock()

	for _, sub := range subs {
		if sub.filter != nil && !sub.filter.Matches(evt) {
			continue
		}

		atomic.AddInt64(&sub.received, 1)

		select {
		case sub.channel <- evt:
			atomic.AddInt64(&sub.handled, 1)
		default:
			atomic.AddInt64(&sub.dropped, 1)
			atomic.AddInt64(&eb.totalDropped, 1)
		}
	}
}

// Subscribe creates a new subscription with the given filter and options.
// Returns the subscription channel and a cancel function.
func (eb *EventBus) Subscribe(filter EventFilter, opts SubscriptionOptions) (<-chan Event, func()) {
	if opts.BufferSize <= 0 {
		opts.BufferSize = 100
	}

	sub := &subscription{
		name:         opts.Name,
		channel:      make(chan Event, opts.BufferSize),
		filter:       filter,
		priority:     opts.Priority,
		backpressure: opts.Backpressure,
		timeout:      opts.Timeout,
		stop:         make(chan struct{}),
	}

	eb.subMu.Lock()
	eb.subs = append(eb.subs, sub)
	// Sort by priority (higher = first)
	for i := len(eb.subs) - 1; i > 0 && eb.subs[i].priority > eb.subs[i-1].priority; i-- {
		eb.subs[i], eb.subs[i-1] = eb.subs[i-1], eb.subs[i]
	}
	eb.subMu.Unlock()

	cancel := func() {
		eb.subMu.Lock()
		defer eb.subMu.Unlock()
		for i, s := range eb.subs {
			if s == sub {
				close(sub.stop)
				eb.subs = append(eb.subs[:i], eb.subs[i+1:]...)
				break
			}
		}
	}

	logger.DebugCF("events", "Subscription created", map[string]interface{}{
		"name": opts.Name,
	})

	return sub.channel, cancel
}

// Stats returns aggregate bus statistics.
func (eb *EventBus) Stats() BusStats {
	eb.subMu.RLock()
	defer eb.subMu.RUnlock()

	return BusStats{
		TotalReceived: atomic.LoadInt64(&eb.totalReceived),
		TotalDropped:  atomic.LoadInt64(&eb.totalDropped),
		Subscriptions: len(eb.subs),
	}
}

// BusStats holds aggregate bus metrics.
type BusStats struct {
	TotalReceived int64 `json:"total_received"`
	TotalDropped  int64 `json:"total_dropped"`
	Subscriptions int   `json:"subscriptions"`
}

func generateEventID() string {
	return fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&nextEventID, 1))
}

var nextEventID int64

func init() {
	nextEventID = 0
}

// FilterString returns a string representation of event kind.
func kindPrefix(kind EventKind) string {
	return string(kind)
}

func kindMatches(kind, prefix EventKind) bool {
	return strings.HasPrefix(string(kind), string(prefix))
}
