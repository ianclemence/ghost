package browser

import (
	"os"
	"path/filepath"
	"strings"
)

// Browser child processes (the agent-browser CLI and the Chrome it launches)
// need somewhere writable to put their sockets, daemon files, and profile.
// Ghost's service runs with the home directory sealed off (ProtectHome) and a
// read-only filesystem, so the CLI's default location failed with "Failed to
// create socket directory: Read-only file system" and every browser action
// died in milliseconds. The resolution lives here, in one place, so the tool
// path and the screencast broker agree on the same HOME and runtime directory:
// if they disagree, the broker looks in the wrong socket directory and reports
// that streaming is unavailable while a perfectly good stream server is
// listening.

// StateRoot is where the browser may keep its sockets and state when the
// user's home directory can't be written.
func StateRoot() string {
	if d := strings.TrimSpace(os.Getenv("GHOST_DIR")); d != "" {
		return filepath.Join(d, "browser-home")
	}
	if st, err := os.Stat("/var/ghost"); err == nil && st.IsDir() {
		return "/var/ghost/browser-home"
	}
	return filepath.Join(os.TempDir(), "ghost-browser-home")
}

// WritableDir reports whether files can be created under dir.
func WritableDir(dir string) bool {
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".w-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// WithWritableBrowserHome gives a browser child somewhere to put its socket
// and state. An operator-chosen HOME or XDG_RUNTIME_DIR that already works is
// never overridden.
func WithWritableBrowserHome(env []string) []string {
	get := func(key string) string {
		for _, kv := range env {
			if strings.HasPrefix(kv, key+"=") {
				return strings.TrimPrefix(kv, key+"=")
			}
		}
		return ""
	}
	homeOK := WritableDir(filepath.Join(get("HOME"), ".agent-browser")) && get("HOME") != ""
	runtimeOK := get("XDG_RUNTIME_DIR") != "" && WritableDir(get("XDG_RUNTIME_DIR"))
	if homeOK && runtimeOK {
		return env
	}
	root := StateRoot()
	run := filepath.Join(root, "run")
	if !WritableDir(root) || !WritableDir(run) {
		return env
	}
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if (!homeOK && strings.HasPrefix(kv, "HOME=")) || (!runtimeOK && strings.HasPrefix(kv, "XDG_RUNTIME_DIR=")) {
			continue
		}
		out = append(out, kv)
	}
	if !homeOK {
		out = append(out, "HOME="+root)
	}
	if !runtimeOK {
		out = append(out, "XDG_RUNTIME_DIR="+run)
	}
	return out
}
