package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Vault file format: magic "GVS1" + 12-byte nonce + AES-256-GCM ciphertext.
// The magic prefix lets readers distinguish sealed files from legacy
// plaintext JSON and upgrade them on load.
//
// Key hierarchy (first hit wins, read-only unless stated):
//  1. GHOST_MASTER_KEY env — passphrase or base64/hex key. The raw value
//     is hashed with SHA-256, so any string works (containers, tests).
//  2. <dir>/.master-env — KEY=VALUE env-file format written by
//     vault-rotate as a root-only EnvironmentFile. This is the personal AI
//     key store; it wins over legacy files so a rotation takes effect
//     even when an older key file lingers beside it.
//  3. <dir>/.master-key — 32 random bytes (base64), 0600, generated once
//     on first seal next to the secrets file it protects (legacy).
//
// ResolveMasterKey implements exactly this precedence and never writes:
// planning, diagnostic, and read paths must be side-effect free. Key
// generation happens only in EnsureMasterKey, used by onboarding and
// rotation (write paths).
//
// A stolen disk image without the key file (or env) yields nothing: the
// secrets file alone is opaque. This replaces the old 0600-perms-only
// defense, which any backup, export, or mis-set permission defeated.
var vaultMagic = []byte("GVS1")

const MasterKeyFileName = ".master-key"

// ApplianceKeyFileName is the rotation-installed key store next to the
// secrets file: a KEY=VALUE env file holding GHOST_MASTER_KEY.
const ApplianceKeyFileName = ".master-env"

// masterKeyFileName is the on-disk name (kept as an alias for brevity).
const masterKeyFileName = MasterKeyFileName

// MasterKeyPath returns the sibling key-file path for a secrets file.
func MasterKeyPath(secretsPath string) string {
	return filepath.Join(filepath.Dir(secretsPath), masterKeyFileName)
}

// IsSealed reports whether blob is a vault-sealed file.
func IsSealed(blob []byte) bool {
	return len(blob) >= len(vaultMagic)+12 && string(blob[:len(vaultMagic)]) == string(vaultMagic)
}

// Seal encrypts plaintext with key. Output is vault-format bytes.
func Seal(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("vault nonce: %w", err)
	}
	out := make([]byte, 0, len(vaultMagic)+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, vaultMagic...)
	out = append(out, nonce...)
	out = append(out, gcm.Seal(nil, nonce, plaintext, nil)...)
	return out, nil
}

// Unseal decrypts vault-format bytes. Tampered or wrong-key blobs fail
// loudly — never return partial secrets.
func Unseal(key, blob []byte) ([]byte, error) {
	if !IsSealed(blob) {
		return nil, fmt.Errorf("vault: not a sealed file")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault gcm: %w", err)
	}
	nonce := blob[len(vaultMagic) : len(vaultMagic)+gcm.NonceSize()]
	ct := blob[len(vaultMagic)+gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: authentication failed — wrong master key or tampered file (set GHOST_MASTER_KEY to the backup key)")
	}
	return plain, nil
}

// ResolveMasterKey returns the master key protecting secretsPath using
// the documented hierarchy (env → .master-env → .master-key). It is
// strictly read-only: a missing key is a hard error, never a generated
// one. Planning, diagnostic, export, and unlock paths must use this — a
// migrator that mints keys as a side effect strands vaults under keys
// nobody holds.
func ResolveMasterKey(secretsPath string) ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("GHOST_MASTER_KEY")); v != "" {
		return deriveKey(v), nil
	}
	dir := filepath.Dir(secretsPath)
	if raw, err := os.ReadFile(filepath.Join(dir, ApplianceKeyFileName)); err == nil {
		if v := envFileValue(raw, "GHOST_MASTER_KEY"); v != "" {
			return deriveKey(v), nil
		}
		// Present but unusable: say so instead of falling through to an
		// older key that would fail auth with a misleading error.
		return nil, fmt.Errorf("vault: %s holds no GHOST_MASTER_KEY entry", filepath.Join(dir, ApplianceKeyFileName))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("vault: read key file: %w", err)
	}
	keyPath := MasterKeyPath(secretsPath)
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("vault: no master key for %s (set GHOST_MASTER_KEY to the backup key)", secretsPath)
		}
		return nil, fmt.Errorf("vault: read master key: %w", err)
	}
	key, err := decodeMasterKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("vault: corrupt master key %s: %w", keyPath, err)
	}
	return key, nil
}

// deriveKey maps an operator-supplied passphrase to a 32-byte key:
// raw base64 32-byte values pass through, anything else is hashed.
func deriveKey(v string) []byte {
	if raw, err := base64.StdEncoding.DecodeString(v); err == nil && len(raw) == 32 {
		return raw
	}
	sum := sha256.Sum256([]byte(v))
	return sum[:]
}

// envFileValue extracts one KEY=value from env-file bytes, skipping
// blanks, comments, and an optional leading "export ". Quotes are
// stripped; the first match wins.
func envFileValue(data []byte, key string) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		return value
	}
	return ""
}

// EnsureMasterKey loads the master key protecting secretsPath, generating
// and persisting a legacy .master-key on first use. For onboarding and
// seal paths only — never for reads. The key file is 0600 in a 0700 dir.
func EnsureMasterKey(secretsPath string) ([]byte, error) {
	if key, err := ResolveMasterKey(secretsPath); err == nil {
		return key, nil
	} else if !isNoKeyError(err) {
		// Present-but-corrupt (or unreadable) key material must fail
		// loudly instead of being silently replaced by a fresh key
		// that would strand the existing vault.
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("vault: generate master key: %w", err)
	}
	keyPath := MasterKeyPath(secretsPath)
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("vault: create key dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".master-key-*")
	if err != nil {
		return nil, fmt.Errorf("vault: create temp key file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("vault: write master key: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("vault: chmod master key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("vault: close master key: %w", err)
	}
	if err := os.Rename(tmpName, keyPath); err != nil {
		return nil, fmt.Errorf("vault: install master key: %w", err)
	}
	return key, nil
}

// LoadKeyFile reads and decodes a single key file: no env lookup, no
// search, no generation. For rotation checks and forensics, where the
// question is "what does THIS file hold", not "what would resolve".
func LoadKeyFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := decodeMasterKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("vault: corrupt master key %s: %w", path, err)
	}
	return key, nil
}

// isNoKeyError reports the "no key material anywhere" outcome — the only
// case where generation is justified.
func isNoKeyError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "vault: no master key for ")
}

// MasterKeyFor is the historical entry point, kept for compatibility.
// Read paths should use ResolveMasterKey (never generates); seal paths
// should use EnsureMasterKey.
func MasterKeyFor(secretsPath string) ([]byte, error) {
	return EnsureMasterKey(secretsPath)
}

func decodeMasterKey(s string) ([]byte, error) {
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return raw, nil
	}
	fields := strings.Fields(s)
	if len(fields) == 1 && len(fields[0]) >= 16 {
		sum := sha256.Sum256([]byte(fields[0]))
		return sum[:], nil
	}
	return nil, fmt.Errorf("unrecognized master key format")
}
