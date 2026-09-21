package appliance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		cand, cur string
		want      bool
	}{
		{"v0.24.0", "v0.23.45", true},
		{"v0.23.45", "v0.23.45", false},
		{"v0.23.46", "v0.23.45", true},
		{"v0.22.0", "v0.23.45", false},
		{"v1.0.0", "v0.23.45", true},
		{"dev", "v0.23.45", false},
		{"v0.23.45", "dev", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.cand, c.cur); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", c.cand, c.cur, got, c.want)
		}
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "ghost_linux_arm64")
	if err := os.WriteFile(f, []byte("ghost"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("ghost"))
	hexSum := hex.EncodeToString(sum[:])
	if err := VerifySHA256(f, hexSum); err != nil {
		t.Fatalf("verify should pass: %v", err)
	}
	if err := VerifySHA256(f, "deadbeef"); err == nil {
		t.Fatal("verify should fail on a bad digest")
	}
	if err := VerifySHA256(f, ""); err != nil {
		t.Fatalf("empty digest should skip: %v", err)
	}
}

func TestVerifyEd25519(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "ghost")
	if err := os.WriteFile(f, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("payload"))
	sig := ed25519.Sign(priv, sum[:])
	if err := VerifyEd25519(f, hex.EncodeToString(pub), hex.EncodeToString(sig)); err != nil {
		t.Fatalf("valid signature should verify: %v", err)
	}
	// Tamper with the file.
	if err := os.WriteFile(f, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEd25519(f, hex.EncodeToString(pub), hex.EncodeToString(sig)); err == nil {
		t.Fatal("tampered payload must fail verification")
	}
	// Unsigned release is a no-op.
	if err := VerifyEd25519(f, "", ""); err != nil {
		t.Fatalf("unsigned should skip: %v", err)
	}
}

func TestAtomicInstall(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "bin", "ghost")
	if err := os.WriteFile(src, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicInstall(src, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "bin" {
		t.Fatalf("content wrong: %q", b)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("not executable: %v", fi.Mode())
	}
	if _, err := os.Stat(dst + ".new"); !os.IsNotExist(err) {
		t.Fatal("temp .new must not remain")
	}
}
