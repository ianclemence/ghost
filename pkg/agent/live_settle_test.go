package agent

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/live"
)

func settleHarness(t *testing.T) (*gateHarness, *bus.MessageBus) {
	t.Helper()
	h := newGateHarness(t)
	h.loop.SetLivePlane(live.NewRegistry(h.ghostID))
	b := bus.NewMessageBus()
	h.loop.bus = b
	return h, b
}

func browserSurfaceState(h *gateHarness) live.State {
	for _, s := range h.loop.livePlane.List() {
		if s.Kind == live.KindBrowser {
			return s.State
		}
	}
	return ""
}

// Success retires the turn's surfaces; the conversation outcome and the
// surface lifecycle agree without being forced identical.
func TestSettleSuccessCompletesTaskSurfaces(t *testing.T) {
	h, _ := settleHarness(t)
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-ok"), nil)
	if res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	if st := browserSurfaceState(h); st != live.StateActive {
		t.Fatalf("surface must be active after work, got %q", st)
	}
	if n := h.loop.SettleSessionSurfaces("sess-ok", "success"); n != 1 {
		t.Fatalf("expected 1 settled surface, got %d", n)
	}
	if st := browserSurfaceState(h); st != live.StateCompleted {
		t.Fatalf("surface must complete on success, got %q", st)
	}
}

// Failure fails them; waiting parks them truthfully.
func TestSettleFailureAndWaiting(t *testing.T) {
	h, _ := settleHarness(t)
	taskState := func(task string) live.State {
		for _, s := range h.loop.livePlane.List() {
			if s.Kind == live.KindBrowser && s.Task == task {
				return s.State
			}
		}
		return ""
	}
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-f"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	if n := h.loop.SettleSessionSurfaces("sess-f", "failed"); n != 1 {
		t.Fatalf("expected 1 failed surface, got %d", n)
	}
	if st := taskState("sess-f"); st != live.StateFailed {
		t.Fatalf("surface must fail on failure, got %q", st)
	}

	tc2 := fakeToolCall("browser_click", map[string]interface{}{"ref": "e1"})
	h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc2, h.opts("sess-w"), nil)
	if st := taskState("sess-w"); st != live.StateWaiting {
		t.Fatalf("surface must wait during approval, got %q", st)
	}
	if n := h.loop.SettleSessionSurfaces("sess-w", "waiting_for_permission"); n != 0 {
		t.Fatalf("waiting must settle nothing, got %d", n)
	}
	if st := taskState("sess-w"); st != live.StateWaiting {
		t.Fatalf("waiting surface must persist, got %q", st)
	}
}

// Full chain: takeover pauses, release parks, resume returns control,
// and the next real operation executes with evidence and a canonical
// event — the runtime proves every step.
func TestTakeoverReleaseResumeChain(t *testing.T) {
	h, _ := settleHarness(t)
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-chain"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	var surfaceID string
	for _, s := range h.loop.livePlane.List() {
		if s.Kind == live.KindBrowser {
			surfaceID = s.ID
		}
	}
	if surfaceID == "" {
		t.Fatalf("no browser surface registered")
	}
	if _, err := h.loop.livePlane.Takeover(surfaceID, "dev-1", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	before := h.stubs["browser_snapshot"].calls
	h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-chain"), nil)
	if h.stubs["browser_snapshot"].calls != before {
		t.Fatalf("Ghost must be refused while the user holds control")
	}
	if err := h.loop.livePlane.Release(surfaceID, "dev-1", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := h.loop.livePlane.Resume(surfaceID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-chain"), nil)
	if res.IsError {
		t.Fatalf("resumed Ghost must execute: %s", res.ForLLM)
	}
	s, _ := h.loop.livePlane.Snapshot(surfaceID)
	if s.Control != live.OwnerGhost || s.State != live.StateActive {
		t.Fatalf("surface must be Ghost-active after resume: %+v", s)
	}
	// Evidence reached the canonical stream: the run is proven, not claimed.
	found := false
	for _, e := range h.events.ByRequest("req-sess-chain-1") {
		if e.Type == cevents.ToolCompleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a ToolCompleted canonical event for the resumed run")
	}
}

// Revoked permission during takeover: resume succeeds at the plane, but
// the next consequential operation must wait, never execute.
func TestResumeAfterRevokeNeverExecutes(t *testing.T) {
	h, _ := settleHarness(t)
	surfaceID := "sess-revoke-surface"
	h.loop.livePlane.Register(surfaceID, live.KindBrowser)
	h.loop.livePlane.SetTask(surfaceID, "sess-revoke")
	if _, err := h.loop.livePlane.Takeover(surfaceID, "dev-1", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if err := h.loop.livePlane.Release(surfaceID, "dev-1", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := h.loop.livePlane.Resume(surfaceID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	before := h.stubs["browser_click"].calls
	tc := fakeToolCall("browser_click", map[string]interface{}{"ref": "e1"})
	res, governed, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-revoke"), nil)
	if !governed || res.IsError {
		t.Fatalf("ungranted consequential op must park with a message, not execute")
	}
	if h.stubs["browser_click"].calls != before {
		t.Fatalf("executor must not run without authorization")
	}
}

// Expired leases fail closed through release and resume.
func TestExpiredLeaseFailsClosed(t *testing.T) {
	h, _ := settleHarness(t)
	h.loop.livePlane.Register("sess-exp", live.KindBrowser)
	if _, err := h.loop.livePlane.Takeover("sess-exp", "dev-1", time.Nanosecond); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if ok, _ := h.loop.livePlane.GhostMayAct("sess-exp"); ok {
		t.Fatalf("expired lease must not authorize Ghost")
	}
	// Lazy expiry already cleared control: there is nothing to release,
	// and a release attempt correctly reports that.
	if err := h.loop.livePlane.Release("sess-exp", "dev-1", false); err == nil {
		t.Fatalf("release with nothing held must fail")
	}
	if err := h.loop.livePlane.Resume("sess-exp"); err != nil {
		t.Fatalf("resume after expiry: %v", err)
	}
	s, _ := h.loop.livePlane.Snapshot("sess-exp")
	if s.Control != live.OwnerGhost {
		t.Fatalf("control must return to Ghost, got %+v", s)
	}
}

// Stale and foreign control attempts fail safely.
func TestStaleSurfaceControlRefused(t *testing.T) {
	h, _ := settleHarness(t)
	if err := h.loop.livePlane.Resume("no-such-surface"); err == nil {
		t.Fatalf("resume of unknown surface must fail")
	}
	h.loop.livePlane.Register("sess-held", live.KindBrowser)
	if _, err := h.loop.livePlane.Takeover("sess-held", "dev-1", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if err := h.loop.livePlane.Resume("sess-held"); err == nil {
		t.Fatalf("resume under user control must fail")
	}
	if err := h.loop.livePlane.Release("sess-held", "dev-2", false); err == nil {
		t.Fatalf("foreign release must fail")
	}
	if n := h.loop.SettleSessionSurfaces("", "success"); n != 0 {
		t.Fatalf("empty session must settle nothing")
	}
}

// When a turn ends, the owner's devices hear about it. The phone's card used
// to say "Ghost is working…" under the finished reply because the settle was
// silent.
func TestSettleAnnouncesEachSettledSurface(t *testing.T) {
	h, b := settleHarness(t)
	ch, unsub := b.SubscribeOutbound("settle-announce", false, 32)
	defer unsub()
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-say"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	for len(ch) > 0 {
		<-ch
	}
	if n := h.loop.SettleSessionSurfaces("sess-say", "success"); n != 1 {
		t.Fatalf("expected 1 settled surface, got %d", n)
	}
	select {
	case msg := <-ch:
		if msg.Metadata["type"] != "surface_update" || msg.Metadata["session_id"] != "sess-say" {
			t.Fatalf("bad settle announcement: %v", msg.Metadata)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("settling must announce the change")
	}
	for _, s := range h.loop.livePlane.List() {
		if s.Kind == live.KindBrowser && (s.State != live.StateCompleted || s.Activity != "") {
			t.Fatalf("settled surface must be completed with no activity, got %+v", s)
		}
	}
}

// The card says what Ghost is doing during a step, and keeps the page it knows
// when a later step reports less.
func TestBrowserSurfaceCarriesSessionStepAndPage(t *testing.T) {
	h, _ := settleHarness(t)
	h.stubs["browser_snapshot"].evidence = map[string]interface{}{"url": "https://news.ycombinator.com/", "domain": "news.ycombinator.com", "title": "Hacker News"}
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-page"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	h.stubs["browser_snapshot"].evidence = map[string]interface{}{}
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-page"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	var s *live.Surface
	for _, x := range h.loop.livePlane.List() {
		if x.Kind == live.KindBrowser {
			cp := x
			s = &cp
		}
	}
	if s == nil || s.Session != "sess-page" || s.Started.IsZero() {
		t.Fatalf("surface must carry its conversation and start time, got %+v", s)
	}
	if s.Obs.Title != "Hacker News" || s.Obs.Domain != "news.ycombinator.com" {
		t.Fatalf("a step that reports no page must keep the known page, got %+v", s.Obs)
	}
	if s.Activity != "" {
		t.Fatalf("activity is cleared once the step is done, got %q", s.Activity)
	}
}

func TestBrowserStepLabelIsPlainWords(t *testing.T) {
	cases := map[string]struct {
		tool string
		args map[string]interface{}
	}{
		"Opening news.ycombinator.com": {"browser_navigate", map[string]interface{}{"url": "https://www.news.ycombinator.com/item?id=1"}},
		"Opening a page":               {"browser_navigate", map[string]interface{}{"url": "not a url"}},
		"Typing":                       {"browser_type", map[string]interface{}{"ref": "e3", "text": "secret"}},
		"Reading the page":             {"browser_snapshot", nil},
		"Working in the browser":       {"browser_mystery", nil},
	}
	for want, c := range cases {
		if got := browserStepLabel(c.tool, c.args); got != want {
			t.Errorf("browserStepLabel(%s) = %q, want %q", c.tool, got, want)
		}
	}
}

func TestOwnerAskedForPicture(t *testing.T) {
	for msg, want := range map[string]bool{
		"Take a screenshot of the flights page":                    true,
		"send me a picture of the results":                         true,
		"open hacker news and tell me the top 3":                   false,
		"Open Google Flights and check Bangkok to Shenzhen prices": false,
	} {
		if got := ownerAskedForPicture(msg); got != want {
			t.Errorf("ownerAskedForPicture(%q) = %v, want %v", msg, got, want)
		}
	}
}

// A follow-up in a conversation that was just browsing keeps the browser
// ("send me a screenshot of that results page" names no site); another
// conversation does not inherit it.
func TestRecentBrowserWorkIsPerConversation(t *testing.T) {
	h, _ := settleHarness(t)
	if h.loop.recentBrowserWork("sess-follow") {
		t.Fatalf("no browser work yet")
	}
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-follow"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	h.loop.SettleSessionSurfaces("sess-follow", "success")
	if !h.loop.recentBrowserWork("sess-follow") {
		t.Fatalf("a follow-up right after browsing must keep the browser")
	}
	if h.loop.recentBrowserWork("sess-other") {
		t.Fatalf("another conversation must not inherit the browser")
	}
}
