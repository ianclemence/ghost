package appliance

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func signedFile(t *testing.T, content string) (path, sig string, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := SHA256File(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := hex.DecodeString(sum)
	return path, hex.EncodeToString(ed25519.Sign(priv, digest)), pub
}

func TestVerifyDetachedAcceptsAGoodSignature(t *testing.T) {
	path, sig, pub := signedFile(t, "abc  ghost_linux_arm64\n")
	if err := VerifyDetached(path, sig, []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
}

// The point of the strict verifier: nothing missing is ever "fine".
func TestVerifyDetachedFailsClosed(t *testing.T) {
	path, sig, pub := signedFile(t, "abc  ghost_linux_arm64\n")
	keys := []ed25519.PublicKey{pub}
	other, _, _ := ed25519.GenerateKey(rand.Reader)

	if err := VerifyDetached(path, sig, nil); !errors.Is(err, ErrNoTrustedKeys) {
		t.Errorf("no trusted keys: got %v", err)
	}
	for _, empty := range []string{"", "  \n"} {
		if err := VerifyDetached(path, empty, keys); !errors.Is(err, ErrUnsigned) {
			t.Errorf("empty signature %q: got %v", empty, err)
		}
	}
	bad := map[string]func() error{
		"signature from another key": func() error { return VerifyDetached(path, sig, []ed25519.PublicKey{other}) },
		"malformed signature":        func() error { return VerifyDetached(path, "zz", keys) },
		"short signature":            func() error { return VerifyDetached(path, "abcd", keys) },
	}
	for name, call := range bad {
		if err := call(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestVerifyDetachedRejectsAChangedFile(t *testing.T) {
	path, sig, pub := signedFile(t, "abc  ghost_linux_arm64\n")
	if err := os.WriteFile(path, []byte("evil  ghost_linux_arm64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDetached(path, sig, []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("a checksum list changed after signing must not verify")
	}
}

func TestVerifyDetachedAcceptsAnyTrustedKeyForRotation(t *testing.T) {
	path, sig, pub := signedFile(t, "x  y\n")
	old, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifyDetached(path, sig, []ed25519.PublicKey{old, pub}); err != nil {
		t.Fatalf("a release signed by the second listed key was refused: %v", err)
	}
}

func TestVerifyChecksum(t *testing.T) {
	f := filepath.Join(t.TempDir(), "ghost_linux_arm64")
	os.WriteFile(f, []byte("binary"), 0o755)
	sum, _ := SHA256File(f)
	if err := VerifyChecksum(f, sum); err != nil {
		t.Fatalf("matching checksum refused: %v", err)
	}
	if err := VerifyChecksum(f, strings.ToUpper(sum)); err != nil {
		t.Errorf("checksum comparison must ignore case: %v", err)
	}
	if err := VerifyChecksum(f, strings.Repeat("0", 64)); err == nil {
		t.Error("a wrong checksum was accepted")
	}
	if err := VerifyChecksum(f, ""); !errors.Is(err, ErrNoChecksum) {
		t.Errorf("a binary with no listed checksum must be refused: %v", err)
	}
}

func TestParseTrustedKeys(t *testing.T) {
	a, _, _ := ed25519.GenerateKey(rand.Reader)
	b, _, _ := ed25519.GenerateKey(rand.Reader)
	in := hex.EncodeToString(a) + ",\n" + base64.StdEncoding.EncodeToString(b)
	keys, err := ParseTrustedKeys(in)
	if err != nil || len(keys) != 2 || !keys[0].Equal(a) || !keys[1].Equal(b) {
		t.Fatalf("got %v, %v", keys, err)
	}
	if keys, err := ParseTrustedKeys(""); err != nil || len(keys) != 0 {
		t.Errorf("empty input: %v, %v", keys, err)
	}
	if _, err := ParseTrustedKeys("not-a-key"); err == nil {
		t.Error("a malformed key must be an error, not skipped")
	}
}

func TestParseChecksumsAndAssetLookup(t *testing.T) {
	sums := ParseChecksums("ABC123  ghost_linux_arm64\n" + strings.Repeat("f", 64) + " *ghost-web_linux_arm64\n\nbroken line here now\n")
	if sums["ghost_linux_arm64"] != "abc123" || sums["ghost-web_linux_arm64"] != strings.Repeat("f", 64) {
		t.Fatalf("got %v", sums)
	}
	rel := &Release{Assets: []Asset{{Name: "ghost_linux_arm64_other"}, {Name: "ghost_linux_arm64"}}}
	if a, ok := rel.FindAsset("ghost_linux_arm64"); !ok || a.Name != "ghost_linux_arm64" {
		t.Errorf("exact lookup returned %v %v (a name that merely contains it must not match)", a, ok)
	}
	if _, ok := rel.FindAsset("ghost_linux"); ok {
		t.Error("a partial name matched")
	}
	if BinaryAssetName("ghost-web", "linux", "amd64") != "ghost-web_linux_amd64" || ChecksumsSigAssetName != ChecksumsAssetName+".sig" {
		t.Error("asset naming drifted")
	}
	// Older updaters pick a binary by a substring of its name. Nothing a
	// release publishes besides the binary may contain it.
	for _, name := range []string{ChecksumsAssetName, ChecksumsSigAssetName, BinaryAssetName("ghost-web", "linux", "arm64")} {
		if strings.Contains(strings.ToLower(name), "ghost_linux_arm64") {
			t.Errorf("%s contains a binary's name; an older updater could mistake it for the binary", name)
		}
	}
}
