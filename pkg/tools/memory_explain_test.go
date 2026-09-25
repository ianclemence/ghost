package tools

import (
	"context"
	"strings"
	"testing"
	"time"

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
