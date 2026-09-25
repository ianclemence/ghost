package tools

import (
	"strings"
	"testing"
)

// Media decoding of untrusted files must never run with network access, and on
// a machine with bubblewrap it must run inside the isolation profile. This
// guards the policy without spawning ffmpeg.
func TestMediaSandboxArgvDeniesNetwork(t *testing.T) {
	ws := t.TempDir()
	base := []string{"ffmpeg", "-i", "input.mp4", "out.png"}

	argv, err := mediaSandboxArgv(base, ws, ws)
	if err != nil {
		t.Fatalf("media sandbox argv: %v", err)
	}
	if !bwrapAvailable() {
		t.Skip("bubblewrap not installed; auto mode passes argv through")
	}
	if argv[0] != "bwrap" {
		t.Fatalf("expected the isolation wrapper, got %q", argv[0])
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--unshare-net") {
		t.Error("media sandbox must deny network access")
	}
	if !strings.Contains(joined, "ffmpeg") {
		t.Error("the wrapped command must remain intact")
	}
}
