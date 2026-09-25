package browser

import (
	"os"
	"path/filepath"
	"syscall"
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

// Reparenting happens after the parent dies, so a single scan misses the
// Chromium tree. The loop must rescan and catch newly-orphaned children.
func TestReapPassesCatchesChildrenReparentedAfterTheDaemonDies(t *testing.T) {
	root := buildProc(t, []struct {
		pid, ppid int
		cmdline   string
	}{
		{201, 1, "/usr/local/lib/node_modules/agent-browser/bin/agent-browser-linux-arm64"},
	})

	killed := []int{}
	kill := func(pid int, sig syscall.Signal) error {
		killed = append(killed, pid)
		// Simulate the daemon's death reparenting its Chromium child: remove
		// the daemon and introduce a newly-orphaned Chromium on this pass.
		os.RemoveAll(filepath.Join(root, itoa(pid)))
		if pid == 201 {
			dir := filepath.Join(root, "202")
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "stat"), []byte("202 (chromium) S 1 0 0 0 0"), 0o644)
			os.WriteFile(filepath.Join(dir, "cmdline"), []byte("/usr/lib/chromium/chromium --user-data-dir=/tmp/agent-browser-chrome-x\x00"), 0o644)
		}
		return nil
	}

	got := reapPasses(root, 4, kill, nil)
	if len(got) != 2 || got[0] != 201 || got[1] != 202 {
		t.Fatalf("reapPasses = %v, want [201 202] (the reparented child must be caught)", got)
	}
}
