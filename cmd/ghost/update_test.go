package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderUnitMatchesTheInstalledShape(t *testing.T) {
	got := renderUnit("User=__USER__\nExecStart=__BIN_DIR__/ghost serve\nWorkingDirectory=__GHOST_DIR__\n", "/usr/local/bin")
	want := "User=root\nExecStart=/usr/local/bin/ghost serve\nWorkingDirectory=/var/ghost\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRemoveStaleShadowsOnlyRemovesGhost(t *testing.T) {
	canon := t.TempDir()
	stale, other := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(stale, "ghost"), []byte("...👻 Ghost - Your Local AI..."), 0o755)
	os.WriteFile(filepath.Join(other, "ghost"), []byte("a different program named ghost"), 0o755)
	removeStaleShadows(canon, []string{stale, other, canon})
	if _, err := os.Stat(filepath.Join(stale, "ghost")); !os.IsNotExist(err) {
		t.Fatal("a stale Ghost earlier in PATH must be removed")
	}
	if _, err := os.Stat(filepath.Join(other, "ghost")); err != nil {
		t.Fatal("an unrelated program called ghost must never be touched")
	}
}
