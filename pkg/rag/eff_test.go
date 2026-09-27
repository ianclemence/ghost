package rag

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

// countingEmbedder records how many times the embedding provider was hit.
// The embedding cache's whole purpose is to stop paying this cost for a
// repeated query, so the call count is the measurable contract.
type countingEmbedder struct {
	mu    sync.Mutex
	calls int
	vec   []float32
	err   error
}

func (c *countingEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	c.mu.Lock()
	c.calls++
	v, err := c.vec, c.err
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (c *countingEmbedder) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func newRAGTestStore(t *testing.T, provider providers.EmbeddingProvider) *Store {
	t.Helper()
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	return NewStore(database, provider, config.RAGConfig{})
}

// seedEmbeddedChunk inserts a chunk with a real vector and loads the index so
// the store is ready with exactly one searchable item.
func seedEmbeddedChunk(t *testing.T, s *Store, id, content string, vec []float32) {
	t.Helper()
	raw, err := json.Marshal(vec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO memory_chunks (id, content, embedding, source, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, content, string(raw), "memory_tool", "2026-09-12",
	); err != nil {
		t.Fatalf("seed chunk: %v", err)
	}
	if err := s.LoadIndex(context.Background()); err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
}

// An empty index is a no-op: retrieval must not pay an embedding round-trip
// to search nothing.
func TestEmptyIndexSkipsEmbedding(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{1, 0, 0}}
	s := newRAGTestStore(t, emb)
	if err := s.LoadIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.RetrieveScoped(context.Background(), "anything at all", 5, nil)
	if err != nil {
		t.Fatalf("RetrieveScoped: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty index returned %d results", len(got))
	}
	if emb.callCount() != 0 {
		t.Fatalf("empty index embedded the query %d times, want 0", emb.callCount())
	}
}

// Blank queries never reach the embedder.
func TestBlankQuerySkipsEmbedding(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{1, 0, 0}}
	s := newRAGTestStore(t, emb)
	seedEmbeddedChunk(t, s, "c1", "the owner prefers tea", []float32{1, 0, 0})

	for _, q := range []string{"", "   ", "\t\n"} {
		if _, err := s.RetrieveScoped(context.Background(), q, 5, nil); err != nil {
			t.Fatalf("RetrieveScoped(%q): %v", q, err)
		}
	}
	if emb.callCount() != 0 {
		t.Fatalf("blank queries embedded %d times, want 0", emb.callCount())
	}
}

// A repeated query is answered from the bounded cache, so only the first
// lookup pays the embedding cost.
func TestRepeatedQueryUsesEmbeddingCache(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{1, 0, 0}}
	s := newRAGTestStore(t, emb)
	seedEmbeddedChunk(t, s, "c1", "the owner prefers tea", []float32{1, 0, 0})

	for i := 0; i < 5; i++ {
		if _, err := s.RetrieveScoped(context.Background(), "What tea does the owner like?", 5, nil); err != nil {
			t.Fatalf("RetrieveScoped: %v", err)
		}
	}
	if emb.callCount() != 1 {
		t.Fatalf("embedding provider called %d times, want 1 (cache)", emb.callCount())
	}
	hits, misses := s.EmbedStats()
	if hits != 4 || misses != 1 {
		t.Fatalf("embed stats = %d hits/%d misses, want 4/1", hits, misses)
	}
}

// The cache is bounded: more distinct queries than capacity never grow it
// without limit.
func TestEmbeddingCacheIsBounded(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{1, 0, 0}}
	s := newRAGTestStore(t, emb)
	seedEmbeddedChunk(t, s, "c1", "the owner prefers tea", []float32{1, 0, 0})

	for i := 0; i < embedCacheMaxSize+25; i++ {
		q := "query number " + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26))
		if _, err := s.RetrieveScoped(context.Background(), q, 5, nil); err != nil {
			t.Fatalf("RetrieveScoped: %v", err)
		}
	}
	s.embedMu.Lock()
	size := len(s.embedCache)
	s.embedMu.Unlock()
	if size > embedCacheMaxSize {
		t.Fatalf("cache grew to %d, cap is %d", size, embedCacheMaxSize)
	}
}

// With no embedding provider the store degrades honestly instead of
// panicking on a nil interface.
func TestNilEmbeddingProviderIsHonest(t *testing.T) {
	s := newRAGTestStore(t, nil)
	if err := s.LoadIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	seedEmbeddedChunk(t, s, "c1", "the owner prefers tea", []float32{1, 0, 0})
	_, err := s.RetrieveScoped(context.Background(), "tea", 5, nil)
	if err == nil {
		t.Fatal("nil provider must report an error, not panic or fabricate results")
	}
}

// BenchmarkRetrieveCachedQuery measures the warm interactive path: one
// seeded chunk and a repeated query, which the cache serves without an
// embedding round-trip. Run with -bench=. -benchmem.
func BenchmarkRetrieveCachedQuery(b *testing.B) {
	emb := &countingEmbedder{vec: []float32{1, 0, 0}}
	database, err := db.NewDB(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	s := NewStore(database, emb, config.RAGConfig{})
	raw, _ := json.Marshal([]float32{1, 0, 0})
	if _, err := s.db.Exec(
		`INSERT INTO memory_chunks (id, content, embedding, source, created_at) VALUES ('c1','the owner prefers tea',?,'memory_tool','2026-09-12')`,
		string(raw),
	); err != nil {
		b.Fatal(err)
	}
	if err := s.LoadIndex(context.Background()); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	// Warm the cache.
	if _, err := s.RetrieveScoped(ctx, "what tea does the owner like?", 5, nil); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.RetrieveScoped(ctx, "what tea does the owner like?", 5, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRetrieveEmptyIndex measures the interactive path when the memory
// index is empty: it must return without touching the embedder.
func BenchmarkRetrieveEmptyIndex(b *testing.B) {
	emb := &countingEmbedder{vec: []float32{1, 0, 0}}
	database, err := db.NewDB(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	s := NewStore(database, emb, config.RAGConfig{})
	if err := s.LoadIndex(context.Background()); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.RetrieveScoped(ctx, "what tea does the owner like?", 5, nil); err != nil {
			b.Fatal(err)
		}
	}
	if emb.callCount() != 0 {
		b.Fatalf("empty index embedded %d times", emb.callCount())
	}
}
