package main

import "testing"

func TestRenderUnitMatchesTheInstalledShape(t *testing.T) {
	got := renderUnit("User=__USER__\nExecStart=__BIN_DIR__/ghost serve\nWorkingDirectory=__GHOST_DIR__\n", "/usr/local/bin")
	want := "User=root\nExecStart=/usr/local/bin/ghost serve\nWorkingDirectory=/var/ghost\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
