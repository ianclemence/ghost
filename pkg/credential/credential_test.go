package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	passphrase := "test-passphrase-123"
	plaintext := []byte("Hello, World! This is a secret.")

	encrypted, err := Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, passphrase)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("Expected '%s', got '%s'", plaintext, decrypted)
	}
}

func TestEncryptDecrypt_DifferentPassphrases(t *testing.T) {
	plaintext := []byte("secret data")

	encrypted, err := Encrypt(plaintext, "passphrase-1")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, "passphrase-2")
	if err == nil {
		t.Error("Expected decryption to fail with wrong passphrase")
	}
}

func TestEncryptDecrypt_EmptyPassphrase(t *testing.T) {
	_, err := Encrypt([]byte("data"), "")
	if err == nil {
		t.Error("Expected error for empty passphrase")
	}

	_, err = Decrypt([]byte("data"), "")
	if err == nil {
		t.Error("Expected error for empty passphrase")
	}
}

func TestEncryptDecrypt_ShortCiphertext(t *testing.T) {
	_, err := Decrypt([]byte("short"), "passphrase")
	if err == nil {
		t.Error("Expected error for short ciphertext")
	}
}

func TestEncryptValue(t *testing.T) {
	passphrase := "test-pass"
	value := "sk-abc123"

	encrypted, err := EncryptValue(value, passphrase)
	if err != nil {
		t.Fatalf("EncryptValue failed: %v", err)
	}

	if !IsEncrypted(encrypted) {
		t.Error("Expected encrypted value")
	}

	// Resolve should decrypt it
	resolver := NewResolver(t.TempDir())
	resolver.SetPassphrase(passphrase)

	decrypted, err := resolver.Resolve(encrypted)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if decrypted != value {
		t.Errorf("Expected '%s', got '%s'", value, decrypted)
	}
}

func TestResolver_PlaintextPassthrough(t *testing.T) {
	resolver := NewResolver(t.TempDir())

	resolved, err := resolver.Resolve("plain-api-key")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if resolved != "plain-api-key" {
		t.Errorf("Expected 'plain-api-key', got '%s'", resolved)
	}
}

func TestResolver_EmptyValue(t *testing.T) {
	resolver := NewResolver(t.TempDir())

	resolved, err := resolver.Resolve("")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if resolved != "" {
		t.Errorf("Expected empty string, got '%s'", resolved)
	}
}

func TestResolver_FileReference(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewResolver(tmpDir)

	// Create a credential file
	credFile := filepath.Join(tmpDir, "api_key.txt")
	if err := os.WriteFile(credFile, []byte("file-credential-value\n"), 0600); err != nil {
		t.Fatalf("Failed to create credential file: %v", err)
	}

	resolved, err := resolver.Resolve("file://api_key.txt")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if resolved != "file-credential-value" {
		t.Errorf("Expected 'file-credential-value', got '%s'", resolved)
	}
}

func TestResolver_FileReference_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewResolver(tmpDir)

	_, err := resolver.Resolve("file://nonexistent.txt")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestResolver_FileReference_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewResolver(tmpDir)

	_, err := resolver.Resolve("file://../../../etc/passwd")
	if err == nil {
		t.Error("Expected error for path traversal")
	}
}

func TestResolver_EncryptedNoPassphrase(t *testing.T) {
	resolver := NewResolver(t.TempDir())

	// Encrypt with some passphrase
	encrypted, _ := EncryptValue("secret", "passphrase")

	// Try to resolve without setting passphrase
	_, err := resolver.Resolve(encrypted)
	if err == nil {
		t.Error("Expected error when no passphrase configured")
	}
}

func TestResolver_PassphraseProvider(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewResolver(tmpDir)

	provider := func() string {
		return "dynamic-passphrase"
	}
	resolver.SetPassphraseProvider(provider)

	encrypted, _ := EncryptValue("secret", "dynamic-passphrase")

	resolved, err := resolver.Resolve(encrypted)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if resolved != "secret" {
		t.Errorf("Expected 'secret', got '%s'", resolved)
	}
}

func TestSecureStore(t *testing.T) {
	store := NewSecureStore()

	if store.GetPassphrase() != "" {
		t.Error("Expected empty passphrase initially")
	}

	store.SetPassphrase("my-passphrase")
	if store.GetPassphrase() != "my-passphrase" {
		t.Errorf("Expected 'my-passphrase', got '%s'", store.GetPassphrase())
	}
}

func TestIsEncrypted(t *testing.T) {
	if !IsEncrypted("enc://abc123") {
		t.Error("Expected true for enc:// prefix")
	}
	if IsEncrypted("plain-value") {
		t.Error("Expected false for plain value")
	}
}

func TestIsFileReference(t *testing.T) {
	if !IsFileReference("file://key.txt") {
		t.Error("Expected true for file:// prefix")
	}
	if IsFileReference("plain-value") {
		t.Error("Expected false for plain value")
	}
}

func TestEncryptDecrypt_DifferentSalts(t *testing.T) {
	// Two encryptions of the same plaintext should produce different ciphertexts
	// due to random salt
	passphrase := "test-pass"
	plaintext := []byte("same plaintext")

	enc1, _ := Encrypt(plaintext, passphrase)
	enc2, _ := Encrypt(plaintext, passphrase)

	if string(enc1) == string(enc2) {
		t.Error("Expected different ciphertexts due to random salt")
	}

	// Both should decrypt to the same plaintext
	dec1, _ := Decrypt(enc1, passphrase)
	dec2, _ := Decrypt(enc2, passphrase)

	if string(dec1) != string(dec2) {
		t.Error("Expected both to decrypt to same plaintext")
	}
}
