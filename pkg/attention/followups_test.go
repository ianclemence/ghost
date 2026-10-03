package attention

import (
	"strings"
	"testing"
	"time"
)

func TestLooksForward(t *testing.T) {
	for _, m := range []string{
		"I want to buy a mechanical keyboard",
		"I'll call the bank tomorrow",
		"I'm planning a trip to Shenzhen in October",
		"Can't decide between the Pixel and the iPhone",
		"Waiting for the visa decision",
		"Nervous about the interview on Thursday",
	} {
		if !LooksForward(m) {
			t.Errorf("should look forward: %q", m)
		}
	}
	for _, m := range []string{
		"Hello",
		"Thanks, that's all",
		"What is the weather today?",
		"Remind me what my name is",
	} {
		if LooksForward(m) {
			t.Errorf("should not look forward: %q", m)
		}
	}
}

func TestParseFollowups(t *testing.T) {
	heard := day(2026, 10, 3)
	msg := "I want to buy a mechanical keyboard and I'll call the bank tomorrow"
	raw := `{"followups":[
		{"kind":"purchase","what":"buy a mechanical keyboard","due":"","quote":"want to buy a mechanical keyboard"},
		{"kind":"task","what":"call the bank","due":"2026-10-04","quote":"I'll call the bank tomorrow"},
		{"kind":"gossip","what":"something else","due":"","quote":""},
		{"kind":"task","what":"","due":"","quote":""}
	]}`
	got := ParseFollowups(raw, msg, heard, bkk)
	if len(got) != 2 {
		t.Fatalf("only known kinds with words survive, got %+v", got)
	}
	if got[0].Kind != KindPurchase || got[0].What != "buy a mechanical keyboard" {
		t.Fatalf("purchase = %+v", got[0])
	}
	if got[0].Quote == "" {
		t.Fatal("a quote from the message must be kept")
	}
	if got[1].Due == nil || got[1].Due.Format("2006-01-02") != "2026-10-04" {
		t.Fatalf("due date must be kept, got %+v", got[1])
	}
	if got[0].AskAt.IsZero() || got[1].AskAt.IsZero() {
		t.Fatal("every thread needs a time worth bringing up")
	}
	// A quote that is not the owner's words is dropped, not kept as a receipt.
	raw2 := `{"followups":[{"kind":"task","what":"call the bank","due":"","quote":"words never said"}]}`
	got2 := ParseFollowups(raw2, msg, heard, bkk)
	if len(got2) != 1 || got2[0].Quote != "" {
		t.Fatalf("invented quote must be dropped, got %+v", got2)
	}
	if ParseFollowups("not json", msg, heard, bkk) != nil {
		t.Fatal("garbage must parse to nothing")
	}
}

func TestAskAt(t *testing.T) {
	heard := day(2026, 10, 3)
	due := day(2026, 10, 10)
	task := Followup{Kind: KindTask, Heard: heard, Due: &due}
	if got := AskAt(task, bkk); !got.Equal(due) {
		t.Fatalf("a dated task is worth bringing up that morning, got %v", got)
	}
	worry := Followup{Kind: KindWorry, Heard: heard, Due: &due}
	if got := AskAt(worry, bkk); !got.Equal(due.AddDate(0, 0, 1)) {
		t.Fatalf("a worry is how-did-it-go the day after, got %v", got)
	}
	purchase := Followup{Kind: KindPurchase, Heard: heard}
	if got := AskAt(purchase, bkk); !got.Equal(heard.Add(6 * 24 * time.Hour)) {
		t.Fatalf("a purchase waits about a week, got %v", got)
	}
}

func TestFollowupStoreDueOnce(t *testing.T) {
	s := OpenFollowups(t.TempDir())
	now := day(2026, 10, 3).Add(9 * time.Hour)
	if n := s.Add([]Followup{
		{ID: "a", Kind: KindPurchase, What: "buy a mechanical keyboard", Heard: now.Add(-7 * 24 * time.Hour), AskAt: now.Add(-time.Hour)},
		{ID: "b", Kind: KindTask, What: "call the bank", Heard: now, AskAt: now.Add(48 * time.Hour)},
	}); n != 2 {
		t.Fatalf("added = %d", n)
	}
	// Same words twice is one open thread, not two.
	if n := s.Add([]Followup{
		{ID: "a2", Kind: KindPurchase, What: "Buy a mechanical keyboard!", Heard: now, AskAt: now.Add(-time.Hour)},
	}); n != 0 {
		t.Fatalf("duplicate added = %d", n)
	}
	due := s.Due(now)
	if len(due) != 1 || due[0].ID != "a" {
		t.Fatalf("only what is due comes up, got %+v", due)
	}
	if again := s.Due(now); len(again) != 0 {
		t.Fatalf("each thread is brought up once, got %+v", again)
	}
	s.Close("b")
	if len(s.Due(now.Add(72 * time.Hour))) != 0 {
		t.Fatal("a closed thread never comes up")
	}
}

func TestFollowupLine(t *testing.T) {
	cases := []struct {
		f    Followup
		want []string
	}{
		{Followup{Kind: KindPurchase, What: "buy a mechanical keyboard"}, []string{"planning to buy a mechanical keyboard", "Still on?"}},
		{Followup{Kind: KindDecision, What: "the Pixel and the iPhone"}, []string{"deciding on", "Settled it?"}},
		{Followup{Kind: KindWaiting, What: "hear back about the visa"}, []string{"waiting to hear back", "Any news?"}},
		{Followup{Kind: KindWorry, What: "the job interview"}, []string{"How did", "go?"}},
	}
	for _, c := range cases {
		line := c.f.Line()
		for _, w := range c.want {
			if !strings.Contains(line, w) {
				t.Errorf("%v line %q misses %q", c.f.Kind, line, w)
			}
		}
	}
	if text, label := (Followup{Kind: KindPurchase, What: "buy a keyboard"}.Reply()); text == "" || label == "" {
		t.Fatal("a purchase earns a next step the owner can tap")
	}
	if text, _ := (Followup{Kind: KindTask, What: "call the bank"}.Reply()); text != "" {
		t.Fatal("a task needs no suggested step")
	}
}
