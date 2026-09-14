package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The volatile per-turn tail (time, session, summary) must sit below the
// cache boundary so the stable prefix stays byte-identical across turns.
func TestSystemPromptCacheBoundary(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "GHOST.md"), []byte("# Ghost\nBe helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cb := NewContextBuilder(ws)
	msgs := cb.BuildMessages(context.Background(), nil, "prior summary", "hello", nil, "mobile", "chat1", nil, nil)
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatal("expected system message first")
	}
	sys := msgs[0].Content
	boundary := "<!-- SYSTEM_PROMPT_CACHE_BOUNDARY -->"
	idx := strings.Index(sys, boundary)
	if idx < 0 {
		t.Fatal("system prompt must contain the cache boundary marker")
	}
	prefix, suffix := sys[:idx], sys[idx:]
	_ = prefix
	for _, stable := range []string{"# Skills", "GHOST.md"} {
		if strings.Contains(suffix, stable) {
			t.Fatalf("stable section %q must stay above the boundary", stable)
		}
	}
	for _, volatile := range []string{"## Current Time", "## Current Session", "prior summary"} {
		if !strings.Contains(suffix, volatile) {
			t.Fatalf("volatile %q must sit below the boundary", volatile)
		}
	}
}
