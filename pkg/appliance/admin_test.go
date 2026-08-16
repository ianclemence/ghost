package appliance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetAndVerifyAdminPassword(t *testing.T) {
	ghostDir := filepath.Join(t.TempDir(), "ghost")

	if AdminConfigured(ghostDir) {
		t.Fatal("admin should not be configured before SetAdminPassword")
	}

	ok, err := VerifyAdminPassword(ghostDir, "hunter2")
	if err != nil {
		t.Fatalf("VerifyAdminPassword before set failed: %v", err)
	}
	if ok {
		t.Fatal("VerifyAdminPassword should be false before any password is set")
	}

	if err := SetAdminPassword(ghostDir, "hunter2"); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}

	if !AdminConfigured(ghostDir) {
		t.Fatal("admin should be configured after SetAdminPassword")
	}

	ok, err = VerifyAdminPassword(ghostDir, "hunter2")
	if err != nil {
		t.Fatalf("VerifyAdminPassword failed: %v", err)
	}
	if !ok {
		t.Fatal("correct password should verify")
	}

	ok, err = VerifyAdminPassword(ghostDir, "wrong-password")
	if err != nil {
		t.Fatalf("VerifyAdminPassword failed: %v", err)
	}
	if ok {
		t.Fatal("wrong password should not verify")
	}
}

func TestSetAdminPasswordEmpty(t *testing.T) {
	if err := SetAdminPassword(t.TempDir(), ""); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestAdminHashFilePermissions(t *testing.T) {
	ghostDir := filepath.Join(t.TempDir(), "ghost")
	if err := SetAdminPassword(ghostDir, "s3cret"); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}

	info, err := os.Stat(AdminHashPath(ghostDir))
	if err != nil {
		t.Fatalf("failed to stat admin hash: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("admin hash should be 0600, got %o", perm)
	}
}

func TestSetAdminPasswordReplacesHash(t *testing.T) {
	ghostDir := filepath.Join(t.TempDir(), "ghost")
	if err := SetAdminPassword(ghostDir, "first-pass"); err != nil {
		t.Fatalf("SetAdminPassword failed: %v", err)
	}

	if ok, _ := VerifyAdminPassword(ghostDir, "second-pass"); ok {
		t.Fatal("old password should not verify yet")
	}

	if err := SetAdminPassword(ghostDir, "second-pass"); err != nil {
		t.Fatalf("second SetAdminPassword failed: %v", err)
	}

	if ok, _ := VerifyAdminPassword(ghostDir, "first-pass"); ok {
		t.Fatal("first password should no longer verify after replacement")
	}
	if ok, _ := VerifyAdminPassword(ghostDir, "second-pass"); !ok {
		t.Fatal("replacement password should verify")
	}
}

func TestGenerateBridgeSecret(t *testing.T) {
	s1, err := GenerateBridgeSecret()
	if err != nil {
		t.Fatalf("GenerateBridgeSecret failed: %v", err)
	}
	s2, err := GenerateBridgeSecret()
	if err != nil {
		t.Fatalf("GenerateBridgeSecret failed: %v", err)
	}

	if s1 == "" || s2 == "" {
		t.Fatal("generated secrets must not be empty")
	}
	if len(s1) != 64 || len(s2) != 64 {
		t.Fatalf("expected 64 hex chars, got %d and %d", len(s1), len(s2))
	}
	if s1 == s2 {
		t.Fatal("two generated secrets must not be identical")
	}
}
