package browser

import (
	"os"
	"path/filepath"
	"testing"
)

// mkExe creates an executable file at path, creating parent dirs.
func mkExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestDiscoverExecutablePrefersOperatorOverride(t *testing.T) {
	t.Setenv(ExecutableEnv, "/opt/custom/chrome")
	root := t.TempDir()
	mkExe(t, filepath.Join(root, "chromium_headless_shell-9999", "chrome-linux", "headless_shell"))

	// An operator-set path means discovery must not override it.
	if got := DiscoverExecutable([]string{root}); got != "" {
		t.Fatalf("operator override must win and yield empty (leave alone); got %q", got)
	}
}

func TestDiscoverExecutableFindsPlaywrightHeadlessShell(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	root := t.TempDir()
	want := filepath.Join(root, "chromium_headless_shell-1234", "chrome-linux", "headless_shell")
	mkExe(t, want)

	if got := DiscoverExecutable([]string{root}); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDiscoverExecutableNewestBuildWins(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	root := t.TempDir()
	mkExe(t, filepath.Join(root, "chromium_headless_shell-1000", "chrome-linux", "headless_shell"))
	newest := filepath.Join(root, "chromium_headless_shell-2000", "chrome-linux", "headless_shell")
	mkExe(t, newest)

	if got := DiscoverExecutable([]string{root}); got != newest {
		t.Fatalf("got %q, want newest %q", got, newest)
	}
}

func TestDiscoverExecutableIgnoresNonExecutable(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	root := t.TempDir()
	// Present but not executable: must be skipped.
	bad := filepath.Join(root, "chromium_headless_shell-1234", "chrome-linux", "headless_shell")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DiscoverExecutable([]string{root}); got != "" {
		t.Fatalf("non-executable must be skipped; got %q", got)
	}
}

func TestDiscoverExecutableIgnoresOtherChromiumDirs(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	root := t.TempDir()
	// Full chromium (not headless_shell) must not be selected — it is the
	// binary that crashes; only the headless shell is trusted.
	mkExe(t, filepath.Join(root, "chromium-1234", "chrome-linux", "chrome"))
	if got := DiscoverExecutable([]string{root}); got != "" {
		t.Fatalf("full chromium must not be selected; got %q", got)
	}
}

func TestDiscoverExecutableMissingRootIsSafe(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	if got := DiscoverExecutable([]string{"/nonexistent/root/xyz"}); got != "" {
		t.Fatalf("missing root must return empty; got %q", got)
	}
}

func TestDiscoverExecutableOrderAcrossRoots(t *testing.T) {
	t.Setenv(ExecutableEnv, "")
	first := t.TempDir()
	second := t.TempDir()
	mkExe(t, filepath.Join(second, "chromium_headless_shell-1", "chrome-linux", "headless_shell"))
	want := filepath.Join(first, "chromium_headless_shell-1", "chrome-linux", "headless_shell")
	mkExe(t, want)

	if got := DiscoverExecutable([]string{first, second}); got != want {
		t.Fatalf("first root should win on equal build; got %q want %q", got, want)
	}
}
