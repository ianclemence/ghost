package channels

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
)

// Channel registration must be truthful: a channel appears in the manager
// (and therefore in health/voice wiring) exactly when it is configured,
// never as a dead entry.
func TestManagerInitChannels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels.SMS = config.SMSConfig{
		Enabled: true, AccountSID: "AC123", AuthToken: "tok", From: "+1000",
	}
	cfg.Channels.WeChat = config.WeChatConfig{
		Enabled: true, CorpID: "corp", Secret: "sec", AgentID: "1",
	}

	m, err := NewManager(cfg, bus.NewMessageBus(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for _, name := range []string{"sms", "wechat"} {
		ch, ok := m.GetChannel(name)
		if !ok || ch == nil {
			t.Fatalf("configured %s channel must be registered", name)
		}
		if !ch.IsRunning() {
			// Registration without Start is enough for wiring; running
			// state is asserted separately by lifecycle tests.
			t.Logf("%s registered (not started)", name)
		}
	}
	status := m.GetOperationalStatus()
	for _, name := range []string{"sms", "wechat"} {
		entry, ok := status[name].(map[string]interface{})
		if !ok {
			t.Fatalf("operational status must cover %s", name)
		}
		if entry["enabled"] != true {
			t.Fatalf("%s must report enabled", name)
		}
	}
}

func TestManagerSkipsUnconfiguredChannels(t *testing.T) {
	cfg := config.DefaultConfig()
	m, err := NewManager(cfg, bus.NewMessageBus(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for _, name := range []string{"sms", "wechat", "telegram", "whatsapp"} {
		if _, ok := m.GetChannel(name); ok {
			t.Fatalf("unconfigured %s channel must not be registered", name)
		}
	}
}
