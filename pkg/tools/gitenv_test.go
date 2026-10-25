package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitEnvironmentNeverPromptsAndSurvivesTheSecretFilter(t *testing.T) {
	joined := strings.Join(gitSandboxEnv(), "\n")
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "safe.directory=*"} {
		if !strings.Contains(joined, want) {
			t.Errorf("git environment must set %q", want)
		}
	}
	// The environment filter drops anything named like a secret; the git
	// settings must come through it.
	got := strings.Join(EnvGuard{Extra: gitSandboxEnv()}.Env(t.TempDir()), "\n")
	for _, want := range []string{"GIT_CONFIG_PARAMETERS=", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was filtered out of the command environment", want)
		}
	}
}

func TestGitIdentityIsCreatedOnceAndNeverReplaced(t *testing.T) {
	ws := t.TempDir()
	ensureGitIdentity(ws)
	path := filepath.Join(ws, "tmp", ".gitconfig")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "name = Ghost") {
		t.Fatalf("no starting identity written: %v %q", err, b)
	}
	if err := os.WriteFile(path, []byte("[user]\n\tname = Someone Else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ensureGitIdentity(ws)
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "Someone Else") {
		t.Fatalf("the owner's own git configuration was overwritten: %q", b)
	}
}

// What the live Ghost would have met on its first commit: a repo that belongs to
// someone else (the service is root, the folders are the owner's), no identity,
// and no credentials. Needs git, the sandbox and root, so it only runs there.
func TestGitWorksInTheSandboxForARepoSomeoneElseOwns(t *testing.T) {
	if os.Geteuid() != 0 || !bwrapAvailable() || isolationMode() == IsolationOff {
		t.Skip("needs root and the bubblewrap sandbox")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home, ws := projectsHome(t)
	repo := filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=x", "-c", "user.email=x@x", "commit", "-q", "--allow-empty", "-m", "first"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := exec.Command("chown", "-R", "1234:1234", repo).Run(); err != nil {
		t.Fatal(err)
	}
	// Run git the way the sandbox does: workspace is home's child.
	tool := NewExecTool(ws, true)
	run := func(cmd string) *ToolResult {
		return tool.Execute(context.Background(), map[string]interface{}{"command": cmd, "working_dir": "projects/demo", "timeout_seconds": float64(30)})
	}
	if res := run("git status --short"); res.IsError {
		t.Fatalf("git refused a repo the owner owns: %s", res.ForLLM)
	}
	res := run("echo more >> notes.txt && git add -A && git commit -q -m second && git log -1 --format='%an <%ae>'")
	if res.IsError || !strings.Contains(res.ForLLM, "Ghost <ghost@ghost.local>") {
		t.Fatalf("a commit needs an identity Ghost supplies: %+v", res)
	}
	res = run("git remote add origin https://github.com/example/none.git && git push origin HEAD 2>&1; true")
	if !strings.Contains(res.ForLLM, "terminal prompts disabled") && !strings.Contains(res.ForLLM, "Could not resolve") && !strings.Contains(res.ForLLM, "could not read") {
		t.Fatalf("a push with no credentials must fail at once, not wait: %+v", res)
	}
}
