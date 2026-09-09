package agent

import (
	"testing"
)

// benchT adapts *testing.B to the gate harness's minimal hT surface.
type benchT struct{ b *testing.B }

func (x benchT) Helper()                           {}
func (x benchT) TempDir() string                   { return x.b.TempDir() }
func (x benchT) Cleanup(f func())                  { x.b.Cleanup(f) }
func (x benchT) Fatal(args ...interface{})         { x.b.Fatal(args...) }
func (x benchT) Fatalf(f string, a ...interface{}) { x.b.Fatalf(f, a...) }

func benchHarness(b *testing.B) *gateHarness {
	b.Helper()
	return newGateHarnessOnWS(benchT{b}, b.TempDir())
}

// BenchmarkBrowserGateDecisionAllow measures the steady-state cost of a
// gate-allowed read-only browser decision (binding resolution + broker
// evaluate + session reuse). Tracked against the enable/disable of the
// gate; the binding + broker path must stay cheap because every model
// browser call pays it.
func BenchmarkBrowserGateDecisionAllow(b *testing.B) {
	h := benchHarness(b)
	al := h.loop
	// First call mints the session; benchmark the steady state.
	al.authorizeBrowserCall("bench-1", "bench-sess", "browser_navigate", map[string]interface{}{"url": "https://example.com"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := al.authorizeBrowserCall("bench-1", "bench-sess", "browser_navigate", map[string]interface{}{"url": "https://example.com"})
		if d.decision != "allow" {
			b.Fatalf("expected allow, got %s", d.decision)
		}
	}
}

// BenchmarkBrowserGateDecisionWait measures the approval-wait path cost
// (broker Require, which is idempotent per request id after the first).
func BenchmarkBrowserGateDecisionWait(b *testing.B) {
	h := benchHarness(b)
	al := h.loop
	al.authorizeBrowserCall("bench-wait", "bench-sess-w", "browser_click", map[string]interface{}{"ref": "@e1"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := al.authorizeBrowserCall("bench-wait", "bench-sess-w", "browser_click", map[string]interface{}{"ref": "@e1"})
		if d.decision != "wait" {
			b.Fatalf("expected wait, got %s", d.decision)
		}
	}
}
