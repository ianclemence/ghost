package channels

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestDetectToolProfile(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		clientType string
		heartbeat  bool
		want       tools.ToolProfile
	}{
		{"heartbeat wins", "mobile", "", true, tools.ProfileHeartbeatSafe},
		{"mobile client type", "cli", "mobile", false, tools.ProfileMobileSafe},
		{"mobile channel", "mobile", "", false, tools.ProfileMobileSafe},
		{"cron channel", "cron", "", false, tools.ProfileHeartbeatSafe},
		{"default full", "cli", "", false, tools.ProfileFull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectToolProfile(tt.channel, tt.clientType, tt.heartbeat)
			if got != tt.want {
				t.Fatalf("DetectToolProfile() = %s, want %s", got, tt.want)
			}
		})
	}
}
