package browser

import (
	"strings"
	"testing"
)

// The tool path and the screencast broker must resolve the same browser HOME
// and runtime directory. When they disagree, the broker looks in the wrong
// socket directory and reports streaming unavailable while the stream server
// is listening elsewhere — the exact failure the owner saw ("live view isn't
// available") at a human-check step.
func TestWithWritableBrowserHomeRedirectsWhenHomeUnwritable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GHOST_DIR", root)

	env := []string{"HOME=/proc/nonexistent-home", "PATH=/usr/bin"}
	got := WithWritableBrowserHome(env)

	home := envValue(got, "HOME")
	runtime := envValue(got, "XDG_RUNTIME_DIR")
	if !strings.HasPrefix(home, root) {
		t.Fatalf("HOME must move under the state root, got %q (root %q)", home, root)
	}
	if !strings.HasPrefix(runtime, root) {
		t.Fatalf("XDG_RUNTIME_DIR must move under the state root, got %q", runtime)
	}
	if !WritableDir(home) || !WritableDir(runtime) {
		t.Fatalf("the redirected dirs must be writable: home=%q runtime=%q", home, runtime)
	}
}

// An operator-set HOME and runtime that already work are never overridden.
func TestWithWritableBrowserHomeKeepsGoodEnv(t *testing.T) {
	home := t.TempDir()
	runtime := t.TempDir()
	env := []string{"HOME=" + home, "XDG_RUNTIME_DIR=" + runtime}
	got := WithWritableBrowserHome(env)
	if envValue(got, "HOME") != home || envValue(got, "XDG_RUNTIME_DIR") != runtime {
		t.Fatalf("good env must be preserved, got %v", got)
	}
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"=")
		}
	}
	return ""
}
