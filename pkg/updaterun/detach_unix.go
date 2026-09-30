//go:build !windows

package updaterun

import (
	"os/exec"
	"syscall"
)

// detach lets the update outlive the process that started it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
