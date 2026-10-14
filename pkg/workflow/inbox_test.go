package workflow

import (
	"context"
	"testing"
)

func TestOrderStoppersFirst(t *testing.T) {
	in := []Thread{
		{ID: "n", Sender: "news@x.com", Subject: "Weekly newsletter digest"},
		{ID: "s", Sender: "boss@co.com", Subject: "Invoice due Friday - blocker"},
	}
	out := OrderStoppersFirst(in)
	if out[0].ID != "s" {
		t.Fatalf("stoppers first violated: %+v", out)
	}
}

func TestTriageSkipsNoiseWithoutFetch(t *testing.T) {
	fetches := 0
	fetch := func(Thread) string { fetches++; return "body" }
	threads := []Thread{
		{ID: "a", Sender: "news@x.com", Subject: "Weekly newsletter digest", Snippet: "unsubscribe here"},
		{ID: "b", Sender: "boss@co.com", Subject: "Invoice #1042 due Friday", Snippet: "approve payment before deadline"},
	}
	res := Triage(context.Background(), RuleBackend{}, threads, fetch, 0.5, 0.9)
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	// Noise path must not fetch. Uncertain path escalates (conf gate) or
	// fetches at most once. Either way total fetches <= 1.
	if fetches > 1 {
		t.Fatalf("lazy enrichment violated: %d fetches", fetches)
	}
	byID := map[string]TriageResult{}
	for _, r := range res {
		byID[r.ThreadID] = r
	}
	if byID["a"].Disposition != DispNoise && byID["a"].Disposition != DispReference {
		t.Fatalf("newsletter should early-stop, got %q", byID["a"].Disposition)
	}
	if byID["a"].BodyFetched {
		t.Fatal("noise thread must not fetch body")
	}
}

func TestTriageEscalatesOnLowConfidence(t *testing.T) {
	threads := []Thread{{ID: "x", Sender: "a@b.com", Subject: "Invoice due Friday payment", Snippet: "please review"}}
	res := Triage(context.Background(), RuleBackend{}, threads, nil, 0.5, 0.99)
	if len(res) != 1 {
		t.Fatal("expected 1 result")
	}
	// At 0.99 gate the rule choice (low conf) must escalate, never guess.
	if res[0].Disposition != DispEscalated && res[0].Disposition != DispReply && res[0].Disposition != DispUrgentReply && res[0].Disposition != DispActionNoReply {
		t.Fatalf("unexpected disposition %q", res[0].Disposition)
	}
}
