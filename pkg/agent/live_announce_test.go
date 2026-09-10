package agent

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/live"
)

func TestAnnounceSurfaceDeliversIdentityOnly(t *testing.T) {
	b := bus.NewMessageBus()
	ch, unsub := b.SubscribeOutbound("test", false, 10)
	defer unsub()
	al := &AgentLoop{bus: b}
	al.announceSurface("mobile:default", "sess-abc", live.KindBrowser)
	select {
	case msg := <-ch:
		if msg.Channel != "mobile" {
			t.Fatalf("announcement must ride the mobile channel, got %q", msg.Channel)
		}
		if msg.Metadata["type"] != "surface_update" {
			t.Fatalf("wrong frame type: %v", msg.Metadata)
		}
		if msg.Metadata["surface_id"] != "sess-abc" || msg.Metadata["session_id"] != "mobile:default" {
			t.Fatalf("missing linkage: %v", msg.Metadata)
		}
		if msg.Metadata["kind"] != "browser" {
			t.Fatalf("missing kind: %v", msg.Metadata)
		}
		if msg.Content != "" {
			t.Fatalf("announcement must carry identity only, got content %q", msg.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no announcement delivered")
	}
}

func TestAnnounceSurfaceNilSafe(t *testing.T) {
	var al *AgentLoop
	al.announceSurface("s", "id", live.KindBrowser) // must not panic
	(&AgentLoop{}).announceSurface("s", "id", live.KindBrowser)
	(&AgentLoop{bus: bus.NewMessageBus()}).announceSurface("", "id", live.KindBrowser)
	(&AgentLoop{bus: bus.NewMessageBus()}).announceSurface("s", "", live.KindBrowser)
}
