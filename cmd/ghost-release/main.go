// ghost-release makes the signed files a Ghost release is published with.
//
//	ghost-release keygen <private-key-file>        make a signing key; prints the PUBLIC key
//	ghost-release checksums <out> <file>...        write a sha256sum-style checksum file
//	ghost-release sign <private-key-file> <checksums>  write <checksums>.sig
//	ghost-release verify <checksums> <public-key> <file>...  check a release the way `ghost update` will
//
// Only the checksum list is signed; it then binds every binary. A signature is
// an Ed25519 signature, in hex, over the list's sha256 digest.
// The private key never leaves the file it is written to; only the public key
// is ever printed, and it is the one built into the `ghost` binary.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ianclemence/ghost/pkg/appliance"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "checksums":
		err = checksums(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghost-release:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ghost-release keygen|checksums|sign|verify ... (see the package comment)")
	os.Exit(2)
}

func keygen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("keygen needs one argument: the file to write the private key to")
	}
	path := args[0]
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a signing key", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(hex.EncodeToString(priv) + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(hex.EncodeToString(pub))
	return nil
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by others (mode %v); chmod 600 it", path, st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s is not a ghost-release signing key", path)
	}
	return ed25519.PrivateKey(raw), nil
}

func digestOf(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, "", err
	}
	sum := h.Sum(nil)
	return sum, hex.EncodeToString(sum), nil
}

func checksums(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("checksums needs an output file and at least one input file")
	}
	var lines []string
	for _, p := range args[1:] {
		_, sum, err := digestOf(p)
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+filepath.Base(p))
	}
	sort.Strings(lines)
	return os.WriteFile(args[0], []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func sign(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("sign needs a private key file and the checksum file to sign")
	}
	key, err := loadKey(args[0])
	if err != nil {
		return err
	}
	digest, _, err := digestOf(args[1])
	if err != nil {
		return err
	}
	sig := hex.EncodeToString(ed25519.Sign(key, digest))
	return os.WriteFile(args[1]+".sig", []byte(sig+"\n"), 0o644)
}

// verify is the release's own acceptance test: it applies exactly the check
// `ghost update` applies, so a release that passes here will install.
func verify(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("verify needs a checksums file, a public key and at least one file")
	}
	keys, err := appliance.ParseTrustedKeys(args[1])
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(args[0] + ".sig")
	if err != nil {
		return fmt.Errorf("%s: no signature file: %w", filepath.Base(args[0]), err)
	}
	if err := appliance.VerifyDetached(args[0], string(sig), keys); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(args[0]), err)
	}
	fmt.Printf("ok  %s (signature)\n", filepath.Base(args[0]))
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sums := appliance.ParseChecksums(string(data))
	for _, p := range args[2:] {
		name := filepath.Base(p)
		if err := appliance.VerifyChecksum(p, sums[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Printf("ok  %s\n", name)
	}
	return nil
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }
