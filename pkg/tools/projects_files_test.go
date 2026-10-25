package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectWithSkillsPackage(t *testing.T) (home, ws string) {
	t.Helper()
	home, ws = projectsHome(t)
	dir := filepath.Join(home, "projects", "scout", "pkg", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills.go"), []byte("package skills\n\nconst Name = \"one\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, ws
}

// The live Ghost called read_file("projects/scout/pkg/mcpclient/client.go") and
// was told the file did not exist, because projects/ lives beside the workspace.
func TestFileToolsReachProjectFiles(t *testing.T) {
	_, ws := projectWithSkillsPackage(t)
	ctx := context.Background()

	res := NewReadFileTool(ws, true).Execute(ctx, map[string]interface{}{"path": "projects/scout/pkg/skills/skills.go"})
	if res.IsError || !strings.Contains(res.ForLLM, "package skills") {
		t.Fatalf("read_file cannot see a project file: %+v", res)
	}
	res = NewListDirTool(ws, true).Execute(ctx, map[string]interface{}{"path": "projects/scout/pkg"})
	if res.IsError || !strings.Contains(res.ForLLM, "skills") {
		t.Fatalf("list_dir cannot see a project folder: %+v", res)
	}
}

// scout has a package called skills. The estate guard judged by folder name and
// refused edits to it as "the skills estate".
func TestProjectFoldersNamedLikeGhostsAreTheProjectsOwn(t *testing.T) {
	home, ws := projectWithSkillsPackage(t)
	ctx := context.Background()
	target := filepath.Join(home, "projects", "scout", "pkg", "skills", "skills.go")

	res := NewEditFileTool(ws, true).Execute(ctx, map[string]interface{}{
		"path": "projects/scout/pkg/skills/skills.go", "old_text": `"one"`, "new_text": `"two"`,
	})
	if res.IsError {
		t.Fatalf("edit_file refused a project's own skills package: %s", res.ForLLM)
	}
	if b, _ := os.ReadFile(target); !strings.Contains(string(b), `"two"`) {
		t.Fatalf("the edit did not land in the project: %q", b)
	}
	res = NewWriteFileTool(ws, true).Execute(ctx, map[string]interface{}{
		"path": "projects/scout/pkg/state/state_test.go", "content": "package state\n",
	})
	if res.IsError {
		t.Fatalf("write_file refused a project's own state package: %s", res.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "scout", "pkg", "state", "state_test.go")); err != nil {
		t.Fatalf("the new file is not in the project: %v", err)
	}
}

// None of that loosens Ghost's own estate.
func TestProjectAccessDoesNotLoosenGhostsEstate(t *testing.T) {
	home, ws := projectWithSkillsPackage(t)
	ctx := context.Background()
	for _, p := range []string{"skills/x/SKILL.md", "GHOST.md", "memory/MEMORY.md", "state/x.json", "sessions/x"} {
		res := NewWriteFileTool(ws, true).Execute(ctx, map[string]interface{}{"path": p, "content": "x"})
		if !res.IsError {
			t.Errorf("write_file must still refuse %s", p)
		}
	}
	// A link inside a project that leads into the estate is not a way in.
	if err := os.MkdirAll(filepath.Join(ws, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "skills"), filepath.Join(home, "projects", "scout", "sneak")); err != nil {
		t.Fatal(err)
	}
	res := NewWriteFileTool(ws, true).Execute(ctx, map[string]interface{}{"path": "projects/scout/sneak/evil.md", "content": "x"})
	if !res.IsError {
		t.Fatal("a link out of a project reached the skills estate")
	}
	if _, err := os.Stat(filepath.Join(ws, "skills", "evil.md")); err == nil {
		t.Fatal("a file was written through the link")
	}
}

// "Remove that project" has to be possible through the gated tool, and only for
// a project: never the projects folder itself, never through a link.
func TestDeleteFileCanRemoveAProject(t *testing.T) {
	home, ws := projectWithSkillsPackage(t)
	ctx := context.Background()
	del := NewDeleteFileTool(ws, true)

	if res := del.Execute(ctx, map[string]interface{}{"path": "projects", "recursive": true}); !res.IsError {
		t.Fatal("the whole projects folder must not be deletable")
	}
	if res := del.Execute(ctx, map[string]interface{}{"path": "projects/scout"}); !res.IsError {
		t.Fatal("a non-empty project needs recursive, as any folder does")
	}
	res := del.Execute(ctx, map[string]interface{}{"path": "projects/scout", "recursive": true})
	if res.IsError {
		t.Fatalf("a project could not be removed: %s", res.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "scout")); !os.IsNotExist(err) {
		t.Fatal("the project folder is still there")
	}
	if _, err := os.Stat(filepath.Join(home, "projects")); err != nil {
		t.Fatal("deleting a project removed the projects folder")
	}
	if err := del.Verify(ctx, map[string]interface{}{"path": "projects/scout"}); err != nil {
		t.Fatalf("the deletion should verify: %v", err)
	}
}

func TestDeleteFileCannotUseAProjectLinkToReachTheEstate(t *testing.T) {
	home, ws := projectWithSkillsPackage(t)
	if err := os.MkdirAll(filepath.Join(ws, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "notes"), filepath.Join(home, "projects", "scout", "out")); err != nil {
		t.Fatal(err)
	}
	res := NewDeleteFileTool(ws, true).Execute(context.Background(), map[string]interface{}{"path": "projects/scout/out", "recursive": true})
	if !res.IsError {
		t.Fatalf("a link inside a project reached the workspace: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(ws, "notes")); err != nil {
		t.Fatal("the workspace folder was removed through the link")
	}
}

func TestDeleteDescriptionWarnsAboutUnpushedWork(t *testing.T) {
	d := NewDeleteFileTool(t.TempDir(), true).Description()
	for _, want := range []string{"projects/<name>", "git status", "only here"} {
		if !strings.Contains(d, want) {
			t.Errorf("delete_file description must mention %q", want)
		}
	}
}
