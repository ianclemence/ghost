package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/live"

	_ "modernc.org/sqlite"
)

// Full conceptual lifecycle without providers: Ghost opens the browser,
// the surface goes active with an observation, Ghost acts, hands back a
// validated artifact, the conversation succeeds, and the surface
// terminates instead of lingering.
func TestBrowserSurfaceArtifactLifecycle(t *testing.T) {
	h := newGateHarness(t)
	h.loop.SetLivePlane(live.NewRegistry(h.ghostID))
	h.loop.bus = bus.NewMessageBus()

	ws := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatalf("artifact store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "result.md"), []byte("# Result"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Ghost opens the browser: surface active.
	tc := fakeToolCall("browser_navigate", map[string]interface{}{"url": "https://example.com"})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc, h.opts("sess-life"), nil); res.IsError {
		t.Fatalf("navigate failed: %s", res.ForLLM)
	}
	var surfaceID string
	for _, s := range h.loop.livePlane.List() {
		if s.Kind == live.KindBrowser && s.State == live.StateActive {
			surfaceID = s.ID
		}
	}
	if surfaceID == "" {
		t.Fatalf("no active browser surface after navigation")
	}

	// Ghost acts and hands back a validated artifact.
	tc2 := fakeToolCall("browser_snapshot", map[string]interface{}{})
	if res, _, _ := h.loop.maybeRunBrowserTool(h.toolCtx(), h.loop.tools, tc2, h.opts("sess-life"), nil); res.IsError {
		t.Fatalf("snapshot failed: %s", res.ForLLM)
	}
	a, err := store.Publish(artifacts.Input{
		SessionKey: "sess-life", Kind: "file", Title: "Result", Path: "result.md",
	})
	if err != nil {
		t.Fatalf("artifact must validate: %v", err)
	}
	listed, err := store.List("sess-life", 50)
	if err != nil || len(listed) != 1 || listed[0].ID != a.ID {
		t.Fatalf("artifact must persist and list: %v %d", err, len(listed))
	}

	// Conversation succeeds: the surface terminates instead of lingering.
	if n := h.loop.SettleSessionSurfaces("sess-life", "success"); n != 1 {
		t.Fatalf("expected 1 settled surface, got %d", n)
	}
	s, _ := h.loop.livePlane.Snapshot(surfaceID)
	if s.State != live.StateCompleted {
		t.Fatalf("surface must complete with the turn, got %q", s.State)
	}
	got, err := store.Get(a.ID)
	if err != nil || got.State != artifacts.StateAvailable {
		t.Fatalf("artifact must survive turn completion: %v %+v", err, got)
	}
}
