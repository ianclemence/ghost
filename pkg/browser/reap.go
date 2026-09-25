package browser

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// Browser processes left behind by a previous Ghost run are the known cause of
// every browser action hanging until its timeout: new invocations attach to
// the wedged daemon instead of launching a fresh browser, so the failure looks
// identical for every site. Reaping them at startup (and after a timeout)
// turns a wedged subsystem back into a working one.
//
// Only orphaned processes (reparented to PID 1) owned by this user, and only
// browser processes (the agent-browser daemon, its Chromium user-data-dir
// marker, or Playwright's headless shell), are eligible. A live session is
// never a target: it still has a parent.

// OrphanBrowserPIDs returns the PIDs that ReapOrphans would kill. It is pure
// selection (no signals), so it can be tested against a fake /proc.
func OrphanBrowserPIDs(procRoot string) []int {
	if procRoot == "" {
		procRoot = "/proc"
	}
	uid := os.Getuid()
	var out []int
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		dir := filepath.Join(procRoot, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(raw))
		if len(fields) < 4 || fields[3] != "1" {
			continue // not an orphan
		}
		if st, err := os.Stat(dir); err == nil {
			if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != uid {
				continue // someone else's process
			}
		}
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		if isBrowserProcess(strings.ReplaceAll(string(cmdline), "\x00", " ")) {
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out
}

// isBrowserProcess recognizes the three process shapes Ghost's browser stack
// creates. The Chromium user-data-dir marker keeps this from matching a
// desktop browser a person may be using.
func isBrowserProcess(cmd string) bool {
	switch {
	case strings.Contains(cmd, "agent-browser"):
		return true
	case strings.Contains(cmd, "chrome-linux/headless_shell"):
		return true
	case strings.Contains(cmd, "agent-browser-chrome-"):
		return true
	default:
		return false
	}
}

// ReapOrphans kills the orphaned browser processes and returns the PIDs it
// killed. Safe to call at startup and after a browser timeout.
func ReapOrphans(procRoot string) []int {
	killed := []int{}
	for _, pid := range OrphanBrowserPIDs(procRoot) {
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed = append(killed, pid)
		}
	}
	return killed
}
