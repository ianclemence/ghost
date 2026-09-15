// Package ghostproto defines the versioned Ghost Agent Protocol envelopes:
// the semantic operations between Ghost components, independent of Ollama,
// llama.cpp, Core AI, cloud providers, or UI. Replay protection uses
// nonce/request-ID tracking; idempotency keys make retries safe.
package ghostproto

import (
	"errors"
	"sync"
	"time"
)

// Version is the current protocol version spoken by this build.
const Version = 1

// MinSupportedVersion is the oldest peer version we interoperate with.
const MinSupportedVersion = 1

// Type is the semantic operation.
type Type string

const (
	TypeAuthenticate  Type = "authenticate"
	TypePair          Type = "pair"
	TypeCapabilities  Type = "capabilities"
	TypeSession       Type = "session"
	TypeTask          Type = "task"
	TypeInference     Type = "inference"
	TypeToolInvoke    Type = "tool_invoke"
	TypeToolResult    Type = "tool_result"
	TypeMemoryOp      Type = "memory_op"
	TypeEvent         Type = "event"
	TypeProgress      Type = "progress"
	TypeClarification Type = "clarification"
	TypeSteering      Type = "steering"
	TypeCancel        Type = "cancel"
	TypeSync          Type = "sync"
	TypeHealth        Type = "health"
)

// Envelope is the wire unit. IDs correlate devices, sessions, tasks,
// messages, tool calls, events and sync ops. IdempotencyKey makes retries
// safe; Nonce+Timestamp give replay protection.
type Envelope struct {
	ProtoVersion   int            `json:"proto_version"`
	Type           Type           `json:"type"`
	MessageID      string         `json:"message_id"`
	CorrelationID  string         `json:"correlation_id,omitempty"`
	DeviceID       string         `json:"device_id,omitempty"`
	InstallID      string         `json:"install_id,omitempty"`
	SessionID      string         `json:"session_id,omitempty"`
	TaskID         string         `json:"task_id,omitempty"`
	ToolCallID     string         `json:"tool_call_id,omitempty"`
	EventID        string         `json:"event_id,omitempty"`
	SyncOpID       string         `json:"sync_op_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Nonce          string         `json:"nonce,omitempty"`
	TimestampUnix  int64          `json:"timestamp_unix,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

// Validate checks version compatibility and required IDs.
func (e Envelope) Validate() error {
	if e.ProtoVersion < MinSupportedVersion || e.ProtoVersion > Version {
		return errors.New("protocol version mismatch")
	}
	if e.Type == "" || e.MessageID == "" {
		return errors.New("type and message_id required")
	}
	return nil
}

// NonceWindow rejects replayed messages within a time window.
type NonceWindow struct {
	mu     sync.Mutex
	seen   map[string]int64
	window time.Duration
}

// NewNonceWindow builds a replay-protection window (default 5 minutes).
func NewNonceWindow(window time.Duration) *NonceWindow {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &NonceWindow{seen: map[string]int64{}, window: window}
}

// Check records nonce and returns false when it is a replay.
func (n *NonceWindow) Check(nonce string, nowUnix int64) bool {
	if nonce == "" {
		return true // nonces required only where relevant; absence is not replay
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, dup := n.seen[nonce]; dup {
		return false
	}
	// Opportunistic expiry.
	cutoff := nowUnix - int64(n.window.Seconds())
	for k, ts := range n.seen {
		if ts < cutoff {
			delete(n.seen, k)
		}
	}
	n.seen[nonce] = nowUnix
	return true
}
