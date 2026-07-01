package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// EncPrefix marks an encrypted credential value.
	EncPrefix = "enc://"
	// FilePrefix marks a file-referenced credential value.
	FilePrefix = "file://"
	// KeyLen is the length of the derived encryption key.
	KeyLen = 32
	// SaltLen is the length of the random salt.
	SaltLen = 16
	// NonceLen is the length of the AES-GCM nonce.
	NonceLen = 12
	// Iterations for PBKDF2 key derivation.
	Iterations = 100000
)

// PassphraseProvider is a function that returns the current passphrase.
type PassphraseProvider func() string

// SecureStore manages passphrase resolution with thread-safe updates.
type SecureStore struct {
	passphrase atomicValue
}

type atomicValue struct {
	value string
	mu    sync.RWMutex
}

func (a *atomicValue) Load() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.value
}

func (a *atomicValue) Store(v string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.value = v
}

// NewSecureStore creates a new SecureStore.
func NewSecureStore() *SecureStore {
	return &SecureStore{}
}

// SetPassphrase sets the encryption passphrase.
func (s *SecureStore) SetPassphrase(passphrase string) {
	s.passphrase.Store(passphrase)
}

// GetPassphrase returns the current passphrase.
func (s *SecureStore) GetPassphrase() string {
	return s.passphrase.Load()
}

// Resolver resolves credential values from various formats.
type Resolver struct {
	store         *SecureStore
	configDir     string
	override      PassphraseProvider
}

// NewResolver creates a new credential Resolver.
func NewResolver(configDir string) *Resolver {
	return &Resolver{
		store:     NewSecureStore(),
		configDir: configDir,
	}
}

// SetPassphrase sets the encryption passphrase.
func (r *Resolver) SetPassphrase(passphrase string) {
	r.store.SetPassphrase(passphrase)
}

// SetPassphraseProvider sets a custom passphrase provider.
func (r *Resolver) SetPassphraseProvider(provider PassphraseProvider) {
	r.override = provider
}

// Resolve resolves a credential value.
// - Plaintext values are returned as-is
// - "enc://..." values are decrypted
// - "file://..." values are read from a file
func (r *Resolver) Resolve(value string) (string, error) {
	if value == "" {
		return "", nil
	}

	if strings.HasPrefix(value, EncPrefix) {
		return r.resolveEncrypted(value)
	}

	if strings.HasPrefix(value, FilePrefix) {
		return r.resolveFileReference(value)
	}

	return value, nil
}

// resolveEncrypted decrypts an "enc://..." value.
func (r *Resolver) resolveEncrypted(value string) (string, error) {
	encoded := strings.TrimPrefix(value, EncPrefix)
	if encoded == "" {
		return "", fmt.Errorf("empty encrypted value")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	passphrase := r.getPassphrase()
	if passphrase == "" {
		return "", fmt.Errorf("no passphrase configured for decryption")
	}

	plaintext, err := Decrypt(ciphertext, passphrase)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// resolveFileReference reads a credential from a file.
func (r *Resolver) resolveFileReference(value string) (string, error) {
	filename := strings.TrimPrefix(value, FilePrefix)
	if filename == "" {
		return "", fmt.Errorf("empty file reference")
	}

	// Resolve path relative to config directory
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(r.configDir, filename)
	}

	// Path traversal protection
	cleaned := filepath.Clean(filename)
	configClean := filepath.Clean(r.configDir)
	if !strings.HasPrefix(cleaned, configClean) && !isInAllowedDir(cleaned) {
		return "", fmt.Errorf("path traversal detected: %s", filename)
	}

	data, err := os.ReadFile(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to read credential file: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// getPassphrase returns the active passphrase.
func (r *Resolver) getPassphrase() string {
	if r.override != nil {
		return r.override()
	}
	return r.store.GetPassphrase()
}

// isInAllowedDir checks if a path is in an allowed directory.
func isInAllowedDir(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	sshDir := filepath.Join(home, ".ssh")
	return strings.HasPrefix(filepath.Clean(path), filepath.Clean(sshDir))
}

// Encrypt encrypts plaintext using AES-256-GCM with PBKDF2-derived key.
// Returns: salt (16 bytes) || nonce (12 bytes) || ciphertext
func Encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase cannot be empty")
	}

	// Generate random salt
	salt := make([]byte, SaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key using PBKDF2
	key := pbkdf2.Key([]byte(passphrase), salt, Iterations, KeyLen, sha256.New)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, NonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := aesGCM.Seal(nil, nonce, plaintext, nil)

	// Combine: salt || nonce || ciphertext
	result := make([]byte, 0, SaltLen+NonceLen+len(ciphertext))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
func Decrypt(data []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase cannot be empty")
	}

	if len(data) < SaltLen+NonceLen {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// Extract salt, nonce, ciphertext
	salt := data[:SaltLen]
	nonce := data[SaltLen : SaltLen+NonceLen]
	ciphertext := data[SaltLen+NonceLen:]

	// Derive key using PBKDF2
	key := pbkdf2.Key([]byte(passphrase), salt, Iterations, KeyLen, sha256.New)

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// EncryptValue encrypts a string value and returns the "enc://..." format.
func EncryptValue(value, passphrase string) (string, error) {
	encrypted, err := Encrypt([]byte(value), passphrase)
	if err != nil {
		return "", err
	}
	return EncPrefix + base64.StdEncoding.EncodeToString(encrypted), nil
}

// IsEncrypted checks if a value is an encrypted credential.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, EncPrefix)
}

// IsFileReference checks if a value is a file reference.
func IsFileReference(value string) bool {
	return strings.HasPrefix(value, FilePrefix)
}
