package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func curateVersion(t *testing.T, res *ToolResult) string {
	t.Helper()
	var out struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &out); err != nil || out.Version == "" {
		t.Fatalf("response carries no version token: %s", res.ForLLM)
	}
	return out.Version
}

// A stale read must fail instead of clobbering: two turns read v1, one
// replaces, the other's replace with the old token loses with the current
// state attached — and a retry with the fresh token wins.
func TestCuratorOptimisticLocking(t *testing.T) {
	ws := t.TempDir()
	tool := NewMemoryCurateTool(ws)
	ctx := context.Background()
	add := func(content string) *ToolResult {
		return tool.Execute(ctx, map[string]interface{}{"action": "add", "target": "memory", "content": content})
	}
	if res := add("Owner prefers morning briefs"); res.IsError {
		t.Fatalf("add failed: %s", res.ForLLM)
	}
	v1 := curateVersion(t, add("Pod lives in Nairobi"))
	// Both turns saw v1. One replaces first.
	rep := map[string]interface{}{"action": "replace", "target": "memory", "old_text": "Nairobi", "content": "Pod lives in Kisumu", "expected_version": v1}
	if res := tool.Execute(ctx, rep); res.IsError {
		t.Fatalf("first replace failed: %s", res.ForLLM)
	}
	// The loser's stale token fails with the current state, touching nothing.
	stale := tool.Execute(ctx, rep)
	if !stale.IsError {
		t.Fatal("stale replace must fail, not clobber")
	}
	for _, want := range []string{"changed since you read it", "version", "entries", "Kisumu"} {
		if !strings.Contains(stale.ForLLM, want) {
			t.Fatalf("conflict must carry current state, missing %q:\n%s", want, stale.ForLLM)
		}
	}
	// Retry with the fresh token wins.
	var cur struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(stale.ForLLM), &cur); err != nil || cur.Version == "" {
		t.Fatalf("conflict carries no retry token: %s", stale.ForLLM)
	}
	rep["expected_version"] = cur.Version
	rep["old_text"] = "Kisumu"
	rep["content"] = "Pod lives in Kisumu town"
	if res := tool.Execute(ctx, rep); res.IsError {
		t.Fatalf("fresh retry failed: %s", res.ForLLM)
	}
	// Remove obeys the same lock.
	rm := map[string]interface{}{"action": "remove", "target": "memory", "old_text": "morning briefs", "expected_version": v1}
	if res := tool.Execute(ctx, rm); !res.IsError || !strings.Contains(res.ForLLM, "changed since you read it") {
		t.Fatalf("stale remove must conflict, got: %s", res.ForLLM)
	}
	// And without a token everything behaves as before.
	if res := tool.Execute(ctx, map[string]interface{}{"action": "remove", "target": "memory", "old_text": "morning briefs"}); res.IsError {
		t.Fatalf("untokened remove failed: %s", res.ForLLM)
	}
}

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
