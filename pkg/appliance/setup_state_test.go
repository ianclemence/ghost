package appliance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsSetup_NoFlagNoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	fb := &SetupState{
		GhostDir:   filepath.Join(tmpDir, "ghost"),
		ConfigDir:  filepath.Join(tmpDir, "ghost", "config"),
		DataDir:    filepath.Join(tmpDir, "ghost", "data"),
		Workspace:  filepath.Join(tmpDir, "ghost", "workspace"),
		ConfigPath: filepath.Join(tmpDir, "ghost", "config", "config.json"),
		EnvPath:    filepath.Join(tmpDir, "ghost", ".env"),
	}

	if !fb.NeedsSetup() {
		t.Fatal("expected setup when no flag and no config")
	}
}

func TestNeedsSetup_WithFlag(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)
	os.WriteFile(filepath.Join(ghostDir, SetupCompleteFlag), []byte("done"), 0644)

	fb := &SetupState{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	if fb.NeedsSetup() {
		t.Fatal("expected NOT setup when flag exists")
	}
}

func TestMarkSetupComplete(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)

	fb := &SetupState{
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

	if fb.NeedsSetup() {
		t.Fatal("NeedsSetup should return false after MarkSetupComplete")
	}
}

func TestResetSetup(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")
	os.MkdirAll(ghostDir, 0755)

	fb := &SetupState{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	fb.MarkSetupComplete()
	if fb.NeedsSetup() {
		t.Fatal("expected NOT setup after mark")
	}

	fb.ResetSetup()
	if !fb.NeedsSetup() {
		t.Fatal("expected setup after reset")
	}
}

func TestEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	ghostDir := filepath.Join(tmpDir, "ghost")

	fb := &SetupState{
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

	fb := &SetupState{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	fb.EnsureDirectories()

	if !fb.NeedsSetup() {
		t.Fatal("expected setup initially")
	}

	fb.MarkSetupComplete()

	if fb.NeedsSetup() {
		t.Fatal("expected NOT setup after setup complete")
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

	fb := &SetupState{
		GhostDir:   ghostDir,
		ConfigDir:  filepath.Join(ghostDir, "config"),
		DataDir:    filepath.Join(ghostDir, "data"),
		Workspace:  filepath.Join(ghostDir, "workspace"),
		ConfigPath: filepath.Join(ghostDir, "config", "config.json"),
		EnvPath:    filepath.Join(ghostDir, ".env"),
	}

	fb.MarkSetupComplete()
	fb.MarkSetupComplete()

	if fb.NeedsSetup() {
		t.Fatal("expected NOT setup after double mark")
	}
}

func TestSetupCodeLifecycle(t *testing.T) {
	fb := &SetupState{GhostDir: t.TempDir()}

	if fb.VerifySetupCode("000000") {
		t.Fatal("no code file must never verify")
	}

	code, err := fb.RotateSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("expected a 6-digit code, got %q", code)
	}
	if !fb.VerifySetupCode(code) {
		t.Fatal("the minted code must verify")
	}
	if fb.VerifySetupCode(code + "0") {
		t.Fatal("a longer code must not verify")
	}
	if fb.VerifySetupCode("") {
		t.Fatal("an empty code must not verify")
	}

	// Rotation invalidates the previous code (barring a 1-in-10^6 collision).
	next, err := fb.RotateSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if next != code && fb.VerifySetupCode(code) {
		t.Fatal("rotation must invalidate the previous code")
	}
	if !fb.VerifySetupCode(next) {
		t.Fatal("the rotated code must verify")
	}

	fb.ClearSetupCode()
	if fb.VerifySetupCode(next) {
		t.Fatal("a cleared code must not verify")
	}
}
