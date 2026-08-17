package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// BundledManifestFile is the name of the manifest stored in a skills directory.
// It records the origin content hash of every bundled skill so that sync can
// detect user edits and never stomp them with upstream updates.
const BundledManifestFile = ".bundled_manifest"

// ManifestEntry tracks a single bundled skill's state.
type ManifestEntry struct {
	Origin       string `json:"origin"`        // hash of the skill when it was seeded
	UserModified bool   `json:"user_modified"` // true once the user edits the skill; it is then never overwritten
}

// Manifest maps skill name -> entry. Skills absent from the map are not bundled.
type Manifest struct {
	Version int                      `json:"version"`
	Skills  map[string]ManifestEntry `json:"skills"`
}

// NewManifest returns an empty manifest.
func NewManifest() *Manifest {
	return &Manifest{Version: 1, Skills: map[string]ManifestEntry{}}
}

// LoadManifest reads the manifest from dir (if present), otherwise returns an
// empty manifest.
func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, BundledManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewManifest(), nil
		}
		return nil, fmt.Errorf("read bundled manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse bundled manifest: %w", err)
	}
	if m.Skills == nil {
		m.Skills = map[string]ManifestEntry{}
	}
	return &m, nil
}

// SaveManifest writes the manifest into dir.
func (m *Manifest) SaveManifest(dir string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, BundledManifestFile), data, 0644)
}

// SkillDirHash returns a stable hash of a skill directory's contents. Only
// regular files count; the bundled manifest itself is ignored so that saving
// the manifest never invalidates the recorded origin hash.
func SkillDirHash(dir string) (string, error) {
	h := sha256.New()
	var rels []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == BundledManifestFile {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(rels)
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}