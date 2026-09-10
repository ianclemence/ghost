// Package contextcache is a bounded, versioned cache for compiled context.
//
// It exists because rebuilding logically identical context on every internal
// turn is wasted work (the software analogue of avoiding repeated prefill).
// Every entry is keyed by the inputs that produced it and expires by TTL; an
// explicit version in the key is how callers invalidate when memory or task
// state changes, rather than guessing. Stats are exposed so cache behaviour is
// observable (hit/miss/invalidations) instead of invisible.
package contextcache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

type entry struct {
	value   string
	expires time.Time
}

// Stats is a snapshot of cache behaviour for Doctor/debug output.
type Stats struct {
	Hits          int64
	Misses        int64
	Invalidations int64
	Size          int
}

// Cache is a thread-safe bounded LRU with TTL and prefix invalidation. The
// zero value is not usable; construct with New.
type Cache struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	items map[string]*entry
	order []string // LRU: front = oldest
	hits  int64
	miss  int64
	inval int64
}

// New builds a cache holding at most max entries, each valid for ttl.
// A ttl <= 0 means entries never expire on their own (still bounded by max).
func New(ttl time.Duration, max int) *Cache {
	if max <= 0 {
		max = 64
	}
	return &Cache{ttl: ttl, max: max, items: map[string]*entry{}}
}

// Get returns the cached value and true on a live hit. Expired entries are
// evicted and counted as misses.
func (c *Cache) Get(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		c.miss++
		return "", false
	}
	if c.ttl > 0 && time.Now().After(e.expires) {
		c.removeLocked(key)
		c.miss++
		return "", false
	}
	c.touchLocked(key)
	c.hits++
	return e.value, true
}

// Put stores a value, evicting the least-recently-used entry if at capacity.
func (c *Cache) Put(key, value string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	exp := time.Time{}
	if c.ttl > 0 {
		exp = time.Now().Add(c.ttl)
	}
	if _, exists := c.items[key]; !exists && len(c.items) >= c.max {
		// Evict LRU (front).
		if len(c.order) > 0 {
			c.removeLocked(c.order[0])
		}
	}
	c.items[key] = &entry{value: value, expires: exp}
	c.touchLocked(key)
}

// Invalidate drops every entry whose key has the given prefix. It is how a
// caller invalidates a whole dependency class (e.g. all sessions after a
// memory change) without enumerating keys.
func (c *Cache) Invalidate(prefix string) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var dropped []string
	for k := range c.items {
		if strings.HasPrefix(k, prefix) {
			dropped = append(dropped, k)
		}
	}
	for _, k := range dropped {
		c.removeLocked(k)
	}
	c.inval += int64(len(dropped))
	return len(dropped)
}

// Stats returns a snapshot of cache behaviour.
func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Hits: c.hits, Misses: c.miss, Invalidations: c.inval, Size: len(c.items)}
}

func (c *Cache) touchLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

func (c *Cache) removeLocked(key string) {
	delete(c.items, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// Key builds a stable cache key from parts. Empty parts are skipped. The result
// is a short hash so keys stay bounded regardless of input length.
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "ctx:" + hex.EncodeToString(h.Sum(nil)[:16])
}
