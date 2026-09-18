package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/sting"
)

func stingTestSidecar(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		respond(w, r)
	}))
}

func stingTestConfig(url string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Sting.Enabled = true
	cfg.Sting.SidecarURL = url
	cfg.Sting.ConfidenceThreshold = 0.5
	cfg.Sting.TimeoutSecs = 5
	// Deterministic gate: confidence-only unless a test sets a ledger.
	cfg.Sting.Ledger = "off"
	return cfg
}

func stingUnscoredSidecar(t *testing.T) *httptest.Server {
	t.Helper()
	return stingTestSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "call",
			"function_calls": []map[string]interface{}{
				{"name": "web_search", "arguments": map[string]interface{}{"query": "x"}},
			},
		})
	})
}

func stingChat(p *StingProvider) (*LLMResponse, error) {
	return p.Chat(context.Background(),
		[]Message{{Role: "user", Content: "search for x"}},
		[]ToolDefinition{{Type: "function", Function: ToolFunctionDefinition{
			Name: "web_search", Description: "search",
			Parameters: map[string]interface{}{"type": "object"},
		}}},
		"sting/base", nil)
}

// TestStingProviderLedgerAuthorizesUnscored proves the wiring: tuned
// weights report no confidence, and only a measured at-target scope acts.
func TestStingProviderLedgerAuthorizesUnscored(t *testing.T) {
	srv := stingUnscoredSidecar(t)
	defer srv.Close()

	good := filepath.Join(t.TempDir(), "ledger.json")
	tr := sting.NewTracker([]sting.Prior{
		{Scope: sting.ScopePositiveSingle, N: 30, Correct: 29, Source: "test"},
	})
	if err := tr.Save(good); err != nil {
		t.Fatal(err)
	}
	cfg := stingTestConfig(srv.URL)
	cfg.Sting.Ledger = good
	resp, err := stingChat(NewStingProvider(cfg))
	if err != nil {
		t.Fatalf("expected ledger-authorized act, got %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("unexpected %+v", resp)
	}
}

// TestStingProviderLedgerEscalatesWeakScope is the other half: an
// unscored turn must not act for a scope below target.
func TestStingProviderLedgerEscalatesWeakScope(t *testing.T) {
	srv := stingUnscoredSidecar(t)
	defer srv.Close()

	weak := filepath.Join(t.TempDir(), "ledger.json")
	tr := sting.NewTracker([]sting.Prior{
		{Scope: sting.ScopePositiveSingle, N: 30, Correct: 20, Source: "test"},
	})
	if err := tr.Save(weak); err != nil {
		t.Fatal(err)
	}
	cfg := stingTestConfig(srv.URL)
	cfg.Sting.Ledger = weak
	if _, err := stingChat(NewStingProvider(cfg)); err == nil {
		t.Fatal("expected escalate on below-target scope")
	}
}

func TestStingProviderRoutesToolCall(t *testing.T) {
	srv := stingTestSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		conf := 0.9
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"type":       "call",
			"confidence": conf,
			"function_calls": []map[string]interface{}{
				{"name": "web_search", "arguments": map[string]interface{}{"query": "x"}},
			},
		})
	})
	defer srv.Close()

	p := NewStingProvider(stingTestConfig(srv.URL))
	resp, err := p.Chat(context.Background(),
		[]Message{{Role: "user", Content: "search for x"}},
		[]ToolDefinition{{Type: "function", Function: ToolFunctionDefinition{
			Name: "web_search", Description: "search",
			Parameters: map[string]interface{}{"type": "object"},
		}}},
		"sting/base", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "web_search" {
		t.Fatalf("unexpected %+v", resp)
	}
}

func TestStingProviderEscalatesOnLowConfidence(t *testing.T) {
	srv := stingTestSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		conf := 0.1
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"type":       "call",
			"confidence": conf,
			"function_calls": []map[string]interface{}{
				{"name": "web_search", "arguments": map[string]interface{}{"query": "x"}},
			},
		})
	})
	defer srv.Close()

	p := NewStingProvider(stingTestConfig(srv.URL))
	_, err := p.Chat(context.Background(),
		[]Message{{Role: "user", Content: "search for x"}},
		[]ToolDefinition{{Type: "function", Function: ToolFunctionDefinition{
			Name: "web_search", Description: "search",
			Parameters: map[string]interface{}{"type": "object"},
		}}},
		"sting/base", nil)
	if err == nil {
		t.Fatal("expected escalate error")
	}
}

func TestStingProviderEscalatesWhenDown(t *testing.T) {
	p := NewStingProvider(stingTestConfig("http://127.0.0.1:1"))
	_, err := p.Chat(context.Background(),
		[]Message{{Role: "user", Content: "search for x"}},
		[]ToolDefinition{{Type: "function", Function: ToolFunctionDefinition{
			Name: "web_search", Description: "search",
			Parameters: map[string]interface{}{"type": "object"},
		}}},
		"sting/base", nil)
	if err == nil {
		t.Fatal("expected escalate error")
	}
}

func TestCreateProviderSting(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "sting"
	cfg.Agents.Defaults.Model = "base"
	p, err := CreateProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.GetDefaultModel() != StingBaseModel {
		t.Fatalf("unexpected default %q", p.GetDefaultModel())
	}
}
