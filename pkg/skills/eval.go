package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill evals: static pass/fail gates every installed or updated skill must
// clear. A skill that fails eval is not recorded as installed (hub Update
// rolls back to the working version). Evals never execute skill code —
// they check shape, declarations, and package hygiene.

type SkillEval struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// EvalSkill runs the static eval suite for an installed skill directory.
func EvalSkill(skillDir string) []SkillEval {
	return []SkillEval{
		evalManifest(skillDir),
		evalRequiresKnown(skillDir),
		evalPackageHygiene(skillDir),
	}
}

// EvalPassed reports whether every eval passed.
func EvalPassed(evals []SkillEval) bool {
	for _, e := range evals {
		if !e.Passed {
			return false
		}
	}
	return true
}

// evalFailure summarizes the first failing eval for error messages.
func evalFailure(evals []SkillEval) string {
	for _, e := range evals {
		if !e.Passed {
			if e.Detail != "" {
				return e.Name + ": " + e.Detail
			}
			return e.Name
		}
	}
	return "unknown"
}

func evalManifest(skillDir string) SkillEval {
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		return SkillEval{Name: "manifest", Passed: false, Detail: "SKILL.md missing"}
	}
	meta := (&SkillsLoader{}).getSkillMetadata(filepath.Join(skillDir, "SKILL.md"))
	if meta == nil || strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		return SkillEval{Name: "manifest", Passed: false, Detail: "SKILL.md needs name + description"}
	}
	return SkillEval{Name: "manifest", Passed: true}
}

func evalRequiresKnown(skillDir string) SkillEval {
	meta := (&SkillsLoader{}).getSkillMetadata(filepath.Join(skillDir, "SKILL.md"))
	if meta == nil {
		return SkillEval{Name: "requires", Passed: true, Detail: "no metadata"}
	}
	for _, req := range meta.RequiresCapabilities {
		if !HasCapability(req) && !isKnownGhostCapability(req) {
			return SkillEval{Name: "requires", Passed: false, Detail: fmt.Sprintf("unknown capability %q", req)}
		}
	}
	return SkillEval{Name: "requires", Passed: true}
}

func evalPackageHygiene(skillDir string) SkillEval {
	var bad []string
	_ = filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		if SkillFileBlocked(rel) {
			bad = append(bad, rel)
		}
		return nil
	})
	if len(bad) > 0 {
		return SkillEval{Name: "hygiene", Passed: false, Detail: "blocked files: " + strings.Join(bad, ", ")}
	}
	return SkillEval{Name: "hygiene", Passed: true}
}
