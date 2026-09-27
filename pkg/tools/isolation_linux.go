//go:build linux

package tools

import (
	"os"
	"os/exec"
	"path/filepath"
)

// bwrapAvailable reports whether bubblewrap is installed.
func bwrapAvailable() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

// writableWorkspaceDirs are the only workspace areas executed code may
// write: the scratch directory (also HOME) plus the owner-content areas
// skills legitimately produce.
var writableWorkspaceDirs = []string{"tmp", "knowledge", "data", "learning"}

// sandboxHiddenDirs are Ghost's governed stores — the scoped memory
// journal, long-term memory, runtime state, session logs, the scheduler,
// the event trail, in-flight work and reflection state. They are not even
// visible to executed code: an empty tmpfs is mounted over each, so a user
// project can neither read nor alter Ghost's own state. The real files stay
// untouched on the host.
var sandboxHiddenDirs = []string{
	"personal-context", "state", "memory", "sessions", "commitments",
	"pending", "events", "cron", "journal", "dreams", "proactive",
	"conversations",
}

// bwrapArgs builds the isolation profile. Only existing system directories are
// bound read-only, so the same profile works on distributions where /lib64 or
// /sbin do not exist (e.g. some ARM images).
func bwrapArgs(cwd, workspace string, allowNetwork bool) []string {
	args := []string{
		"--die-with-parent",
		"--unshare-pid", "--unshare-ipc", "--unshare-uts",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
	}
	for _, p := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"} {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", p, p)
		}
	}
	if workspace != "" {
		// Ghost's own tree is read-only to executed code. The scratch and
		// owner-content areas are re-bound read-write; HOME points at
		// scratch so tool caches (go, pip, npm, python) land somewhere
		// writable instead of failing against the read-only estate.
		args = append(args, "--ro-bind", workspace, workspace)
		for _, d := range writableWorkspaceDirs {
			p := filepath.Join(workspace, d)
			if d == "tmp" {
				if err := os.MkdirAll(p, 0o700); err != nil {
					continue
				}
			} else if _, err := os.Stat(p); err != nil {
				continue
			}
			args = append(args, "--bind", p, p)
		}
		for _, d := range sandboxHiddenDirs {
			p := filepath.Join(workspace, d)
			if _, err := os.Stat(p); err == nil {
				args = append(args, "--tmpfs", p)
			}
		}
		if _, err := os.Stat(filepath.Join(workspace, "tmp")); err == nil {
			args = append(args, "--setenv", "HOME", filepath.Join(workspace, "tmp"))
		}
	}
	if cwd == "" {
		cwd = workspace
	}
	if cwd != "" {
		if _, err := os.Stat(cwd); err == nil {
			// A directory outside the workspace (a user project on the Pod)
			// is bound read-write so real work can happen there. Inside the
			// workspace the read-only bind and the writable areas already
			// decided access, so no extra bind is added; Ghost's own
			// runtime home beside the workspace is never bound.
			if workspace == "" || !under(workspace, cwd) {
				if !ghostRuntimeHomePath(workspace, cwd) {
					args = append(args, "--bind", cwd, cwd)
				}
			}
			args = append(args, "--chdir", cwd)
		}
	}
	if !allowNetwork {
		args = append(args, "--unshare-net")
	}
	return args
}
