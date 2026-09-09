package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FindSkillRoot locates the single top-level skill directory under base
// (used by archive installers whose zip layout is unknown). It fails when the
// archive contains no skill, or more than one skill, so an ambiguous extract
// can never be silently treated as valid.
func FindSkillRoot(base string) (string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", err
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, e.Name(), "SKILL.md")); err == nil {
			roots = append(roots, filepath.Join(base, e.Name()))
		}
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("archive contains no skill (no SKILL.md at its root)")
	}
	if len(roots) > 1 {
		return "", fmt.Errorf("archive contains multiple skills; refusing ambiguous install")
	}
	return roots[0], nil
}

// ValidateInstalledSkillDir applies the bounded external-skill policy to a
// skill that is already on disk (e.g. after safe archive extraction). Used so
// every install path converges on the SAME validation boundary.
func ValidateInstalledSkillDir(skillDir string) error {
	var rels []string
	var total int64
	err := filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(skillDir, path)
		if rerr != nil {
			return rerr
		}
		if SkillFileBlocked(rel) {
			return fmt.Errorf("skill contains a blocked file type (%s)", rel)
		}
		if info.Size() > MaxSkillFileSize {
			return fmt.Errorf("skill file %s exceeds %d bytes", rel, MaxSkillFileSize)
		}
		total += info.Size()
		if total > MaxSkillTotalSize {
			return fmt.Errorf("skill exceeds %d bytes total", MaxSkillTotalSize)
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return err
	}
	if len(rels) > MaxSkillFiles {
		return fmt.Errorf("skill has more than %d files", MaxSkillFiles)
	}
	sort.Strings(rels)
	hasRoot := false
	for _, rel := range rels {
		if rel == "SKILL.md" {
			hasRoot = true
		}
	}
	if !hasRoot {
		return fmt.Errorf("no SKILL.md at the skill root")
	}
	body, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return err
	}
	if !hasNameAndDescription(body) {
		return fmt.Errorf("SKILL.md must declare a name and a description")
	}
	return nil
}
