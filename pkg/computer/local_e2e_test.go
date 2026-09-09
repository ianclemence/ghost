package computer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Real LocalComputer executor E2E. Opt-in via GHOST_E2E_COMPUTER=1 and a
// running X display with xdotool+scrot/import (e.g. Xvfb :99). Skipped
// otherwise — a real executor run is never replaced by a fake success.
func TestLocalComputerRealExecutorE2E(t *testing.T) {
	if os.Getenv("GHOST_E2E_COMPUTER") == "" {
		t.Skip("set GHOST_E2E_COMPUTER=1 on an X display with xdotool+scrot to run the real executor E2E")
	}
	c := NewLocalComputer("local")
	state, _ := c.Authority()
	if state == "none" {
		t.Skip("no computer executor on this host")
	}
	shot := filepath.Join(t.TempDir(), "screen.png")
	res, err := c.Do(context.Background(), OpScreenshot, Args{"path": shot})
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if !res.Verified {
		t.Fatal("screenshot must be verified by observed file content")
	}
	if fi, err := os.Stat(shot); err != nil || fi.Size() == 0 {
		t.Fatalf("screenshot file missing or empty: %v", err)
	}
	if state == "control" {
		// Dispatch a harmless click at 0,0: bounded input through the real
		// executor; evidence must be recorded and NOT claim verification.
		cr, err := c.Do(context.Background(), OpClick, Args{"x": "0", "y": "0"})
		if err != nil {
			t.Fatalf("click: %v", err)
		}
		if cr.Verified {
			t.Fatal("dispatched click must not claim independent verification")
		}
		if cr.Evidence["outcome"] != "dispatched" {
			t.Fatalf("click evidence wrong: %+v", cr.Evidence)
		}
	}
}
