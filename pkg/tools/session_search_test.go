package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
)

func TestSessionSearchToolReturnsRankedResults(t *testing.T) {
	workspace := t.TempDir()
	database, err := db.NewDB(workspace)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer database.Close()

	_, err = database.Exec(`
		INSERT INTO messages (id, session_id, role, content) VALUES
		('m1', 's1', 'user', 'alpha beta gamma'),
		('m2', 's1', 'assistant', 'beta response details'),
		('m3', 's2', 'user', 'unrelated text'),
		('m4', 's1', 'assistant', 'beta archived row')
	`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	_, err = database.Exec(`UPDATE messages SET archived = 1 WHERE id = 'm4'`)
	if err != nil {
		t.Fatalf("archive update failed: %v", err)
	}

	tool := NewSessionSearchTool(database.DB)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"query":      "beta",
		"session_id": "s1",
		"limit":      10,
	})
	if result.IsError {
		t.Fatalf("tool returned error: %s", result.ForLLM)
	}

	var payload struct {
		Count   int                   `json:"count"`
		Results []SessionSearchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}

	if payload.Count == 0 {
		t.Fatalf("expected matches, got none")
	}
	for _, item := range payload.Results {
		if item.SessionID != "s1" {
			t.Fatalf("expected filtered session s1, got %s", item.SessionID)
		}
		if item.Content == "" {
			t.Fatalf("expected non-empty snippet content")
		}
	}
}

func TestSessionSearchToolRequiresQuery(t *testing.T) {
	tool := NewSessionSearchTool(nil)
	res := tool.Execute(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected error when db and query are missing")
	}
}
