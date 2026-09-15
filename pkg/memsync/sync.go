// Package memsync implements operation-based memory synchronization between
// phone and Pod. No whole-database copies, no naive last-write-wins: every
// synchronized mutation is an Op with identity, ordering, dedup, tombstones
// and versioning metadata.
package memsync

import (
	"errors"
	"sort"
	"sync"
)

// Scope determines ownership and replication for one entity kind.
type Scope string

const (
	// ScopePhoneLocal never leaves the phone (UI state, active mobile session).
	ScopePhoneLocal Scope = "phone_local"
	// ScopePodOwned lives on the Pod; phones get read projections.
	ScopePodOwned Scope = "pod_owned"
	// ScopePhoneOwned lives on the phone; Pod gets read projections.
	ScopePhoneOwned Scope = "phone_owned"
	// ScopeSharedDura is replicated durable identity/profile/preferences.
	ScopeSharedDura Scope = "shared_durable"
	// ScopeEphemeral is never synced (hot working context).
	ScopeEphemeral Scope = "ephemeral"
)

// OpType is the mutation kind.
type OpType string

const (
	OpUpsert OpType = "upsert"
	OpDelete OpType = "delete" // tombstone
)

// Op is one synchronized mutation.
type Op struct {
	OpID        string `json:"op_id"`
	Origin      string `json:"origin_device"`
	EntityID    string `json:"entity_id"`
	EntityKind  string `json:"entity_kind"`
	EntityVers  int64  `json:"entity_version"`
	Scope       Scope  `json:"scope"`
	Type        OpType `json:"type"`
	Payload     []byte `json:"payload,omitempty"`
	OriginClock int64  `json:"origin_clock"`
}

// Validate checks op coherence.
func (o Op) Validate() error {
	if o.OpID == "" || o.Origin == "" || o.EntityID == "" || o.EntityKind == "" {
		return errors.New("op_id, origin_device, entity_id, entity_kind required")
	}
	if o.EntityVers <= 0 || o.OriginClock <= 0 {
		return errors.New("entity_version and origin_clock must be positive")
	}
	if o.Type != OpUpsert && o.Type != OpDelete {
		return errors.New("unknown op type")
	}
	return nil
}

// Log is a deduplicating, idempotent op store. Apply is safe to call twice
// with the same op (duplicate sync events collapse) and out of order (per
// entity, highest version wins; deletes are tombstones that beat older
// upserts but lose to newer versions).
type Log struct {
	mu   sync.Mutex
	seen map[string]bool
	ents map[string]Op // entity key -> winning op
}

// NewLog builds an empty op log.
func NewLog() *Log {
	return &Log{seen: map[string]bool{}, ents: map[string]Op{}}
}

func key(o Op) string { return o.EntityKind + "\x00" + o.EntityID }

// Apply ingests one op. It returns true when the op changed visible state.
func (l *Log) Apply(o Op) (bool, error) {
	if err := o.Validate(); err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[o.OpID] {
		return false, nil // duplicate delivery: collapse
	}
	l.seen[o.OpID] = true
	cur, ok := l.ents[key(o)]
	if !ok || o.EntityVers > cur.EntityVers {
		l.ents[key(o)] = o
		return true, nil
	}
	if o.EntityVers == cur.EntityVers && o.Type == OpDelete && cur.Type != OpDelete {
		l.ents[key(o)] = o // same-version delete wins ties deterministically
		return true, nil
	}
	return false, nil // stale event
}

// Get returns the current op for an entity, if any.
func (l *Log) Get(kind, id string) (Op, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	o, ok := l.ents[kind+"\x00"+id]
	return o, ok
}

// Since returns ops with OriginClock greater than cursor, ordered by
// (OriginClock, OpID) for deterministic replay.
func (l *Log) Since(cursor int64) []Op {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Op
	for _, o := range l.ents {
		if o.OriginClock > cursor {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OriginClock != out[j].OriginClock {
			return out[i].OriginClock < out[j].OriginClock
		}
		return out[i].OpID < out[j].OpID
	})
	return out
}
