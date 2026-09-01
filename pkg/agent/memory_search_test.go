package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeNote(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryStoreSearchRelevance(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	writeNote(t, ws, "memory/202608/20260831.md", "# 2026-08-31\nShopping list: eggs, milk, flour.\n")
	writeNote(t, ws, "memory/202609/20260901.md", "# 2026-09-01\nCall grandma about her birthday gift.\n")

	hits := ms.Search("shopping list", 5)
	if len(hits) == 0 {
		t.Fatalf("expected a hit for 'shopping list'")
	}
	// The note with the exact phrase should rank first.
	if filepath.Base(hits[0].Path) != "20260831.md" {
		t.Fatalf("expected the shopping note first, got %s", hits[0].Path)
	}
}

func TestMemoryStoreSearchNoMatch(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	writeNote(t, ws, "memory/202608/20260831.md", "unrelated content")
	if hits := ms.Search("zebra", 5); len(hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(hits))
	}
}

func TestMemoryStoreSearchRecencyRanking(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	// Two notes mentioning 'flight'; the recent one should outrank the old one
	// when keyword counts are equal.
	old := filepath.Join(ws, "memory/202601/20260101.md")
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(old, []byte("flight EK1"), 0644)
	_ = os.Chtimes(old, time.Now().Add(-60*24*time.Hour), time.Now().Add(-60*24*time.Hour))

	recent := filepath.Join(ws, "memory/202609/20260901.md")
	if err := os.MkdirAll(filepath.Dir(recent), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(recent, []byte("flight EK1"), 0644)

	hits := ms.Search("flight", 5)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if filepath.Base(hits[0].Path) != "20260901.md" {
		t.Fatalf("expected the recent note first, got %s", hits[0].Path)
	}
}

func TestMemoryStoreSearchLimit(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	for i := 0; i < 5; i++ {
		writeNote(t, ws, "memory/202608/"+dayName(i)+".md", "topic shared "+dayName(i))
	}
	hits := ms.Search("topic shared", 3)
	if len(hits) != 3 {
		t.Fatalf("expected limit 3, got %d", len(hits))
	}
}

func dayName(i int) string {
	return "note" + string(rune('a'+i)) + ".md"
}
