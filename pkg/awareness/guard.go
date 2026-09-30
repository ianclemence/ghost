// Package awareness is how Ghost notices its own situation and tells its owner:
// someone poking at it over the network, a part of itself that has stopped
// working, a room that has become uncomfortable. A friend who lived in your
// house would mention these things without being asked; so should Ghost.
//
// Nothing here acts on the world. It observes, decides whether the owner would
// want to know, and says so once (with a cooldown) instead of repeating itself.
package awareness

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is a category of security-relevant event.
type Kind string

const (
	// AuthFailed: a request reached the Pod without valid device credentials.
	AuthFailed Kind = "auth"
	// PairFailed: a pairing attempt with a wrong, used or expired code.
	PairFailed Kind = "pairing"
)

const (
	window        = 10 * time.Minute
	authLimit     = 5
	pairLimit     = 3
	scanSources   = 4
	alertCooldown = 30 * time.Minute
)

// Guard watches for repeated failed access. It keeps only counts and times, in
// memory, and forgets them after the window.
type Guard struct {
	mu     sync.Mutex
	now    func() time.Time
	alert  func(text string)
	events map[string][]time.Time // "kind|source" -> times
	last   map[string]time.Time   // alert key -> last alert
}

// NewGuard builds a guard that calls alert with owner-facing text.
func NewGuard(alert func(text string)) *Guard {
	return &Guard{now: time.Now, alert: alert, events: map[string][]time.Time{}, last: map[string]time.Time{}}
}

// Source reduces "192.168.0.9:51234" to "192.168.0.9".
func Source(remote string) string {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		return h
	}
	return remote
}

// Note records one failed attempt from a source and alerts the owner when the
// pattern is no longer an honest mistake.
func (g *Guard) Note(kind Kind, source string) {
	if g == nil || source == "" {
		return
	}
	g.mu.Lock()
	now := g.now()
	key := string(kind) + "|" + source
	cut := now.Add(-window)
	kept := g.events[key][:0]
	for _, t := range g.events[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	g.events[key] = append(kept, now)
	count := len(g.events[key])

	var text, alertKey string
	limit := authLimit
	if kind == PairFailed {
		limit = pairLimit
	}
	switch {
	case count >= limit && kind == AuthFailed:
		alertKey = "auth|" + source
		text = fmt.Sprintf("Someone at %s tried to reach me %d times in the last %d minutes without valid credentials. I turned every attempt away. If that isn't one of your devices, it's worth checking who is on your network.", source, count, int(window/time.Minute))
	case count >= limit && kind == PairFailed:
		alertKey = "pair|" + source
		text = fmt.Sprintf("Someone at %s tried to pair with me %d times using a wrong or used code. Nothing was paired. If that wasn't you, someone on your network is guessing.", source, count)
	}
	// Many different sources each failing: a sweep of the network, not a typo.
	if text == "" {
		if n, list := g.distinctFailing(now, kind); n >= scanSources {
			alertKey = "sweep|" + string(kind)
			text = fmt.Sprintf("Several different devices (%s) are all failing to get in. That looks like something sweeping the network. I turned them all away.", strings.Join(list, ", "))
		}
	}
	if text == "" || (!g.last[alertKey].IsZero() && now.Sub(g.last[alertKey]) < alertCooldown) {
		g.mu.Unlock()
		return
	}
	g.last[alertKey] = now
	alert := g.alert
	g.mu.Unlock()
	if alert != nil {
		alert(text)
	}
}

// distinctFailing counts sources with at least two failures in the window.
// The caller holds the lock.
func (g *Guard) distinctFailing(now time.Time, kind Kind) (int, []string) {
	prefix := string(kind) + "|"
	cut := now.Add(-window)
	var list []string
	for k, ts := range g.events {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		n := 0
		for _, t := range ts {
			if t.After(cut) {
				n++
			}
		}
		if n >= 2 {
			list = append(list, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(list)
	if len(list) > 5 {
		list = list[:5]
	}
	return len(list), list
}
