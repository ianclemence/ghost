package skills

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProviderPriorityAndFallback(t *testing.T) {
	m := NewProviderManager()
	providers := []Provider{
		{Name: "primary", Priority: 1, Timeout: time.Second, Try: func(ctx context.Context) ProviderResult {
			return ProviderResult{Status: ProviderUnavailable, Error: "down"}
		}, Validate: func(s string) bool { return s != "" }},
		{Name: "fallback", Priority: 2, Timeout: time.Second, Try: func(ctx context.Context) ProviderResult {
			return ProviderResult{Status: ProviderOK, Content: "Bangkok 27C", Source: "fallback"}
		}, Validate: func(s string) bool { return len(s) > 3 }},
	}
	res := m.Execute(context.Background(), "weather.current", providers, 2)
	if res.Status != ProviderOK || res.Content == "" {
		t.Fatalf("expected fallback success, got %+v", res)
	}
}

func TestProviderCircuitBreaking(t *testing.T) {
	m := NewProviderManager()
	bad := Provider{Name: "bad", Priority: 1, Try: func(ctx context.Context) ProviderResult {
		return ProviderResult{Status: ProviderUnavailable, Error: "down"}
	}}
	for i := 0; i < 3; i++ {
		m.Execute(context.Background(), "cap.test", []Provider{bad}, 1)
	}
	// 4th call should skip due to cooldown (no Try executed).
	called := false
	good := Provider{Name: "bad", Priority: 1, Try: func(ctx context.Context) ProviderResult {
		called = true
		return ProviderResult{Status: ProviderOK, Content: "x"}
	}}
	_ = good
	// Use same name to hit circuit; execute with only bad provider that would succeed if called.
	m2res := m.Execute(context.Background(), "cap.test", []Provider{{Name: "bad", Priority: 1, Try: func(ctx context.Context) ProviderResult {
		called = true
		return ProviderResult{Status: ProviderOK, Content: "ok"}
	}}}, 1)
	if called {
		t.Fatalf("circuit should be open after 3 fails")
	}
	if m2res.Status == ProviderOK {
		t.Fatalf("expected unavailable due to circuit, got ok")
	}
	_ = errors.New // keep import if unused in future
}

func TestProviderCache(t *testing.T) {
	m := NewProviderManager()
	m.Store("weather:bangkok", "27C", "wttr.in", 5*time.Minute)
	if res, ok := m.Cached("weather:bangkok"); !ok || res.Content != "27C" {
		t.Fatalf("expected cached result")
	}
}

func TestClassifyHTTP(t *testing.T) {
	if ClassifyHTTP(200) != ProviderOK {
		t.Fatalf("200 should be ok")
	}
	if ClassifyHTTP(429) != ProviderRateLimited {
		t.Fatalf("429 should be rate_limited")
	}
	if ClassifyHTTP(401) != ProviderUnauthorized {
		t.Fatalf("401 should be unauthorized")
	}
}
