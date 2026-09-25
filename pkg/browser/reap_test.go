package browser

import (
	"os"
	"path/filepath"
	"testing"
)

// buildProc writes a fake /proc entry: a stat line with the given ppid and a
// NUL-separated cmdline, returning the root to scan.
func buildProc(t *testing.T, entries []struct {
	pid, ppid int
	cmdline   string
}) string {
	t.Helper()
	root := t.TempDir()
	for _, e := range entries {
		dir := filepath.Join(root, itoa(e.pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stat := itoa(e.pid) + " (proc) S " + itoa(e.ppid) + " 0 0 0 0"
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := e.cmdline + "\x00"
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmd), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestOrphanBrowserPIDsSelectsOnlyOrphanedBrowserProcesses(t *testing.T) {
	root := buildProc(t, []struct {
		pid, ppid int
		cmdline   string
	}{
		// Orphaned agent-browser daemon: reap.
		{101, 1, "/usr/local/lib/node_modules/agent-browser/bin/agent-browser-linux-arm64"},
		// Orphaned agent-browser Chromium: reap.
		{102, 1, "/usr/lib/chromium/chromium --user-data-dir=/tmp/agent-browser-chrome-abc --headless=new"},
		// Orphaned Playwright headless shell: reap.
		{103, 1, "/home/u/.cache/ms-playwright/chromium_headless_shell-1234/chrome-linux/headless_shell --remote-debugging-port=0"},
		// Live session (has a parent): never touch.
		{104, 50, "/usr/local/lib/node_modules/agent-browser/bin/agent-browser-linux-arm64"},
		// Orphaned but unrelated: never touch.
		{105, 1, "/usr/bin/python3 /srv/worker.py"},
		// A person's desktop browser: never touch.
		{106, 1, "/usr/bin/google-chrome --profile-directory=Default"},
	})

	got := OrphanBrowserPIDs(root)
	want := []int{101, 102, 103}
	if len(got) != len(want) {
		t.Fatalf("OrphanBrowserPIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OrphanBrowserPIDs = %v, want %v", got, want)
		}
	}
}
