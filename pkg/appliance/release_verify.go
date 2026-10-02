package appliance

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Strict release verification.
//
// VerifySHA256 and VerifyEd25519 above treat an empty checksum, key or
// signature as "nothing to check" and return success, which is right for an
// optional extra and wrong for the only thing standing between a release
// download and a root-owned binary. The functions here fail closed: a release
// is installable only when checksums.txt carries a signature that verifies
// under a key this binary already trusts, and every binary matches the checksum
// that signed list gives for it.

var (
	// ErrNoTrustedKeys means this build has no release key to check against.
	ErrNoTrustedKeys = errors.New("no release signing key is configured, so a downloaded release cannot be verified")
	// ErrUnsigned means the release carries no signature for the asset.
	ErrUnsigned = errors.New("release asset has no signature")
	// ErrNoChecksum means the release lists no checksum for the asset.
	ErrNoChecksum = errors.New("release lists no checksum for this asset")
)

// ParseTrustedKeys reads Ed25519 public keys written as hex or base64,
// separated by commas, spaces or newlines. Several may be listed so the
// signing key can be rotated: ship a release signed by the old key that
// trusts both, then switch.
func ParseTrustedKeys(s string) ([]ed25519.PublicKey, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	var out []ed25519.PublicKey
	for _, f := range fields {
		k, err := decodePublicKey(f)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func decodePublicKey(s string) (ed25519.PublicKey, error) {
	var raw []byte
	if len(s) == 2*ed25519.PublicKeySize {
		if b, err := hex.DecodeString(s); err == nil {
			raw = b
		}
	}
	if raw == nil {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, err := enc.DecodeString(s); err == nil {
				raw = b
				break
			}
		}
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%q is not an Ed25519 public key", shorten(s))
	}
	return ed25519.PublicKey(raw), nil
}

func shorten(s string) string {
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}

// VerifyDetached checks that sigHex is a valid signature, under one of keys,
// over the sha256 digest of the file at path. This is what authenticates a
// release: the signature covers checksums.txt, and checksums.txt then binds
// every binary (VerifyChecksum). Nothing missing is ever acceptable.
func VerifyDetached(path, sigHex string, keys []ed25519.PublicKey) error {
	if len(keys) == 0 {
		return ErrNoTrustedKeys
	}
	sigHex = strings.TrimSpace(sigHex)
	if sigHex == "" {
		return ErrUnsigned
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("the signature for %s is malformed", baseName(path))
	}
	sum, err := SHA256File(path)
	if err != nil {
		return err
	}
	digest, err := hex.DecodeString(sum)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, digest, sig) {
			return nil
		}
	}
	return fmt.Errorf("the signature for %s does not match a trusted key", baseName(path))
}

// VerifyChecksum checks the file at path against the checksum a (signed)
// checksum list gives for it. An empty checksum is an error, not a pass.
func VerifyChecksum(path, wantSHA string) error {
	wantSHA = strings.TrimSpace(wantSHA)
	if wantSHA == "" {
		return fmt.Errorf("%w: %s", ErrNoChecksum, baseName(path))
	}
	got, err := SHA256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, wantSHA) {
		return fmt.Errorf("checksum mismatch for %s: got %s, the signed list says %s", baseName(path), got, wantSHA)
	}
	return nil
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// ParseChecksums reads a sha256sum-style file ("<hex>  <name>") into
// name -> lowercase hex. Names may carry the binary-mode "*" prefix.
func ParseChecksums(data string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out
}

// FindAsset returns the asset with exactly this name.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// BinaryAssetName is the release file name for a binary on a platform:
// "ghost_linux_arm64", "ghost-web_linux_amd64". The release tool and the
// updater both use this, so they cannot drift apart.
func BinaryAssetName(binary, goos, goarch string) string {
	return binary + "_" + goos + "_" + goarch
}

// ChecksumsAssetName is the checksum list every release carries, and
// ChecksumsSigAssetName its signature. These are the only two assets whose
// names do not start with a binary's name: an updater that looks a binary up
// by a substring of its name (older releases of `ghost update` did) must never
// find a signature there.
const (
	ChecksumsAssetName    = "checksums.txt"
	ChecksumsSigAssetName = "checksums.txt.sig"
)
