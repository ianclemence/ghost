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
	sawSecret := false
	for i, in := range stdins {
		if in == "hunter2" {
			sawSecret = true
			if calls[i][0] != "auth" || calls[i][1] != "save" {
				t.Fatalf("the password may only go to auth save, went to %v", calls[i])
			}
		} else if in != "" {
			t.Fatalf("unexpected stdin %q", in)
		}
	}
	if !sawSecret {
		t.Fatalf("password must travel on stdin, got stdins=%q", stdins)
	}
	if strings.Contains(res.ForLLM, "hunter2") || strings.Contains(res.ForLLM, "ian@example.com") {
		t.Fatalf("result must be scrubbed, got %q", res.ForLLM)
	}
}

func loginFixture(t *testing.T) {
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	if err := credentials.SaveWebLogin(credentials.WebLogin{
		URL: "https://shop.example.com", Username: "ian@example.com", Password: "hunter2",
	}); err != nil {
		t.Fatal(err)
	}
}

// A saved front-page address still finds the sign-in form through the page's
// own "Log in" link, and signs in there.
func TestBrowserLoginFindsTheLoginPageFromTheFrontPage(t *testing.T) {
	loginFixture(t)
	orig := browserCLIRun
	defer func() { browserCLIRun = orig }()
	clicked := false
	var saveURL string
	browserCLIRun = func(ctx context.Context, env []string, stdin string, args ...string) ([]byte, error) {
		switch args[0] {
		case "snapshot":
			if clicked {
				return []byte(`- textbox "Email" [ref=e4]\n- textbox "Password" [ref=e5]\n- button "Log in" [ref=e6]`), nil
			}
			return []byte(`- link "Log in" [ref=e7]\n- link "Register" [ref=e8]`), nil
		case "click":
			clicked = true
		case "get":
			return []byte("https://shop.example.com/login\n"), nil
		case "auth":
			if args[1] == "save" {
				for i, a := range args {
					if a == "--url" {
						saveURL = args[i+1]
					}
				}
			}
		}
		return []byte("ok"), nil
	}
	res := NewBrowserTool(t.TempDir(), "login").executeLogin(context.Background(), map[string]interface{}{"host": "shop.example.com"})
	if res.IsError || !clicked || saveURL != "https://shop.example.com/login" {
		t.Fatalf("must follow the Log in link and use that page: err=%v clicked=%v url=%q %s", res.IsError, clicked, saveURL, res.ForLLM)
	}
}

// A page that asks for a human check is reported plainly and nothing is typed.
func TestBrowserLoginStopsAtAHumanCheck(t *testing.T) {
	loginFixture(t)
	orig := browserCLIRun
	defer func() { browserCLIRun = orig }()
	authCalls := 0
	browserCLIRun = func(ctx context.Context, env []string, stdin string, args ...string) ([]byte, error) {
		if args[0] == "auth" {
			authCalls++
		}
		if args[0] == "snapshot" {
			return []byte(`- textbox "Password" [ref=e5]\n- Iframe "Widget containing a Cloudflare security challenge" [ref=e10]\n- checkbox "Verify you are human" [ref=e14]`), nil
		}
		return []byte("ok"), nil
	}
	res := NewBrowserTool(t.TempDir(), "login").executeLogin(context.Background(), map[string]interface{}{"host": "shop.example.com"})
	if !res.IsError || !strings.Contains(res.ForLLM, "human check") {
		t.Fatalf("must say a human check blocks it: %+v", res)
	}
	if authCalls != 0 {
		t.Fatal("no credential may be handed to a form Ghost cannot finish")
	}
}
