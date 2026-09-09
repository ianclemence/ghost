package computer

import (
	"fmt"
	"sync"
	"time"
)

// Descriptor describes one computer Ghost may drive. Availability is
// always honest: local reflects the appliance itself, paired reflects
// last contact, and future placements report unavailable with a reason
// until they exist.
type Descriptor struct {
	ID           string
	Placement    Placement
	Owner        string
	DisplayName  string
	LastSeen     time.Time
	Capabilities []Op
	MaxSessions  int
}

// Availability is the product-level state of a computer. It uses Ghost's
// existing outcome vocabulary (available / unavailable / offline /
// temporarily_unavailable), never raw infrastructure errors.
type Availability struct {
	State  string
	Detail string
}

// Availability states.
const (
	Available              = "available"
	Unavailable            = "unavailable"
	Offline                = "offline"
	TemporarilyUnavailable = "temporarily_unavailable"
)

// stalePairedAfter bounds how long a paired computer may go silent before
// it is reported offline rather than available.
const stalePairedAfter = 5 * time.Minute

// AvailabilityOf answers whether a computer can be driven right now.
func AvailabilityOf(d Descriptor, now time.Time) Availability {
	switch d.Placement {
	case PlacementLocal:
		return Availability{State: Available, Detail: "Ghost appliance"}
	case PlacementPaired:
		if d.LastSeen.IsZero() {
			return Availability{State: Offline, Detail: "paired computer has never checked in"}
		}
		if now.Sub(d.LastSeen) > stalePairedAfter {
			return Availability{State: Offline, Detail: fmt.Sprintf("paired computer last seen %s", d.LastSeen.Format(time.RFC3339))}
		}
		return Availability{State: Available, Detail: "paired computer"}
	case PlacementRemote:
		return Availability{State: Unavailable, Detail: "remote computers are not enabled on this Ghost"}
	case PlacementSandbox:
		return Availability{State: Unavailable, Detail: "sandbox computers are not enabled on this Ghost"}
	default:
		return Availability{State: Unavailable, Detail: fmt.Sprintf("unknown placement %q", d.Placement)}
	}
}

// Registry holds the computers one Ghost knows about. It is inventory,
// not execution: driving a computer still requires a lease.
type Registry struct {
	mu       sync.RWMutex
	computers map[string]Descriptor
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{computers: map[string]Descriptor{}}
}

// Register adds or replaces a computer descriptor.
func (r *Registry) Register(d Descriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.computers[d.ID] = d
}

// Remove forgets a computer. Outstanding leases are unaffected (expiry
// and recovery handle them); removal only stops new acquisitions, which
// look the descriptor up first.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.computers, id)
}

// Get returns a descriptor and whether it is known.
func (r *Registry) Get(id string) (Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.computers[id]
	return d, ok
}

// List returns all known descriptors in stable order.
func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.computers))
	for _, d := range r.computers {
		out = append(out, d)
	}
	return out
}

// Touch records contact from a paired computer (heartbeat path).
func (r *Registry) Touch(id string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.computers[id]; ok {
		d.LastSeen = at
		r.computers[id] = d
	}
}
