package provider

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// classifyErr maps Go errors to failure classes without string-matching
// provider internals beyond what is needed for honest classification.
func classifyErr(err error) FailureClass {
	if errors.Is(err, context.DeadlineExceeded) {
		return FailTimeout
	}
	if errors.Is(err, context.Canceled) {
		return FailTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FailDNS
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return FailTimeout
		}
		return FailNetwork
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "network unreachable"), strings.Contains(msg, "connection reset"):
		return FailNetwork
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return FailTimeout
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"),
		strings.Contains(msg, "invalid api key"), strings.Contains(msg, "invalid key"):
		return FailAuth
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "403"):
		return FailAuthorization
	case strings.Contains(msg, "too many requests"), strings.Contains(msg, "429"),
		strings.Contains(msg, "rate limit"):
		return FailRateLimited
	case strings.Contains(msg, "not configured"), strings.Contains(msg, "missing key"),
		strings.Contains(msg, "no api key"):
		return FailNotConfigured
	default:
		return FailServer
	}
}

// Cache is a small selective cache with TTL, timestamp, provenance, and
// stale-data semantics. Good for weather/currency/static metadata. Never
// for mutable actions.
type Cache[T any] struct {
	mu    sync.RWMutex
	items map[string]cachedItem[T]
	ttl   time.Duration
}

type cachedItem[T any] struct {
	value      T
	storedAt   time.Time
	provenance string // which provider produced it
}

// NewCache creates a cache with the given TTL.
func NewCache[T any](ttl time.Duration) *Cache[T] {
	return &Cache[T]{items: map[string]cachedItem[T]{}, ttl: ttl}
}

// Get returns the value, whether it was served stale, and provenance.
// Fresh hits return stale=false. Expired entries return stale=true with
// the last value so the caller can decide (Ghost must know it is stale).
func (c *Cache[T]) Get(key string) (val T, ok bool, stale bool, provenance string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	it, found := c.items[key]
	if !found {
		var zero T
		return zero, false, false, ""
	}
	if time.Since(it.storedAt) <= c.ttl {
		return it.value, true, false, it.provenance
	}
	return it.value, true, true, it.provenance
}

// Set stores a value with its provenance.
func (c *Cache[T]) Set(key string, val T, provenance string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cachedItem[T]{value: val, storedAt: time.Now(), provenance: provenance}
}

// Invalidate drops a key.
func (c *Cache[T]) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}
