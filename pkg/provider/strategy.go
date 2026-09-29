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
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
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
	// FailBilling: the account is out of credit or over its quota. The
	// same request fails identically until the owner acts; never retried.
	FailBilling FailureClass = "billing"
	// FailRejected: the provider refused the request itself (a 4xx such as
	// 400, 404, 413, 422) — retrying the identical request cannot succeed.
	FailRejected FailureClass = "request_rejected"
)

// Retryable reports whether the class merits a bounded retry.
func (f FailureClass) Retryable() bool {
	switch f {
	case FailAuth, FailAuthorization, FailCredentialBad, FailNotConfigured, FailBilling, FailRejected:
		return false
	default:
		return true
	}
}

// Status codes as model providers report them: Ghost's HTTP provider
// ("Status: 402") and the Anthropic SDK (`POST "https://…": 402 Payment
// Required`).
var (
	llmStatusRe = regexp.MustCompile(`(?i)\bstatus:\s*(\d{3})\b`)
	sdkStatusRe = regexp.MustCompile(`"[a-zA-Z]+://[^"]*":\s*(\d{3})\b`)
)

// billingMarkers identify out-of-credit / over-quota bodies. Some providers
// send these with 429 (OpenAI insufficient_quota) or 400 (Anthropic's
// "credit balance is too low"), so the body wins over the status code.
var billingMarkers = []string{
	"insufficient balance", "insufficient_quota", "insufficient quota",
	"credit balance is too low", "exceeded your current quota",
	"payment required", "billing",
}

func isBillingError(msg string) bool {
	for _, m := range billingMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// ClassifyError maps a model/provider call error onto the failure taxonomy,
// so every layer retries exactly the same transient classes: timeouts, rate
// limits, network blips, unavailable servers. Auth, config, validation, and
// caller cancellation yield "" or a non-retryable class and never retry.
func ClassifyError(err error) FailureClass {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return FailTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ""
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return FailTimeout
		}
		msg := strings.ToLower(urlErr.Error())
		switch {
		case strings.Contains(msg, "no such host"), strings.Contains(msg, "dns"):
			return FailDNS
		case strings.Contains(msg, "refused"):
			return FailNetwork
		case strings.Contains(msg, "reset"), strings.Contains(msg, "broken pipe"):
			return FailNetwork
		}
		return FailNetwork
	}
	msg := strings.ToLower(err.Error())
	if isBillingError(msg) {
		return FailBilling
	}
	m := llmStatusRe.FindStringSubmatch(err.Error())
	if m == nil {
		m = sdkStatusRe.FindStringSubmatch(err.Error())
	}
	if m != nil {
		switch m[1] {
		case "429":
			return FailRateLimited
		case "401":
			return FailAuth
		case "402":
			return FailBilling
		case "403":
			return FailAuthorization
		case "408", "409", "425":
			return FailTimeout // request timeout / conflict / too early: transient
		case "502", "503", "504":
			return FailUnavailable
		}
		if strings.HasPrefix(m[1], "5") {
			return FailServer
		}
		if strings.HasPrefix(m[1], "4") {
			return FailRejected
		}
		return FailInvalid
	}
	switch {
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"):
		return FailRateLimited
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "invalid api key"), strings.Contains(msg, "authentication"):
		return FailAuth
	case strings.Contains(msg, "forbidden"):
		return FailAuthorization
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return FailTimeout
	case strings.Contains(msg, "connection reset"), strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"), strings.Contains(msg, "network is unreachable"):
		return FailNetwork
	}
	return FailServer
}

// DoWithRetry runs fn, retrying transient-class failures with the given
// backoff delays. Non-retryable classes return immediately, context
// cancellation is honored between attempts, and onRetry (may be nil)
// observes each scheduled retry.
func DoWithRetry[T any](ctx context.Context, delays []time.Duration, onRetry func(attempt int, class FailureClass), fn func() (T, error)) (T, error) {
	resp, err := fn()
	if err == nil {
		return resp, nil
	}
	class := ClassifyError(err)
	if class == "" || !class.Retryable() {
		return resp, err
	}
	for attempt, delay := range delays {
		select {
		case <-ctx.Done():
			return resp, err
		case <-time.After(delay):
		}
		if onRetry != nil {
			onRetry(attempt+2, class)
		}
		resp, err = fn()
		if err == nil {
			return resp, nil
		}
		if next := ClassifyError(err); next == "" || !next.Retryable() {
			return resp, err
		} else {
			class = next
		}
	}
	return resp, err
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

// Explain turns a model-call failure into one sentence the owner can act
// on. It never echoes provider response bodies (they can carry request
// ids, internal URLs, or prompt fragments). provider names who failed
// when known ("deepseek"); empty is fine.
func Explain(err error, provider string) string {
	if err == nil {
		return ""
	}
	who := "the AI provider"
	if p := strings.TrimSpace(provider); p != "" {
		who = p
	}
	switch ClassifyError(err) {
	case FailBilling:
		return "Your " + who + " account is out of credit or over its quota. Top it up, or switch to another model with /model."
	case FailAuth, FailCredentialBad:
		return who + " rejected Ghost's credentials. Reconnect it in settings, then try again."
	case FailAuthorization:
		return who + " refused access for this key or model. Check the key's permissions, or pick another model with /model."
	case FailRateLimited:
		return who + " is rate-limiting requests right now. Try again in a moment."
	case FailTimeout, FailNetwork, FailDNS:
		return "Ghost couldn't reach " + who + ". Check the connection (or that your local AI is running), then try again."
	case FailUnavailable, FailServer:
		return who + " is having trouble right now. Try again shortly, or switch models with /model."
	case FailRejected:
		return who + " rejected the request (the model may not exist or may not support it). Try another model with /model."
	}
	return "The model call failed. Try again, or switch models with /model."
}

// IsModelCallFailure reports whether a turn error came from the model call
// (as opposed to a store or tool error).
func IsModelCallFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "llm call failed") || strings.Contains(msg, "cooling down") ||
		strings.Contains(msg, "no available providers") || ClassifyError(err) == FailBilling
}

// TurnErrorText is what every surface (terminal, web, app, evaluation)
// shows for a failed turn: model failures explained in the owner's terms,
// anything else as-is. One function so the surfaces cannot disagree.
func TurnErrorText(err error, providerName string) string {
	if err == nil {
		return ""
	}
	if IsModelCallFailure(err) {
		return Explain(err, providerName)
	}
	return err.Error()
}
