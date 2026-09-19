package connector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SourceRecordName is the provenance file written next to an installed
// connector. It records what was installed, from where, and the content hash.
const SourceRecordName = ".ghost-source.json"

// Install validates a connector and installs it into dir/<id>/, writing a
// provenance record. It refuses to silently replace a different connector
// under the same id unless force is true.
func Install(path, dir string, force bool) (string, error) {
	m, verrs, err := Load(path)
	if err != nil {
		return "", err
	}
	if len(verrs) > 0 {
		return "", fmt.Errorf("connector is invalid:\n%s", FormatErrors(verrs))
	}

	hash, err := manifestHash(m)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, m.ID)
	existing := filepath.Join(dest, FileName)

	if _, statErr := os.Stat(existing); statErr == nil && !force {
		if prev, _, lerr := Load(dest); lerr == nil {
			prevHash, herr := manifestHash(prev)
			if herr == nil && prevHash != hash {
				return "", fmt.Errorf("a different connector %q is already installed at %s (use --force to replace)", m.ID, dest)
			}
		}
	}

	if err := Save(m, existing); err != nil {
		return "", err
	}
	source := m.Provenance.Source
	if source == "" {
		source = "local"
	}
	record := map[string]interface{}{
		"id":           m.ID,
		"version":      m.Version,
		"source":       source,
		"source_path":  path,
		"hash":         hash,
		"installed_at": time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dest, SourceRecordName), append(raw, '\n'), 0600); err != nil {
		return "", err
	}
	return dest, nil
}

// manifestHash is the content hash recorded in provenance. It hashes the
// canonical JSON of the manifest, so an unchanged connector is idempotent and
// a changed one is detectable.
func manifestHash(m *Manifest) (string, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
