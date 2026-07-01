package agent

import (
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// SteeringMode controls how queued steering messages are dequeued.
type SteeringMode int

const (
	// SteeringOneAtATime dequeues one message per poll (default).
	SteeringOneAtATime SteeringMode = iota
	// SteeringAll drains the entire queue at once.
	SteeringAll
)

// SteeringMessage represents a user-injected message into a running agent turn.
type SteeringMessage struct {
	Content     string    `json:"content"`
	SessionKey  string    `json:"session_key"`
	Timestamp   time.Time `json:"timestamp"`
	Channel     string    `json:"channel"`
	ChatID      string    `json:"chat_id"`
	IsInterrupt bool      `json:"is_interrupt"` // If true, requests graceful stop
	IsHardAbort bool      `json:"is_hard_abort"` // If true, requests immediate cancellation
}

// SteeringQueue is a thread-safe, session-scoped queue for steering messages.
type SteeringQueue struct {
	queues    map[string][]SteeringMessage
	maxSize   int
	mode      SteeringMode
	mu        sync.Mutex
}

// NewSteeringQueue creates a new SteeringQueue with the given mode and max size.
func NewSteeringQueue(mode SteeringMode, maxSize int) *SteeringQueue {
	if maxSize <= 0 {
		maxSize = 10
	}
	return &SteeringQueue{
		queues:  make(map[string][]SteeringMessage),
		maxSize: maxSize,
		mode:    mode,
	}
}

// Enqueue adds a steering message to the queue for the given session scope.
// Returns false if the queue is full.
func (sq *SteeringQueue) Enqueue(msg SteeringMessage) bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	scope := msg.SessionKey
	if scope == "" {
		scope = "__default__"
	}

	queue := sq.queues[scope]
	if len(queue) >= sq.maxSize {
		logger.WarnCF("steering", "Steering queue full, dropping oldest", map[string]interface{}{
			"scope": scope,
			"size":  len(queue),
		})
		// Drop oldest to make room
		queue = queue[1:]
	}

	sq.queues[scope] = append(queue, msg)
	logger.DebugCF("steering", "Message enqueued", map[string]interface{}{
		"scope": scope,
		"queue_len": len(sq.queues[scope]),
	})
	return true
}

// Dequeue removes and returns steering messages for the given session scope.
func (sq *SteeringQueue) Dequeue(sessionKey string) []SteeringMessage {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	scope := sessionKey
	if scope == "" {
		scope = "__default__"
	}

	queue, ok := sq.queues[scope]
	if !ok || len(queue) == 0 {
		return nil
	}

	switch sq.mode {
	case SteeringAll:
		result := make([]SteeringMessage, len(queue))
		copy(result, queue)
		delete(sq.queues, scope)
		return result
	default: // SteeringOneAtATime
		result := []SteeringMessage{queue[0]}
		sq.queues[scope] = queue[1:]
		if len(sq.queues[scope]) == 0 {
			delete(sq.queues, scope)
		}
		return result
	}
}

// PendingCount returns the number of queued messages for the given session.
func (sq *SteeringQueue) PendingCount(sessionKey string) int {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	scope := sessionKey
	if scope == "" {
		scope = "__default__"
	}
	return len(sq.queues[scope])
}

// TotalCount returns the total number of queued messages across all sessions.
func (sq *SteeringQueue) TotalCount() int {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	total := 0
	for _, q := range sq.queues {
		total += len(q)
	}
	return total
}

// Clear removes all queued messages for the given session.
func (sq *SteeringQueue) Clear(sessionKey string) {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	scope := sessionKey
	if scope == "" {
		scope = "__default__"
	}
	delete(sq.queues, scope)
}

// ClearAll removes all queued messages across all sessions.
func (sq *SteeringQueue) ClearAll() {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	sq.queues = make(map[string][]SteeringMessage)
}

// SteeringManager manages steering for an agent loop.
type SteeringManager struct {
	queue         *SteeringQueue
	interruptFlag sync.Map // sessionKey -> bool
	mu            sync.Mutex
}

// NewSteeringManager creates a new SteeringManager.
func NewSteeringManager() *SteeringManager {
	return &SteeringManager{
		queue: NewSteeringQueue(SteeringOneAtATime, 10),
	}
}

// Inject adds a steering message into the running agent's queue.
func (sm *SteeringManager) Inject(msg SteeringMessage) bool {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	return sm.queue.Enqueue(msg)
}

// Interrupt signals a graceful interrupt for the given session.
func (sm *SteeringManager) Interrupt(sessionKey string) {
	sm.interruptFlag.Store(sessionKey, true)
	sm.Inject(SteeringMessage{
		Content:     "[SYSTEM] User requested interruption. Complete the current tool call and stop.",
		SessionKey:  sessionKey,
		Timestamp:   time.Now(),
		IsInterrupt: true,
	})
}

// HardAbort signals an immediate abort for the given session.
func (sm *SteeringManager) HardAbort(sessionKey string) {
	sm.Inject(SteeringMessage{
		Content:     "[SYSTEM] User requested immediate abort. Stop all operations.",
		SessionKey:  sessionKey,
		Timestamp:   time.Now(),
		IsHardAbort: true,
	})
}

// CheckInterrupt checks and clears the interrupt flag for a session.
func (sm *SteeringManager) CheckInterrupt(sessionKey string) bool {
	val, loaded := sm.interruptFlag.LoadAndDelete(sessionKey)
	if !loaded {
		return false
	}
	return val.(bool)
}

// DrainPending dequeues all pending steering messages for the session.
func (sm *SteeringManager) DrainPending(sessionKey string) []SteeringMessage {
	return sm.queue.Dequeue(sessionKey)
}

// PendingCount returns how many steering messages are queued for the session.
func (sm *SteeringManager) PendingCount(sessionKey string) int {
	return sm.queue.PendingCount(sessionKey)
}

// FormatForPrompt formats steering messages for injection into the LLM context.
func FormatForPrompt(msgs []SteeringMessage) string {
	if len(msgs) == 0 {
		return ""
	}

	result := "\n\n---\n**User Instructions (injected mid-conversation):**\n\n"
	for _, msg := range msgs {
		if msg.IsInterrupt {
			result += "- [INTERRUPT] " + msg.Content + "\n"
		} else if msg.IsHardAbort {
			result += "- [ABORT] " + msg.Content + "\n"
		} else {
			result += "- " + msg.Content + "\n"
		}
	}
	result += "---\n"
	return result
}
