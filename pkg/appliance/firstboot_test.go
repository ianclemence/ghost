package appliance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsFirstBoot_NoFlagNoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	fb := &FirstBoot{
		GhostDir:   filepath.Join(tmpDir, "ghost"),
		ConfigDir:  filepath.Join(tmpDir, "ghost", "config"),
		DataDir:    filepath.Join(tmpDir, "ghost", "data"),
		Workspace:  filepath.Join(tmpDir, "ghost", "workspace"),
		ConfigPath: filepath.Join(tmpDir, "ghost", "config", "config.json"),
		EnvPath:    filepath.Join(tmpDir, "ghost", ".env"),
	}

	if !fb.IsFirstBoot() {
		t.Fatal("expected first boot when no flag and no config")
	}
}

func TestIsFirstBoot_WithFlag(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)
	os.WriteFile(filepath.Join(ghostDir, SetupCompleteFlag), []byte("done"), 0644)

	fb := &FirstBoot{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	if fb.IsFirstBoot() {
		t.Fatal("expected NOT first boot when flag exists")
	}
}

func TestMarkSetupComplete(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)

	fb := &FirstBoot{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	if err := fb.MarkSetupComplete(); err != nil {
		t.Fatalf("MarkSetupComplete failed: %v", err)
	}

	flagPath := filepath.Join(ghostDir, SetupCompleteFlag)
	if _, err := os.Stat(flagPath); os.IsNotExist(err) {
		t.Fatal("setup-complete flag was not created")
	}

	if fb.IsFirstBoot() {
		t.Fatal("IsFirstBoot should return false after MarkSetupComplete")
	}
}

func TestResetSetup(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)

	fb := &FirstBoot{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	fb.MarkSetupComplete()
	if fb.IsFirstBoot() {
		t.Fatal("expected NOT first boot after mark")
	}

	fb.ResetSetup()
	if !fb.IsFirstBoot() {
		t.Fatal("expected first boot after reset")
	}
}

func TestEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")

	fb := &FirstBoot{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	if err := fb.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	requiredDirs := []string{
		ghostDir,
		filepath.Join(ghostDir, "config"),
		filepath.Join(ghostDir, "data"),
		filepath.Join(ghostDir, "workspace"),
		filepath.Join(ghostDir, "workspace", "skills"),
		filepath.Join(ghostDir, "workspace", "memory"),
	}

	for _, dir := range requiredDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory not created: %s", dir)
		}
	}
}

func TestSetupCompleteTransition(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")

	fb := &FirstBoot{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	fb.EnsureDirectories()

	if !fb.IsFirstBoot() {
		t.Fatal("expected first boot initially")
	}

	fb.MarkSetupComplete()

	if fb.IsFirstBoot() {
		t.Fatal("expected NOT first boot after setup complete")
	}

	flagPath := filepath.Join(ghostDir, SetupCompleteFlag)
	data, err := os.ReadFile(flagPath)
	if err != nil {
		t.Fatalf("failed to read flag: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("flag file is empty")
	}
}

func TestSetupCompleteIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)

	fb := &FirstBoot{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	fb.MarkSetupComplete()
	fb.MarkSetupComplete()

	if fb.IsFirstBoot() {
		t.Fatal("expected NOT first boot after double mark")
	}
}
