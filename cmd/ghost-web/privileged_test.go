package main

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// privileged records what the console would have run as root. Tests never
// run it: a test run must not restart the Ghost on the machine running it,
// change its firewall, rename it or reboot it.
var privileged struct {
	mu    sync.Mutex
	calls []string
}

func TestMain(m *testing.M) {
	runPrivileged = func(name string, args ...string) ([]byte, error) {
		privileged.mu.Lock()
		privileged.calls = append(privileged.calls, strings.Join(append([]string{name}, args...), " "))
		privileged.mu.Unlock()
		return nil, nil
	}
	os.Exit(m.Run())
}
