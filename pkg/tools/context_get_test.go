package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func newContextGetTestStore(t *testing.T) *personalcontext.Store {
	t.Helper()
	store, err := personalcontext.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open personal context store: %v", err)
	}
	return store
}

func createPCValue(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	raw, err := personalcontext.RawValue(v)
	if err != nil {
		t.Fatalf("RawValue(%v): %v", v, err)
	}
	return raw
}

func createTestPCEntry(t *testing.T, store *personalcontext.Store, id, kind, subject, predicate string, value string) personalcontext.Entry {
	t.Helper()
	e, err := store.Create(personalcontext.Entry{
		ID:         id,
		Kind:       personalcontext.Kind(kind),
		Subject:    subject,
		Predicate:  predicate,
		Value:      createPCValue(t, value),
		Status:     personalcontext.StatusCurrent,
		Confidence: 0.95,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserDeclared,
			Ref:       "s1:m1",
			Timestamp: time.Now().UTC(),
		}},
	})
	if err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return e
}

func runContextGet(t *testing.T, store *personalcontext.Store, args map[string]interface{}) (contextGetPayload, *ToolResult) {
	t.Helper()
	tool := NewContextGetTool(store)
	res := tool.Execute(context.Background(), args)
	if res.IsError {
		t.Fatalf("context_get returned error: %s", res.ForLLM)
	}
	var payload contextGetPayload
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("invalid context_get JSON output: %v\n%s", err, res.ForLLM)
	}
	return payload, res
}

func pcEntryValue(e personalcontext.Entry) string {
	var s string
	if err := json.Unmarshal(e.Value, &s); err != nil {
		return string(e.Value)
	}
	return s
}

// Query by predicate returns the current entry.
func TestContextGetByPredicate(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "fact", "user", "fact/location", "Bangkok")

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "fact/location"})
	if payload.Count != 1 || len(payload.Entries) != 1 {
		t.Fatalf("count=%d entries=%d, want 1/1: %+v", payload.Count, len(payload.Entries), payload)
	}
	e := payload.Entries[0]
	if pcEntryValue(e) != "Bangkok" {
		t.Errorf("value = %q, want Bangkok", pcEntryValue(e))
	}
	if e.Predicate != "fact/location" {
		t.Errorf("predicate = %q, want fact/location", e.Predicate)
	}
	if e.Status != personalcontext.StatusCurrent {
		t.Errorf("status = %q, want current", e.Status)
	}
}

// Query by kind returns only the current entries of that kind.
func TestContextGetByKind(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "fact", "user", "fact/location", "Bangkok")
	createTestPCEntry(t, store, "ec2", "preference", "user", "preference/favorite_color", "blue")

	payload, _ := runContextGet(t, store, map[string]interface{}{"kind": "fact"})
	if payload.Count != 1 || len(payload.Entries) != 1 {
		t.Fatalf("kind=fact count=%d entries=%d, want 1/1", payload.Count, len(payload.Entries))
	}
	if payload.Entries[0].Predicate != "fact/location" {
		t.Errorf("kind=fact returned predicate %q", payload.Entries[0].Predicate)
	}

	payload, _ = runContextGet(t, store, map[string]interface{}{"kind": "preference"})
	if payload.Count != 1 || payload.Entries[0].Predicate != "preference/favorite_color" {
		t.Fatalf("kind=preference returned %+v, want favorite_color", payload)
	}
}

// Query by subject plus predicate works.
func TestContextGetBySubjectAndPredicate(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "fact", "user", "fact/location", "Bangkok")
	createTestPCEntry(t, store, "ec2", "fact", "someone_else", "fact/location", "Tokyo")

	payload, _ := runContextGet(t, store, map[string]interface{}{
		"subject":   "user",
		"predicate": "fact/location",
	})
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1: %+v", payload.Count, payload)
	}
	if pcEntryValue(payload.Entries[0]) != "Bangkok" {
		t.Errorf("value = %q, want Bangkok", pcEntryValue(payload.Entries[0]))
	}
}

// Superseded entries are not returned as current facts.
func TestContextGetSupersededExcluded(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "preference", "user", "preference/favorite_color", "blue")
	store.Supersede("user", "preference/favorite_color", personalcontext.Entry{
		ID:         "ec2",
		Kind:       personalcontext.KindPreference,
		Subject:    "user",
		Predicate:  "preference/favorite_color",
		Value:      createPCValue(t, "green"),
		Status:     personalcontext.StatusCurrent,
		Confidence: 1.0,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserCorrected,
			Ref:       "s1:m2",
			Timestamp: time.Now().UTC(),
		}},
	})

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "preference/favorite_color"})
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1: %+v", payload.Count, payload)
	}
	if got := pcEntryValue(payload.Entries[0]); got != "green" {
		t.Errorf("value = %q, want green (superseded blue must not appear)", got)
	}
	for _, e := range payload.Entries {
		if pcEntryValue(e) == "blue" {
			t.Error("superseded blue entry leaked into current results")
		}
	}
}

// Rejected/forgotten entries are not returned as current.
func TestContextGetRejectedExcluded(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "fact", "user", "fact/location", "Bangkok")
	if err := store.Forget("ec1"); err != nil {
		t.Fatalf("forget: %v", err)
	}

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "fact/location"})
	if payload.Count != 0 || len(payload.Entries) != 0 {
		t.Fatalf("count=%d entries=%d, want 0/0: %+v", payload.Count, len(payload.Entries), payload)
	}
}

// Expired entries are not returned.
func TestContextGetExpiredExcluded(t *testing.T) {
	store := newContextGetTestStore(t)
	past := time.Now().UTC().Add(-24 * time.Hour)
	store.Create(personalcontext.Entry{
		ID:         "ec1",
		Kind:       personalcontext.KindFact,
		Subject:    "user",
		Predicate:  "fact/location",
		Value:      createPCValue(t, "Tokyo"),
		Status:     personalcontext.StatusCurrent,
		Confidence: 0.95,
		ValidUntil: &past,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserDeclared,
			Ref:       "s1:m1",
			Timestamp: time.Now().UTC(),
		}},
	})

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "fact/location"})
	if payload.Count != 0 || len(payload.Entries) != 0 {
		t.Fatalf("expired entry leaked into current results: %+v", payload)
	}
}

// Future-valid entries are not returned.
func TestContextGetFutureValidExcluded(t *testing.T) {
	store := newContextGetTestStore(t)
	future := time.Now().UTC().Add(24 * time.Hour)
	store.Create(personalcontext.Entry{
		ID:         "ec1",
		Kind:       personalcontext.KindFact,
		Subject:    "user",
		Predicate:  "fact/location",
		Value:      createPCValue(t, "London"),
		Status:     personalcontext.StatusCurrent,
		Confidence: 0.95,
		ValidFrom:  &future,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserDeclared,
			Ref:       "s1:m1",
			Timestamp: time.Now().UTC(),
		}},
	})

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "fact/location"})
	if payload.Count != 0 || len(payload.Entries) != 0 {
		t.Fatalf("future-valid entry leaked into current results: %+v", payload)
	}
}

// Conflicting entries are surfaced explicitly as unresolved, never as a
// normal current fact, and never silently resolved to one of them.
func TestContextGetConflictsExposedNotResolved(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "preference", "user", "preference/foo", "A")
	createTestPCEntry(t, store, "ec2", "preference", "user", "preference/foo", "B")
	if err := store.DeclareConflict("user", "preference/foo", "ec1", "ec2"); err != nil {
		t.Fatalf("declare conflict: %v", err)
	}

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "preference/foo"})
	if payload.Count != 0 || len(payload.Entries) != 0 {
		t.Fatalf("conflicting entries presented as current facts: %+v", payload)
	}
	if len(payload.Unresolved) != 2 {
		t.Fatalf("unresolved = %d, want 2: %+v", len(payload.Unresolved), payload)
	}
	if payload.Note == "" {
		t.Error("conflict result should carry an explicit note")
	}
	values := map[string]bool{}
	for _, e := range payload.Unresolved {
		if e.Status != personalcontext.StatusConflicting {
			t.Errorf("unresolved entry status = %q, want conflicting", e.Status)
		}
		values[pcEntryValue(e)] = true
	}
	if !values["A"] || !values["B"] {
		t.Errorf("unresolved values = %v, want both A and B (no silent resolution)", values)
	}
}

// Provenance is preserved in the result.
func TestContextGetPreservesProvenance(t *testing.T) {
	store := newContextGetTestStore(t)
	now := time.Now().UTC()
	store.Create(personalcontext.Entry{
		ID:         "ec1",
		Kind:       personalcontext.KindPreference,
		Subject:    "user",
		Predicate:  "preference/favorite_color",
		Value:      createPCValue(t, "green"),
		Status:     personalcontext.StatusCurrent,
		Confidence: 1.0,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserCorrected,
			Ref:       "s1:m42",
			Timestamp: now,
		}},
	})

	payload, _ := runContextGet(t, store, map[string]interface{}{"predicate": "preference/favorite_color"})
	e := payload.Entries[0]
	if len(e.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(e.Sources))
	}
	src := e.Sources[0]
	if src.Type != personalcontext.SourceConversation {
		t.Errorf("source type = %q, want conversation", src.Type)
	}
	if src.Kind != personalcontext.SourceUserCorrected {
		t.Errorf("source kind = %q, want user_corrected", src.Kind)
	}
	if src.Ref != "s1:m42" {
		t.Errorf("source ref = %q, want s1:m42", src.Ref)
	}
	if src.Timestamp.IsZero() {
		t.Error("source timestamp is zero")
	}
}

// Empty/no-match queries return a clean structured result, never an error.
func TestContextGetNoMatchReturnsCleanResult(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "fact", "user", "fact/location", "Bangkok")

	payload, res := runContextGet(t, store, map[string]interface{}{"predicate": "preference/favorite_color"})
	if res.Silent != true {
		t.Error("context_get result should be silent (structured output for the LLM, not user prose)")
	}
	if payload.Count != 0 || len(payload.Entries) != 0 {
		t.Fatalf("count=%d entries=%d, want 0/0", payload.Count, len(payload.Entries))
	}
	if len(payload.Unresolved) != 0 {
		t.Errorf("unresolved = %d, want 0", len(payload.Unresolved))
	}
	if payload.Note != "" {
		t.Errorf("note should be empty on no-match, got %q", payload.Note)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if string(raw) == "" {
		t.Error("empty result should still marshal to structured JSON")
	}
}

// A query with no filters is rejected rather than dumping the whole store.
func TestContextGetRequiresFilter(t *testing.T) {
	store := newContextGetTestStore(t)
	createTestPCEntry(t, store, "ec1", "fact", "user", "fact/location", "Bangkok")

	tool := NewContextGetTool(store)
	res := tool.Execute(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatal("expected error when no filter is supplied")
	}
}

// An invalid kind is rejected with a clear message.
func TestContextGetRejectsInvalidKind(t *testing.T) {
	store := newContextGetTestStore(t)
	tool := NewContextGetTool(store)
	res := tool.Execute(context.Background(), map[string]interface{}{"kind": "not-a-kind"})
	if !res.IsError {
		t.Fatal("expected error for invalid kind")
	}
}

// A nil store reports an unavailable error instead of panicking.
func TestContextGetNilStore(t *testing.T) {
	tool := NewContextGetTool(nil)
	res := tool.Execute(context.Background(), map[string]interface{}{"predicate": "fact/location"})
	if !res.IsError {
		t.Fatal("expected error when store is nil")
	}
}
