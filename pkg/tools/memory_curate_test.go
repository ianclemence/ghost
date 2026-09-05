package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCuratorContextIsolation(t *testing.T) {
	ws := t.TempDir()
	newTool := func(ctxID string) *MemoryCurateTool {
		tool := NewMemoryCurateTool(ws)
		tool.ContextFor = func(session string) string { return ctxID }
		return tool
	}
	personal, work := newTool("personal"), newTool("work")
	if res := personal.Execute(context.Background(), map[string]interface{}{"action": "add", "target": "memory", "content": "Personal note: anniversary June 1"}); res.IsError {
		t.Fatalf("personal add failed: %s", res.ForLLM)
	}
	if res := work.Execute(context.Background(), map[string]interface{}{"action": "add", "target": "memory", "content": "Work note: deploy Friday"}); res.IsError {
		t.Fatalf("work add failed: %s", res.ForLLM)
	}
	// Personal reads global only (its own file); work reads global + work.
	pRead := personal.Execute(context.Background(), map[string]interface{}{"action": "list", "target": "memory"})
	_ = pRead
	// list action exists? use Entries-equivalent via readEntriesFor through Execute list? Fall back to direct reads:
	pEntries := personal.readEntriesFor("memory", "personal")
	for _, e := range pEntries {
		if strings.Contains(e, "deploy Friday") {
			t.Fatal("personal must not read work note")
		}
	}
	wEntries := work.readEntriesFor("memory", "work")
	foundGlobal, foundWork := false, false
	for _, e := range wEntries {
		if strings.Contains(e, "anniversary") {
			foundGlobal = true
		}
		if strings.Contains(e, "deploy Friday") {
			foundWork = true
		}
	}
	if !foundGlobal || !foundWork {
		t.Fatal("work must read global + own notes")
	}
	// Global files contain no work content (no cross-contamination).
	raw, _ := os.ReadFile(filepath.Join(ws, "knowledge", "self", "curated-memory.md"))
	if strings.Contains(string(raw), "deploy Friday") {
		t.Fatal("work note leaked into the global file")
	}
}

func TestCuratorLegacyGlobal(t *testing.T) {
	// Unwired tool (nil ContextFor) preserves legacy global behavior.
	ws := t.TempDir()
	legacy := NewMemoryCurateTool(ws)
	res := legacy.Execute(context.Background(), map[string]interface{}{"action": "add", "target": "memory", "content": "legacy note"})
	if res.IsError {
		t.Fatalf("legacy add failed: %s", res.ForLLM)
	}
	if got := legacy.readEntries("memory"); len(got) != 1 {
		t.Fatal("legacy writes the global file")
	}
}
