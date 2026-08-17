package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// SyncReport describes what a SyncBundled pass did.
type SyncReport struct {
	Seeded        []string `json:"seeded"`         // newly copied from bundled source
	Updated       []string `json:"updated"`        // previously unchanged, refreshed from bundled source
	UserModified  []string `json:"user_modified"`  // edited locally, left untouched forever
	Unchanged     []string `json:"unchanged"`      // already in sync
	SkippedErrors []string `json:"skipped_errors"` // bundled skills that failed to process
}

// SyncBundled reconciles the runtime skills directory against the bundled
// skills source, Hermes-style: new bundled skills are seeded, unchanged skills
// receive upstream updates, and skills the user has edited are marked
// user-modified and never overwritten. Skills present only in the runtime
// directory (installed from the hub, created manually) are never touched.
//
// If bundledDir and runtimeSkillsDir resolve to the same directory, sync is a
// no-op that only records which skills have drifted from their recorded origin.
func SyncBundled(bundledDir, runtimeSkillsDir string) (*SyncReport, error) {
	return syncBundledFS(os.DirFS(bundledDir), runtimeSkillsDir, bundledDir)
}

// SyncBundledFromFS is like SyncBundled but reads the bundled source from a
// filesystem (e.g. an embedded FS). src is expected to be rooted at the skills
// directory itself (containing one subdirectory per skill).
func SyncBundledFromFS(src fs.FS, runtimeSkillsDir string) (*SyncReport, error) {
	return syncBundledFS(src, runtimeSkillsDir, "")
}

func syncBundledFS(src fs.FS, runtimeSkillsDir, bundledDir string) (*SyncReport, error) {
	report := &SyncReport{}
	if err := os.MkdirAll(runtimeSkillsDir, 0755); err != nil {
		return report, err
	}

	manifest, err := LoadManifest(runtimeSkillsDir)
	if err != nil {
		return report, err
	}

	sameDir := false
	if bundledDir != "" {
		absB, errB := filepath.Abs(bundledDir)
		absR, errR := filepath.Abs(runtimeSkillsDir)
		sameDir = errB == nil && errR == nil && absB == absR
	}

	names, err := skillNamesFromFS(src)
	if err != nil {
		return report, err
	}

	dirty := false
	for _, name := range names {
		entry, known := manifest.Skills[name]
		if known && entry.UserModified {
			report.UserModified = append(report.UserModified, name)
			continue
		}

		srcHash, err := hashSkillDirFS(src, name)
		if err != nil {
			report.SkippedErrors = append(report.SkippedErrors, name)
			continue
		}

		local := filepath.Join(runtimeSkillsDir, name)
		localHash, localErr := SkillDirHash(local)
		if localErr != nil && !os.IsNotExist(localErr) {
			report.SkippedErrors = append(report.SkippedErrors, name)
			continue
		}
		missing := os.IsNotExist(localErr)

		// Seed a bundled skill that is new (or was deleted while unmodified).
		if !known || missing {
			if !sameDir {
				if err := copySkillDirFS(src, name, runtimeSkillsDir); err != nil {
					report.SkippedErrors = append(report.SkippedErrors, name)
					continue
				}
			}
			manifest.Skills[name] = ManifestEntry{Origin: srcHash}
			dirty = true
			report.Seeded = append(report.Seeded, name)
			continue
		}

		if localHash == entry.Origin {
			if localHash == srcHash {
				report.Unchanged = append(report.Unchanged, name)
				continue
			}
			// User never touched it; upstream moved on. Refresh.
			if !sameDir {
				if err := copySkillDirFS(src, name, runtimeSkillsDir); err != nil {
					report.SkippedErrors = append(report.SkippedErrors, name)
					continue
				}
			}
			entry.Origin = srcHash
			manifest.Skills[name] = entry
			dirty = true
			report.Updated = append(report.Updated, name)
			continue
		}

		// Local content differs from what was seeded: user-modified, hands off.
		entry.UserModified = true
		manifest.Skills[name] = entry
		dirty = true
		report.UserModified = append(report.UserModified, name)
	}

	if dirty {
		if err := manifest.SaveManifest(runtimeSkillsDir); err != nil {
			return report, err
		}
	}

	sort.Strings(report.Seeded)
	sort.Strings(report.Updated)
	sort.Strings(report.UserModified)
	sort.Strings(report.Unchanged)
	sort.Strings(report.SkippedErrors)
	return report, nil
}

// IsBundled reports whether name is recorded as a bundled skill in the runtime
// skills directory.
func IsBundled(runtimeSkillsDir, name string) bool {
	manifest, err := LoadManifest(runtimeSkillsDir)
	if err != nil {
		return false
	}
	_, ok := manifest.Skills[name]
	return ok
}

// IsUserModified reports whether name has been edited locally and is therefore
// excluded from upstream updates.
func IsUserModified(runtimeSkillsDir, name string) bool {
	manifest, err := LoadManifest(runtimeSkillsDir)
	if err != nil {
		return false
	}
	entry, ok := manifest.Skills[name]
	return ok && entry.UserModified
}

// MarkUserModified records that the user has edited the skill, protecting it
// from future bundled updates.
func MarkUserModified(runtimeSkillsDir, name string) error {
	manifest, err := LoadManifest(runtimeSkillsDir)
	if err != nil {
		return err
	}
	entry, ok := manifest.Skills[name]
	if !ok {
		entry = ManifestEntry{Origin: ""}
	}
	entry.UserModified = true
	manifest.Skills[name] = entry
	return manifest.SaveManifest(runtimeSkillsDir)
}

// MarkSeeded records a freshly seeded bundled skill's origin hash. It is used
// when a skill dir is (re)created outside of a full sync pass.
func MarkSeeded(runtimeSkillsDir, name string) error {
	manifest, err := LoadManifest(runtimeSkillsDir)
	if err != nil {
		return err
	}
	if _, ok := manifest.Skills[name]; ok {
		return nil
	}
	hash, err := SkillDirHash(filepath.Join(runtimeSkillsDir, name))
	if err != nil {
		return err
	}
	manifest.Skills[name] = ManifestEntry{Origin: hash}
	return manifest.SaveManifest(runtimeSkillsDir)
}

func skillNamesFromFS(src fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(src, e.Name()+"/SKILL.md"); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func hashSkillDirFS(src fs.FS, name string) (string, error) {
	sub, err := fs.Sub(src, name)
	if err != nil {
		return "", err
	}
	rels := []string{}
	contents := map[string][]byte{}
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		rels = append(rels, path)
		contents[path] = data
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(rels)
	h := sha256.New()
	for _, rel := range rels {
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(contents[rel])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copySkillDirFS(src fs.FS, name, destSkillsDir string) error {
	sub, err := fs.Sub(src, name)
	if err != nil {
		return err
	}
	dest := filepath.Join(destSkillsDir, name)
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := sub.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		target := filepath.Join(dest, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(out, f)
		eerr := out.Close()
		if cerr != nil {
			return cerr
		}
		return eerr
	})
}

// skillHasher removed: hashSkillDirFS mirrors SkillDirHash (disk) byte-for-byte.