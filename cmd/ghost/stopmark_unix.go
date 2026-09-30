//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ianclemence/ghost/pkg/hardware"
)

// markCleanStopOnSignal removes the run marker when the daemon is asked to stop,
// then lets the signal do what it would have done.
func markCleanStopOnSignal(marker string) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-c
		hardware.MarkStopped(marker)
		signal.Stop(c)
		_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
	}()
}
