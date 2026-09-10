package tools

import (
	"os"
	"os/exec"
	"strings"
)

// EnvGuard builds a sanitized environment for model-initiated subprocesses.
//
// Ghost's own process environment holds provider credentials and secrets. A
// shell command proposed by the model must never be able to read them: the
// model is not the security boundary, and "don't echo $API_KEY" is not
// enforcement. Subprocesses therefore start from a minimal allowlist and only
// inherit explicitly safe variables. This is the lightest practical isolation
// (no namespaces required) and it closes the most direct secret-exfiltration
// path on the Pi.
type EnvGuard struct {
	// Extra are additional KEY=VALUE pairs the caller explicitly permits
	// (e.g. a tool's own documented variables). They are still screened.
	Extra []string
}

// safeInherit are environment variable names that are safe and useful to pass
// through: locale, time, shell basics. Everything else is dropped unless it
// matches an explicit allow rule.
var safeInherit = map[string]bool{
	"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TZ": true, "TERM": true, "TMPDIR": true, "USER": true, "LOGNAME": true,
	"SHELL": true, "PWD": true,
}

// secretMarkers are substrings that mark a variable name as sensitive. A name
// containing any of these is never inherited, even if it would otherwise be
// safe.
var secretMarkers = []string{
	"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL",
	"AUTH", "API", "PRIVATE", "SESSION", "COOKIE", "BEARER",
}

// IsSecretEnvName reports whether an environment variable name looks like it
// carries secret material.
func IsSecretEnvName(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if upper == "" {
		return true
	}
	// Ghost's own namespace is always internal.
	if strings.HasPrefix(upper, "GHOST_") {
		return true
	}
	for _, m := range secretMarkers {
		if strings.Contains(upper, m) {
			return true
		}
	}
	return false
}

// Env returns the sanitized environment. HOME is forced to workspace when
// provided, so tools that resolve "~" write inside the confined area rather
// than the service account's home.
func (g EnvGuard) Env(workspace string) []string {
	out := []string{}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !safeInherit[strings.ToUpper(name)] || IsSecretEnvName(name) {
			continue
		}
		out = append(out, kv)
	}
	if workspace != "" {
		out = append(out, "HOME="+workspace)
	}
	for _, kv := range g.Extra {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || IsSecretEnvName(name) {
			continue // never let a caller re-introduce a secret-shaped var
		}
		out = append(out, kv)
	}
	if len(out) == 0 {
		out = append(out, "PATH=/usr/local/bin:/usr/bin:/bin")
	}
	return out
}

// HardenCommand applies the sanitized environment and a working directory to a
// subprocess about to run. It is the single place exec/sandbox tools route
// through, so a new risky tool cannot accidentally inherit secrets.
func HardenCommand(cmd *exec.Cmd, workspace string, extra []string) {
	if cmd == nil {
		return
	}
	cmd.Env = EnvGuard{Extra: extra}.Env(workspace)
	if workspace != "" && cmd.Dir == "" {
		cmd.Dir = workspace
	}
}
