// Package clock answers one appliance question: is wall-clock time
// trustworthy enough for time-sensitive automation to act on?
//
// A stock Raspberry Pi has no battery-backed clock. After a power loss
// without network, the system can boot believing it is 1970 (or firmware
// build time) — and every routine, TTL, and cron comparison in Ghost runs
// on wall time. Firing a day's automations "because 2026 finally arrived"
// or expiring every pending approval at once is a predictable appliance
// failure, so the schedulers consult this package before firing.
//
// Policy (deliberate, offline-first):
//   - INVALID (provably wrong: before the sanity floor) blocks scheduler
//     firing. Conversational and local functions are unaffected.
//   - Anything else (synced, unsynced-but-sane, unknown) allows firing.
//     An unsynced-but-sane clock behaves normally in practice; blocking
//     it would brick automation for every offline user, which is worse
//     than seconds of drift.
// Time sync itself is left to the OS (systemd-timesyncd/chrony/NTP); this
// package only observes.
package clock

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

// State is the assessed trustworthiness of wall-clock time.
type State int

const (
	// Unknown means sync state could not be determined (no timedatectl,
	// command failed) but the clock itself looks sane.
	Unknown State = iota
	// Invalid means the clock is provably wrong. Schedulers must not fire.
	Invalid
	// Unsynced means sane time with NTP explicitly not synchronized
	// (typical offline boot with a persisted clock).
	Unsynced
	// Synced means NTP reports synchronization.
	Synced
)

func (s State) String() string {
	switch s {
	case Invalid:
		return "invalid"
	case Unsynced:
		return "unsynced"
	case Synced:
		return "synced"
	default:
		return "unknown"
	}
}

// minSaneTime is the floor below which wall time is definitely wrong.
// Ghost did not exist before this date; a Pi without RTC boots far below
// it after power loss.
var minSaneTime = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

// ntpSynchronized shells out to timedatectl (2s timeout). It reports
// whether NTP claims synchronization; any failure is an error, never a
// guess. Assigned to a variable so tests can stub the OS out.
var ntpSynchronized = func() (bool, error) {
	out, err := exec.Command("timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "yes", nil
}

// Assess classifies the current wall-clock time.
func Assess() State {
	return assessAt(time.Now())
}

func assessAt(now time.Time) State {
	if now.Before(minSaneTime) {
		return Invalid
	}
	synced, err := ntpSynchronized()
	if err != nil {
		return Unknown
	}
	if synced {
		return Synced
	}
	return Unsynced
}

// Gate caches assessments so a 1-second scheduler tick does not spawn a
// timedatectl process every second. Safe for concurrent use.
type Gate struct {
	mu       sync.Mutex
	interval time.Duration
	last     State
	checked  time.Time
	set      bool
}

// NewGate returns a gate that re-assesses at most every interval.
func NewGate(interval time.Duration) *Gate {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Gate{interval: interval}
}

// Safe reports whether schedulers may fire. Only an Invalid clock blocks;
// everything else degrades to normal operation (see package policy).
func (g *Gate) Safe() bool {
	return g.State() != Invalid
}

// State returns the cached assessment, refreshing when stale.
func (g *Gate) State() State {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.set && now.Sub(g.checked) < g.interval {
		return g.last
	}
	g.last = Assess()
	g.checked = now
	g.set = true
	return g.last
}

// Watcher detects wall-clock jumps between observations by comparing wall
// elapsed against monotonic elapsed. Both times come from time.Now(); the
// mono argument must carry its monotonic reading (plain time.Now() does).
type Watcher struct {
	lastWall time.Time
	lastMono time.Time
	seen     bool
}

// JumpThreshold is the wall-vs-monotonic divergence that counts as a jump.
// Below this, normal NTP slews and scheduling jitter are ignored.
const JumpThreshold = 2 * time.Minute

// Observe records an observation. It reports true when wall time moved by
// more than JumpThreshold relative to monotonic time passing — i.e. the
// clock was stepped, not merely ticking.
func (w *Watcher) Observe(wall, mono time.Time) bool {
	if !w.seen {
		w.lastWall, w.lastMono, w.seen = wall, mono, true
		return false
	}
	wallDelta := wall.Sub(w.lastWall)
	monoDelta := mono.Sub(w.lastMono)
	w.lastWall, w.lastMono = wall, mono
	diff := wallDelta - monoDelta
	if diff < 0 {
		diff = -diff
	}
	return diff > JumpThreshold
}
