package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// What the live Ghost did: from its default directory it ran
// "cd projects && git clone ..." and never named a working_dir. The natural
// path has to work, and land in the real folder beside the workspace.
func TestExecFromTheDefaultDirectoryCanUseProjects(t *testing.T) {
	if !bwrapAvailable() || isolationMode() == IsolationOff {
		t.Skip("needs the bubblewrap sandbox")
	}
	home, ws := projectsHome(t)
	res := NewExecTool(ws, true).Execute(context.Background(), map[string]interface{}{
		"command": "cd projects && mkdir demo && echo hi > demo/f",
	})
	if res.IsError {
		t.Fatalf("cd projects && mkdir failed: %s", res.ForLLM)
	}
	got, err := os.ReadFile(filepath.Join(home, "projects", "demo", "f"))
	if err != nil || strings.TrimSpace(string(got)) != "hi" {
		t.Fatalf("the write did not reach the real projects folder: %v %q", err, got)
	}
	res = NewExecTool(ws, true).Execute(context.Background(), map[string]interface{}{"command": "touch elsewhere"})
	if !res.IsError {
		t.Fatal("the rest of the workspace must stay read-only")
	}
}

func TestExecInsideOneProjectDoesNotSeeTheOthers(t *testing.T) {
	if !bwrapAvailable() || isolationMode() == IsolationOff {
		t.Skip("needs the bubblewrap sandbox")
	}
	home, ws := projectsHome(t)
	for _, n := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(home, "projects", n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	res := NewExecTool(ws, true).Execute(context.Background(), map[string]interface{}{
		"command": "ls " + filepath.Join(home, "projects"), "working_dir": "projects/a",
	})
	if res.IsError || strings.Contains(res.ForLLM, "b") || !strings.Contains(res.ForLLM, "a") {
		t.Fatalf("a command in project a must see a and not b: %+v", res)
	}
}

// The sandbox binds a working directory outside the workspace read-write, so
// where a command may run is the boundary that keeps Ghost's config out of reach.
func TestExecCannotRunInGhostsOwnConfigOrOtherSiblings(t *testing.T) {
	home, ws := projectsHome(t)
	cfg := filepath.Join(home, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "secret.key"), []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewExecTool(ws, true)
	for _, wd := range []string{cfg, home, filepath.Join(home, "state"), "/etc"} {
		res := tool.Execute(context.Background(), map[string]interface{}{"command": "cat secret.key; echo x > planted", "working_dir": wd})
		if !res.IsError || strings.Contains(res.ForLLM, "TOPSECRET") {
			t.Errorf("working_dir %q must be refused: %+v", wd, res)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg, "planted")); err == nil {
		t.Fatal("a command wrote into the config directory")
	}
}

func TestExecCannotLeaveAProjectThroughALink(t *testing.T) {
	home, ws := projectsHome(t)
	cfg := filepath.Join(home, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "projects", "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cfg, filepath.Join(home, "projects", "p", "out")); err != nil {
		t.Fatal(err)
	}
	res := NewExecTool(ws, true).Execute(context.Background(), map[string]interface{}{
		"command": "true", "working_dir": "projects/p/out",
	})
	if !res.IsError {
		t.Fatalf("a link out of a project must not be a way to run in the config dir: %+v", res)
	}
}

func TestExecStillRunsInTheWorkspaceAndProjects(t *testing.T) {
	_, ws := projectsHome(t)
	tool := NewExecTool(ws, true)
	for _, wd := range []string{"", ws, "tmp", "projects/ok"} {
		args := map[string]interface{}{"command": "true"}
		if wd != "" {
			args["working_dir"] = wd
		}
		if res := tool.Execute(context.Background(), args); res.IsError {
			t.Errorf("working_dir %q should run: %s", wd, res.ForLLM)
		}
	}
}

// The service runs as root. Folders it creates in the owner's workspace belong
// to the owner, not to root. Skipped unless run as root.
func TestProjectFoldersBelongToTheWorkspaceOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership only changes when run as root")
	}
	home, ws := projectsHome(t)
	const owner = 1234
	for _, p := range []string{home, ws} {
		if err := os.Chown(p, owner, owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureProjectDir(ws, filepath.Join(home, "projects", "scout")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(home, "projects"), filepath.Join(home, "projects", "scout")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != owner {
			t.Fatalf("%s is not owned by the workspace owner: %+v", p, info.Sys())
		}
	}
}
