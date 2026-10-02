package main

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tool's own output must pass the check `ghost update` applies, and must
// stop passing the moment anything is altered.
func TestRoundTripAndTamper(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if err := keygen([]string{key}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(key); st.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode %v, want 0600", st.Mode().Perm())
	}
	if err := keygen([]string{key}); err == nil {
		t.Fatal("keygen overwrote an existing signing key")
	}
	pub := publicOf(t, key)

	bin := filepath.Join(dir, "ghost_linux_arm64")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := checksums([]string{sums, bin}); err != nil {
		t.Fatal(err)
	}
	if err := sign([]string{key, sums}); err != nil {
		t.Fatal(err)
	}
	if err := verify([]string{sums, pub, bin}); err != nil {
		t.Fatalf("a freshly signed release failed its own check: %v", err)
	}

	if err := os.WriteFile(bin, []byte("binary!"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verify([]string{sums, pub, bin}); err == nil {
		t.Fatal("a tampered binary verified")
	}
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sums, []byte(strings.Repeat("0", 64)+"  ghost_linux_arm64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify([]string{sums, pub, bin}); err == nil {
		t.Fatal("a checksum list changed after signing verified")
	}
}

func TestSignRefusesAKeyOthersCanRead(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if err := keygen([]string{key}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "checksums.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	if err := sign([]string{key, f}); err == nil || !strings.Contains(err.Error(), "readable by others") {
		t.Fatalf("signed with a world-readable key: %v", err)
	}
}

func TestVerifyFailsWithoutASignature(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if err := keygen([]string{key}); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "ghost_linux_arm64")
	os.WriteFile(bin, []byte("b"), 0o755)
	sums := filepath.Join(dir, "checksums.txt")
	checksums([]string{sums, bin})
	if err := verify([]string{sums, publicOf(t, key), bin}); err == nil {
		t.Fatal("an unsigned file verified")
	}
}

func publicOf(t *testing.T, keyFile string) string {
	t.Helper()
	k, err := loadKey(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	return hexOf(k.Public().(ed25519.PublicKey))
}
