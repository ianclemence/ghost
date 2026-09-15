package modelreg

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func sha256Of(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func testManifest() Manifest {
	return Manifest{
		ID: "ghost-mini-1", Version: "1.0.0", ManifestVers: ManifestVersion,
		Role: "language", Capabilities: []string{"chat", "tool_calling", "structured_output"},
		Runtime: "mobile-local", Format: "gguf", Quantization: "Q4_K_M",
		SizeBytes: 100, SHA256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		Platforms: []string{"android"}, Archs: []string{"arm64"},
		MinRAMMB: 4000, RecRAMMB: 6000,
		DownloadURL: "https://models.example.com/ghost-mini-1.gguf",
	}
}

func TestValidate(t *testing.T) {
	if err := testManifest().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := testManifest()
	bad.DownloadURL = "http://insecure.example.com/x"
	if err := bad.Validate(); err == nil {
		t.Fatal("want https enforcement")
	}
	bad2 := testManifest()
	bad2.ManifestVers = 999
	if err := bad2.Validate(); err == nil {
		t.Fatal("want manifest version check")
	}
}

func TestSignatureRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := testManifest()
	sig := ed25519.Sign(priv, []byte(m.signPayload()))
	m.Signature = hex.EncodeToString(sig)
	m.Signer = "test-key-1"
	if err := m.VerifySignature(pub); err != nil {
		t.Fatal(err)
	}
	m.Version = "9.9.9" // tamper: signed content changed
	if err := m.VerifySignature(pub); err == nil {
		t.Fatal("want signature failure after tampering")
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "model.bin")
	content := []byte("ghost-test-artifact")
	if err := os.WriteFile(p, content, 0600); err != nil {
		t.Fatal(err)
	}
	m := testManifest()
	m.SizeBytes = int64(len(content))
	m.SizeEstimated = false
	// pin the real hash, like ModelManager does after a verified download
	h := sha256Of(content)
	m.SHA256 = h
	if err := m.VerifyFile(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyFile(p); err == nil {
		t.Fatal("want mismatch on tampered file")
	}
}
func TestRegistryUpdateRollback(t *testing.T) {
	r, err := New([]Manifest{testManifest()})
	if err != nil {
		t.Fatal(err)
	}
	r.MarkInstalled("ghost-mini-1", "1.0.0")
	if _, need := r.NeedsUpdate("ghost-mini-1"); need {
		t.Fatal("same version must not need update")
	}
	r2, _ := New([]Manifest{func() Manifest { m := testManifest(); m.Version = "1.1.0"; return m }()})
	r2.MarkInstalled("ghost-mini-1", "1.0.0")
	next, need := r2.NeedsUpdate("ghost-mini-1")
	if !need || next.Version != "1.1.0" {
		t.Fatalf("want update to 1.1.0, got %+v %v", next, need)
	}
}
