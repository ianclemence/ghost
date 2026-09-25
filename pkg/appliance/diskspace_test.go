//go:build linux || darwin

package appliance

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestEnsureDiskSpaceReportsFullDisk(t *testing.T) {
	orig := statfsFn
	defer func() { statfsFn = orig }()

	// Simulate a nearly-full disk: 1 MB available.
	statfsFn = func(path string, st *syscall.Statfs_t) error {
		st.Bsize = 4096
		st.Bavail = 256 // 1 MB
		return nil
	}
	err := EnsureDiskSpace("/var/lib/ghost", 64<<20)
	if err == nil {
		t.Fatal("a full disk must be reported, never silently ignored")
	}
	if !strings.Contains(err.Error(), "free some space") {
		t.Errorf("error should be actionable: %v", err)
	}

	// Plenty of space: no error.
	statfsFn = func(path string, st *syscall.Statfs_t) error {
		st.Bsize = 4096
		st.Bavail = 1 << 20 // 4 GB
		return nil
	}
	if err := EnsureDiskSpace("/var/lib/ghost", 64<<20); err != nil {
		t.Fatalf("ample space should pass: %v", err)
	}
}

func TestAtomicInstallNeverLeavesATruncatedBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, "ghost")
	if err := os.WriteFile(dst, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Make the destination directory unwritable so the temp write fails.
	if err := os.Chmod(dstDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dstDir, 0o755)

	if err := AtomicInstall(src, dst); err == nil {
		t.Fatal("install into an unwritable directory must fail")
	}
	if _, err := os.Stat(dst + ".new"); !os.IsNotExist(err) {
		t.Fatal("a failed install must not leave a .new file behind")
	}
	// The original binary is untouched.
	if b, _ := os.ReadFile(dst); string(b) != "old-binary" {
		t.Fatalf("destination was modified on failure: %q", string(b))
	}
}
