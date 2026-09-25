package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/credentials"
)

func TestBrowserLoginNeedsASavedLogin(t *testing.T) {
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	tool := NewBrowserTool(t.TempDir(), "login")
	res := tool.executeLogin(context.Background(), map[string]interface{}{"host": "accounts.example.com"})
	if res == nil || !res.IsError {
		t.Fatal("a missing login must be an honest error")
	}
	if !strings.Contains(res.ForLLM, "Website logins") {
		t.Errorf("must point at the secure setup screen, got %q", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "password?") || strings.Contains(res.ForLLM, "What is your password") {
		t.Error("must never ask for a password in chat")
	}
}

// The credential lives in the vault and travels to the browser on stdin: it
// must never appear in argv (process list) or in anything returned to the
// model — even if the CLI echoes it.
func TestBrowserLoginPipesPasswordOnStdinNeverArgv(t *testing.T) {
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	if err := credentials.SaveWebLogin(credentials.WebLogin{
		URL: "https://accounts.example.com/login", Username: "ian@example.com", Password: "hunter2",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	orig := browserCLIRun
	defer func() { browserCLIRun = orig }()
	var calls [][]string
	var stdins []string
	browserCLIRun = func(ctx context.Context, env []string, stdin string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		stdins = append(stdins, stdin)
		// Deliberately echo the secret so we can prove it is scrubbed.
		return []byte("ok hunter2 ian@example.com"), nil
	}

	tool := NewBrowserTool(t.TempDir(), "login")
	res := tool.executeLogin(context.Background(), map[string]interface{}{"host": "accounts.example.com"})
	if res == nil || res.IsError {
		t.Fatalf("login should succeed: %+v", res)
	}
	// The password is the secret and must never be in argv. The username is a
	// required positional-ish flag of agent-browser's `auth save`; it stays
	// local (owner-only process list) and Ghost never returns or logs it.
	for _, args := range calls {
		for _, a := range args {
			if strings.Contains(a, "hunter2") {
				t.Fatalf("password leaked into argv: %v", args)
			}
		}
	}
	if len(stdins) == 0 || stdins[0] != "hunter2" {
		t.Fatalf("password must travel on stdin, got stdins=%q", stdins)
	}
	if len(stdins) == 0 || stdins[0] != "hunter2" {
		t.Fatalf("password must travel on stdin, got stdins=%q", stdins)
	}
	if strings.Contains(res.ForLLM, "hunter2") || strings.Contains(res.ForLLM, "ian@example.com") {
		t.Fatalf("result must be scrubbed, got %q", res.ForLLM)
	}
}
