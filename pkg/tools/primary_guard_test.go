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

// Ghost's runtime-owned estate — session logs, the scheduler, the event
// trail, the personal-context journal, reflection and in-flight state, the
// runtime journal, portable conversations, the heartbeat log and the
// installer-owned docs — is never created, changed, or deleted through the
// model's file tools. Owner content (memory notes, knowledge, captures,
// scratch, learning) stays writable so note-taking keeps working.
func TestFileToolsNeverWriteRuntimeEstate(t *testing.T) {
	ws := t.TempDir()
	fixture := func(rel string) string {
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

	estate := []string{
		"sessions/2026-01-01.jsonl",
		"commitments/active.json",
		"cron/jobs.json",
		"events/events.ndjson",
		"dreams/marker",
		"proactive/marker",
		"pending/question.json",
		"personal-context/entries.jsonl",
		"state/state.json",
		"journal/trace.log",
		"conversations/c1.jsonl",
		"heartbeat.log",
		"README.md",
		"USER.md",
	}
	for _, rel := range estate {
		p := fixture(rel)

		w := NewWriteFileTool(ws, false)
		res := w.Execute(ctx, map[string]interface{}{"path": p, "content": "clobbered"})
		if !res.IsError || !strings.Contains(res.ForLLM, "runtime") {
			t.Errorf("write_file must refuse estate path %s, got IsError=%v msg=%q", rel, res.IsError, res.ForLLM)
		}

		e := NewEditFileTool(ws, false)
		res = e.Execute(ctx, map[string]interface{}{"path": p, "old_text": "original", "new_text": "clobbered"})
		if !res.IsError || !strings.Contains(res.ForLLM, "runtime") {
			t.Errorf("edit_file must refuse estate path %s, got IsError=%v msg=%q", rel, res.IsError, res.ForLLM)
		}

		a := NewAppendFileTool(ws, false)
		res = a.Execute(ctx, map[string]interface{}{"path": p, "content": "clobbered"})
		if !res.IsError || !strings.Contains(res.ForLLM, "runtime") {
			t.Errorf("append_file must refuse estate path %s, got IsError=%v msg=%q", rel, res.IsError, res.ForLLM)
		}

		if data, err := os.ReadFile(p); err != nil || string(data) != "original" {
			t.Errorf("estate file %s was modified: %q err=%v", rel, string(data), err)
		}
	}

	// Controls: owner content and scratch stay fully writable.
	for _, rel := range []string{
		"memory/2026-01-01.md",
		"knowledge/note.md",
		"data/shopping_list.txt",
		"tmp/scratch.txt",
		"learning/context.json",
		"notes/todo.txt",
	} {
		w := NewWriteFileTool(ws, false)
		res := w.Execute(ctx, map[string]interface{}{"path": filepath.Join(ws, rel), "content": "ok"})
		if res.IsError {
			t.Errorf("owner content %s must stay writable: %s", rel, res.ForLLM)
		}
	}
}

// Outside the workspace, Ghost's own runtime home (backups, install
// snapshots) is never a write target even though validatePath's
// project-root allowance reaches the parent directory.
func TestFileToolsNeverWriteRuntimeHome(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "workspace")
	if err := os.MkdirAll(ws, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, dir := range []string{"backups", "workspace-orig-20260101", "skills-backup-20260101"} {
		target := filepath.Join(home, dir, "x.txt")
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		w := NewWriteFileTool(ws, false)
		res := w.Execute(ctx, map[string]interface{}{"path": target, "content": "clobbered"})
		if !res.IsError || !strings.Contains(res.ForLLM, "runtime") {
			t.Errorf("write to runtime home %s must be refused, got IsError=%v msg=%q", dir, res.IsError, res.ForLLM)
		}
	}
	// Control: a user project beside the workspace stays writable.
	proj := filepath.Join(home, "myproject", "main.go")
	w := NewWriteFileTool(ws, false)
	res := w.Execute(ctx, map[string]interface{}{"path": proj, "content": "package main"})
	if res.IsError {
		t.Fatalf("a user project file must stay writable: %s", res.ForLLM)
	}
}

// Executed code never gets a writable view of Ghost's own tree: even when
// the command string names no primary file, the read-only mount stops the
// write, and scratch remains writable.
func TestExecSandboxKeepsGhostEstateReadOnly(t *testing.T) {
	if !IsolationActive() {
		t.Skip("OS isolation unavailable on this machine")
	}
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", "keep.txt"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewExecTool(ws, false)
	ctx := context.Background()

	// "README.md" is not a guarded name, so this command reaches the
	// sandbox; the read-only mount is what must refuse it.
	res := tool.Execute(ctx, map[string]interface{}{"command": "echo clobbered > README.md"})
	if !res.IsError {
		t.Errorf("write into Ghost's tree must fail under the read-only mount, got %q", res.ForLLM)
	}
	if data, _ := os.ReadFile(filepath.Join(ws, "README.md")); string(data) != "original" {
		t.Errorf("README.md was modified: %q", string(data))
	}

	// The skills estate is read-only to executed code too.
	res = tool.Execute(ctx, map[string]interface{}{"command": "python3 -c \"open('skills/keep.txt','w').write('x')\""})
	if !res.IsError {
		t.Errorf("skill write must fail, got %q", res.ForLLM)
	}
	if data, _ := os.ReadFile(filepath.Join(ws, "skills", "keep.txt")); string(data) != "original" {
		t.Errorf("skills/keep.txt was modified: %q", string(data))
	}

	// Control: scratch is writable, and it is HOME.
	res = tool.Execute(ctx, map[string]interface{}{"command": "echo ok > tmp/scratch.txt && cat tmp/scratch.txt && echo HOME=$HOME"})
	if res.IsError {
		t.Fatalf("scratch write must work: %s", res.ForLLM)
	}
	if data, err := os.ReadFile(filepath.Join(ws, "tmp", "scratch.txt")); err != nil || !strings.Contains(string(data), "ok") {
		t.Errorf("scratch file missing: err=%v data=%q", err, string(data))
	}
	if !strings.Contains(res.ForLLM, "HOME="+filepath.Join(ws, "tmp")) {
		t.Errorf("HOME must point at scratch, got %q", res.ForLLM)
	}
}

// A user project outside the workspace remains a writable working
// directory; Ghost's runtime home does not.
func TestExecWorkingDirBoundary(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "workspace")
	if err := os.MkdirAll(ws, 0755); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(home, "backups")
	if err := os.MkdirAll(backups, 0755); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "myproject")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	tool := NewExecTool(ws, false)
	ctx := context.Background()

	res := tool.Execute(ctx, map[string]interface{}{"command": "echo hi", "working_dir": backups})
	if !res.IsError || !strings.Contains(res.ForLLM, "runtime") {
		t.Errorf("Ghost runtime home must not be a working dir, got IsError=%v msg=%q", res.IsError, res.ForLLM)
	}

	res = tool.Execute(ctx, map[string]interface{}{"command": "pwd", "working_dir": proj})
	if res.IsError {
		t.Errorf("a user project directory must run: %s", res.ForLLM)
	}
}

// Ghost's governed stores are invisible to executed code: a user project
// can neither read the long-term memory or runtime state nor alter the real
// files, even when it writes to the shadowed path.
func TestExecSandboxHidesGovernedStores(t *testing.T) {
	if !IsolationActive() {
		t.Skip("OS isolation unavailable on this machine")
	}
	ws := t.TempDir()
	mem := filepath.Join(ws, "memory")
	if err := os.MkdirAll(mem, 0755); err != nil {
		t.Fatal(err)
	}
	memFile := filepath.Join(mem, "MEMORY.md")
	if err := os.WriteFile(memFile, []byte("PRIVATE-MEMORY"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(ws, "state")
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(state, "state.json")
	if err := os.WriteFile(stateFile, []byte("PRIVATE-STATE"), 0644); err != nil {
		t.Fatal(err)
	}
	kn := filepath.Join(ws, "knowledge")
	if err := os.MkdirAll(kn, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kn, "note.md"), []byte("owner-knowledge"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewExecTool(ws, false)
	ctx := context.Background()

	res := tool.Execute(ctx, map[string]interface{}{"command": "cat memory/MEMORY.md"})
	if strings.Contains(res.ForLLM, "PRIVATE-MEMORY") {
		t.Errorf("long-term memory must be hidden from executed code, got %q", res.ForLLM)
	}
	res = tool.Execute(ctx, map[string]interface{}{"command": "cat state/state.json"})
	if strings.Contains(res.ForLLM, "PRIVATE-STATE") {
		t.Errorf("runtime state must be hidden from executed code, got %q", res.ForLLM)
	}
	// Control: owner content stays readable.
	res = tool.Execute(ctx, map[string]interface{}{"command": "cat knowledge/note.md"})
	if res.IsError || !strings.Contains(res.ForLLM, "owner-knowledge") {
		t.Errorf("owner content must stay readable: %s", res.ForLLM)
	}
	// The real files never change, even if a write to a shadowed path runs.
	_ = tool.Execute(ctx, map[string]interface{}{"command": "echo tampered > memory/MEMORY.md"})
	if data, _ := os.ReadFile(memFile); string(data) != "PRIVATE-MEMORY" {
		t.Errorf("real memory file changed: %q", string(data))
	}
}
