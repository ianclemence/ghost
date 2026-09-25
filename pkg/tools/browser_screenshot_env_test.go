package tools

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/browser"
)

// A cold screenshot must steer agent-browser to a working browser engine, the
// same as navigate/click/snapshot. Without this, the first screenshot on a
// fresh session launches the platform's system Chromium, which can hang until
// the 90-second deadline on some distributions.
func TestScreenshotCommandCarriesBrowserSteering(t *testing.T) {
	cmd := browserScreenshotCommand(context.Background(), "/tmp/shot.png")
	if cmd == nil {
		t.Fatal("expected a screenshot command")
	}
	exe := browser.DiscoverExecutable(nil)
	if exe == "" {
		t.Skip("no Playwright headless shell on this machine to steer to")
	}
	want := browser.ExecutableEnv + "=" + exe
	for _, e := range cmd.Env {
		if e == want {
			return
		}
	}
	t.Fatalf("screenshot command must carry %q so it never launches system Chromium", want)
}
