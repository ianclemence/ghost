package rag

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func readChunkVec(t *testing.T, s *Store, id string) []float32 {
	t.Helper()
	var raw string
	if err := s.db.QueryRow(`SELECT embedding FROM memory_chunks WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatalf("read chunk %s: %v", id, err)
	}
	var vec []float32
	if err := json.Unmarshal([]byte(raw), &vec); err != nil {
		t.Fatalf("unmarshal chunk %s: %v", id, err)
	}
	return vec
}

func readStamp(t *testing.T, s *Store) string {
	t.Helper()
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM kv_store WHERE key = ?`, embedModelStampKey).Scan(&raw); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	var stamped string
	if err := json.Unmarshal([]byte(raw), &stamped); err != nil {
		t.Fatalf("unmarshal stamp: %v", err)
	}
	return stamped
}

func stampModel(t *testing.T, s *Store, model string) {
	t.Helper()
	raw, _ := json.Marshal(model)
	if _, err := s.db.Exec(
		`INSERT INTO kv_store (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		embedModelStampKey, string(raw), time.Now()); err != nil {
		t.Fatalf("stamp: %v", err)
	}
}

// A model change re-embeds stored content with the new provider and
// stamps the new model; content is preserved, vectors are replaced.
func TestEnsureEmbedModelMigratesOnChange(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{9, 9, 9}}
	s := newRAGTestStore(t, emb)
	seedEmbeddedChunk(t, s, "c1", "the owner prefers tea", []float32{1, 2, 3})
	stampModel(t, s, "nomic-embed-text")

	if err := s.EnsureEmbedModel(context.Background(), "embeddinggemma"); err != nil {
		t.Fatalf("EnsureEmbedModel: %v", err)
	}
	if got := emb.callCount(); got != 1 {
		t.Errorf("embed calls = %d, want 1 (one stored chunk)", got)
	}
	vec := readChunkVec(t, s, "c1")
	if len(vec) != 3 || vec[0] != 9 {
		t.Errorf("chunk not re-embedded: %v", vec)
	}
	if got := readStamp(t, s); got != "embeddinggemma" {
		t.Errorf("stamp = %q, want embeddinggemma", got)
	}
}

// Matching stamp is a no-op: no provider calls, vectors untouched.
func TestEnsureEmbedModelNoopWhenCurrent(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{9, 9, 9}}
	s := newRAGTestStore(t, emb)
	seedEmbeddedChunk(t, s, "c1", "the owner prefers tea", []float32{1, 2, 3})
	stampModel(t, s, "embeddinggemma")

	if err := s.EnsureEmbedModel(context.Background(), "embeddinggemma"); err != nil {
		t.Fatalf("EnsureEmbedModel: %v", err)
	}
	if got := emb.callCount(); got != 0 {
		t.Errorf("embed calls = %d, want 0", got)
	}
	vec := readChunkVec(t, s, "c1")
	if len(vec) != 3 || vec[0] != 1 {
		t.Errorf("chunk should be untouched: %v", vec)
	}
}

// Empty index still stamps, so a first run is never mistaken for a
// legacy nomic index later.
func TestEnsureEmbedModelStampsEmptyIndex(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{9, 9, 9}}
	s := newRAGTestStore(t, emb)

	if err := s.EnsureEmbedModel(context.Background(), "embeddinggemma"); err != nil {
		t.Fatalf("EnsureEmbedModel: %v", err)
	}
	if got := emb.callCount(); got != 0 {
		t.Errorf("embed calls = %d, want 0", got)
	}
	if got := readStamp(t, s); got != "embeddinggemma" {
		t.Errorf("stamp = %q, want embeddinggemma", got)
	}
}
