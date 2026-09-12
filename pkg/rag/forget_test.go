package rag

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/db"
)

func newForgetTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	return NewStore(database, nil, config.RAGConfig{})
}

func seedChunk(t *testing.T, s *Store, content string) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO memory_chunks (id, content, embedding, source, created_at) VALUES (?, ?, ?, ?, ?)`,
		"test-"+content[:4], content, "[]", "memory_tool", "2026-09-12",
	); err != nil {
		t.Fatalf("seed chunk: %v", err)
	}
}

func chunkCount(t *testing.T, s *Store, like string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM memory_chunks WHERE content LIKE ?`, "%"+like+"%",
	).Scan(&n); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	return n
}

// Forgetting removes every chunk carrying the retired value, and only those.
func TestForgetValueRemovesMatchingChunks(t *testing.T) {
	s := newForgetTestStore(t)
	seedChunk(t, s, "mango sticky rice is the snack")
	seedChunk(t, s, "TRAVEL NOTE mango sticky rice recalled")
	seedChunk(t, s, "The user's name is Ian")

	n, err := s.ForgetValue(context.Background(), "mango sticky rice")
	if err != nil {
		t.Fatalf("ForgetValue: %v", err)
	}
	if n != 2 {
		t.Fatalf("removed %d chunks, want 2", n)
	}
	if got := chunkCount(t, s, "mango"); got != 0 {
		t.Fatalf("%d mango chunks remain", got)
	}
	if got := chunkCount(t, s, "Ian"); got != 1 {
		t.Fatalf("unrelated chunk removed")
	}
}

// Particles and blanks never drive deletes.
func TestForgetValueIgnoresShortValues(t *testing.T) {
	s := newForgetTestStore(t)
	seedChunk(t, s, "The user's name is Ian")
	for _, v := range []string{"", "  ", "it", "a"} {
		if n, err := s.ForgetValue(context.Background(), v); err != nil || n != 0 {
			t.Fatalf("ForgetValue(%q) = %d, %v; want 0, nil", v, n, err)
		}
	}
	if got := chunkCount(t, s, "Ian"); got != 1 {
		t.Fatal("short value deleted a chunk")
	}
}
