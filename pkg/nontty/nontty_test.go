package nontty

import (
	"testing"
)

func TestRequireInteractiveFailsWithoutTTY(t *testing.T) {
	// Test runners have no TTY on stdin: the guard must fire.
	if Interactive() {
		t.Skip("stdin is a terminal; guard is a no-op here")
	}
	if err := RequireInteractive("onboarding", "--yes"); err == nil {
		t.Fatal("must fail instead of prompting")
	}
	t.Setenv("CI", "1")
	if Interactive() {
		t.Fatal("CI=1 must force non-interactive")
	}
}
