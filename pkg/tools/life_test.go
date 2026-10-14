package tools

import (
	"context"
	"testing"
	"time"
)

func TestForgettingAPersonTellsTheRuntimeTheirName(t *testing.T) {
	ws := t.TempDir()
	tool := NewPeopleTool(ws, func() *time.Location { return time.UTC })
	var told string
	tool.OnForgotten = func(n string) { told = n }
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "remember", "name": "Dara", "relation": "friend", "source": "conversation"})
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	all, _ := tool.store.All()
	if len(all) != 1 {
		t.Fatalf("expected one person, got %d", len(all))
	}
	res = tool.Execute(context.Background(), map[string]interface{}{"action": "forget", "id": all[0].ID, "confirmed": true})
	if res.IsError || told != "Dara" {
		t.Fatalf("forgetting a person must pass their name on: %q %s", told, res.ForLLM)
	}
}
