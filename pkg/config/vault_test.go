package config

import (
	"encoding/base64"
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

// TestResolvePrecedence pins the unified hierarchy: env wins over the
// appliance key file, which wins over the legacy sibling key file.
func TestResolvePrecedence(t *testing.T) {
	dir := t.TempDir()
	secrets := filepath.Join(dir, ".secrets.json")
	// Legacy key first: 32 fixed bytes so we can distinguish it later.
	legacyRaw := make([]byte, 32)
	for i := range legacyRaw {
		legacyRaw[i] = byte(200 + i)
	}
	legacy, err := func() ([]byte, error) {
		if err := os.WriteFile(filepath.Join(dir, ".master-key"), []byte(base64.StdEncoding.EncodeToString(legacyRaw)+"\n"), 0600); err != nil {
			return nil, err
		}
		return legacyRaw, nil
	}()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".master-env"), []byte("GHOST_MASTER_KEY=appliance-passphrase\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Without env, the appliance file wins over the legacy file. Prove
	// it by sealing under the appliance key and resolving without env.
	t.Setenv("GHOST_MASTER_KEY", "")
	got, err := ResolveMasterKey(secrets)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := Seal(got, []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unseal(deriveKey("appliance-passphrase"), sealed); err != nil {
		t.Fatal("appliance key file must win over legacy .master-key")
	}
	if string(got) == string(legacy) {
		t.Fatal("resolved key must be the appliance key, not the legacy one")
	}
	// Env wins over everything.
	t.Setenv("GHOST_MASTER_KEY", "env-passphrase")
	got, err = ResolveMasterKey(secrets)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(deriveKey("env-passphrase")) {
		t.Fatal("env must win over key files")
	}
}

// TestResolveNeverMints is the core regression test for the update-path
// bug: resolving a missing key must error without creating key files.
func TestResolveNeverMints(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "")
	if _, err := ResolveMasterKey(filepath.Join(dir, ".secrets.json")); err == nil {
		t.Fatal("missing key must be a hard error")
	} else if !isNoKeyError(err) {
		t.Fatalf("missing key must be a no-key error, got: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("resolve must not write anything, found %d entries", len(entries))
	}
}

// TestEnsureGeneratesOnlyWhenBare proves generation happens exactly when
// no key material exists anywhere.
func TestEnsureGeneratesOnlyWhenBare(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "")
	k, err := EnsureMasterKey(filepath.Join(dir, ".secrets.json"))
	if err != nil || len(k) != 32 {
		t.Fatalf("ensure must generate on bare dir: %v", err)
	}
	// A corrupt legacy file must fail loudly, never be replaced.
	if err := os.WriteFile(filepath.Join(dir, "c", ".master-key"), []byte("x"), 0600); err == nil {
		_ = err
	}
	cdir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "")
	if err := os.WriteFile(filepath.Join(cdir, ".master-key"), []byte("too-short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureMasterKey(filepath.Join(cdir, ".secrets.json")); err == nil {
		t.Fatal("corrupt key file must fail, not regenerate")
	}
}

// TestApplianceEnvWithoutEntryFailsClosed: a .master-env that holds no
// GHOST_MASTER_KEY entry must error, not silently fall through to an
// older key that would fail auth with a misleading message.
func TestApplianceEnvWithoutEntryFailsClosed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_MASTER_KEY", "")
	if err := os.WriteFile(filepath.Join(dir, ".master-env"), []byte("# rotated out\nOTHER=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveMasterKey(filepath.Join(dir, ".secrets.json")); err == nil {
		t.Fatal("entry-less .master-env must be a hard error")
	}
}

func TestEnvFileValue(t *testing.T) {
	data := []byte("# comment\n\nexport GHOST_MASTER_KEY=\"quoted value\"\nOTHER=1\nGHOST_MASTER_KEY=second\n")
	if got := envFileValue(data, "GHOST_MASTER_KEY"); got != "quoted value" {
		t.Fatalf("got %q, want first-match unquoted value", got)
	}
	if got := envFileValue([]byte("GHOST_MASTER_KEY=plain\n"), "GHOST_MASTER_KEY"); got != "plain" {
		t.Fatalf("got %q", got)
	}
	if got := envFileValue([]byte("OTHER=1\n"), "GHOST_MASTER_KEY"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
