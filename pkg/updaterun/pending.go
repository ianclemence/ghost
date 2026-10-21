package updaterun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Pending is what an update asked from a conversation leaves behind, so the
// release that comes up afterwards can say how it went to the same chat. The
// update restarts the very process that asked for it, so nothing in memory
// survives to do the telling.
type Pending struct {
	From    string    `json:"from"` // the release that was running
	Channel string    `json:"channel,omitempty"`
	ChatID  string    `json:"chat_id,omitempty"`
	At      time.Time `json:"at"`
}

func pendingPath() string {
	return filepath.Join(filepath.Dir(UpdateLogPath()), "update-pending.json")
}

// WritePending records an update about to start.
func WritePending(p Pending) error {
	if p.At.IsZero() {
		p.At = time.Now()
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	path := pendingPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ClearPending forgets a recorded update (it never started).
func ClearPending() { _ = os.Remove(pendingPath()) }

// TakePending returns the recorded update and removes it, so it is reported
// once. A record older than maxAge is dropped unreported: it belongs to an
// update nobody is waiting on any more.
func TakePending(maxAge time.Duration) (Pending, bool) {
	path := pendingPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return Pending{}, false
	}
	_ = os.Remove(path)
	var p Pending
	if json.Unmarshal(b, &p) != nil || p.From == "" {
		return Pending{}, false
	}
	if maxAge > 0 && time.Since(p.At) > maxAge {
		return Pending{}, false
	}
	return p, true
}
