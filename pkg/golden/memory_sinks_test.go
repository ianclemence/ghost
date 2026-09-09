package golden

import (
	"os"
	"path/filepath"
	"testing"
)

// The grader's memory view spans every real sink: personal-context
// entries, the remember tool's MEMORY.md, its RAG rows, and the
// memory_curate user profile. A fact stored by ANY mechanism must satisfy
// a value-level memory_present assertion.
func TestReadMemoriesSeesAllSinks(t *testing.T) {
	ws := t.TempDir()
	// personal-context (extraction)
	pc := `{"id":"e1","kind":"preference","subject":"user","predicate":"preference/prefers","value":"the colour teal.","status":"current","confidence":1,"sources":[{"type":"conversation","kind":"inferred","ref":"r","timestamp":"2026-01-01T00:00:00Z"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}
{"id":"e2","kind":"fact","subject":"user","predicate":"relation/sister-name","value":"Ana.","status":"current","confidence":1,"sources":[{"type":"conversation","kind":"inferred","ref":"r","timestamp":"2026-01-01T00:00:00Z"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}
`
	mustWrite(t, filepath.Join(ws, "personal-context", "entries.jsonl"), pc)
	// remember tool file sink
	mustWrite(t, filepath.Join(ws, "memory", "MEMORY.md"), "# Memory\n\n- [2026-09-09] (user_preference) I prefer the colour teal.\n")
	// memory_curate profile sink
	mustWrite(t, filepath.Join(ws, "knowledge", "self", "user-profile.md"), "The user prefers the colour teal.\n")

	rows := readMemories(ws)
	if !matchMemory(rows, Match{Value: "teal"}, false) {
		t.Fatalf("teal not found across sinks: %+v", rows)
	}
	if !matchMemory(rows, Match{Value: "ana"}, false) {
		t.Fatalf("ana not found across sinks: %+v", rows)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
