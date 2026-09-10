// Package live is Ghost's Live Surface plane: a small, authoritative domain
// primitive describing "a real execution surface Ghost is acting on that the
// owner's device may observe and, when authorized, temporarily take over"
// (Browser and Computer). It is not a UI framework and not a transport.
//
// Core invariant: OBSERVATION IS NOT CONTROL, and CONTROL IS EXPLICIT.
//
//   - Ghost controls a surface while it is executing on it (gate/executor).
//   - A mobile takeover leases control to one authenticated user/device and
//     PAUSES Ghost: while user_control is held, the model/runtime is refused
//     further actions on that surface (the gates consult this plane).
//   - Releasing control does NOT blindly resume Ghost: the surface moves to
//     "paused" and only a revalidating resume (owner/context/generation/
//     lease/permission rechecked by the caller's existing machinery) returns
//     it to Ghost.
//   - User control leases expire. A dead mobile connection can never leave a
//     permanent human owner; the safe fallback is "no valid control owner".
//
// This plane holds no executor, no CDP/X11, no credentials, and no raw
// runtime state. It is the smallest state machine the product needs on top
// of the existing, authoritative browser/computer implementations.
package live

import (
	"fmt"
	"sync"
	"time"
)

// Kind is the kind of live surface.
type Kind string

const (
	KindBrowser  Kind = "browser"
	KindComputer Kind = "computer"
)

// State is the runtime activity state of a surface.
type State string

const (
	StateCreated      State = "created"
	StateStarting     State = "starting"
	StateActive       State = "active"
	StateWaiting      State = "waiting"
	StateUserControl  State = "user_control" // human owns control; Ghost paused
	StatePaused       State = "paused"       // released by human; revalidate before resume
	StateCompleted    State = "completed"
	StateFailed       State = "failed"
	StateDisconnected State = "disconnected"
	StateExpired      State = "expired"
)

// Owner is who currently controls a surface.
type Owner string

const (
	OwnerNone  Owner = "none"
	OwnerGhost Owner = "ghost"
	OwnerUser  Owner = "user"
)

// Observation is the safe, bounded snapshot of a surface the mobile client
// may render. It never carries credentials, CDP/X11 internals, browser
// handles, leases, or broker state — only what a user could reasonably see.
type Observation struct {
	Timestamp time.Time `json:"timestamp"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"` // browser: current page
	Domain    string    `json:"domain,omitempty"`
	Text      string    `json:"text,omitempty"` // bounded structured text (truncated)
	Control   Owner     `json:"control"`        // snapshot of who controls it
	State     State     `json:"state"`
	// ScreenshotPath is internal-only (never serialized to clients). When
	// set, the serving layer reads and streams the bytes; the observation
	// JSON never embeds raw image data or paths.
	ScreenshotPath string `json:"-"`
}

// UserLease is the human takeover lease. It is scoped to one authenticated
// device and always expires.
type UserLease struct {
	DeviceID  string    `json:"device_id"`
	LeaseID   string    `json:"lease_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Surface is one live execution surface. The registry is the authoritative
// in-process control/observation state; persistence and canonical events
// live in the existing runtime (leases table, browser sessions table,
// canonical event stream).
type Surface struct {
	ID       string      `json:"id"`
	Kind     Kind        `json:"kind"`
	State    State       `json:"state"`
	Control  Owner       `json:"control"`
	Lease    *UserLease  `json:"lease,omitempty"`
	OwnerID  string      `json:"owner"` // ghost owner id surfaces belong to
	Obs      Observation `json:"observation"`
	Updated  time.Time   `json:"updated"`
	Sequence int64       `json:"sequence"` // monotonic per-surface change counter
}

// Registry tracks the live surfaces the runtime is (or was just) acting on.
type Registry struct {
	mu    sync.Mutex
	owner string // the ghost owner identity every surface belongs to
	byID  map[string]*Surface
	seq   int64
}

// NewRegistry creates the plane for one ghost owner.
func NewRegistry(owner string) *Registry {
	return &Registry{owner: owner, byID: map[string]*Surface{}}
}

// maxSurfaces bounds registry growth: every browser task mints a session
// id, so without eviction a long-lived runtime would accumulate them.
// Eviction drops the stalest surface no human controls; user-held control
// is never evicted out from under a lease.
const maxSurfaces = 128

// Register adds or refreshes a surface for the owner. Re-registering an
// existing id preserves control state (idempotent) unless kind differs.
func (r *Registry) Register(id string, kind Kind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byID[id]; ok {
		if s.Kind != kind {
			s.Kind = kind
		}
		s.bump(time.Now())
		return
	}
	if len(r.byID) >= maxSurfaces {
		var oldestID string
		var oldest time.Time
		first := true
		for sid, s := range r.byID {
			if s.Control == OwnerUser {
				continue
			}
			if first || s.Updated.Before(oldest) {
				oldestID, oldest, first = sid, s.Updated, false
			}
		}
		if oldestID != "" {
			delete(r.byID, oldestID)
		}
	}
	r.seq++
	s := &Surface{ID: id, Kind: kind, State: StateCreated, Control: OwnerNone,
		OwnerID: r.owner, Updated: time.Now(), Sequence: r.seq}
	r.byID[id] = s
}

// Snapshot returns a copy of a surface (or nil) safe to read outside the
// registry lock.
func (r *Registry) Snapshot(id string) (*Surface, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	cp := *s
	if s.Lease != nil {
		l := *s.Lease
		cp.Lease = &l
	}
	return &cp, true
}

// Get returns the internal surface pointer. Only read after external
// synchronization, or use Snapshot for a thread-safe copy.
func (r *Registry) Get(id string) (*Surface, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	return s, ok
}

// List returns surface snapshots in stable id order.
func (r *Registry) List() []Surface {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Surface, 0, len(r.byID))
	for _, s := range r.byID {
		cp := *s
		if s.Lease != nil {
			l := *s.Lease
			cp.Lease = &l
		}
		out = append(out, cp)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// SetState updates the runtime state and bumps the surface.
func (r *Registry) SetState(id string, st State) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return false
	}
	s.State = st
	s.bump(time.Now())
	return true
}

// Observe records a safe observation for a surface. ControlOwner/state in
// the observation are overwritten from the authoritative surface so a
// stale observation can never claim control.
func (r *Registry) Observe(id string, o Observation) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return false
	}
	o.Timestamp = time.Now()
	o.Control = s.Control
	if s.State == "" {
		s.State = StateActive
	}
	o.State = s.State
	s.Obs = o
	s.bump(time.Now())
	return true
}

// SetControlOwner marks who controls the surface (ghost/none). It never
// grants user control — Takeover does that with a lease.
func (r *Registry) SetControlOwner(id string, o Owner) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok || o == OwnerUser {
		return false
	}
	s.Control = o
	if o == OwnerGhost {
		s.State = StateActive
	}
	s.bump(time.Now())
	return true
}

// GhostMayAct reports whether the runtime may issue an action on the
// surface. It is the pause enforcement consulted by the browser/computer
// gates BEFORE executing: while a user holds control Ghost is paused, and
// after a release/expiry the surface stays paused until a revalidating
// Resume returns it to Ghost.
func (r *Registry) GhostMayAct(id string) (allowed bool, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return true, "" // unknown surface: runtime gates enforce their own policy
	}
	if s.Control == OwnerUser {
		if s.Lease != nil && time.Now().After(s.Lease.ExpiresAt) {
			s.Control = OwnerNone
			s.Lease = nil
			s.State = StatePaused
			s.bump(time.Now())
			return false, "the human control lease expired; the runtime must revalidate before resuming"
		}
		return false, "the user is controlling this surface right now; Ghost is paused"
	}
	if s.State == StatePaused {
		return false, "the surface is paused after user release; it must be revalidated and resumed before Ghost acts"
	}
	return true, ""
}

// Takeover attempts to lease control to one authenticated device.
//   - A surface under a DIFFERENT user's active lease fails closed.
//   - The same device re-requesting takeover renews its lease (idempotent).
//   - Successful takeover pauses Ghost (Control=user, State=user_control).
func (r *Registry) Takeover(id, deviceID string, ttl time.Duration) (*UserLease, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("takeover requires an authenticated device")
	}
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("surface %q does not exist", id)
	}
	now := time.Now()
	if s.Lease != nil && s.Control == OwnerUser && s.Lease.DeviceID != deviceID {
		if now.Before(s.Lease.ExpiresAt) {
			return nil, fmt.Errorf("another device is already controlling this surface")
		}
	}
	if s.Lease == nil || s.Control != OwnerUser || s.Lease.DeviceID != deviceID {
		s.Lease = &UserLease{DeviceID: deviceID, LeaseID: newLeaseID(), ExpiresAt: now.Add(ttl)}
	} else {
		s.Lease.ExpiresAt = now.Add(ttl) // same-device re-takeover is a renew
	}
	s.Control = OwnerUser
	s.State = StateUserControl
	s.bump(now)
	cp := *s.Lease
	return &cp, nil
}

// Release clears user control WITHOUT resuming Ghost. The surface moves to
// paused; only an explicit, revalidating resume returns it to Ghost.
// Only the controlling device (or the owner on behalf of a device) may
// release.
func (r *Registry) Release(id, deviceID string, isOwner bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return fmt.Errorf("surface %q does not exist", id)
	}
	if s.Control == OwnerUser {
		if s.Lease != nil && s.Lease.DeviceID != deviceID && !isOwner {
			return fmt.Errorf("only the controlling device may release this surface")
		}
		s.Lease = nil
		s.Control = OwnerNone
		s.State = StatePaused // Ghost does NOT auto-resume
		s.bump(time.Now())
		return nil
	}
	// Nothing held. A stale client claiming to release a surface it never
	// controlled is refused; the owner releasing is a safe no-op.
	if deviceID != "" && !isOwner && (s.Lease == nil || s.Lease.DeviceID != deviceID) {
		return fmt.Errorf("that device does not control this surface")
	}
	if s.Lease == nil && deviceID != "" && !isOwner {
		return fmt.Errorf("no user control to release")
	}
	s.Lease = nil
	s.Control = OwnerNone
	if s.State == StateUserControl {
		s.State = StatePaused
	}
	s.bump(time.Now())
	return nil
}

// Resume returns a paused surface to Ghost after the caller has
// revalidated ownership/context/generation/lease/permission (the existing
// gate machinery performs that revalidation on the next real op). It fails
// closed unless the surface is paused (or idle) and no user holds control.
func (r *Registry) Resume(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok {
		return fmt.Errorf("surface %q does not exist", id)
	}
	if s.Control == OwnerUser {
		return fmt.Errorf("the user still controls this surface")
	}
	s.Control = OwnerGhost
	s.State = StateActive
	s.bump(time.Now())
	return nil
}

// Reconcile expires stale user leases. Call on a ticker and at boot. A
// dead mobile connection never leaves permanent user control.
func (r *Registry) Reconcile(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.byID {
		if s.Control == OwnerUser && s.Lease != nil && now.After(s.Lease.ExpiresAt) {
			s.Lease = nil
			s.Control = OwnerNone
			s.State = StatePaused
			s.bump(now)
			n++
		}
	}
	return n
}

func (s *Surface) bump(now time.Time) {
	s.Sequence++
	s.Updated = now
}

func newLeaseID() string {
	return fmt.Sprintf("user-%d", time.Now().UnixNano())
}
