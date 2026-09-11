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
// Key hierarchy (first hit wins):
//  1. GHOST_MASTER_KEY env — passphrase or base64/hex key. The raw value
//     is hashed with SHA-256, so any string works (containers, tests).
//  2. <dir>/.master-key — 32 random bytes (base64), 0600, generated once
//     on first seal next to the secrets file it protects.
//
// A stolen disk image without the key file (or env) yields nothing: the
// secrets file alone is opaque. This replaces the old 0600-perms-only
// defense, which any backup, export, or mis-set permission defeated.
var vaultMagic = []byte("GVS1")

const MasterKeyFileName = ".master-key"

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

// MasterKeyFor loads the master key protecting secretsPath, generating and
// persisting one on first use. The key file is 0600 in a 0700 dir.
func MasterKeyFor(secretsPath string) ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("GHOST_MASTER_KEY")); v != "" {
		if raw, err := base64.StdEncoding.DecodeString(v); err == nil && len(raw) == 32 {
			return raw, nil
		}
		sum := sha256.Sum256([]byte(v))
		return sum[:], nil
	}
	keyPath := MasterKeyPath(secretsPath)
	if raw, err := os.ReadFile(keyPath); err == nil {
		key, err := decodeMasterKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("vault: corrupt master key %s: %w", keyPath, err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("vault: read master key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("vault: generate master key: %w", err)
	}
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
