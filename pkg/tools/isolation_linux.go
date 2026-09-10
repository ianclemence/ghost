//go:build linux

package tools

import (
	"os"
	"os/exec"
)

// bwrapAvailable reports whether bubblewrap is installed.
func bwrapAvailable() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
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
		args = append(args, "--bind", workspace, workspace)
		args = append(args, "--setenv", "HOME", workspace)
	}
	if cwd == "" {
		cwd = workspace
	}
	if cwd != "" {
		if _, err := os.Stat(cwd); err == nil {
			if cwd != workspace {
				args = append(args, "--bind", cwd, cwd)
			}
			args = append(args, "--chdir", cwd)
		}
	}
	if !allowNetwork {
		args = append(args, "--unshare-net")
	}
	return args
}
