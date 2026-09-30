// Package updaterun starts and observes a detached `ghost update`. It is a leaf
// package (no Ghost imports) so both the agent tools and the console can use it.
package updaterun

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Detached update.
//
// The console and the agent both need to say "update Ghost", but neither can
// do it in-process. ghost-web and ghost run inside a read-only systemd sandbox
// (ProtectSystem=strict) that cannot write /usr/local/bin, and an update
// restarts the very service that started it. So the update runs as its own
// transient systemd unit, outside that sandbox and outliving the caller, and
// writes to a log the caller can poll from afterwards.

// UpdateUnit is the transient unit name; a second update while one runs is
// refused rather than queued.
const UpdateUnit = "ghost-update-run"

const (
	updateDoneMarker   = "Update complete!"
	updateFailedMarker = "GHOST_UPDATE_FAILED"
)

// UpdateLogPath is where a detached update writes its output.
func UpdateLogPath() string {
	dir := os.Getenv("GHOST_DIR")
	if dir == "" {
		dir = "/var/ghost"
	}
	return filepath.Join(dir, "update.log")
}

// UpdateProgress is what a poller sees.
type UpdateProgress struct {
	Running bool   `json:"running"`
	Success bool   `json:"success"`
	Log     string `json:"log"`
}

func systemdIsInit() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

func ghostBinaryPath() string {
	if p := os.Getenv("GHOST_BIN"); p != "" {
		return p
	}
	for _, p := range []string{"/usr/local/bin/ghost", "/usr/bin/ghost"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("ghost"); err == nil {
		return p
	}
	return "ghost"
}

// updateScript is the whole job: run the updater, then leave an unambiguous
// marker either way, so a poller never has to guess from exit codes it can't see.
func updateScript(bin string) string {
	q := "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	return fmt.Sprintf(`%s update && echo %q || echo %s`, q, updateDoneMarker, updateFailedMarker)
}

// StartDetachedUpdate begins `ghost update` outside the caller's sandbox.
func StartDetachedUpdate() error {
	if p := DetachedUpdateStatus(); p.Running {
		return errors.New("an update is already running")
	}
	log := UpdateLogPath()
	if err := os.MkdirAll(filepath.Dir(log), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(log, []byte("Starting update...\n"), 0644); err != nil {
		return err
	}
	script := updateScript(ghostBinaryPath())
	if systemdIsInit() {
		if _, err := exec.LookPath("systemd-run"); err == nil {
			out, err := exec.Command("systemd-run", "--unit="+UpdateUnit, "--collect", "--quiet",
				"-p", "StandardOutput=append:"+log, "-p", "StandardError=append:"+log,
				"/bin/sh", "-c", script).CombinedOutput()
			if err != nil {
				return fmt.Errorf("could not start the update unit: %v: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	return startWithoutSystemd()
}

// startWithoutSystemd runs the update in a detached session. It is also the
// path tests exercise, since a unit test must not talk to the real systemd.
func startWithoutSystemd() error {
	log := UpdateLogPath()
	if err := os.MkdirAll(filepath.Dir(log), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(log, []byte("Starting update...\n"), 0644); err != nil {
		return err
	}
	script := updateScript(ghostBinaryPath())
	f, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Stdout, cmd.Stderr = f, f
	detach(cmd)
	if err := cmd.Start(); err != nil {
		f.Close()
		return err
	}
	go func() { _ = cmd.Wait(); f.Close() }()
	return nil
}

// DetachedUpdateStatus reads the log and unit state of the last update.
func DetachedUpdateStatus() UpdateProgress {
	b, err := os.ReadFile(UpdateLogPath())
	if err != nil {
		return UpdateProgress{}
	}
	text := string(b)
	done := strings.Contains(text, updateDoneMarker)
	failed := strings.Contains(text, updateFailedMarker)
	p := UpdateProgress{Success: done && !failed, Log: strings.ReplaceAll(text, updateFailedMarker, "Update failed.")}
	if done || failed {
		return p
	}
	if systemdIsInit() {
		p.Running = exec.Command("systemctl", "is-active", "--quiet", UpdateUnit).Run() == nil
		return p
	}
	// Without systemd, a log still being written recently means it is running.
	if st, err := os.Stat(UpdateLogPath()); err == nil && time.Since(st.ModTime()) < 10*time.Minute {
		p.Running = true
	}
	return p
}
