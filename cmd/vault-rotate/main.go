// Vault master-key rotation helper. Re-seals .secrets.json under a fresh
// random 32-byte key and installs it as a root-only EnvironmentFile.
// Prints NO key material: success is "rotation ok", anything else fails
// loudly with the old files untouched.
//
// Usage (as root): go run ./cmd/vault-rotate /var/ghost/config
// Check mode (proves the on-disk key is dead after cleanup):
//   go run ./cmd/vault-rotate check /var/ghost/config
// prints "old key rejected: ok" when the file key no longer unseals the
// vault, "old key file absent: ok" when it is gone, and fails otherwise.
// Never prints key material.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ianclemence/ghost/pkg/config"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "check" {
		checkOldKeyDead(os.Args[2])
		return
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: vault-rotate <config-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	secretsPath := filepath.Join(dir, ".secrets.json")
	envPath := filepath.Join(dir, ".master-env")

	blob, err := os.ReadFile(secretsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read secrets:", err)
		os.Exit(1)
	}
	var plain []byte
	if config.IsSealed(blob) {
		// Read-only: rotation must never mint a key while loading the
		// old one (a minted key would fail auth and mask the real one).
		oldKey, err := config.ResolveMasterKey(secretsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load old master key:", err)
			os.Exit(1)
		}
		// Refuse to rotate when the key comes from the environment: that
		// would mean the on-disk key is already out of the loop or the
		// operator pointed at the wrong vault.
		if v := os.Getenv("GHOST_MASTER_KEY"); v != "" {
			fmt.Fprintln(os.Stderr, "GHOST_MASTER_KEY is set; unset it to rotate from the on-disk key")
			os.Exit(1)
		}
		plain, err = config.Unseal(oldKey, blob)
		if err != nil {
			fmt.Fprintln(os.Stderr, "unseal with on-disk key:", err)
			os.Exit(1)
		}
	} else {
		plain = blob
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		fmt.Fprintln(os.Stderr, "entropy:", err)
		os.Exit(1)
	}
	sealed, err := config.Seal(raw, plain)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seal:", err)
		os.Exit(1)
	}
	// Verify round-trip before touching disk.
	if _, err := config.Unseal(raw, sealed); err != nil {
		fmt.Fprintln(os.Stderr, "round-trip check:", err)
		os.Exit(1)
	}
	tmp, err := os.CreateTemp(dir, ".secrets-rotate-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp:", err)
		os.Exit(1)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(sealed); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	if err := tmp.Chmod(0600); err != nil {
		fmt.Fprintln(os.Stderr, "chmod:", err)
		os.Exit(1)
	}
	if err := tmp.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "close:", err)
		os.Exit(1)
	}
	if err := os.Rename(tmpName, secretsPath); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
	envBody := "GHOST_MASTER_KEY=" + base64.StdEncoding.EncodeToString(raw) + "\n"
	if err := os.WriteFile(envPath, []byte(envBody), 0600); err != nil {
		fmt.Fprintln(os.Stderr, "write env file (vault already re-sealed; old key still valid):", err)
		os.Exit(1)
	}
	fmt.Println("rotation ok")
}

// checkOldKeyDead proves the legacy on-disk key no longer opens the
// vault. It reads the .master-key file directly — never the unified
// resolver, which would (correctly) find the live rotation key in
// .master-env and always report STALE.
// Run with a clean environment (env -i) so nothing leaks in via env.
func checkOldKeyDead(dir string) {
	keyPath := filepath.Join(dir, ".master-key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		fmt.Println("old key file absent: ok")
		return
	}
	blob, err := os.ReadFile(filepath.Join(dir, ".secrets.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "read blob:", err)
		os.Exit(1)
	}
	key, err := config.LoadKeyFile(keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load file key:", err)
		os.Exit(1)
	}
	if _, err := config.Unseal(key, blob); err != nil {
		fmt.Println("old key rejected: ok")
		return
	}
	fmt.Fprintln(os.Stderr, "STALE KEY STILL UNSEALS THE VAULT")
	os.Exit(3)
}
