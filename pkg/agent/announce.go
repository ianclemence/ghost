package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/constants"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/provider"
)

// Announcements are how Ghost tells its owner something without being asked:
// a part of itself stopped working, something unusual is touching its network,
// the room got uncomfortable, a new version is out. Each has a key and a
// cooldown, remembered across restarts, so Ghost says a thing once and does not
// repeat it every time it wakes.

var announceMu sync.Mutex

func (al *AgentLoop) announcedPath() string {
	return filepath.Join(al.workspace, "state", "announced.json")
}

func (al *AgentLoop) readAnnounced() map[string]time.Time {
	out := map[string]time.Time{}
	if b, err := os.ReadFile(al.announcedPath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// Announce tells the owner text, unless the same key was announced within
// cooldown. urgent marks something that should reach them even in quiet hours
// and even when the app is closed. It reports whether anything was said.
func (al *AgentLoop) Announce(key, text string, cooldown time.Duration, urgent bool) bool {
	if al == nil || al.workspace == "" || strings.TrimSpace(text) == "" {
		return false
	}
	announceMu.Lock()
	seen := al.readAnnounced()
	now := time.Now()
	if t, ok := seen[key]; ok && now.Sub(t) < cooldown {
		announceMu.Unlock()
		return false
	}
	seen[key] = now
	// Forget entries that are long expired so the file stays small.
	for k, t := range seen {
		if now.Sub(t) > 90*24*time.Hour {
			delete(seen, k)
		}
	}
	if b, err := json.Marshal(seen); err == nil {
		_ = os.MkdirAll(filepath.Dir(al.announcedPath()), 0o700)
		_ = os.WriteFile(al.announcedPath(), b, 0o600)
	}
	announceMu.Unlock()

	// Say it where the owner last talked to Ghost; the shared conversation
	// (and so the app) always gets it.
	channel, chatID := "mobile", "default"
	if al.state != nil {
		if c, id := al.state.GetLastActiveSession(); c != "" && id != "" && !constants.IsInternalChannel(c) {
			channel, chatID = c, id
		}
	}
	al.DeliverToOwner(channel, chatID, text, map[string]interface{}{"announce": key, "urgent": urgent})
	logger.InfoCF("agent", "announced to owner", map[string]interface{}{"key": key, "urgent": urgent})
	return true
}

// noteModelFailure is called when a model call fails. If the cause is
// something only the owner can fix (an empty balance, a rejected key) and the
// failing work was Ghost's own (a background job, a routine), nobody was
// waiting to be told, so tell them. Turns the owner started already show them
// the error.
func (al *AgentLoop) noteModelFailure(sessionKey string, err error) {
	if err == nil || !isMachineTurn(sessionKey) {
		return
	}
	var key string
	switch provider.ClassifyError(err) {
	case provider.FailBilling:
		key = "model-billing"
	case provider.FailAuth, provider.FailCredentialBad, provider.FailAuthorization:
		key = "model-credentials"
	case provider.FailNotConfigured:
		key = "model-not-configured"
	default:
		return
	}
	al.Announce(key, "I can't think right now, and I have work waiting. "+provider.Explain(err, al.providerName()), 6*time.Hour, true)
}

// providerName is the active provider's name, for owner-facing sentences.
func (al *AgentLoop) providerName() string {
	if al.cfg != nil {
		return al.cfg.Agents.Defaults.Provider
	}
	return ""
}
