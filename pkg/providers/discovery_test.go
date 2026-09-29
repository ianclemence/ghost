package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
)

func withFetchHook(t *testing.T, hook func(ctx context.Context, cfg *config.Config, name string) ([]string, error)) {
	t.Helper()
	prev := fetchModelsHook
	fetchModelsHook = hook
	InvalidateModelDiscovery()
	t.Cleanup(func() { fetchModelsHook = prev; InvalidateModelDiscovery() })
}

// OpenAI-compatible list: bearer auth, newest first, non-chat entries
// (embeddings, speech, images) dropped.
func TestFetchOpenAICompatModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-x" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"old-chat","created":1},{"id":"text-embedding-3-large","created":5},{"id":"new-chat","created":9},{"id":"whisper-1","created":3}]}`))
	}))
	defer srv.Close()
	ids, err := fetchOpenAICompatModels(context.Background(), srv.URL+"/v1", "sk-x")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(rankModels("zz", ids), ",")
	if got != "new-chat,old-chat" {
		t.Fatalf("got %s", got)
	}
	if _, err := fetchOpenAICompatModels(context.Background(), srv.URL+"/v1", "wrong"); err == nil {
		t.Fatal("HTTP 401 must be an error, not an empty list")
	}
}

// Anthropic list goes through the SDK: x-api-key auth, the configured
// "/v1" base normalized, pagination followed.
func TestFetchAnthropicModels(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/models" || r.Header.Get("X-Api-Key") != "sk-ant" {
			http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"no"}}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5-5","type":"model","display_name":"Opus 5.5","created_at":"2026-08-01T00:00:00Z"}],"has_more":true,"first_id":"claude-opus-5-5","last_id":"claude-opus-5-5"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-haiku-4-5","type":"model","display_name":"Haiku","created_at":"2025-10-01T00:00:00Z"}],"has_more":false,"first_id":"claude-haiku-4-5","last_id":"claude-haiku-4-5"}`))
	}))
	defer srv.Close()
	cfg := &config.Config{}
	cfg.Providers.Anthropic.APIKey = "sk-ant"
	cfg.Providers.Anthropic.APIBase = srv.URL + "/v1"
	ids, err := fetchAnthropicModels(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "claude-opus-5-5,claude-haiku-4-5" {
		t.Fatalf("got %v", ids)
	}
}

func TestFetchOllamaModelsSkipsEmbedders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:0.6b"},{"name":"nomic-embed-text:latest"}]}`))
	}))
	defer srv.Close()
	ids, err := fetchOllamaModels(context.Background(), srv.URL)
	if err != nil || strings.Join(ids, ",") != "qwen3:0.6b" {
		t.Fatalf("got %v %v", ids, err)
	}
}

// Recommended models the provider serves lead, in curated order; ones it
// doesn't serve are not invented.
func TestRankModelsCuratedFirst(t *testing.T) {
	got := rankModels("anthropic", []string{"claude-x-9", "claude-haiku-4-5", "claude-opus-5-5"})
	if strings.Join(got, ",") != "claude-opus-5-5,claude-haiku-4-5,claude-x-9" {
		t.Fatalf("got %v", got)
	}
}

// Every configured provider's live list reaches the picker; unreachable
// providers fall back to the catalog, say so, and carry the reason.
func TestDiscoveryFeedsModelOptions(t *testing.T) {
	withFetchHook(t, func(_ context.Context, _ *config.Config, name string) ([]string, error) {
		switch name {
		case "anthropic":
			return []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-new-6"}, nil
		case "deepseek":
			return nil, errors.New("dial tcp: no route to host")
		}
		return nil, errors.New("not configured in test")
	})
	cfg := &config.Config{}
	cfg.Providers.Anthropic.APIKey = "sk-ant"
	cfg.Providers.DeepSeek.APIKey = "sk-ds"

	all := DiscoverAll(context.Background(), cfg)
	if a := all["anthropic"]; a.Source != ModelSourceLive || len(a.Models) != 3 {
		t.Fatalf("anthropic: %+v", a)
	}
	if d := all["deepseek"]; d.Source != ModelSourceCatalog || d.Error == "" || len(d.Models) == 0 {
		t.Fatalf("deepseek must fall back to catalog with a reason: %+v", d)
	}
	if g := all["groq"]; g.Source != ModelSourceCatalog || g.Error != "" {
		t.Fatalf("unconfigured provider: catalog, no error: %+v", g)
	}

	targets := map[string]ModelOption{}
	for _, o := range AvailableModelOptions(cfg) {
		targets[o.Target] = o
	}
	for _, want := range []string{"anthropic:claude-opus-5-5", "anthropic:claude-sonnet-5-5", "anthropic:claude-new-6", "deepseek:deepseek-flash"} {
		if _, ok := targets[want]; !ok {
			t.Errorf("picker missing %s (have %v)", want, targets)
		}
	}
	if targets["anthropic:claude-new-6"].Source != ModelSourceLive {
		t.Error("discovered entry must be marked live")
	}
	if o, ok := FindModelOption(cfg, "claude-new-6"); !ok || o.Target != "anthropic:claude-new-6" {
		t.Errorf("a discovered model must resolve by bare id, got %+v %v", o, ok)
	}
}

// Changing a provider's key re-discovers at once instead of serving the
// list fetched with the old credential for hours.
func TestDiscoveryRefetchesOnCredentialChange(t *testing.T) {
	var n int32
	withFetchHook(t, func(context.Context, *config.Config, string) ([]string, error) {
		atomic.AddInt32(&n, 1)
		return []string{"m1"}, nil
	})
	cfg := &config.Config{}
	cfg.Providers.Groq.APIKey = "key-one"
	DiscoverModels(context.Background(), cfg, "groq")
	DiscoverModels(context.Background(), cfg, "groq")
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("fresh cache must be reused, fetched %d times", n)
	}
	cfg.Providers.Groq.APIKey = "key-two"
	DiscoverModels(context.Background(), cfg, "groq")
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("new key must re-discover, fetched %d times", n)
	}
}

// The hot path never blocks on the network: a cold cache answers from the
// catalog and fills in the background.
func TestCachedProviderModelsNonBlocking(t *testing.T) {
	release := make(chan struct{})
	withFetchHook(t, func(context.Context, *config.Config, string) ([]string, error) {
		<-release
		return []string{"live-model"}, nil
	})
	cfg := &config.Config{}
	cfg.Providers.Groq.APIKey = "k"
	start := time.Now()
	pm := CachedProviderModels(cfg, "groq")
	if time.Since(start) > 200*time.Millisecond || pm.Source != ModelSourceCatalog {
		t.Fatalf("cold read must return the catalog immediately: %+v", pm)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pm = CachedProviderModels(cfg, "groq"); pm.Source == ModelSourceLive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background refresh never landed: %+v", pm)
}

// OAuth logins count as configured (they were offered as "no API key").
func TestOAuthProviderIsAvailable(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.Anthropic.AuthMethod = "oauth"
	if ok, reason := PresetAvailable(cfg, "anthropic", "claude-opus-5-5"); !ok {
		t.Fatalf("oauth-configured anthropic unavailable: %s", reason)
	}
}

// A caller that stops waiting must not turn a slow-but-healthy provider
// into a cached failure: the fetch finishes detached and lands live.
func TestShortWaitDoesNotPoisonCache(t *testing.T) {
	withFetchHook(t, func(context.Context, *config.Config, string) ([]string, error) {
		time.Sleep(150 * time.Millisecond)
		return []string{"slow-model"}, nil
	})
	cfg := &config.Config{}
	cfg.Providers.Groq.APIKey = "k"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if pm := DiscoverModels(ctx, cfg, "groq"); pm.Source != ModelSourceCatalog {
		t.Fatalf("impatient caller gets the catalog: %+v", pm)
	}
	pm := DiscoverModels(context.Background(), cfg, "groq")
	if pm.Source != ModelSourceLive || pm.Models[0] != "slow-model" {
		t.Fatalf("slow fetch must land live, got %+v", pm)
	}
}
