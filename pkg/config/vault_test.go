package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plain := []byte(`{"provider_api_keys":{"openai":"sk-xxx"}}`)
	sealed, err := Seal(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(sealed) {
		t.Fatal("sealed output must carry the magic prefix")
	}
	if strings.Contains(string(sealed), "sk-xxx") {
		t.Fatal("sealed output must not contain plaintext secrets")
	}
	back, err := Unseal(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(plain) {
		t.Fatalf("roundtrip mismatch: %q", back)
	}
}

func TestVaultRejectsWrongKeyAndTamper(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := Seal(key, []byte(`{"a":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrong := make([]byte, 32)
	if _, err := Unseal(wrong, sealed); err == nil {
		t.Fatal("wrong key must fail loudly")
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := Unseal(key, sealed); err == nil {
		t.Fatal("tampered blob must fail loudly")
	}
	if _, err := Unseal(key, []byte(`{"plain":"json"}`)); err == nil {
		t.Fatal("plaintext must not unseal")
	}
}

func TestMasterKeyFileGeneratedOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "")
	k1, err := MasterKeyFor(filepath.Join(dir, ".secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 32 {
		t.Fatalf("key length = %d, want 32", len(k1))
	}
	info, err := os.Stat(filepath.Join(dir, ".master-key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("key file perms = %o, want 600", info.Mode().Perm())
	}
	k2, err := MasterKeyFor(filepath.Join(dir, ".secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) {
		t.Fatal("key must be stable across loads")
	}
}

func TestMasterKeyEnvPassphrase(t *testing.T) {
	t.Setenv("GHOST_MASTER_KEY", "test-passphrase-for-unit-tests")
	k1, err := MasterKeyFor("/nonexistent/.secrets.json")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := MasterKeyFor("/nonexistent/.secrets.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) || len(k1) != 32 {
		t.Fatal("env passphrase must derive a stable 32-byte key")
	}
}

func TestSecretsSealedAtRestAndMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "")
	path := filepath.Join(dir, ".secrets.json")

	s := &Secrets{ProviderAPIKeys: map[string]string{"openai": "sk-live"}, TelegramToken: "tok"}
	if err := SaveSecrets(path, s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(raw) {
		t.Fatal("saved secrets must be sealed")
	}
	if strings.Contains(string(raw), "sk-live") {
		t.Fatal("secrets file must be opaque at rest")
	}
	got, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderAPIKeys["openai"] != "sk-live" || got.TelegramToken != "tok" {
		t.Fatalf("sealed load mismatch: %+v", got)
	}

	// Legacy plaintext file upgrades to sealed form on load.
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"telegram_token":"oldtok"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadSecrets(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got.TelegramToken != "oldtok" {
		t.Fatalf("legacy load mismatch: %+v", got)
	}
	raw, _ = os.ReadFile(legacy)
	if !IsSealed(raw) {
		t.Fatal("legacy file must be upgraded to sealed form on load")
	}
	if strings.Contains(string(raw), "oldtok") {
		t.Fatal("upgraded file must be opaque")
	}
}

func TestSecretsWrongKeyFailsLoud(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "correct-key-for-this-test-1234")
	path := filepath.Join(dir, ".secrets.json")
	if err := SaveSecrets(path, &Secrets{TelegramToken: "x"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a fresh machine without the key file: point at a new dir.
	dir2 := t.TempDir()
	raw, _ := os.ReadFile(path)
	path2 := filepath.Join(dir2, ".secrets.json")
	if err := os.WriteFile(path2, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHOST_MASTER_KEY", "wrong-key-for-this-test-12345678")
	if _, err := LoadSecrets(path2); err == nil {
		t.Fatal("wrong master key must fail loudly, never silently boot keyless")
	}
}
