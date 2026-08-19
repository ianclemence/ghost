package ghoststate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// Format is the interchange format identifier carried by every Ghost State
// archive. It is the contract for portable state, independent of the
// on-disk filesystem layout that produced it.
const Format = "ghost-state"

// CurrentSchemaVersion is the version of the manifest schema understood by
// this build. Bump it whenever the manifest contract changes.
const CurrentSchemaVersion = 1

// Category classifies a persistent artifact against the three-state model:
// portable (user-owned, canonical), derived (reconstructible), rebound
// (device-specific) — plus secret and disposable for non-exportable data.
type Category string

const (
	// CategoryPortable is user-owned state that must survive migration:
	// identity, conversations, memories, skills, preferences, workflows.
	CategoryPortable Category = "portable"
	// CategoryDerived is state that can be rebuilt (embeddings, indexes,
	// caches, routing history). It may travel in an archive but is not
	// canonical.
	CategoryDerived Category = "derived"
	// CategoryRebound is device-specific state that is recorded in the
	// manifest so its absence is deliberate, but never exported.
	CategoryRebound Category = "rebound"
	// CategorySecret is a credential. Excluded unless explicitly opted in.
	CategorySecret Category = "secret"
	// CategoryDisposable is transient data that is skipped silently.
	CategoryDisposable Category = "disposable"
)

// FileEntry describes one artifact inside the archive.
type FileEntry struct {
	Path     string   `json:"path"`     // logical path inside the archive
	Category Category `json:"category"` // portable | derived | secret
	Digest   string   `json:"digest"`   // "sha256:<hex>"
	Size     int64    `json:"size"`
	Mode     uint32   `json:"mode"` // os.FileMode bits
}

// Origin records where an archive was produced, for inspection only. It is
// device-specific and is never used to restore identity.
type Origin struct {
	Hostname string `json:"hostname,omitempty"`
}

// Manifest is the canonical definition of a portable Ghost State. It is the
// schema contract for the archive, not a directory listing.
type Manifest struct {
	Format            string      `json:"format"`
	SchemaVersion     int         `json:"schema_version"`
	GhostID           string      `json:"ghost_id"`
	IdentityCreatedAt string      `json:"identity_created_at,omitempty"`
	ExportedAt        string      `json:"exported_at"`
	SecretsIncluded   bool        `json:"secrets_included"`
	Origin            Origin      `json:"origin"`
	Files             []FileEntry `json:"files"`
	Rebound           []string    `json:"rebound,omitempty"`
	SecretsExcluded   []string    `json:"secrets_excluded,omitempty"`
}

// Validate checks that a manifest is a well-formed Ghost State contract of a
// schema version this build understands.
func (m *Manifest) Validate() error {
	if m.Format != Format {
		return fmt.Errorf("unrecognized archive format %q", m.Format)
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema version %d (this build understands %d)", m.SchemaVersion, CurrentSchemaVersion)
	}
	if m.GhostID == "" {
		return fmt.Errorf("manifest missing ghost_id")
	}
	for _, f := range m.Files {
		if f.Path == "" || f.Path == "/" || f.Path == "." {
			return fmt.Errorf("manifest contains invalid file path %q", f.Path)
		}
		if f.Category != CategoryPortable && f.Category != CategoryDerived && f.Category != CategorySecret {
			return fmt.Errorf("manifest file %q has non-exportable category %q", f.Path, f.Category)
		}
	}
	return nil
}

// File returns the entry for a logical path, or nil.
func (m *Manifest) File(path string) *FileEntry {
	for i := range m.Files {
		if m.Files[i].Path == path {
			return &m.Files[i]
		}
	}
	return nil
}

func digestFile(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), int64(len(data)), nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeManifestJSON(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
