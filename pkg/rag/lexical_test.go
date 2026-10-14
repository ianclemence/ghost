package rag

import (
	"context"
	"testing"
)

func TestFTSQueryQuotesWordsAndDropsNoise(t *testing.T) {
	got := ftsQuery(`What did I say about Somchai's "TS-999" OR NEAR(x)?`)
	for _, want := range []string{`"somchai"`, `"999"`, `"say"`} {
		if !contains(got, want) {
			t.Errorf("%q missing %s", got, want)
		}
	}
	if contains(got, `"what"`) || contains(got, `"about"`) {
		t.Errorf("stop words kept: %q", got)
	}
	if ftsQuery("a ?! the") != "" {
		t.Error("a query with nothing to match must be empty")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestFuseRanksFavoursWhatBothSearchesFound(t *testing.T) {
	got := fuseRanks([]string{"a", "b", "c"}, []string{"c", "d"})
	if got[0] != "c" {
		t.Fatalf("found by both searches, c must lead: %v", got)
	}
	if len(got) != 4 {
		t.Fatalf("every candidate once: %v", got)
	}
}

func TestLexicalSearchFindsAnExactNameTheVectorsMiss(t *testing.T) {
	s := newRAGTestStore(t, &countingEmbedder{vec: []float32{1, 0}})
	seedEmbeddedChunk(t, s, "c1", "Somchai is a mechanic in Pattaya", []float32{0, 1})
	seedEmbeddedChunk(t, s, "c2", "Green tea is my favourite drink", []float32{0, 1})
	ids := s.lexicalIDs("Who is Somchai?", 5)
	if len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("exact name must be found by words alone: %v", ids)
	}
	_ = context.Background()
}

func TestLexicalIndexFollowsDeletes(t *testing.T) {
	s := newRAGTestStore(t, &countingEmbedder{vec: []float32{1, 0}})
	seedEmbeddedChunk(t, s, "c1", "Somchai is a mechanic", []float32{0, 1})
	if _, err := s.db.Exec(`DELETE FROM memory_chunks WHERE id = 'c1'`); err != nil {
		t.Fatal(err)
	}
	if ids := s.lexicalIDs("Somchai", 5); len(ids) != 0 {
		t.Fatalf("a forgotten chunk must leave the word index too: %v", ids)
	}
}
