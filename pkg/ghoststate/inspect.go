package ghoststate

import (
	"fmt"
	"os"
)

// Inspect decrypts an archive and returns its manifest so a user can see what
// an archive contains without importing it. No files are extracted.
func Inspect(source, passphrase string) (*Manifest, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase is required")
	}
	blob, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	plain, err := decryptBytes(blob, passphrase)
	if err != nil {
		return nil, err
	}
	manifest, _, err := readArchive(plain)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}
