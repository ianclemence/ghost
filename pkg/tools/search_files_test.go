package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchFilesFindsTheLineAndIgnoresInternalStores(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"notes/trip.md":            "# Packing\nBring a Hanoi map\nsunscreen",
		"memory/MEMORY.md":         "Hanoi is private here",
		"personal-context/x.jsonl": "hanoi",
		"skills/travel/SKILL.md":   "hanoi",
		"notes/binary.bin":         "hanoi\x00\x01",
		".hidden/secret.txt":       "hanoi",
	})
	res := NewSearchFilesTool(ws, true).Execute(context.Background(), map[string]interface{}{"query": "HANOI"})
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "notes/trip.md:2: Bring a Hanoi map") {
		t.Fatalf("missing the match with its line:\n%s", res.ForLLM)
	}
	for _, leak := range []string{"memory/", "personal-context", "skills/", "binary.bin", ".hidden"} {
		if strings.Contains(res.ForLLM, leak) {
			t.Fatalf("searched %s, which it must not:\n%s", leak, res.ForLLM)
		}
	}
}

func TestSearchFilesSaysSoWhenNothingMatches(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"a.txt": "hello"})
	res := NewSearchFilesTool(ws, true).Execute(context.Background(), map[string]interface{}{"query": "zebra"})
	if res.IsError || !strings.Contains(res.ForLLM, "No file") {
		t.Fatalf("an empty search must say so plainly: %+v", res)
	}
}

func TestSearchFilesStaysInsideTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	res := NewSearchFilesTool(ws, true).Execute(context.Background(), map[string]interface{}{"query": "x", "path": "../.."})
	if !res.IsError {
		t.Fatal("a path outside the workspace must be refused")
	}
}

func TestSearchFilesStopsAtTheLimit(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"a.txt": strings.Repeat("needle\n", 50)})
	res := NewSearchFilesTool(ws, true).Execute(context.Background(), map[string]interface{}{"query": "needle", "limit": float64(5)})
	if strings.Count(res.ForLLM, "a.txt:") != 5 || !strings.Contains(res.ForLLM, "stopped at 5") {
		t.Fatalf("limit not honoured:\n%s", res.ForLLM)
	}
}
