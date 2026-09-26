package commitments

import (
	"context"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestExtractDeterministicAcceptsRealObligation(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC) // Wednesday
	got := ExtractDeterministic("I need to send Alex those photos Friday", now, "UTC")
	if len(got) != 1 {
		t.Fatalf("expected one obligation, got %d (%+v)", len(got), got)
	}
	c := got[0]
	if c.Kind != KindSend {
		t.Fatalf("kind = %s, want send", c.Kind)
	}
	if c.Subject != "Alex" {
		t.Fatalf("subject = %q, want Alex", c.Subject)
	}
	if c.DueAt == nil {
		t.Fatal("a named weekday must resolve to a due time")
	}
	// Friday, 25 September 2026, 09:00 UTC — the day arriving, not an
	// invented hour.
	want := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	if !c.DueAt.Equal(want) {
		t.Fatalf("due = %s, want %s", c.DueAt, want)
	}
	if c.Quote == "" || c.Origin != "deterministic" {
		t.Fatalf("provenance missing: %+v", c)
	}
}

// Speculation, hypotheticals, questions, requests to Ghost, and explicit
// reminder asks are all rejected: only a real promise creates durable state.
func TestExtractDeterministicRejectsNonCommitments(t *testing.T) {
	now := time.Now()
	for _, msg := range []string{
		"I might send Alex those photos Friday",
		"Maybe I should send Alex the photos",
		"I could email the landlord tomorrow",
		"Should I send Alex those photos?",
		"Can you send Alex those photos Friday",
		"Please send the report tomorrow",
		"Remind me to send Alex those photos Friday",
		"Set a reminder to call the bank",
		"Thinking about writing the report next week",
		"Not sure I need to pay the invoice",
	} {
		if got := ExtractDeterministic(msg, now, "UTC"); len(got) != 0 {
			t.Errorf("ExtractDeterministic(%q) extracted %+v; want nothing", msg, got)
		}
	}
}

func TestExtractDeterministicKinds(t *testing.T) {
	now := time.Now()
	cases := map[string]Kind{
		"I need to email the landlord tomorrow": KindEmail,
		"I have to call the bank":               KindCall,
		"I need to buy a new laptop":            KindBuy,
		"I need to pay the electricity bill":    KindPay,
		"I need to book a dentist appointment":  KindBook,
		"I need to submit the tax form":         KindSubmit,
		"I need to tidy the garage":             KindOther,
	}
	for msg, want := range cases {
		got := ExtractDeterministic(msg, now, "UTC")
		if len(got) != 1 {
			t.Errorf("ExtractDeterministic(%q) = %d candidates", msg, len(got))
			continue
		}
		if got[0].Kind != want {
			t.Errorf("ExtractDeterministic(%q).Kind = %s, want %s", msg, got[0].Kind, want)
		}
	}
}

// A promise with no time on it is still a promise; it just has no deadline.
func TestExtractDeterministicWithoutTime(t *testing.T) {
	got := ExtractDeterministic("I need to renew my passport", time.Now(), "UTC")
	if len(got) != 1 {
		t.Fatalf("expected one obligation, got %d", len(got))
	}
	if got[0].DueAt != nil {
		t.Fatalf("no time was stated, so none may be invented: %v", got[0].DueAt)
	}
}

type fakeProvider struct {
	reply string
	calls int
}

func (f *fakeProvider) Chat(ctx context.Context, m []providers.Message, t []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	f.calls++
	return &providers.LLMResponse{Content: f.reply}, nil
}
func (f *fakeProvider) GetDefaultModel() string { return "fake" }

// The model may only contribute phrasing the patterns missed — and only when
// every field it returns appears in the owner's message.
func TestSemanticExtractionGroundsEveryField(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	msg := "I've been meaning to get the car serviced before the trip on Friday"

	// Grounded: quote and time phrase both come from the message.
	good := &fakeProvider{reply: `{"is_commitment":true,"text":"get the car serviced before the trip","subject":"","kind":"other","time_expression":"Friday","quote":"I've been meaning to get the car serviced before the trip on Friday","confidence":0.86}`}
	se := NewSemanticExtractor(good, "fake")
	got, ok := se.Extract(context.Background(), msg, now, "UTC")
	if !ok {
		t.Fatal("a grounded extraction must be accepted")
	}
	if got.DueAt == nil {
		t.Fatal("the quoted time phrase must resolve to a due time")
	}
	if got.Origin != "model" {
		t.Fatalf("origin = %q, want model", got.Origin)
	}

	// Fabricated quote: the model paraphrased instead of quoting.
	badQuote := &fakeProvider{reply: `{"is_commitment":true,"text":"service the car","subject":"","kind":"other","time_expression":"Friday","quote":"I should service the car","confidence":0.9}`}
	if _, ok := NewSemanticExtractor(badQuote, "fake").Extract(context.Background(), msg, now, "UTC"); ok {
		t.Fatal("a paraphrased quote must be rejected — the message must contain it")
	}

	// Invented person: the subject is not in the message.
	badSubject := &fakeProvider{reply: `{"is_commitment":true,"text":"send the photos","subject":"Alex","kind":"send","time_expression":"","quote":"I've been meaning to get the car serviced before the trip on Friday","confidence":0.9}`}
	got2, ok2 := NewSemanticExtractor(badSubject, "fake").Extract(context.Background(), msg, now, "UTC")
	if !ok2 {
		t.Fatal("the grounded quote should still yield an obligation")
	}
	if got2.Subject != "" {
		t.Fatalf("an invented subject must be dropped, got %q", got2.Subject)
	}

	// Invented time: the phrase is not in the message. The runtime must not use
	// the model's invented date; it may still resolve a time from the owner's
	// own words inside the verified quote.
	badTime := &fakeProvider{reply: `{"is_commitment":true,"text":"get the car serviced","subject":"","kind":"other","time_expression":"next Tuesday","quote":"I've been meaning to get the car serviced before the trip on Friday","confidence":0.9}`}
	got3, ok3 := NewSemanticExtractor(badTime, "fake").Extract(context.Background(), msg, now, "UTC")
	if !ok3 {
		t.Fatal("expected the obligation to survive")
	}
	if got3.DuePhrase == "next Tuesday" {
		t.Fatal("an invented time phrase must never be used")
	}
	// Friday 25 September 2026, resolved from the owner's own quote.
	wantDue := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	if got3.DueAt == nil || !got3.DueAt.Equal(wantDue) {
		t.Fatalf("due = %v, want %s (from the owner's words)", got3.DueAt, wantDue)
	}

	// Low confidence and speculation are refused.
	low := &fakeProvider{reply: `{"is_commitment":true,"text":"send the photos","subject":"","kind":"send","time_expression":"","quote":"I've been meaning to get the car serviced before the trip on Friday","confidence":0.4}`}
	if _, ok := NewSemanticExtractor(low, "fake").Extract(context.Background(), msg, now, "UTC"); ok {
		t.Fatal("low-confidence extraction must be refused")
	}
	if _, ok := NewSemanticExtractor(&fakeProvider{reply: `{"is_commitment":true,"text":"send the photos","subject":"","kind":"send","time_expression":"","quote":"maybe I'll send them","confidence":0.9}`}, "fake").
		Extract(context.Background(), "maybe I'll send Alex the photos", now, "UTC"); ok {
		t.Fatal("speculation must be refused even when the model is eager")
	}
}

func TestStoreLifecycle(t *testing.T) {
	ws := t.TempDir()
	store, err := New(ws)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	due := time.Now().UTC().Add(-time.Hour)
	c, err := store.Create(Commitment{
		Text: "send Alex the photos", Subject: "Alex", Kind: KindSend,
		DueAt: &due, Confidence: 0.9, Origin: "deterministic",
		Provenance: Provenance{Session: "main", MessageID: "m1", Quote: "I need to send Alex those photos", At: time.Now().UTC()},
		DedupeKey:  DueKey("send Alex the photos", "Alex", &due),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Status != StatusOpen {
		t.Fatalf("status = %s, want open", c.Status)
	}

	// A restatement is the same obligation, not a second one.
	again, err := store.Create(Commitment{
		Text: "send Alex the photos", Subject: "Alex", Kind: KindSend, DueAt: &due,
		Confidence: 0.9, Origin: "deterministic",
		Provenance: Provenance{Session: "main", MessageID: "m2", Quote: "I need to send Alex those photos", At: time.Now().UTC()},
		DedupeKey:  DueKey("send Alex the photos", "Alex", &due),
	})
	if err != nil {
		t.Fatalf("restate: %v", err)
	}
	if again.ID != c.ID {
		t.Fatalf("a restatement created a second commitment: %s != %s", again.ID, c.ID)
	}
	list, _ := store.List()
	if len(list) != 1 {
		t.Fatalf("expected one commitment, got %d", len(list))
	}

	// Provenance is mandatory: an obligation nobody can be shown the origin of
	// is not one Ghost may act on.
	if _, err := store.Create(Commitment{Text: "do the thing", Origin: "model"}); err == nil {
		t.Fatal("a commitment without the owner's words must be refused")
	}

	// A failed action must not complete the obligation.
	if _, err := store.Settle(c.ID, StatusOpen, "failed", "the channel was unavailable"); err != nil {
		t.Fatalf("settle open: %v", err)
	}
	got, _ := store.Get(c.ID)
	if got.Status != StatusOpen || got.Outcome != "failed" {
		t.Fatalf("failure must leave the obligation open, got %s/%s", got.Status, got.Outcome)
	}

	// Completion is terminal: a late failure cannot reopen closed work.
	if _, err := store.Settle(c.ID, StatusCompleted, "completed", "sent"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := store.Settle(c.ID, StatusOpen, "failed", "late failure"); err == nil {
		t.Fatal("a settled commitment must not reopen")
	}
}
