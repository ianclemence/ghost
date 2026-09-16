package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConsolePortOrder(t *testing.T) {
	// No sticky: requested first, then standard fallbacks, deduped.
	got := consolePortOrder(80, 0)
	want := []int{80, 8080, 8888, 9090}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	// Requested leads so the console returns to it once free; sticky is
	// second for stability while the conflict persists.
	got = consolePortOrder(80, 8080)
	if got[0] != 80 || got[1] != 8080 {
		t.Fatalf("requested must lead, sticky second: %v", got)
	}
	// Custom -port with sticky elsewhere: requested, sticky, fallbacks.
	got = consolePortOrder(9000, 8080)
	if len(got) != 5 || got[0] != 9000 || got[1] != 8080 {
		t.Fatalf("custom order wrong: %v", got)
	}
	// Invalid ports never appear.
	if got := consolePortOrder(0, -5); len(got) == 0 || got[0] == 0 {
		t.Fatalf("invalid ports must be dropped: %v", got)
	}
}

func TestStickyConsolePortRoundtrip(t *testing.T) {
	dir := t.TempDir()
	if got := readStickyConsolePort(dir); got != 0 {
		t.Fatalf("absent file = %d, want 0", got)
	}
	writeStickyConsolePort(dir, 8080)
	if got := readStickyConsolePort(dir); got != 8080 {
		t.Fatalf("roundtrip = %d, want 8080", got)
	}
	// Garbage and out-of-range values degrade to "no sticky", never a crash.
	if err := os.WriteFile(filepath.Join(dir, ".console-port"), []byte("banana"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readStickyConsolePort(dir); got != 0 {
		t.Fatalf("garbage = %d, want 0", got)
	}
	writeStickyConsolePort(dir, 99999)
	if got := readStickyConsolePort(dir); got != 0 {
		t.Fatalf("out-of-range = %d, want 0", got)
	}
}
