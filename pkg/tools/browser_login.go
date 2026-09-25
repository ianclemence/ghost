package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// browser_login signs in to a site using a login the owner saved in Ghost
// settings (Apps → Website logins).
//
// Secret discipline: the credential is read from the sealed vault inside this
// tool. The PASSWORD goes to the browser CLI on STDIN (never argv, never the
// model, never a log line) — that is the secret. agent-browser's `auth save`
// requires the username as a flag, so the username does appear in this
// machine's process list (owner-only, same uid) but is never returned to the
// model, never logged by Ghost, and scrubbed from every byte we return,
// together with the password. A missing login produces the setup message — this tool never asks
// for a password in chat.
func (t *BrowserTool) executeLogin(ctx context.Context, args map[string]interface{}) *ToolResult {
	host := strings.TrimSpace(sarg(args, "host"))
	if host == "" {
		host = credentials.NormalizeWebHost(sarg(args, "url"))
	}
	login, ok := credentials.WebLoginFor(host)
	if !ok {
		who := host
		if who == "" {
			who = "that site"
		}
		return ErrorResult(fmt.Sprintf(
			"No saved login for %s. Ask the owner to add it in Ghost settings under Apps → Website logins, then try again — never ask them for the password here.", who))
	}

	pageURL := strings.TrimSpace(sarg(args, "url"))
	if pageURL == "" {
		pageURL = login.URL
	}
	name := loginProfileName(login.Host)

	saveArgs := []string{"auth", "save", name, "--url", pageURL, "--username", login.Username, "--password-stdin"}
	saveArgs = appendSelector(saveArgs, "--username-selector", sarg(args, "username_selector"), login.UsernameSelector)
	saveArgs = appendSelector(saveArgs, "--password-selector", sarg(args, "password_selector"), login.PasswordSelector)
	saveArgs = appendSelector(saveArgs, "--submit-selector", sarg(args, "submit_selector"), login.SubmitSelector)

	// Bounded: a wedged browser must fail honestly, not eat the turn.
	saveCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if out, err := browserCLIRun(saveCtx, browserEnvironment(t.sessionProfile), login.Password, saveArgs...); err != nil {
		return ErrorResult("Couldn't save the login for " + login.Host + ": " + scrubSecret(string(out), login))
	}

	loginCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	if out, err := browserCLIRun(loginCtx, browserEnvironment(t.sessionProfile), "", "auth", "login", name, "--json"); err != nil {
		return ErrorResult("The sign-in for " + login.Host + " didn't complete: " + scrubSecret(string(out), login) +
			" The saved login is still there; the page may need a selector override in Ghost settings.")
	}
	return NewToolResult("Signed in to " + login.Host + ".")
}

func appendSelector(args []string, flag, arg, fallback string) []string {
	v := strings.TrimSpace(arg)
	if v == "" {
		v = strings.TrimSpace(fallback)
	}
	if v == "" {
		return args
	}
	return append(args, flag, v)
}

// loginProfileName keeps the CLI profile name filesystem- and argv-safe.
func loginProfileName(host string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "site"
	}
	return b.String()
}

// scrubSecret removes the credential from any text before it can reach the
// model or the transcript.
func scrubSecret(s string, l credentials.WebLogin) string {
	if l.Password != "" {
		s = strings.ReplaceAll(s, l.Password, "«redacted»")
	}
	if l.Username != "" {
		s = strings.ReplaceAll(s, l.Username, "«redacted»")
	}
	return strings.TrimSpace(s)
}

// browserCLIRun executes agent-browser with optional stdin. A package variable
// so tests can prove the password travels on stdin and never in argv.
var browserCLIRun = func(ctx context.Context, env []string, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "agent-browser", args...)
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}
