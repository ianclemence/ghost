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

// Scroll and read modes must handle TEXT UUID message ids: the schema stores
// messages.id as TEXT (see pkg/db baseSchemaStatements), so scanning into an
// int64 failed with "converting driver.Value type string ... to a int64".
// Regression test for the 18:31 scroll failure on id 178461d1-....
func TestSessionSearchScrollWithUUIDIDs(t *testing.T) {
	workspace := t.TempDir()
	database, err := db.NewDB(workspace)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer database.Close()

	_, err = database.Exec(`
		INSERT INTO messages (id, session_id, role, content) VALUES
		('178461d1-2f61-4298-b550-95124eea88f9', 's1', 'user', 'first message here'),
		('aaaabbbb-cccc-dddd-eeee-ffffffffffff', 's1', 'assistant', 'second message here'),
		('11112222-3333-4444-5555-666677778888', 's1', 'user', 'third message here')
	`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	tool := NewSessionSearchTool(database.DB)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"mode":              "scroll",
		"session_id":        "s1",
		"around_message_id": "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
		"window":            float64(1),
	})
	if result.IsError {
		t.Fatalf("scroll returned error: %s", result.ForLLM)
	}

	var payload struct {
		Count    int            `json:"count"`
		AnchorID string         `json:"anchor_msg_id"`
		Results  []ScrollResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if payload.AnchorID != "aaaabbbb-cccc-dddd-eeee-ffffffffffff" {
		t.Fatalf("expected anchor id echoed, got %q", payload.AnchorID)
	}
	if payload.Count != 3 {
		t.Fatalf("expected 3 messages in window, got %d: %s", payload.Count, result.ForLLM)
	}
	for _, item := range payload.Results {
		if item.ID == "" {
			t.Fatalf("expected non-empty string id: %s", result.ForLLM)
		}
	}
	if payload.Results[1].ID != "aaaabbbb-cccc-dddd-eeee-ffffffffffff" {
		t.Fatalf("expected anchor in the middle, got %+v", payload.Results)
	}
}

func TestSessionSearchReadWithUUIDIDs(t *testing.T) {
	workspace := t.TempDir()
	database, err := db.NewDB(workspace)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer database.Close()

	_, err = database.Exec(`
		INSERT INTO messages (id, session_id, role, content) VALUES
		('178461d1-2f61-4298-b550-95124eea88f9', 's1', 'user', 'first message here'),
		('aaaabbbb-cccc-dddd-eeee-ffffffffffff', 's1', 'assistant', 'second message here')
	`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	tool := NewSessionSearchTool(database.DB)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"mode":       "read",
		"session_id": "s1",
	})
	if result.IsError {
		t.Fatalf("read returned error: %s", result.ForLLM)
	}

	var payload struct {
		Total   int          `json:"total"`
		Results []ReadResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if payload.Total != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", payload.Total, result.ForLLM)
	}
	if payload.Results[0].ID != "178461d1-2f61-4298-b550-95124eea88f9" {
		t.Fatalf("expected UUID string id in insertion order, got %+v", payload.Results)
	}
}

// A caller in one context must never see another context's transcripts:
// discover matches confined to a foreign session are dropped, an explicit
// foreign session_id is refused, and in-context search still works.
func TestSessionSearchCrossContextIsolation(t *testing.T) {
	workspace := t.TempDir()
	database, err := db.NewDB(workspace)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer database.Close()

	_, err = database.Exec(`
		INSERT INTO messages (id, session_id, role, content) VALUES
		('w1', 'ctx::work-sess', 'assistant', 'Your salary is quasar 200,000 per year'),
		('h1', 'ctx::home-sess', 'user', 'remind me to buy milk tomorrow')
	`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	tool := NewSessionSearchTool(database.DB)
	tool.SetContextOf(func(sessionKey string) string {
		if sessionKey == "ctx::work-sess" {
			return "ctx:work"
		}
		return "ctx:personal"
	})
	homeCtx := WithSessionKey(context.Background(), "ctx::home-sess")

	// Query matching only the foreign transcript returns nothing.
	res := tool.Execute(homeCtx, map[string]interface{}{"query": "quasar", "limit": 10})
	if res.IsError {
		t.Fatalf("discover returned error: %s", res.ForLLM)
	}
	var payload struct {
		Count   int                   `json:"count"`
		Results []SessionSearchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("cross-context discover leaked %d foreign result(s): %s", payload.Count, res.ForLLM)
	}

	// Explicitly targeting the foreign session is refused outright.
	res = tool.Execute(homeCtx, map[string]interface{}{"query": "quasar", "session_id": "ctx::work-sess"})
	if !res.IsError {
		t.Fatalf("expected refusal for foreign session_id, got: %s", res.ForLLM)
	}

	// In-context search still works.
	res = tool.Execute(homeCtx, map[string]interface{}{"query": "milk", "limit": 10})
	if res.IsError {
		t.Fatalf("in-context discover returned error: %s", res.ForLLM)
	}
	payload = struct {
		Count   int                   `json:"count"`
		Results []SessionSearchResult `json:"results"`
	}{}
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if payload.Count == 0 {
		t.Fatalf("expected in-context match, got none")
	}
	for _, item := range payload.Results {
		if item.SessionID != "ctx::home-sess" {
			t.Fatalf("expected only home session results, got %s", item.SessionID)
		}
	}

	// Same-context callers keep full visibility (work caller reads work).
	workCtx := WithSessionKey(context.Background(), "ctx::work-sess")
	res = tool.Execute(workCtx, map[string]interface{}{"query": "quasar", "limit": 10})
	if res.IsError {
		t.Fatalf("same-context discover returned error: %s", res.ForLLM)
	}
	payload = struct {
		Count   int                   `json:"count"`
		Results []SessionSearchResult `json:"results"`
	}{}
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("invalid json output: %v", err)
	}
	if payload.Count == 0 {
		t.Fatalf("expected same-context match, got none")
	}
}
