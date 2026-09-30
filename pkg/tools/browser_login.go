package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
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
			"No saved login for %s. Ask the owner to add it under Apps, Website logins in the web console, or Connected apps in the app, then try again — never ask them for the password here.", who))
	}

	pageURL := strings.TrimSpace(sarg(args, "url"))
	if pageURL == "" {
		pageURL = login.URL
	}
	name := loginProfileName(login.Host)

	// The saved address is often a site's front page. Find the sign-in form
	// first, and stop early and honestly if the page asks for a human check.
	if found, blocked := t.findLoginPage(ctx, pageURL); blocked != "" {
		return ErrorResult(blocked)
	} else if found != "" {
		pageURL = found
	}

	saveArgs := []string{"auth", "save", name, "--url", pageURL, "--username", login.Username, "--password-stdin"}
	saveArgs = appendSelector(saveArgs, "--username-selector", sarg(args, "username_selector"), login.UsernameSelector)
	saveArgs = appendSelector(saveArgs, "--password-selector", sarg(args, "password_selector"), login.PasswordSelector)
	saveArgs = appendSelector(saveArgs, "--submit-selector", sarg(args, "submit_selector"), login.SubmitSelector)

	// Bounded: a wedged browser must fail honestly, not eat the turn.
	saveCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if out, err := browserCLIRun(saveCtx, browserEnvironment(t.sessionProfile), login.Password, saveArgs...); err != nil {
		// This is a fault on Ghost's side, not a missing login: say so, so it is
		// never reported to the owner as "no saved login".
		return ErrorResult("The login for " + login.Host + " IS saved, but the browser could not use it: " + scrubSecret(string(out), login) + " Do not tell the owner the login is missing.")
	}

	loginCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	if out, err := browserCLIRun(loginCtx, browserEnvironment(t.sessionProfile), "", "auth", "login", name, "--json"); err != nil {
		return ErrorResult("The sign-in for " + login.Host + " didn't complete: " + scrubSecret(string(out), login) +
			" The saved login is still there; the page may need a different sign-in form to be picked.")
	}
	return NewToolResult("Signed in to " + login.Host + ".")
}

var (
	passwordFieldRe = regexp.MustCompile(`(?i)textbox "[^"]*pass(word)?[^"]*"`)
	humanCheckRe    = regexp.MustCompile(`(?i)(security challenge|verify you are human|captcha|are you a robot|i'm not a robot)`)
	loginLinkRe     = regexp.MustCompile(`(?i)link "(log ?in|sign ?in)"[^\n]*ref=(e[0-9]+)`)
)

// findLoginPage opens the saved address and returns the address that holds the
// sign-in form: the page itself when it has a password field, otherwise the
// page behind its "Log in" link. If the sign-in page asks for a human check
// (a CAPTCHA or Cloudflare's "Verify you are human"), it returns an owner-facing
// message instead: Ghost does not fill a form it cannot finish, and says who
// has to do the next step. Best effort: on any browser error it returns
// nothing and the normal sign-in path runs.
func (t *BrowserTool) findLoginPage(ctx context.Context, pageURL string) (found, blocked string) {
	env := browserEnvironment(t.sessionProfile)
	c, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if _, err := browserCLIRun(c, env, "", "open", pageURL); err != nil {
		return "", ""
	}
	snap := func() string {
		out, err := browserCLIRun(c, env, "", "snapshot", "-i")
		if err != nil {
			return ""
		}
		return string(out)
	}
	s := snap()
	if !passwordFieldRe.MatchString(s) {
		if m := loginLinkRe.FindStringSubmatch(s); m != nil {
			if _, err := browserCLIRun(c, env, "", "click", "@"+m[2]); err == nil {
				time.Sleep(2 * time.Second)
				s = snap()
			}
		}
	}
	if !passwordFieldRe.MatchString(s) {
		return "", ""
	}
	if humanCheckRe.MatchString(s) {
		return "", "That sign-in page asks for a human check (a CAPTCHA or Cloudflare's \"Verify you are human\"). " +
			"I can't complete that step, so I haven't typed anything into the form. Open that sign-in page with browser_navigate so it shows as a live browser card in the app, " +
			"then tell the owner plainly what to do: in the Ghost app, tap \"Take over and steer\" on the browser card, tick the box and sign in themselves, then tap Done. " +
			"You carry on when they do. If it is their own site they can instead allow Ghost past the check."
	}
	if out, err := browserCLIRun(c, env, "", "get", "url"); err == nil {
		if u := strings.TrimSpace(string(out)); strings.HasPrefix(u, "http") {
			return u, ""
		}
	}
	return "", ""
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

// loginProfileName keeps the CLI profile name filesystem- and argv-safe. The
// browser CLI accepts only letters, digits, hyphen and underscore, so the dots
// in a host become hyphens ("nairobiunwind.com" -> "nairobiunwind-com"); with a
// dot in the name, saving the profile failed and no site login could work.
func loginProfileName(host string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		case r == '.':
			b.WriteRune('-')
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
