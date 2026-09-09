// Package-level helpers for installing EXTERNAL skills (GitHub/ClawHub) and
// recording their provenance. External skills are markdown instruction sets,
// never host-executed code, but their files land on the appliance and their
// text is later shown to the model — so the download surface is governed:
// bounded size/count, blocked binary extensions, no path traversal, a SKILL.md
// must exist with name+description, and every install is traceable to a
// source/revision.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// External-skill install policy. These bound what may be pulled from an
// external source so a repo cannot dump arbitrary/binary content onto the
// appliance. They mirror the guarantees of the original web installer and are
// shared by every install path to prevent drift.
const (
	MaxSkillFiles     = 50
	MaxSkillFileSize  = 200 * 1024 // per file
	MaxSkillTotalSize = 1 << 20    // 1 MiB total
	SkillSourceFile   = ".ghost-source.json"
)

// BlockedSkillFileExts are file types that are never downloaded from an
// external skill source. Skills are text/instructions; binaries are not part
// of the trusted surface.
var BlockedSkillFileExts = map[string]bool{
	".sh": true, ".exe": true, ".bin": true, ".so": true, ".dylib": true,
	".dll": true, ".zip": true, ".tar": true, ".gz": true, ".tgz": true,
}

// SkillFileBlocked reports whether a workspace-relative skill file path is
// blocked for external install (binary extension, hidden/system file, or a
// path that escapes the skill directory).
func SkillFileBlocked(rel string) bool {
	clean := filepath.ToSlash(strings.TrimPrefix(rel, "/"))
	if clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return true
	}
	if strings.HasPrefix(clean, ".") && clean != ".ghost-source.json" {
		return true
	}
	return BlockedSkillFileExts[strings.ToLower(filepath.Ext(clean))]
}

// ValidateSkillDownloadBounds returns an error when an external download set
// violates the bounded install policy (too many files, over-size, blocked
// extensions, or missing a root SKILL.md with name/description).
func ValidateSkillDownloadBounds(relPaths []string, read func(rel string) ([]byte, error)) error {
	if len(relPaths) > MaxSkillFiles {
		return fmt.Errorf("skill has more than %d files", MaxSkillFiles)
	}
	hasSkillMD := false
	var total int64
	for _, rel := range relPaths {
		if SkillFileBlocked(rel) {
			return fmt.Errorf("skill contains a blocked file type (%s)", rel)
		}
		data, err := read(rel)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", rel, err)
		}
		if len(data) > MaxSkillFileSize {
			return fmt.Errorf("skill file %s exceeds %d bytes", rel, MaxSkillFileSize)
		}
		total += int64(len(data))
		if rel == "SKILL.md" {
			hasSkillMD = true
			if !hasNameAndDescription(data) {
				return fmt.Errorf("SKILL.md must declare a name and a description")
			}
		}
	}
	if total > MaxSkillTotalSize {
		return fmt.Errorf("skill exceeds %d bytes total", MaxSkillTotalSize)
	}
	if !hasSkillMD {
		return fmt.Errorf("no SKILL.md found at the skill root")
	}
	return nil
}

func hasNameAndDescription(body []byte) bool {
	text := string(body)
	if !strings.Contains(text, "name:") && !strings.Contains(text, `"name"`) {
		return false
	}
	if !strings.Contains(text, "description:") && !strings.Contains(text, `"description"`) {
		return false
	}
	return len(strings.TrimSpace(string(body))) > 80
}

// Provenance is the traceable source of an installed external skill. It is
// written next to the skill at install time and read for UI/trust display.
type Provenance struct {
	Type        string    `json:"type"` // "github"
	Owner       string    `json:"owner,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Path        string    `json:"path,omitempty"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// WriteProvenance records provenance inside an installed skill directory.
func WriteProvenance(skillDir string, p Provenance) error {
	if p.InstalledAt.IsZero() {
		p.InstalledAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(skillDir, SkillSourceFile), data, 0o600)
}

// ReadProvenance returns recorded provenance for a skill directory (nil when
// the skill is bundled or locally authored — that absence IS the signal that
// the skill is not externally sourced).
func ReadProvenance(skillDir string) (*Provenance, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, SkillSourceFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Provenance
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, nil // malformed provenance is not trust; treat as absent
	}
	return &p, nil
}
