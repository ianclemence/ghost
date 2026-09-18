package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/tools"
)

// stubWeatherTool stands in for the real weather tool. Its name maps to a
// read-only capability, so the fast-path's read-only filter keeps it, and it
// returns canned text so these tests never touch the network.
type stubWeatherTool struct{ answer string }

func (s stubWeatherTool) Name() string        { return "weather_now" }
func (s stubWeatherTool) Description() string { return "Current weather for a place." }
func (s stubWeatherTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{"type": "string"},
		},
		"required": []string{"location"},
	}
}
func (s stubWeatherTool) Execute(_ context.Context, _ map[string]interface{}) *tools.ToolResult {
	return tools.NewToolResult(s.answer)
}

// stingSidecar is a fake Sting engine: /health is up, /complete returns body
// and counts calls so a test can prove the fast-path never spent a turn.
func stingSidecar(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// stingLoop builds an AgentLoop with Sting enabled and the weather tool
// replaced by a stub. ledger mirrors GHOST_STING_LEDGER: "off" restores the
// confidence-only gate; "" falls back to the shipped priors, which put the
// positive-single scope below the auto-act bar.
func stingLoop(t *testing.T, sidecarURL, ledger string) *AgentLoop {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Sting.Enabled = true
	cfg.Sting.SidecarURL = sidecarURL
	cfg.Sting.TimeoutSecs = 5
	cfg.Sting.Ledger = ledger
	cfg.Sting.RolloutLog = "off"
	al, err := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	al.tools.Register(stubWeatherTool{answer: "It is 21C and clear in Berlin."})
	return al
}

func TestTryStingTurnExecutesReadOnlyCall(t *testing.T) {
	srv, _ := stingSidecar(t, `{"type":"call","confidence":0.9,"function_calls":[{"name":"weather_now","arguments":{"location":"Berlin"}}]}`)
	al := stingLoop(t, srv.URL, "off")

	ans, handled := al.tryStingTurn("what's the weather in Berlin right now", "sess-sting")
	if !handled {
		t.Fatalf("expected the fast-path to handle the turn, got %q", ans)
	}
	if !strings.Contains(ans, "21C") {
		t.Fatalf("expected the stub tool answer, got %q", ans)
	}
}

func TestTryStingTurnEscalatesLowConfidence(t *testing.T) {
	srv, _ := stingSidecar(t, `{"type":"call","confidence":0.1,"function_calls":[{"name":"weather_now","arguments":{"location":"Berlin"}}]}`)
	al := stingLoop(t, srv.URL, "off")

	if ans, handled := al.tryStingTurn("what's the weather in Berlin right now", "sess-sting"); handled {
		t.Fatalf("low confidence must escalate, got %q", ans)
	}
}

func TestTryStingTurnLedgerDisablesWeakScope(t *testing.T) {
	// Shipped priors measure positive-single at 0.84 < 0.90, so even a
	// confident single call must escalate: the ledger is the gate.
	srv, _ := stingSidecar(t, `{"type":"call","confidence":0.99,"function_calls":[{"name":"weather_now","arguments":{"location":"Berlin"}}]}`)
	al := stingLoop(t, srv.URL, "")

	if ans, handled := al.tryStingTurn("what's the weather in Berlin right now", "sess-sting"); handled {
		t.Fatalf("below-target scope must escalate even at 0.99, got %q", ans)
	}
}

func TestTryStingTurnUnscoredEscalatesWithoutMeasuredScope(t *testing.T) {
	// Tuned weights report no score; with no measured at-target scope the
	// gate must escalate rather than act blind.
	srv, _ := stingSidecar(t, `{"type":"call","function_calls":[{"name":"weather_now","arguments":{"location":"Berlin"}}]}`)
	al := stingLoop(t, srv.URL, "")

	if ans, handled := al.tryStingTurn("what's the weather in Berlin right now", "sess-sting"); handled {
		t.Fatalf("unscored turn without a measured scope must escalate, got %q", ans)
	}
}

func TestTryStingTurnRefusesNegatedWithoutCallingSidecar(t *testing.T) {
	srv, calls := stingSidecar(t, `{"type":"call","confidence":0.9,"function_calls":[{"name":"weather_now","arguments":{"location":"Berlin"}}]}`)
	al := stingLoop(t, srv.URL, "off")

	if ans, handled := al.tryStingTurn("don't check the weather in Berlin", "sess-sting"); handled {
		t.Fatalf("negated request must not be handled, got %q", ans)
	}
	if n := atomic.LoadInt32(calls); n != 0 {
		t.Fatalf("negated request must not reach the engine, calls=%d", n)
	}
}

func TestTryStingTurnEscalatesWhenSidecarDown(t *testing.T) {
	srv, _ := stingSidecar(t, `{}`)
	url := srv.URL
	srv.Close()

	al := stingLoop(t, url, "off")
	if ans, handled := al.tryStingTurn("what's the weather in Berlin right now", "sess-sting"); handled {
		t.Fatalf("a stopped sidecar must escalate, got %q", ans)
	}
}
