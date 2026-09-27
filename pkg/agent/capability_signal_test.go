package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/modes"
)

// Offline capability is surfaced only when the runtime is actually degraded to
// local mode; cloud/hybrid turns carry no banner.
func TestOfflineCapabilityNote(t *testing.T) {
	local := offlineCapabilityNote(modes.Local)
	if !strings.Contains(local, "local mode") {
		t.Fatalf("local mode must signal capability: %q", local)
	}
	if offlineCapabilityNote(modes.Cloud) != "" || offlineCapabilityNote(modes.Hybrid) != "" {
		t.Fatal("cloud/hybrid turns must not carry an offline banner")
	}
}
