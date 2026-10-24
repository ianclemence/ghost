package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// home returns a Ghost home with a workspace inside it, like /var/ghost.
func projectsHome(t *testing.T) (home, workspace string) {
	t.Helper()
	home = t.TempDir()
	workspace = filepath.Join(home, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, workspace
}

func TestEnsureProjectDirCreatesOnlyTheProjectFolder(t *testing.T) {
	home, ws := projectsHome(t)
	if err := ensureProjectDir(ws, filepath.Join(home, "projects", "scout")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(home, "projects", "scout")); err != nil || !fi.IsDir() {
		t.Fatalf("project folder not created: %v", err)
	}
	// Idempotent, and a path below the project creates the project but not the tree.
	if err := ensureProjectDir(ws, filepath.Join(home, "projects", "scout", "cmd", "deep")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "scout", "cmd")); !os.IsNotExist(err) {
		t.Fatal("the runtime must not build a directory tree for the model")
	}
}

func TestEnsureProjectDirLeavesEverythingElseAlone(t *testing.T) {
	home, ws := projectsHome(t)
	for _, cwd := range []string{
		ws, filepath.Join(ws, "tmp"), filepath.Join(home, "backups"),
		filepath.Join(home, "projects"),           // the root itself is not a project
		filepath.Join(home, "projects-evil", "x"), // a sibling that only shares the prefix
		"",
	} {
		if err := ensureProjectDir(ws, cwd); err != nil {
			t.Errorf("%q: %v", cwd, err)
		}
	}
	for _, name := range []string{"backups", "projects", "projects-evil"} {
		if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Errorf("%s must not have been created", name)
		}
	}
	if err := ensureProjectDir("", "/anywhere"); err != nil {
		t.Fatal("no workspace means nothing to manage")
	}
}

func TestEnsureProjectDirRefusesBadNamesAndLinks(t *testing.T) {
	home, ws := projectsHome(t)
	root := filepath.Join(home, "projects")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".hidden", "-lead", "has space", strings.Repeat("a", 65), "semi;colon"} {
		if err := ensureProjectDir(ws, filepath.Join(root, name)); err == nil {
			t.Errorf("%q must be refused", name)
		}
	}
	// A link planted as a project would be bound read-write into the sandbox.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "sneaky")); err != nil {
		t.Fatal(err)
	}
	if err := ensureProjectDir(ws, filepath.Join(root, "sneaky")); err == nil {
		t.Fatal("a symlinked project must be refused")
	}
	// So would a link in place of the projects folder itself.
	home2, ws2 := projectsHome(t)
	if err := os.Symlink(outside, filepath.Join(home2, "projects")); err != nil {
		t.Fatal(err)
	}
	if err := ensureProjectDir(ws2, filepath.Join(home2, "projects", "ok")); err == nil {
		t.Fatal("a symlinked projects folder must be refused")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("nothing may be created through a link: %v", entries)
	}
}

func TestResolveWorkingDirReadsRelativePathsTheWayTheFileToolsDo(t *testing.T) {
	home, ws := projectsHome(t)
	cases := map[string]string{
		"projects/scout":       filepath.Join(home, "projects", "scout"),
		"./projects/scout/cmd": filepath.Join(home, "projects", "scout", "cmd"),
		"tmp":                  filepath.Join(ws, "tmp"),
		"knowledge/notes":      filepath.Join(ws, "knowledge", "notes"),
		"/var/elsewhere":       "/var/elsewhere",
		"":                     "",
		"projects-evil/x":      filepath.Join(ws, "projects-evil", "x"),
	}
	for in, want := range cases {
		if got := resolveWorkingDir(ws, in); got != want {
			t.Errorf("resolveWorkingDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// The failure that started this: a command asked to work in a project that
// does not exist yet must get a writable folder, and the workspace stays read-only.
func TestExecInANewProjectCanWriteThere(t *testing.T) {
	home, ws := projectsHome(t)
	tool := NewExecTool(ws, true)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"command":     "echo built > artifact.txt && pwd",
		"working_dir": "projects/scout",
	})
	if res.IsError {
		t.Fatalf("exec in a new project failed: %s", res.ForLLM)
	}
	got, err := os.ReadFile(filepath.Join(home, "projects", "scout", "artifact.txt"))
	if err != nil || strings.TrimSpace(string(got)) != "built" {
		t.Fatalf("the command's write did not land in the project: %v %q", err, got)
	}
	if !bwrapAvailable() || isolationMode() == IsolationOff {
		t.Log("bubblewrap is not active here: the write check ran unsandboxed")
		return
	}
	{
		res = tool.Execute(context.Background(), map[string]interface{}{"command": "touch not-allowed", "working_dir": ws})
		if !res.IsError {
			t.Fatal("the workspace itself must stay read-only to a command")
		}
	}
}

func TestExecRefusesAnInvalidProjectName(t *testing.T) {
	home, ws := projectsHome(t)
	res := NewExecTool(ws, true).Execute(context.Background(), map[string]interface{}{
		"command": "true", "working_dir": "projects/.git",
	})
	if !res.IsError || !strings.Contains(res.ForLLM, "valid project name") {
		t.Fatalf("a bad name must be refused plainly: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "projects")); !os.IsNotExist(err) {
		t.Fatal("a refused name must not create anything")
	}
}
