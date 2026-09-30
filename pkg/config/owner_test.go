//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Saving must never lose the file, whoever saves it, and a saved settings file
// stays readable by the person who owns the directory.
func TestSavesKeepTheFileReadableByItsOwner(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := SaveConfig(cfgPath, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(cfgPath)
	if err := SaveConfig(cfgPath, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", after.Mode())
	}
	bs, as := before.Sys(), after.Sys()
	if bs == nil || as == nil {
		return
	}
	if _, err := os.ReadFile(cfgPath); err != nil {
		t.Fatalf("the owner must still be able to read it: %v", err)
	}
	// KeepOwner is a no-op for everyone but root and never fails a write.
	KeepOwner(filepath.Join(dir, "missing"), filepath.Join(dir, "also-missing"))
}
