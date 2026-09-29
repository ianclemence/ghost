package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ianclemence/ghost/pkg/auth"
	"github.com/ianclemence/ghost/pkg/config"
)

// Model discovery: every configured provider is asked which models it
// actually serves, so nobody has to type model ids into config. The built-in
// KnownProviderModels list is only the offline fallback and the source of
// each provider's recommended default — never the whole menu.
//
// Discovery is a read of provider metadata: no prompt, no user data, no
// cost. Results are cached per provider (live lists for discoveryTTL,
// failures for discoveryFailTTL so an offline Ghost doesn't re-dial on every
// picker open) and every list says where it came from.

const (
	discoveryTTL     = 6 * time.Hour
	discoveryFailTTL = 5 * time.Minute
	discoveryTimeout = 6 * time.Second
)

// Discovery sources.
const (
	ModelSourceLive    = "live"    // fetched from the provider just now or within the TTL
	ModelSourceCatalog = "catalog" // built-in list: provider unreachable or has no list endpoint
)

// ProviderModels is one provider's selectable models.
type ProviderModels struct {
	Provider  string    `json:"provider"`
	Models    []string  `json:"models"`
	Source    string    `json:"source"`
	Error     string    `json:"error,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
}

type discoveryEntry struct {
	result  ProviderModels
	expires time.Time
	// credFingerprint ties the entry to the credential/base it was fetched
	// with, so adding or changing a key re-discovers immediately.
	credFingerprint string
}

// flight is one in-progress fetch, shared by every caller that wants the
// same provider meanwhile. It runs detached with its own timeout: a caller
// that stops waiting (a picker with a short budget) must not cancel the
// fetch and have a healthy-but-slow provider cached as failed.
type flight struct {
	done chan struct{}
	res  ProviderModels
}

var discovery = struct {
	mu       sync.Mutex
	entries  map[string]discoveryEntry
	inflight map[string]*flight
}{entries: map[string]discoveryEntry{}, inflight: map[string]*flight{}}

// discoveryHTTP is the client used for list calls.
var discoveryHTTP = &http.Client{Timeout: discoveryTimeout}

// defaultAPIBases mirrors the endpoints CreateProvider dials for
// OpenAI-compatible providers, used when config leaves api_base empty.
var defaultAPIBases = map[string]string{
	"openai":       "https://api.openai.com/v1",
	"groq":         "https://api.groq.com/openai/v1",
	"deepseek":     "https://api.deepseek.com",
	"moonshot":     "https://api.moonshot.cn/v1",
	"qwen":         "https://dashscope.aliyuncs.com/compatible-mode/v1",
	"gemini":       "https://generativelanguage.googleapis.com/v1beta/openai",
	"zhipu":        "https://open.bigmodel.cn/api/paas/v4",
	"openrouter":   "https://openrouter.ai/api/v1",
	"nvidia":       "https://integrate.api.nvidia.com/v1",
	"shengsuanyun": "https://router.shengsuanyun.com/api/v1",
	"ollama":       "http://localhost:11434",
}

// DiscoverableProviders is every provider discovery knows how to list.
func DiscoverableProviders() []string {
	names := make([]string, 0, len(KnownProviderModels)+1)
	for n := range KnownProviderModels {
		names = append(names, n)
	}
	names = append(names, "vllm")
	sort.Strings(names)
	return names
}

// ProviderConfigured reports whether a provider has what it needs to serve:
// a credential (API key or sign-in) for cloud providers, an endpoint for
// vLLM; the local Ollama engine needs nothing.
func ProviderConfigured(cfg *config.Config, name string) bool { return providerConfigured(cfg, name) }

func providerConfigured(cfg *config.Config, name string) bool {
	switch name {
	case "ollama":
		return true
	case "vllm":
		return cfg.Providers.VLLM.APIBase != ""
	}
	return providerKey(cfg, name) != ""
}

// lookup returns the cache entry (if it belongs to the current credential)
// and whether it is still fresh.
func lookup(cfg *config.Config, name string) (ProviderModels, bool, bool) {
	fp := credFingerprint(cfg, name)
	discovery.mu.Lock()
	defer discovery.mu.Unlock()
	e, ok := discovery.entries[name]
	if !ok || e.credFingerprint != fp {
		return ProviderModels{}, false, false
	}
	return e.result, true, time.Now().Before(e.expires)
}

// startFlight joins the in-progress fetch for a provider or starts one.
func startFlight(cfg *config.Config, name string) *flight {
	discovery.mu.Lock()
	if f, ok := discovery.inflight[name]; ok {
		discovery.mu.Unlock()
		return f
	}
	f := &flight{done: make(chan struct{})}
	discovery.inflight[name] = f
	discovery.mu.Unlock()
	snapshot := cfg.Clone()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
		defer cancel()
		f.res = refreshProvider(ctx, snapshot, name)
		discovery.mu.Lock()
		delete(discovery.inflight, name)
		discovery.mu.Unlock()
		close(f.done)
	}()
	return f
}

// CachedProviderModels returns the cached list without blocking. When the
// entry is missing or stale it starts a background refresh and answers with
// the stale list, or the built-in catalog, meanwhile. Safe on hot paths.
func CachedProviderModels(cfg *config.Config, name string) ProviderModels {
	if cfg == nil || !providerConfigured(cfg, name) {
		return catalogModels(name, "")
	}
	res, known, fresh := lookup(cfg, name)
	if !fresh {
		startFlight(cfg, name)
	}
	if known {
		return res
	}
	return catalogModels(name, "")
}

// DiscoverModels returns one provider's list, fetching it when the cache is
// stale. It waits at most until ctx is done; the fetch itself continues and
// lands in the cache for the next caller.
func DiscoverModels(ctx context.Context, cfg *config.Config, name string) ProviderModels {
	if cfg == nil || !providerConfigured(cfg, name) {
		return catalogModels(name, "")
	}
	res, known, fresh := lookup(cfg, name)
	if fresh {
		return res
	}
	f := startFlight(cfg, name)
	select {
	case <-f.done:
		return f.res
	case <-ctx.Done():
		if known {
			return res
		}
		return catalogModels(name, "still loading the provider's model list")
	}
}

// DiscoverAll lists every provider concurrently, waiting at most until ctx
// is done. Unconfigured providers come back with their catalog so a console
// can still show what adding a key would unlock.
func DiscoverAll(ctx context.Context, cfg *config.Config) map[string]ProviderModels {
	out := map[string]ProviderModels{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range DiscoverableProviders() {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			pm := DiscoverModels(ctx, cfg, name)
			mu.Lock()
			out[name] = pm
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return out
}

// InvalidateModelDiscovery drops cached lists (all, or the named providers)
// so the next read re-fetches — used by explicit "refresh" actions.
func InvalidateModelDiscovery(names ...string) {
	discovery.mu.Lock()
	defer discovery.mu.Unlock()
	if len(names) == 0 {
		discovery.entries = map[string]discoveryEntry{}
		return
	}
	for _, n := range names {
		delete(discovery.entries, n)
	}
}

func refreshProvider(ctx context.Context, cfg *config.Config, name string) ProviderModels {
	fp := credFingerprint(cfg, name)
	models, err := fetchProviderModels(ctx, cfg, name)
	var res ProviderModels
	ttl := discoveryTTL
	if err != nil || len(models) == 0 {
		msg := "provider returned no models"
		if err != nil {
			msg = err.Error()
		}
		res = catalogModels(name, msg)
		ttl = discoveryFailTTL
	} else {
		res = ProviderModels{Provider: name, Models: rankModels(name, models), Source: ModelSourceLive, FetchedAt: time.Now()}
	}
	discovery.mu.Lock()
	discovery.entries[name] = discoveryEntry{result: res, expires: time.Now().Add(ttl), credFingerprint: fp}
	discovery.mu.Unlock()
	return res
}

func catalogModels(name, errMsg string) ProviderModels {
	return ProviderModels{Provider: name, Models: append([]string(nil), KnownProviderModels[name]...), Source: ModelSourceCatalog, Error: errMsg}
}

// credFingerprint identifies the credential and endpoint a list was fetched
// with, without holding the secret itself.
func credFingerprint(cfg *config.Config, name string) string {
	if cfg == nil {
		return ""
	}
	base, key := discoveryEndpoint(cfg, name)
	k := key
	if len(k) > 6 {
		k = k[len(k)-6:]
	}
	return fmt.Sprintf("%s|%d|%s", base, len(key), k)
}

func discoveryEndpoint(cfg *config.Config, name string) (base, key string) {
	var pc *config.ProviderConfig
	switch name {
	case "openai":
		pc = &cfg.Providers.OpenAI
	case "anthropic":
		pc = &cfg.Providers.Anthropic
	case "moonshot":
		pc = &cfg.Providers.Moonshot
	case "groq":
		pc = &cfg.Providers.Groq
	case "deepseek":
		pc = &cfg.Providers.DeepSeek
	case "gemini":
		pc = &cfg.Providers.Gemini
	case "zhipu":
		pc = &cfg.Providers.Zhipu
	case "openrouter":
		pc = &cfg.Providers.OpenRouter
	case "ollama":
		pc = &cfg.Providers.Ollama
	case "qwen":
		pc = &cfg.Providers.Qwen
	case "nvidia":
		pc = &cfg.Providers.Nvidia
	case "shengsuanyun":
		pc = &cfg.Providers.ShengSuanYun
	case "vllm":
		pc = &cfg.Providers.VLLM
	}
	if pc == nil {
		return "", ""
	}
	base = strings.TrimRight(strings.TrimSpace(pc.APIBase), "/")
	if base == "" {
		base = defaultAPIBases[name]
	}
	return base, pc.APIKey
}

// fetchModelsHook replaces the network fetch in tests.
var fetchModelsHook func(ctx context.Context, cfg *config.Config, name string) ([]string, error)

func fetchProviderModels(ctx context.Context, cfg *config.Config, name string) ([]string, error) {
	if fetchModelsHook != nil {
		return fetchModelsHook(ctx, cfg, name)
	}
	if testing.Testing() {
		// Unit tests across the repo build configs with fake keys; a
		// background refresh must never dial a real provider from them.
		return nil, fmt.Errorf("model discovery disabled under test")
	}
	switch name {
	case "anthropic":
		return fetchAnthropicModels(ctx, cfg)
	case "ollama":
		base, _ := discoveryEndpoint(cfg, name)
		return fetchOllamaModels(ctx, base)
	case "openai":
		if cfg.Providers.OpenAI.APIKey == "" {
			// A ChatGPT/Codex login has no model-list endpoint.
			return nil, fmt.Errorf("model listing needs an API key (signed in with %s)", cfg.Providers.OpenAI.AuthMethod)
		}
	}
	base, key := discoveryEndpoint(cfg, name)
	if base == "" {
		return nil, fmt.Errorf("no API base for %s", name)
	}
	return fetchOpenAICompatModels(ctx, base, key)
}

func fetchAnthropicModels(ctx context.Context, cfg *config.Config) ([]string, error) {
	opts := []option.RequestOption{option.WithMaxRetries(0), option.WithRequestTimeout(discoveryTimeout)}
	pc := cfg.Providers.Anthropic
	switch {
	case pc.APIKey != "":
		opts = append(opts, option.WithAPIKey(pc.APIKey))
	case pc.AuthMethod != "":
		cred, err := auth.GetCredential("anthropic")
		if err != nil || cred == nil {
			return nil, fmt.Errorf("no anthropic credential")
		}
		opts = append(opts, option.WithAuthToken(cred.AccessToken), option.WithHeaderAdd("anthropic-beta", "oauth-2025-04-20"))
	default:
		return nil, fmt.Errorf("no anthropic credential")
	}
	if b := normalizeAnthropicBase(pc.APIBase); b != "" {
		opts = append(opts, option.WithBaseURL(b))
	}
	client := anthropic.NewClient(opts...)
	var ids []string
	iter := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(100)})
	for iter.Next() {
		ids = append(ids, iter.Current().ID) // newest first, as the API returns them
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func fetchOpenAICompatModels(ctx context.Context, base, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" && key != "ollama" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := discoveryHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model list returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
		Models []struct { // Gemini native shape, if a native base is configured
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unreadable model list: %w", err)
	}
	sort.SliceStable(payload.Data, func(i, j int) bool { return payload.Data[i].Created > payload.Data[j].Created })
	var ids []string
	for _, d := range payload.Data {
		ids = append(ids, d.ID)
	}
	for _, m := range payload.Models {
		ids = append(ids, strings.TrimPrefix(m.Name, "models/"))
	}
	return ids, nil
}

func fetchOllamaModels(ctx context.Context, base string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/v1")+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := discoveryHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&tags); err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range tags.Models {
		// Embedding models serve the memory index, not conversation.
		if !isChatModelID(m.Name) {
			continue
		}
		ids = append(ids, m.Name)
	}
	return ids, nil
}

// nonChatMarkers identify list entries that cannot hold a conversation
// (embeddings, speech, images, moderation, legacy completions).
var nonChatMarkers = []string{
	"embed", "whisper", "tts", "dall-e", "moderation", "davinci", "babbage",
	"transcribe", "realtime", "audio", "image", "rerank", "sora", "-search-", "guard",
}

func isChatModelID(id string) bool {
	l := strings.ToLower(id)
	for _, m := range nonChatMarkers {
		if strings.Contains(l, m) {
			return false
		}
	}
	return l != ""
}

// rankModels keeps chat models only, puts the curated recommendations that
// the provider actually serves first (in curated order), then everything
// else in the provider's own order (newest first where it says so).
func rankModels(provider string, ids []string) []string {
	seen := map[string]bool{}
	served := map[string]bool{}
	for _, id := range ids {
		served[id] = true
	}
	var out []string
	for _, rec := range KnownProviderModels[provider] {
		if served[rec] && !seen[rec] {
			out = append(out, rec)
			seen[rec] = true
		}
	}
	for _, id := range ids {
		if seen[id] || !isChatModelID(id) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
