package agent

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/live"
)

// The gates must keep the Live Surface plane truthful: waiting while an
// approval pends, active while Ghost works, failed when execution errors,
// with a session-linked announcement on each transition.
func TestGateSurfaceStatesAndAnnouncements(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	al.SetLivePlane(live.NewRegistry(h.ghostID))
	b := bus.NewMessageBus()
	al.bus = b
	ch, unsub := b.SubscribeOutbound("test-states", false, 32)
	defer unsub()

	drain := func() map[string]interface{} {
		select {
		case msg := <-ch:
			return msg.Metadata
		case <-time.After(2 * time.Second):
			t.Fatalf("expected surface_update announcement")
			return nil
		}
	}
	findBrowser := func() *live.Surface {
		for _, s := range al.livePlane.List() {
			if s.Kind == live.KindBrowser {
				cp := s
				return &cp
			}
		}
		return nil
	}

	// 1. Consequential op parks for approval: waiting + announced.
	tc := fakeToolCall("browser_click", map[string]interface{}{"ref": "e1"})
	_, governed, _ := al.maybeRunBrowserTool(h.toolCtx(), al.tools, tc, h.opts("sess-wait"), nil)
	if !governed {
		t.Fatalf("click must be governed")
	}
	s := findBrowser()
	if s == nil || s.State != live.StateWaiting {
		t.Fatalf("surface must be waiting during approval, got %+v", s)
	}
	meta := drain()
	if meta["type"] != "surface_update" || meta["session_id"] != "sess-wait" {
		t.Fatalf("bad announcement: %v", meta)
	}

	// 2. Read-only op executes: active + announced.
	tc2 := fakeToolCall("browser_snapshot", map[string]interface{}{})
	res, _, _ := al.maybeRunBrowserTool(h.toolCtx(), al.tools, tc2, h.opts("sess-act"), nil)
	if res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	found := false
	for _, surf := range al.livePlane.List() {
		if surf.Kind == live.KindBrowser && surf.State == live.StateActive {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an active browser surface: %+v", al.livePlane.List())
	}
	meta = drain()
	if meta["type"] != "surface_update" || meta["session_id"] != "sess-act" {
		t.Fatalf("bad announcement: %v", meta)
	}

	// 3. One step erroring is not the task failing: the surface stays active
	// (Ghost usually tries another way) and is still announced. Only the
	// turn's outcome settles it, and a failed turn fails it.
	h.stubs["browser_snapshot"].failWith = true
	res, _, _ = al.maybeRunBrowserTool(h.toolCtx(), al.tools, tc2, h.opts("sess-fail"), nil)
	if !res.IsError {
		t.Fatalf("stub failure must propagate as error")
	}
	for _, surf := range al.livePlane.List() {
		if surf.Kind == live.KindBrowser && surf.Session == "sess-fail" && surf.State != live.StateActive {
			t.Fatalf("a failed step must leave the surface active, got %q", surf.State)
		}
	}
	if n := al.SettleSessionSurfaces("sess-fail", "failed"); n != 1 {
		t.Fatalf("a failed turn must settle its surface, got %d", n)
	}
	for _, surf := range al.livePlane.List() {
		if surf.Session == "sess-fail" && surf.State != live.StateFailed {
			t.Fatalf("a failed turn must fail its surface, got %q", surf.State)
		}
	}
}
