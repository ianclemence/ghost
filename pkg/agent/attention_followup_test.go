package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/attention"
)

// A due open thread joins the morning digest; a thread whose time has not
// come stays out; each is brought up once.
func TestOfferFollowupsJoinsDigest(t *testing.T) {
	al := pinTestLoop(t, nil)
	now := time.Now()
	st := al.followupStore()
	if n := st.Add([]attention.Followup{
		{ID: "fu-due", Kind: attention.KindPurchase, What: "buy a mechanical keyboard",
			Heard: now.Add(-7 * 24 * time.Hour), AskAt: now.Add(-time.Hour)},
		{ID: "fu-later", Kind: attention.KindTask, What: "renew the passport",
			Heard: now, AskAt: now.Add(72 * time.Hour)},
	}); n != 2 {
		t.Fatalf("added = %d", n)
	}
	al.offerFollowups(now, al.ownerLocation())
	pending := al.attentionQueue().Pending()
	if len(pending) != 1 {
		t.Fatalf("only the due thread joins, got %+v", pending)
	}
	if !strings.Contains(pending[0].Line, "keyboard") {
		t.Fatalf("digest line must name the thing: %q", pending[0].Line)
	}
	if pending[0].Source != "followup:"+attention.KindPurchase {
		t.Fatalf("learning tracks the kind of thing: %q", pending[0].Source)
	}
	// Due marks it asked: a second tick does not bring it up again.
	al.offerFollowups(now.Add(time.Minute), al.ownerLocation())
	if len(al.attentionQueue().Pending()) != 1 {
		t.Fatalf("each thread is brought up once")
	}
}
