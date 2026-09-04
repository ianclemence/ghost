package skills

import (
	"sync"
	"time"
)

// PendingContinuation tracks a clarification that expects a short follow-up.
// It enables natural resume: Ghost asks "Which flight number?", user replies
// "TG123", and the original task continues without repeating the request.
//
// This is intentionally separate from the blocking clarify tool: natural
// follow-ups never block for 5 minutes. The pending entry expires quickly.
type PendingContinuation struct {
	CapabilityID string `json:"capability_id"`
	Skill        string `json:"skill"`
	MissingField string `json:"missing_field"`
	Question     string `json:"question"`
	OriginalTask string `json:"original_task"`
	CreatedAt    time.Time `json:"created_at"`
}

var (
	pendingMu sync.RWMutex
	pendingBySession = map[string]PendingContinuation{}
)

// pendingTTL bounds how long a follow-up is treated as an answer.
const pendingTTL = 10 * time.Minute

// SetPending records that session expects a short answer for missingField.
func SetPending(session string, p PendingContinuation) {
	if session == "" {
		return
	}
	p.CreatedAt = time.Now()
	pendingMu.Lock()
	pendingBySession[session] = p
	pendingMu.Unlock()
}

// GetPending returns the pending continuation if still fresh.
func GetPending(session string) (PendingContinuation, bool) {
	pendingMu.RLock()
	p, ok := pendingBySession[session]
	pendingMu.RUnlock()
	if !ok {
		return PendingContinuation{}, false
	}
	if time.Since(p.CreatedAt) > pendingTTL {
		ClearPending(session)
		return PendingContinuation{}, false
	}
	return p, true
}

// ClearPending removes the pending continuation.
func ClearPending(session string) {
	pendingMu.Lock()
	delete(pendingBySession, session)
	pendingMu.Unlock()
}
