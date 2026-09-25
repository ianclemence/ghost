package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func seedBelief(t *testing.T, ws string) string {
	t.Helper()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	acts, err := personalcontext.Apply(store, personalcontext.Input{
		SessionID: "sess-1", MessageID: "msg-7",
		Text:      "my name is Ian",
		Timestamp: time.Now().UTC(),
	})
	if err != nil || len(acts) == 0 {
		t.Fatalf("seed belief: err=%v acts=%d", err, len(acts))
	}
	return acts[0].Entry.ID
}

func TestMemoryExplainReturnsReceipts(t *testing.T) {
	ws := t.TempDir()
	id := seedBelief(t, ws)
	tool := NewMemoryExplainTool(ws)

	res := tool.Execute(context.Background(), map[string]interface{}{"id": id})
	if res == nil || res.IsError {
		t.Fatalf("explain failed: %+v", res)
	}
	for _, want := range []string{"Quote", "msg-7", "Confidence", "Status: current"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("receipt missing %q:\n%s", want, res.ForLLM)
		}
	}
}

func TestMemoryExplainByQueryAndHonestMisses(t *testing.T) {
	ws := t.TempDir()
	seedBelief(t, ws)
	tool := NewMemoryExplainTool(ws)

	byQuery := tool.Execute(context.Background(), map[string]interface{}{"query": "name"})
	if byQuery == nil || byQuery.IsError || !strings.Contains(byQuery.ForLLM, "Quote") {
		t.Fatalf("query receipt failed: %+v", byQuery)
	}

	miss := tool.Execute(context.Background(), map[string]interface{}{"query": "quantum bicycle"})
	if miss == nil || miss.IsError || !strings.Contains(miss.ForLLM, "Nothing in my memory") {
		t.Fatalf("miss should be an honest empty answer: %+v", miss)
	}

	badID := tool.Execute(context.Background(), map[string]interface{}{"id": "ec_nope"})
	if badID == nil || !badID.IsError {
		t.Fatalf("unknown id should be an honest error: %+v", badID)
	}

	neither := tool.Execute(context.Background(), map[string]interface{}{})
	if neither == nil || !neither.IsError {
		t.Fatal("no id and no query must be refused")
	}
}

func TestMemoryExplainPublishesReceiptCard(t *testing.T) {
	ws := t.TempDir()
	id := seedBelief(t, ws)
	tool := NewMemoryExplainTool(ws)
	tool.SetContext("mobile", "chat-1")

	var got cards.Card
	var gotChannel, gotChat string
	tool.SetPublisher(func(channel, chatID, sessionID string, c cards.Card) {
		gotChannel, gotChat, got = channel, chatID, c
	})

	if res := tool.Execute(context.Background(), map[string]interface{}{"id": id}); res == nil || res.IsError {
		t.Fatalf("explain failed: %+v", res)
	}
	if got.Kind != cards.KindMemoryReceipt {
		t.Fatalf("card kind = %q, want memory_receipt", got.Kind)
	}
	if gotChannel != "mobile" || gotChat != "chat-1" {
		t.Fatalf("card routed to %q/%q, want mobile/chat-1", gotChannel, gotChat)
	}
	if got.Data["claim_id"] != id {
		t.Errorf("card claim_id = %v, want %s", got.Data["claim_id"], id)
	}
	if _, ok := got.Data["quote"]; !ok {
		t.Error("card must carry the quote field (even when empty)")
	}
	if got.TextFallback() == "" {
		t.Error("card must carry a plain-text fallback")
	}
}
