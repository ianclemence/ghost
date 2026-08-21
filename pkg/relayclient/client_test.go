package relayclient

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestAddAndListClients(t *testing.T) {
	dir := tempDir(t)
	// Override clients path for testing
	origEnv := os.Getenv("GHOST_DIR")
	os.Setenv("GHOST_DIR", dir)
	defer os.Setenv("GHOST_DIR", origEnv)

	token1, err := AddClient("device-1", "phone")
	if err != nil {
		t.Fatalf("AddClient: %v", err)
	}
	if len(token1) != 64 {
		t.Errorf("token length = %d, want 64", len(token1))
	}

	token2, _ := AddClient("device-1", "tablet")

	clients, err := ListClients("device-1")
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(clients) != 2 {
		t.Errorf("expected 2 clients, got %d", len(clients))
	}

	// Verify token hashes
	hash1 := sha256.Sum256([]byte(token1))
	if clients[0].TokenHash != hex.EncodeToString(hash1[:]) {
		t.Error("token hash mismatch for client 0")
	}

	hash2 := sha256.Sum256([]byte(token2))
	if clients[1].TokenHash != hex.EncodeToString(hash2[:]) {
		t.Error("token hash mismatch for client 1")
	}
}

func TestRemoveClient(t *testing.T) {
	dir := tempDir(t)
	origEnv := os.Getenv("GHOST_DIR")
	os.Setenv("GHOST_DIR", dir)
	defer os.Setenv("GHOST_DIR", origEnv)

	token1, _ := AddClient("device-1", "phone")
	token2, _ := AddClient("device-1", "tablet")

	// Remove first client by prefix of its hash
	hash1 := sha256.Sum256([]byte(token1))
	hash1Hex := hex.EncodeToString(hash1[:])
	if err := RemoveClient("device-1", hash1Hex[:16]); err != nil {
		t.Fatalf("RemoveClient: %v", err)
	}

	clients, _ := ListClients("device-1")
	if len(clients) != 1 {
		t.Errorf("expected 1 client after remove, got %d", len(clients))
	}

	// Remaining client should be the tablet
	hash2 := sha256.Sum256([]byte(token2))
	if clients[0].TokenHash != hex.EncodeToString(hash2[:]) {
		t.Error("wrong client remaining after remove")
	}
}

func TestRemoveClientNotFound(t *testing.T) {
	dir := tempDir(t)
	origEnv := os.Getenv("GHOST_DIR")
	os.Setenv("GHOST_DIR", dir)
	defer os.Setenv("GHOST_DIR", origEnv)

	AddClient("device-1", "phone")

	err := RemoveClient("device-1", "nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent client")
	}
}

func TestClientPersistence(t *testing.T) {
	dir := tempDir(t)
	origEnv := os.Getenv("GHOST_DIR")
	os.Setenv("GHOST_DIR", dir)
	defer os.Setenv("GHOST_DIR", origEnv)

	token, _ := AddClient("device-1", "phone")

	// Reload and verify
	clients, err := ListClients("device-1")
	if err != nil {
		t.Fatalf("ListClients: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}

	hash := sha256.Sum256([]byte(token))
	if clients[0].TokenHash != hex.EncodeToString(hash[:]) {
		t.Error("token hash mismatch after reload")
	}
	if clients[0].Name != "phone" {
		t.Errorf("name = %q, want phone", clients[0].Name)
	}
}

func TestClientFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions differ on Windows")
	}
	dir := tempDir(t)
	origEnv := os.Getenv("GHOST_DIR")
	os.Setenv("GHOST_DIR", dir)
	defer os.Setenv("GHOST_DIR", origEnv)

	AddClient("device-1", "phone")

	path := filepath.Join(dir, "workspace", "state", "relay_clients.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}
