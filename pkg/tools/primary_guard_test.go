package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ghost's primary files — the database, the prompt contract files, the
// skills estate, and the long-term memory digest — must never be
// overwritten or destroyed through the model's file tools, regardless of
// the workspace-restriction setting. Integrity is not a config option:
// the application's own in-process writers never route through these
// tools, so blocking model-side destruction breaks nothing legitimate.
func TestFileToolsNeverDestroyPrimaryFiles(t *testing.T) {
	ws := t.TempDir()
	// Fixtures are created directly on disk: the guard must be what stops
	// the tools, not the absence of a target.
	writeFixture := func(rel string) string {
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("original"), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ctx := context.Background()

	primaryPaths := []string{
		"ghost.db",
		"ghost.db-wal",
		"GHOST.md",
		"GHOST.md.pre-caveat.bak",
		"AGENTS.md",
		"SOUL.md",
		"IDENTITY.md",
		"HEARTBEAT.md",
		"skills/weather/SKILL.md",
		"memory/MEMORY.md",
	}
	for _, rel := range primaryPaths {
		p := writeFixture(rel)

		w := NewWriteFileTool(ws, false) // restriction off: integrity must not depend on it
		res := w.Execute(ctx, map[string]interface{}{"path": p, "content": ""})
		if !res.IsError || !strings.Contains(res.ForLLM, "primary") {
			t.Errorf("write_file must refuse primary file %s, got IsError=%v msg=%q", rel, res.IsError, res.ForLLM)
		}

		e := NewEditFileTool(ws, false)
		res = e.Execute(ctx, map[string]interface{}{"path": p, "old_text": "original", "new_text": "clobbered"})
		if !res.IsError || !strings.Contains(res.ForLLM, "primary") {
			t.Errorf("edit_file must refuse primary file %s, got IsError=%v msg=%q", rel, res.IsError, res.ForLLM)
		}

		a := NewAppendFileTool(ws, false)
		res = a.Execute(ctx, map[string]interface{}{"path": p, "content": "clobbered"})
		if !res.IsError || !strings.Contains(res.ForLLM, "primary") {
			t.Errorf("append_file must refuse primary file %s, got IsError=%v msg=%q", rel, res.IsError, res.ForLLM)
		}

		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("primary file %s vanished: %v", rel, err)
		}
		if string(data) != "original" {
			t.Errorf("primary file %s was modified: %q", rel, string(data))
		}
	}

	// Control: ordinary workspace files stay fully writable.
	w := NewWriteFileTool(ws, false)
	res := w.Execute(ctx, map[string]interface{}{
		"path":    filepath.Join(ws, "notes", "todo.txt"),
		"content": "hi",
	})
	if res.IsError {
		t.Fatalf("normal write must still work: %s", res.ForLLM)
	}
}

// The shell must never reference the database or config at all, and must
// refuse any destructive shape aimed at Ghost's primary estate — even when
// the command would be harmless if it ran (the assertions below run echo).
func TestExecNeverTouchesPrimaryFiles(t *testing.T) {
	ws := t.TempDir()
	tool := NewExecTool(ws, false)
	ctx := context.Background()

	denied := []string{
		"echo ghost.db",            // blanket: the database is never named from the shell
		"echo rm -rf skills",       // destructive verb aimed at the skills estate
		"echo mv GHOST.md /tmp",    // moving the prompt contract out
		"echo rm -rf .",            // broad recursive wipe of the working tree
		"echo rm memory/MEMORY.md", // destroying the long-term memory digest
	}
	for _, cmd := range denied {
		res := tool.Execute(ctx, map[string]interface{}{"command": cmd})
		if !res.IsError || !strings.Contains(res.ForLLM, "primary") {
			t.Errorf("exec must refuse %q, got IsError=%v msg=%q", cmd, res.IsError, res.ForLLM)
		}
	}
	// Control: an ordinary command still runs.
	res := tool.Execute(ctx, map[string]interface{}{"command": "echo hello"})
	if res.IsError {
		t.Errorf("ordinary command must run: %s", res.ForLLM)
	}
	// Control: a non-destructive mention of an estate name still runs
	// (listing/reading through the shell needs no approval-time refusal).
	res = tool.Execute(ctx, map[string]interface{}{"command": "echo listing skills"})
	if res.IsError {
		t.Errorf("non-destructive mention must run, got IsError=%v msg=%q", res.IsError, res.ForLLM)
	}
}

// A model-supplied working directory must stay inside the allowed roots
// (workspace, its project root, media temp) — the same boundary the file
// tools enforce — so exec cannot mount an arbitrary directory writable.
func TestExecConfinesWorkingDir(t *testing.T) {
	ws := t.TempDir()
	tool := NewExecTool(ws, true)
	ctx := context.Background()

	res := tool.Execute(ctx, map[string]interface{}{
		"command":     "echo hi",
		"working_dir": "/etc",
	})
	if !res.IsError || !strings.Contains(res.ForLLM, "working directory") {
		t.Errorf("working_dir outside the allowed roots must be refused, got IsError=%v msg=%q", res.IsError, res.ForLLM)
	}

	res = tool.Execute(ctx, map[string]interface{}{
		"command":     "echo hi",
		"working_dir": ws,
	})
	if res.IsError {
		t.Errorf("working_dir inside the workspace must run: %s", res.ForLLM)
	}
}
