package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, tool interface {
	Execute(context.Context, map[string]interface{}) *ToolResult
}, args map[string]interface{}) *ToolResult {
	t.Helper()
	return tool.Execute(context.Background(), args)
}

func TestDeleteFileRemovesWhatWasAskedAndOnlyInsideTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep.txt")
	os.WriteFile(outside, []byte("mine"), 0o644)
	os.MkdirAll(filepath.Join(ws, "notes"), 0o755)
	os.WriteFile(filepath.Join(ws, "notes", "old.txt"), []byte("x"), 0o644)
	del := NewDeleteFileTool(ws, true)

	if r := run(t, del, map[string]interface{}{"path": "notes/old.txt"}); r.IsError {
		t.Fatalf("a workspace file is deleted: %s", r.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(ws, "notes", "old.txt")); !os.IsNotExist(err) {
		t.Fatal("the file must really be gone")
	}
	if err := del.Verify(context.Background(), map[string]interface{}{"path": "notes/old.txt"}); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if r := run(t, del, map[string]interface{}{"path": outside}); !r.IsError {
		t.Fatal("a file outside the workspace must never be deleted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("the outside file must be untouched")
	}
	if r := run(t, del, map[string]interface{}{"path": "../keep.txt"}); !r.IsError {
		t.Fatal("dot-dot must not escape")
	}
	if r := run(t, del, map[string]interface{}{"path": "."}); !r.IsError {
		t.Fatal("the workspace itself is not deletable")
	}
	if r := run(t, del, map[string]interface{}{"path": "nothing-here.txt"}); !r.IsError || !strings.Contains(r.ForLLM, "does not exist") {
		t.Fatalf("a missing file is reported honestly: %+v", r)
	}
}

func TestDeleteFileNeverTouchesGhostsOwnFiles(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"GHOST.md", "IDENTITY.md", "ghost.db", "USER.md"} {
		os.WriteFile(filepath.Join(ws, name), []byte("x"), 0o644)
	}
	os.MkdirAll(filepath.Join(ws, "personal-context"), 0o755)
	os.WriteFile(filepath.Join(ws, "personal-context", "entries.jsonl"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(ws, "stuff", "skills"), 0o755)
	os.WriteFile(filepath.Join(ws, "stuff", "skills", "a.md"), []byte("x"), 0o644)
	del := NewDeleteFileTool(ws, true)
	for _, p := range []string{"GHOST.md", "IDENTITY.md", "ghost.db", "USER.md", "personal-context/entries.jsonl", "personal-context"} {
		if r := run(t, del, map[string]interface{}{"path": p, "recursive": true}); !r.IsError {
			t.Errorf("%s is Ghost's own and must be refused", p)
		}
		if _, err := os.Stat(filepath.Join(ws, p)); err != nil {
			t.Errorf("%s must still exist", p)
		}
	}
	// A folder cannot be used to reach a protected file inside it.
	if r := run(t, del, map[string]interface{}{"path": "stuff", "recursive": true}); !r.IsError {
		t.Error("a folder holding a protected file must be refused whole")
	}
	if _, err := os.Stat(filepath.Join(ws, "stuff", "skills", "a.md")); err != nil {
		t.Error("nothing inside may be deleted when the folder is refused")
	}
}

func TestDeleteFolderNeedsRecursiveWhenItHasThings(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "trip"), 0o755)
	os.WriteFile(filepath.Join(ws, "trip", "a.txt"), []byte("x"), 0o644)
	del := NewDeleteFileTool(ws, true)
	if r := run(t, del, map[string]interface{}{"path": "trip"}); !r.IsError {
		t.Fatal("a full folder needs an explicit recursive")
	}
	if r := run(t, del, map[string]interface{}{"path": "trip", "recursive": true}); r.IsError {
		t.Fatalf("recursive deletes it: %s", r.ForLLM)
	}
}

func TestASymlinkInsideTheWorkspaceIsNotAWayOut(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o644)
	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := validatePath("link/secret.txt", ws, true); err == nil {
		t.Fatal("a link that leads out of the workspace must be refused")
	}
	if r := run(t, NewDeleteFileTool(ws, true), map[string]interface{}{"path": "link/secret.txt"}); !r.IsError {
		t.Fatal("delete must not follow a link out")
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatal("the outside file must survive")
	}
	if r := run(t, NewReadFileTool(ws, true), map[string]interface{}{"path": "link/secret.txt"}); !r.IsError {
		t.Fatal("read must not follow a link out either")
	}
	// A normal new file in a normal folder still works.
	if _, err := validatePath("notes/new.txt", ws, true); err != nil {
		t.Fatalf("a path that does not exist yet is fine: %v", err)
	}
}

func TestMoveFileOrganizesAndNeverOverwrites(t *testing.T) {
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(ws, "b.txt"), []byte("b"), 0o644)
	mv := NewMoveFileTool(ws, true)
	if r := run(t, mv, map[string]interface{}{"from": "a.txt", "to": "notes/2026/a.txt"}); r.IsError {
		t.Fatalf("move into a new folder: %s", r.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(ws, "notes", "2026", "a.txt")); err != nil {
		t.Fatal("the file must be at its new place")
	}
	if r := run(t, mv, map[string]interface{}{"from": "b.txt", "to": "notes/2026/a.txt"}); !r.IsError {
		t.Fatal("moving onto an existing file would overwrite it and must stop")
	}
	if r := run(t, mv, map[string]interface{}{"from": "b.txt", "to": "../elsewhere.txt"}); !r.IsError {
		t.Fatal("moving out of the workspace must stop")
	}
	if r := run(t, mv, map[string]interface{}{"from": "b.txt", "to": "GHOST.md"}); !r.IsError {
		t.Fatal("moving onto Ghost's own file must stop")
	}
}

func TestScreenshotsAreKeptInsideTheWorkspace(t *testing.T) {
	old := browserShotDir
	defer func() { browserShotDir = old }()
	ws := t.TempDir()
	SetBrowserShotDir(filepath.Join(ws, "screenshots"))
	if got := defaultBrowserShotDir(); !under(ws, got) {
		t.Fatalf("screenshots must land in the workspace, got %s", got)
	}
}

// Ghost's default files and folders are never deleted or moved, however they
// are asked for, by the tool or through the shell.
func TestDefaultWorkspaceIsNeverDeletedOrMoved(t *testing.T) {
	ws := t.TempDir()
	dirs := []string{"memory", "skills", "knowledge", "uploads", "screenshots", "downloads", "notes", "data", "sessions", "state"}
	files := []string{"GHOST.md", "USER.md", "README.md", "IDENTITY.md", "SOUL.md", "AGENTS.md", "HEARTBEAT.md", "ghost.db"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(ws, d), 0o755)
	}
	for _, f := range files {
		os.WriteFile(filepath.Join(ws, f), []byte("x"), 0o644)
	}
	del := NewDeleteFileTool(ws, false) // even with restriction switched off
	mv := NewMoveFileTool(ws, false)
	for _, name := range append(append([]string{}, dirs...), files...) {
		for _, recursive := range []bool{false, true} {
			if r := run(t, del, map[string]interface{}{"path": name, "recursive": recursive}); !r.IsError {
				t.Errorf("deleting %s (recursive=%v) must be refused", name, recursive)
			}
		}
		if r := run(t, mv, map[string]interface{}{"from": name, "to": "moved-" + name}); !r.IsError {
			t.Errorf("moving %s must be refused", name)
		}
		if _, err := os.Stat(filepath.Join(ws, name)); err != nil {
			t.Errorf("%s must still exist", name)
		}
	}
	// What is inside the open folders is Ghost's to tidy.
	os.WriteFile(filepath.Join(ws, "notes", "old.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(ws, "downloads", "a.pdf"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(ws, "screenshots", "s.png"), []byte("x"), 0o644)
	for _, p := range []string{"notes/old.md", "downloads/a.pdf", "screenshots/s.png"} {
		if r := run(t, del, map[string]interface{}{"path": p}); r.IsError {
			t.Errorf("%s is inside an open folder and may be deleted: %s", p, r.ForLLM)
		}
	}
	// The shell is not a way around it.
	for _, cmd := range []string{
		"rm -rf memory", "rm -rf ./uploads", "mv USER.md /tmp/x", "rm README.md",
		"rm -rf /var/lib/ghost/workspace", "rm -rf screenshots downloads", "find . -delete",
		"sh -c 'rm -rf skills'", "truncate -s0 memory/MEMORY.md",
	} {
		if msg, denied := execDeniedPrimaryFiles(cmd); !denied {
			t.Errorf("the shell must refuse %q (got %q)", cmd, msg)
		}
	}
}

// A turn must actually be offered the file tools, or Ghost tells the owner it
// "has no delete tool" while one is registered.
func TestFileManagementToolsAreAlwaysOffered(t *testing.T) {
	for _, name := range []string{"delete_file", "move_file", "write_file"} {
		if !coreToolNames[name] {
			t.Errorf("%s must be part of the always-available core tools", name)
		}
	}
}
