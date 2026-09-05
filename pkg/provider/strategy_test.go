package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func okProvider(name string, val string) Provider[string] {
	return Provider[string]{
		Name:     name,
		Validate: func(s string) error { return nil },
		Do: func(ctx context.Context) (string, *CallMeta, error) {
			return val, &CallMeta{StatusCode: 200}, nil
		},
		Breaker: NewBreaker(3, time.Minute),
	}
}

func failProvider(name string, class FailureClass, errMsg string) Provider[string] {
	return Provider[string]{
		Name:     name,
		Validate: func(s string) error { return nil },
		Do: func(ctx context.Context) (string, *CallMeta, error) {
			return "", &CallMeta{Failure: class}, errors.New(errMsg)
		},
		Breaker: NewBreaker(3, time.Minute),
	}
}

func TestPrimarySuccess(t *testing.T) {
	s := Strategy[string]{Providers: []Provider[string]{okProvider("p1", "ok")}, Retry: DefaultRetryPolicy()}
	r := s.Execute(context.Background())
	if r.Err != nil || r.Value != "ok" || r.Provider != "p1" {
		t.Fatalf("primary success failed: %+v", r)
	}
}

func TestFallbackSuccess(t *testing.T) {
	s := Strategy[string]{
		Providers: []Provider[string]{
			failProvider("p1", FailTimeout, "timeout"),
			okProvider("p2", "fallback-ok"),
		},
		Retry: RetryPolicy{MaxAttempts: 1},
	}
	r := s.Execute(context.Background())
	if r.Err != nil || r.Value != "fallback-ok" || r.Provider != "p2" {
		t.Fatalf("fallback failed: %+v", r)
	}
}

func TestNoRetryOnAuth(t *testing.T) {
	calls := 0
	p := Provider[string]{
		Name:     "auth",
		Validate: func(s string) error { return nil },
		Do: func(ctx context.Context) (string, *CallMeta, error) {
			calls++
			return "", &CallMeta{StatusCode: 401, Failure: FailAuth}, errors.New("401 unauthorized")
		},
		Breaker: NewBreaker(3, time.Minute),
	}
	s := Strategy[string]{Providers: []Provider[string]{p}, Retry: RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}}
	r := s.Execute(context.Background())
	if r.Err == nil {
		t.Fatal("expected failure")
	}
	if calls != 1 {
		t.Fatalf("auth failures must not retry, got %d calls", calls)
	}
	if FailAuth.Retryable() {
		t.Fatal("auth must not be retryable")
	}
}

func TestRateLimitRetryableButBounded(t *testing.T) {
	if !FailRateLimited.Retryable() {
		t.Fatal("rate limit should be retryable (bounded)")
	}
	calls := 0
	p := Provider[string]{
		Name:     "rl",
		Validate: func(s string) error { return nil },
		Do: func(ctx context.Context) (string, *CallMeta, error) {
			calls++
			return "", &CallMeta{StatusCode: 429, Failure: FailRateLimited}, errors.New("429 too many")
		},
	}
	s := Strategy[string]{Providers: []Provider[string]{p}, Retry: RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}}
	s.Execute(context.Background())
	if calls != 3 {
		t.Fatalf("expected exactly 3 bounded attempts, got %d", calls)
	}
}

func TestMalformedValidation(t *testing.T) {
	p := Provider[string]{
		Name: "bad",
		Do: func(ctx context.Context) (string, *CallMeta, error) {
			return `{"temperature":"banana"}`, &CallMeta{StatusCode: 200}, nil
		},
		Validate: func(s string) error { return Invalid("temperature not numeric") },
	}
	s := Strategy[string]{Providers: []Provider[string]{p}, Retry: RetryPolicy{MaxAttempts: 1}}
	r := s.Execute(context.Background())
	if r.Err == nil {
		t.Fatal("invalid response must fail honestly")
	}
}

func TestBreakerOpensAndRecovers(t *testing.T) {
	b := NewBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("call %d should be allowed", i)
		}
		b.RecordFailure(FailServer)
	}
	if b.State() != CircuitOpen {
		t.Fatalf("breaker should be open, got %s", b.State())
	}
	if b.Allow() {
		t.Fatal("open breaker must stop traffic")
	}
	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("breaker should allow probe after cooldown")
	}
	b.RecordSuccess()
	if b.State() != CircuitClosed {
		t.Fatal("breaker should recover after probe success")
	}
	// Half-open failure re-opens.
	for i := 0; i < 3; i++ {
		b.RecordFailure(FailServer)
	}
	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("should probe again")
	}
	b.RecordFailure(FailTimeout)
	if b.State() != CircuitOpen {
		t.Fatal("failed probe must re-open")
	}
}

func TestBreakerSkipsInStrategy(t *testing.T) {
	b := NewBreaker(1, time.Minute)
	b.RecordFailure(FailServer) // open
	p1 := okProvider("p1", "x")
	p1.Breaker = b
	s := Strategy[string]{Providers: []Provider[string]{p1, okProvider("p2", "y")}, Retry: RetryPolicy{MaxAttempts: 1}}
	r := s.Execute(context.Background())
	if r.Provider != "p2" {
		t.Fatalf("open provider must be skipped, got %+v", r)
	}
}

func TestCancellationAware(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Strategy[string]{Providers: []Provider[string]{okProvider("p", "x")}, Retry: DefaultRetryPolicy()}
	r := s.Execute(ctx)
	if r.Err == nil {
		t.Fatal("cancelled ctx must fail")
	}
}

func TestRetryBackoffBounded(t *testing.T) {
	p := DefaultRetryPolicy()
	d2 := p.DelayFor(2)
	if d2 < 150*time.Millisecond || d2 > 300*time.Millisecond {
		t.Fatalf("attempt-2 delay out of range: %v", d2)
	}
	if got := p.DelayFor(1); got != 0 {
		t.Fatalf("first attempt must not wait: %v", got)
	}
	// Growth bounded by MaxDelay.
	if got := p.DelayFor(30); got > p.MaxDelay {
		t.Fatalf("delay must be capped: %v", got)
	}
}

func TestClassifyHTTP(t *testing.T) {
	cases := map[int]FailureClass{429: FailRateLimited, 401: FailAuth, 403: FailAuthorization, 500: FailServer, 503: FailUnavailable, 400: FailInvalid}
	for code, want := range cases {
		if got := ClassifyHTTP(code); got != want {
			t.Fatalf("code %d: got %s want %s", code, got, want)
		}
	}
}

func TestCacheFreshAndStale(t *testing.T) {
	c := NewCache[string](50 * time.Millisecond)
	if _, ok, _, _ := c.Get("k"); ok {
		t.Fatal("empty cache must miss")
	}
	c.Set("k", "v", "open-meteo")
	v, ok, stale, prov := c.Get("k")
	if !ok || stale || v != "v" || prov != "open-meteo" {
		t.Fatalf("fresh hit wrong: %v %v %v %v", v, ok, stale, prov)
	}
	time.Sleep(60 * time.Millisecond)
	v, ok, stale, _ = c.Get("k")
	if !ok || !stale || v != "v" {
		t.Fatal("expired entry must report stale with last value")
	}
	c.Invalidate("k")
	if _, ok, _, _ := c.Get("k"); ok {
		t.Fatal("invalidated key must miss")
	}
}

func TestAllFailHonest(t *testing.T) {
	s := Strategy[string]{
		Providers: []Provider[string]{failProvider("a", FailServer, "500"), failProvider("b", FailTimeout, "timeout")},
		Retry:     RetryPolicy{MaxAttempts: 1},
	}
	r := s.Execute(context.Background())
	if r.Err == nil {
		t.Fatal("must fail honestly when all providers fail")
	}
	if fmt.Sprint(r.Failure) == "" {
		t.Fatal("must carry failure class")
	}
}
