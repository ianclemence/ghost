package connector

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Connector signing: a maintainer signs a manifest's content hash with an
// ed25519 key. The signature covers the manifest with the provenance hash and
// signature cleared, so signing is stable and tampering with any other field
// invalidates it. A directory can pin the public keys it trusts.

// GenerateSigningKey returns a fresh ed25519 keypair.
func GenerateSigningKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// EncodePrivateKey / EncodePublicKey render keys as hex for storage.
func EncodePrivateKey(k ed25519.PrivateKey) string { return hex.EncodeToString(k) }
func EncodePublicKey(k ed25519.PublicKey) string   { return hex.EncodeToString(k) }

// DecodePrivateKey / DecodePublicKey parse hex-encoded keys.
func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key")
	}
	return ed25519.PrivateKey(b), nil
}

func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

// Sign computes the manifest content hash and attaches an ed25519 signature
// over it, recording the signer.
func Sign(m *Manifest, priv ed25519.PrivateKey, signedBy string) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key")
	}
	if strings.TrimSpace(signedBy) == "" {
		return fmt.Errorf("signer name is required")
	}
	hash, err := manifestHash(m)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, []byte(hash))
	m.Provenance.Hash = hash
	m.Provenance.SignedBy = strings.TrimSpace(signedBy)
	m.Provenance.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// VerifySignature checks the manifest's signature and content hash against a
// public key. Any tampering with the manifest (other than the provenance hash
// and signature themselves) fails.
func VerifySignature(m *Manifest, pub ed25519.PublicKey) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	if m.Provenance.Signature == "" {
		return fmt.Errorf("connector is not signed")
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key")
	}
	hash, err := manifestHash(m)
	if err != nil {
		return err
	}
	if m.Provenance.Hash != "" && m.Provenance.Hash != hash {
		return fmt.Errorf("content hash mismatch")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Provenance.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature encoding")
	}
	if !ed25519.Verify(pub, []byte(hash), sig) {
		return fmt.Errorf("signature does not verify")
	}
	return nil
}
