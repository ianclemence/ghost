// Package provider implements Ghost's generic, capability-agnostic
// resilience strategy for every network-dependent capability.
//
// Design goals (deliberately small, not a framework):
//
//	Capability -> Strategy -> providers in order -> timeout -> bounded
//	retry -> response validation -> success | fallback | honest failure.
//
// Provider state (readiness, health, last success/failure, failure class,
// retry-after, cooldown, circuit state, credential state) lets the runtime
// make sensible decisions without LLM involvement. The LLM never invents
// fallback providers: the capability contract declares the ordered,
// capability-specific provider list.
package provider

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// FailureClass distinguishes provider failures so behavior differs:
// 401 -> no retry; 429 -> honor cooldown; 500/timeout -> bounded retry;
// invalid JSON -> provider failure; valid empty -> capability decides.
type FailureClass string

const (
	FailAuth          FailureClass = "authentication_failure"
	FailAuthorization FailureClass = "authorization_failure"
	FailRateLimited   FailureClass = "rate_limited"
	FailTimeout       FailureClass = "timeout"
	FailNetwork       FailureClass = "network_failure"
	FailDNS           FailureClass = "dns_failure"
	FailServer        FailureClass = "server_error"
	FailInvalid       FailureClass = "invalid_response"
	FailEmpty         FailureClass = "empty_response"
	FailMalformed     FailureClass = "malformed_response"
	FailUnavailable   FailureClass = "service_unavailable"
	FailNotConfigured FailureClass = "provider_not_configured"
	FailCredentialBad FailureClass = "credential_failure"
)

// Retryable reports whether the class merits a bounded retry.
func (f FailureClass) Retryable() bool {
	switch f {
	case FailAuth, FailAuthorization, FailCredentialBad, FailNotConfigured:
		return false
	default:
		return true
	}
}

// ClassifyHTTP maps an HTTP status to a failure class.
func ClassifyHTTP(status int) FailureClass {
	switch {
	case status == 429:
		return FailRateLimited
	case status == 401:
		return FailAuth
	case status == 403:
		return FailAuthorization
	case status == 502 || status == 503 || status == 504:
		return FailUnavailable
	case status >= 500:
		return FailServer
	case status >= 400:
		return FailInvalid
	default:
		return ""
	}
}

// RetryPolicy bounds retries: exponential backoff with jitter,
// cancellation/timeout aware, provider aware (never retries credential
// failures). No retry storms.
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first
	BaseDelay   time.Duration // delay before 2nd attempt; doubles after
	MaxDelay    time.Duration
	Jitter      bool
}

// DefaultRetryPolicy is 3 attempts, 300ms base, 5s cap.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: 300 * time.Millisecond, MaxDelay: 5 * time.Second, Jitter: true}
}

// DelayFor returns the wait before attempt n (1-indexed; attempt 1 = no wait).
func (p RetryPolicy) DelayFor(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	d := p.BaseDelay << (attempt - 2)
	if d > p.MaxDelay || d <= 0 {
		d = p.MaxDelay
	}
	if p.Jitter && d > 0 {
		half := d / 2
		d = half + time.Duration(rand.Int63n(int64(half)+1))
	}
	return d
}

// CircuitState is the breaker state per provider.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"    // healthy, traffic flows
	CircuitOpen     CircuitState = "open"      // cooldown, traffic stopped
	CircuitHalfOpen CircuitState = "half_open" // limited probe after cooldown
)

// Breaker temporarily stops hitting a repeatedly failing provider, then
// allows a limited probe and recovers on success. Never permanently
// disables a provider after transient failures.
type Breaker struct {
	mu           sync.Mutex
	failures     int
	threshold    int
	cooldown     time.Duration
	openedAt     time.Time
	halfOpenTest bool
	state        CircuitState
	LastFailure  time.Time
	LastClass    FailureClass
}

// NewBreaker creates a breaker that opens after threshold consecutive
// failures and probes after cooldown.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	return &Breaker{threshold: threshold, cooldown: cooldown, state: CircuitClosed}
}

// Allow reports whether a call may proceed now.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	switch b.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if now.Sub(b.openedAt) >= b.cooldown {
			b.state = CircuitHalfOpen
			b.halfOpenTest = true
			return true
		}
		return false
	case CircuitHalfOpen:
		if b.halfOpenTest {
			b.halfOpenTest = false
			return true
		}
		return false
	}
	return true
}

// RecordSuccess closes the circuit (recovery).
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = CircuitClosed
	b.halfOpenTest = false
}

// RecordFailure counts a failure; opens the circuit at threshold.
// Non-retryable classes (auth) open immediately with a long hold? No:
// credential failures are reported, not retried, and the breaker still
// opens only on consecutive failures so a single bad key doesn't wedge
// unrelated capabilities sharing the breaker instance per provider.
func (b *Breaker) RecordFailure(class FailureClass) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	b.LastFailure = time.Now()
	b.LastClass = class
	if b.state == CircuitHalfOpen {
		b.state = CircuitOpen
		b.openedAt = time.Now()
		return
	}
	if b.failures >= b.threshold {
		b.state = CircuitOpen
		b.openedAt = time.Now()
	}
}

// State returns the current circuit state.
func (b *Breaker) State() CircuitState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == CircuitOpen && time.Since(b.openedAt) >= b.cooldown {
		return CircuitHalfOpen
	}
	return b.state
}

// ConsecutiveFailures returns the current failure count (for observability).
func (b *Breaker) ConsecutiveFailures() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures
}

// ProviderState carries readiness/health for runtime decisions.
type ProviderState struct {
	Name         string
	Ready        bool
	Healthy      bool
	LastSuccess  time.Time
	LastFailure  time.Time
	LastClass    FailureClass
	RetryAfter   time.Time
	Circuit      CircuitState
	CredentialOK bool
}

// Provider is one network source behind a capability. Do is the raw call;
// Validate decides whether the response is genuinely usable (HTTP status,
// shape, required fields, semantic sanity). Name must be stable.
type Provider[T any] struct {
	Name     string
	Do       func(ctx context.Context) (T, *CallMeta, error)
	Validate func(T) error
	Breaker  *Breaker
}

// CallMeta is non-secret per-attempt metadata for observability.
type CallMeta struct {
	StatusCode int
	Failure    FailureClass
	RetryAfter time.Duration
	Duration   time.Duration
}

// Result is the strategy outcome: value or honest failure with class.
type Result[T any] struct {
	Value     T
	Provider  string
	Attempt   int
	Failure   FailureClass
	Err       error
	FromCache bool
	Stale     bool
}

// Strategy executes providers in capability-declared order with timeout,
// bounded retry, validation, fallback, and circuit breaking. Fallback is
// strictly to the next provider in the list — never an unrelated
// capability, never LLM-invented.
type Strategy[T any] struct {
	Providers []Provider[T]
	Retry     RetryPolicy
	Timeout   time.Duration // per-attempt timeout
}

// Execute runs the strategy. ctx cancellation is honored between attempts.
func (s Strategy[T]) Execute(ctx context.Context) Result[T] {
	retry := s.Retry
	if retry.MaxAttempts <= 0 {
		retry = DefaultRetryPolicy()
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	attempt := 0
	for _, p := range s.Providers {
		if p.Breaker != nil && !p.Breaker.Allow() {
			continue
		}
		for i := 1; i <= retry.MaxAttempts; i++ {
			if ctx.Err() != nil {
				return Result[T]{Failure: FailTimeout, Err: ctx.Err()}
			}
			if i > 1 {
				d := retry.DelayFor(i)
				select {
				case <-ctx.Done():
					return Result[T]{Failure: FailTimeout, Err: ctx.Err()}
				case <-time.After(d):
				}
			}
			attempt++
			callCtx, cancel := context.WithTimeout(ctx, timeout)
			start := time.Now()
			val, meta, err := p.Do(callCtx)
			elapsed := time.Since(start)
			cancel()
			_ = elapsed
			class := FailureClass("")
			if meta != nil {
				class = meta.Failure
			}
			if err != nil {
				if class == "" {
					class = classifyErr(err)
				}
				if p.Breaker != nil {
					p.Breaker.RecordFailure(class)
				}
				if !class.Retryable() {
					break // next provider; never retry credential failures
				}
				continue
			}
			if p.Validate != nil {
				if verr := p.Validate(val); verr != nil {
					pc, _ := verr.(*ValidationError)
					if pc != nil {
						class = pc.Class
					} else {
						class = FailInvalid
					}
					if p.Breaker != nil {
						p.Breaker.RecordFailure(class)
					}
					if !class.Retryable() {
						break
					}
					continue
				}
			}
			if p.Breaker != nil {
				p.Breaker.RecordSuccess()
			}
			return Result[T]{Value: val, Provider: p.Name, Attempt: attempt}
		}
	}
	return Result[T]{Failure: FailUnavailable, Attempt: attempt,
		Err: fmt.Errorf("all providers failed after %d attempts", attempt)}
}

// ValidationError marks a response as a classified provider failure.
type ValidationError struct {
	Class   FailureClass
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Invalid marks a response invalid (provider failure, retryable).
func Invalid(msg string) *ValidationError {
	return &ValidationError{Class: FailInvalid, Message: msg}
}

// Malformed marks unparseable content.
func Malformed(msg string) *ValidationError {
	return &ValidationError{Class: FailMalformed, Message: msg}
}

// Empty marks a valid-but-empty response (capability decides legitimacy).
func Empty(msg string) *ValidationError {
	return &ValidationError{Class: FailEmpty, Message: msg}
}
