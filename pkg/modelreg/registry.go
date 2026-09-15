// Package modelreg treats models as downloadable capabilities: manifest
// parsing, version compatibility, integrity verification, deprecation and
// rollback semantics. No network I/O lives here.
package modelreg

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ManifestVersion is the current manifest schema version.
const ManifestVersion = 1

// Manifest describes one downloadable model artifact.
//
// Distribution trust: manifests fetched over HTTPS from the operator's
// distribution URL carry an optional Ed25519 Signature over (ID, Version,
// SHA256). When Signature is present, clients must verify it against the
// Ghost release public key before trusting SHA256. Unsigned manifests are
// HTTPS-only: the client pins the observed hash on first install and shows
// the model as "unverified publisher" until a signed manifest replaces it.
// SHA-256 alone proves integrity, not authenticity; the signature proves
// authenticity.
type Manifest struct {
	ID             string   `json:"id"`
	Version        string   `json:"version"`
	ManifestVers   int      `json:"manifest_version"`
	Role           string   `json:"role"` // language | embedding | vision | speech | reflex
	Capabilities   []string `json:"capabilities"`
	Runtime        string   `json:"runtime"` // mobile-local | ollama | cloud
	Format         string   `json:"format"`  // gguf | coreai | safetensors | api
	Quantization   string   `json:"quantization,omitempty"`
	SizeBytes      int64    `json:"size_bytes"`
	SizeEstimated  bool     `json:"size_estimated,omitempty"`
	SHA256         string   `json:"sha256"`
	Signature      string   `json:"signature,omitempty"` // hex ed25519 over signPayload
	Signer         string   `json:"signer,omitempty"`    // key id, e.g. ghost-release-1
	Platforms      []string `json:"platforms"`
	Archs          []string `json:"architectures"`
	MinOS          string   `json:"min_os,omitempty"`
	MinRAMMB       int64    `json:"minimum_ram_mb,omitempty"`
	RecRAMMB       int64    `json:"recommended_ram_mb,omitempty"`
	DownloadURL    string   `json:"download_url"`
	Deprecated     bool     `json:"deprecated,omitempty"`
	ReplacementID  string   `json:"replacement_id,omitempty"`
	MinRuntimeVers string   `json:"min_runtime_version,omitempty"`
}

// signPayload is the canonical signed content.
func (m Manifest) signPayload() string {
	return m.ID + "\x00" + m.Version + "\x00" + strings.ToLower(m.SHA256)
}

// VerifySignature checks the Ed25519 signature against a 32-byte public key.
func (m Manifest) VerifySignature(pubKey []byte) error {
	if m.Signature == "" {
		return errors.New("manifest is unsigned")
	}
	if len(pubKey) != 32 {
		return errors.New("invalid public key length")
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil || len(sig) != 64 {
		return errors.New("invalid signature encoding")
	}
	msg := []byte(m.signPayload())
	if !ed25519.Verify(ed25519.PublicKey(pubKey), msg, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}

// Validate checks manifest coherence (fields, not reachability).
func (m Manifest) Validate() error {
	if m.ManifestVers != ManifestVersion {
		return fmt.Errorf("unsupported manifest_version %d (want %d)", m.ManifestVers, ManifestVersion)
	}
	if m.ID == "" || m.Version == "" || m.Runtime == "" || m.Format == "" {
		return errors.New("id, version, runtime and format are required")
	}
	if m.SizeBytes <= 0 {
		return errors.New("size_bytes must be positive")
	}
	if m.DownloadURL == "" {
		return errors.New("download_url is required")
	}
	if !strings.HasPrefix(m.DownloadURL, "https://") {
		return errors.New("download_url must be https")
	}
	if len(m.SHA256) != 0 && len(m.SHA256) != 64 {
		return errors.New("sha256 must be empty (pin on install) or 64 hex chars")
	}
	if len(m.SHA256) == 64 {
		if _, err := hex.DecodeString(m.SHA256); err != nil {
			return fmt.Errorf("invalid sha256: %w", err)
		}
	}
	if len(m.Platforms) == 0 {
		return errors.New("platforms must list at least one platform")
	}
	return nil
}

// VerifyFile streams path and compares its SHA-256 against the manifest.
// SizeBytes is an estimate used for storage planning, not a security gate:
// the hash is authoritative. It never loads the whole model into memory.
func (m Manifest) VerifyFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if m.SHA256 == "" {
		return errors.New("no pinned hash for manifest; refusing to verify")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, m.SHA256) {
		return errors.New("sha256 mismatch: artifact corrupt or tampered")
	}
	return nil
}

// Registry is the client-side catalog: known manifests plus installed state.
type Registry struct {
	Models    []Manifest
	installed map[string]string // model id -> installed version
}

// New builds a registry from manifests (validated).
func New(models []Manifest) (*Registry, error) {
	for _, m := range models {
		if err := m.Validate(); err != nil {
			return nil, fmt.Errorf("model %q: %w", m.ID, err)
		}
	}
	return &Registry{Models: models, installed: map[string]string{}}, nil
}

// Find returns the manifest for id, or false.
func (r *Registry) Find(id string) (Manifest, bool) {
	for _, m := range r.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Manifest{}, false
}

// MarkInstalled records an activated version (post-verify, post-atomic-rename).
func (r *Registry) MarkInstalled(id, version string) {
	if r.installed == nil {
		r.installed = map[string]string{}
	}
	r.installed[id] = version
}

// InstalledVersion reports the active version, if any.
func (r *Registry) InstalledVersion(id string) (string, bool) {
	v, ok := r.installed[id]
	return v, ok
}

// NeedsUpdate reports whether a newer catalog version exists. Rollback is
// supported because the previous artifact is kept until the new one verifies
// and activates (staging + verify + activate; see mobile ModelManager).
func (r *Registry) NeedsUpdate(id string) (Manifest, bool) {
	m, ok := r.Find(id)
	if !ok {
		return Manifest{}, false
	}
	if cur, found := r.installed[id]; found && cur == m.Version {
		return Manifest{}, false
	}
	if m.Deprecated && m.ReplacementID != "" {
		if rep, ok := r.Find(m.ReplacementID); ok {
			return rep, true
		}
	}
	_, found := r.installed[id]
	return m, found
}
