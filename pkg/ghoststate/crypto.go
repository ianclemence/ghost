package ghoststate

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/scrypt"
)

// Archive header layout (fixed width, big-endian where applicable):
//
//	0  magic   4 bytes  "GST1"
//	4  version 1 byte    0x01
//	5  kdf     1 byte    0x01 = scrypt(N=1<<15, r=8, p=1)
//	6  salt    16 bytes
//	22 nonce   12 bytes
//	34 ciphertext  rest
const (
	archiveMagic    = "GST1"
	archiveVersion  = 0x01
	kdfScrypt       = 0x01
	saltLen         = 16
	nonceLen        = 12
	headerLen       = 34
	scryptN         = 1 << 15
	scryptR         = 8
	scryptP         = 1
	scryptKeyLength = 32
)

var errBadArchive = errors.New("not a valid Ghost State archive")

func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, scryptKeyLength)
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return b, nil
}

// encryptBytes seals plaintext into a versioned .ghost payload. A random salt
// and nonce are generated per archive, so re-encrypting the same plaintext
// never produces the same bytes.
func encryptBytes(plain []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("a passphrase is required to encrypt a Ghost State archive")
	}
	salt, err := randomBytes(saltLen)
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(nonceLen)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}

	out := make([]byte, headerLen, headerLen+len(plain)+gcm.Overhead())
	copy(out[0:4], archiveMagic)
	out[4] = archiveVersion
	out[5] = kdfScrypt
	copy(out[6:6+saltLen], salt)
	copy(out[22:22+nonceLen], nonce)

	ciphertext := gcm.Seal(nil, nonce, plain, out[:6])
	out = append(out, ciphertext...)
	return out, nil
}

// decryptBytes opens a versioned .ghost payload and returns the plaintext.
func decryptBytes(blob []byte, passphrase string) ([]byte, error) {
	if len(blob) < headerLen {
		return nil, errBadArchive
	}
	if string(blob[0:4]) != archiveMagic {
		return nil, errBadArchive
	}
	if blob[4] != archiveVersion {
		return nil, fmt.Errorf("unsupported archive version %d", blob[4])
	}
	if blob[5] != kdfScrypt {
		return nil, fmt.Errorf("unsupported key derivation %d", blob[5])
	}
	salt := blob[6 : 6+saltLen]
	nonce := blob[22 : 22+nonceLen]

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}

	plain, err := gcm.Open(nil, nonce, blob[headerLen:], blob[:6])
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong passphrase or corrupt archive): %w", err)
	}
	return plain, nil
}
