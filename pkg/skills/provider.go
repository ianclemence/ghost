package skills

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProviderStatus distinguishes capability health from provider health.
// Capability READY + Provider UNAVAILABLE must not look like a broken skill.
type ProviderStatus string

const (
	ProviderOK           ProviderStatus = "ok"
	ProviderRateLimited  ProviderStatus = "rate_limited"
	ProviderUnavailable  ProviderStatus = "unavailable"
	ProviderInvalid      ProviderStatus = "invalid"
	ProviderUnauthorized ProviderStatus = "unauthorized"
)

// ProviderResult is the typed outcome of one provider attempt.
type ProviderResult struct {
	Status  ProviderStatus `json:"status"`
	Content string         `json:"content,omitempty"`
	Source  string         `json:"source,omitempty"`
	Age     time.Duration  `json:"age,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// Provider is one data source for a capability.
type Provider struct {
	Name     string
	Priority int
	Timeout  time.Duration
	// Try executes the provider. It must respect ctx timeout.
	Try func(ctx context.Context) ProviderResult
	// Validate reports whether content is usable.
	Validate func(content string) bool
}

type providerHealth struct {
	failCount   int
	cooldownUntil time.Time
	lastError string
}

// ProviderManager is the generic, small resilience layer:
//
//	Capability -> Manager -> Provider A -> validate -> success
//	                                    -> invalid -> Provider B -> ...
//	                                    -> all fail -> clean failure.
//
// It provides priority, timeout, bounded retries, circuit breaking
// (3 consecutive fails -> 5m cooldown), response validation, and
// short-TTL caching for idempotent reads. No microservices, no DB.
type ProviderManager struct {
	mu     sync.Mutex
	health map[string]*providerHealth
	cache  map[string]cachedResult
}

type cachedResult struct {
	content string
	source  string
	at      time.Time
	ttl     time.Duration
}

// NewProviderManager creates an empty manager.
func NewProviderManager() *ProviderManager {
	return &ProviderManager{health: map[string]*providerHealth{}, cache: map[string]cachedResult{}}
}

// sharedManager is the process-wide instance so health/circuit state and
// cache survive across turns.
var sharedManager = NewProviderManager()

// SharedProviderManager returns the process-wide manager.
func SharedProviderManager() *ProviderManager { return sharedManager }

// Execute runs providers in priority order, at most maxAttempts total.
// It never lets the model wander: selection is controlled here, not by LLM.
func (m *ProviderManager) Execute(ctx context.Context, capabilityID string, providers []Provider, maxAttempts int) ProviderResult {
	if maxAttempts <= 0 {
		maxAttempts = 2
	}
	// Priority order (stable).
	sorted := append([]Provider(nil), providers...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Priority < sorted[i].Priority {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	attempts := 0
	var lastErr string
	for _, p := range sorted {
		if attempts >= maxAttempts {
			break
		}
		m.mu.Lock()
		h := m.health[capabilityID+"/"+p.Name]
		if h == nil {
			h = &providerHealth{}
			m.health[capabilityID+"/"+p.Name] = h
		}
		if time.Now().Before(h.cooldownUntil) {
			m.mu.Unlock()
			continue // circuit open — try next provider, don't hammer.
		}
		m.mu.Unlock()

		timeout := p.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		res := p.Try(attemptCtx)
		cancel()
		attempts++

		valid := res.Status == ProviderOK && (p.Validate == nil || p.Validate(res.Content))
		if valid {
			m.recordSuccess(capabilityID + "/" + p.Name)
			return res
		}
		// Classify for observability; don't expose internals to user.
		errText := res.Error
		if errText == "" {
			errText = res.Content
		}
		m.recordFailure(capabilityID+"/"+p.Name, errText)
		lastErr = errText
		// Rate-limited / unauthorized: don't retry same provider, move on.
		// Invalid content: also move to next provider (bounded).
	}
	return ProviderResult{Status: ProviderUnavailable, Error: lastErr, Content: fmt.Sprintf("all providers failed for %s", capabilityID)}
}

func (m *ProviderManager) recordSuccess(key string) {
	m.mu.Lock()
	m.health[key] = &providerHealth{}
	m.mu.Unlock()
}

func (m *ProviderManager) recordFailure(key, errText string) {
	m.mu.Lock()
	h := m.health[key]
	if h == nil {
		h = &providerHealth{}
		m.health[key] = h
	}
	h.failCount++
	h.lastError = truncateErr(errText)
	if h.failCount >= 3 {
		h.cooldownUntil = time.Now().Add(5 * time.Minute)
	}
	m.mu.Unlock()
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// Cached returns a fresh cached result if within TTL.
func (m *ProviderManager) Cached(key string) (ProviderResult, bool) {
	m.mu.Lock()
	c, ok := m.cache[key]
	m.mu.Unlock()
	if !ok {
		return ProviderResult{}, false
	}
	age := time.Since(c.at)
	if age > c.ttl {
		return ProviderResult{}, false
	}
	return ProviderResult{Status: ProviderOK, Content: c.content, Source: c.source, Age: age}, true
}

// Store caches a successful result.
func (m *ProviderManager) Store(key, content, source string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	m.mu.Lock()
	m.cache[key] = cachedResult{content: content, source: source, at: time.Now(), ttl: ttl}
	m.mu.Unlock()
}

// ClassifyHTTP maps status codes to provider states without leaking bodies.
func ClassifyHTTP(statusCode int) ProviderStatus {
	switch {
	case statusCode == 200:
		return ProviderOK
	case statusCode == 429:
		return ProviderRateLimited
	case statusCode == 401 || statusCode == 403:
		return ProviderUnauthorized
	case statusCode >= 500:
		return ProviderUnavailable
	default:
		return ProviderInvalid
	}
}
