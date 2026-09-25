package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Reflection artifacts — the leash.
//
// Muse's discipline, adopted deliberately: insight is recorded, but it never
// silently rewrites behavior. Reflections live under dreams/, outside the
// memory/ path that feeds the prompt, and carry an explicit
// `prompt_hoisted: false` marker. Nothing here is injected; a human (or an
// explicit promotion) decides what becomes standing guidance.

// dreamsDir is where reflections and their synthesis live.
func (ms *MemoryStore) dreamsDir() string {
	return filepath.Join(ms.workspace, "dreams")
}

// reflectionFrontmatter is the honest header on every reflection artifact.
func reflectionFrontmatter(kind, date string) string {
	return fmt.Sprintf("---\nprompt_hoisted: false\nbody_contains_synthesis: false\nartifact: %s\ndream_path: dreams/%s.md\n---\n\n", kind, date)
}

// AppendReflection records a reflection for a date. The file is written under
// dreams/ (never memory/), so recalled daily notes and the prompt context
// cannot pick it up. Returns the path written.
func (ms *MemoryStore) AppendReflection(now time.Time, body string) (string, error) {
	if err := os.MkdirAll(ms.dreamsDir(), 0755); err != nil {
		return "", err
	}
	date := now.Format("2006-01-02")
	path := filepath.Join(ms.dreamsDir(), date+".md")

	existing, err := os.ReadFile(path)
	if err != nil || len(existing) == 0 {
		content := reflectionFrontmatter("dream", date) + body + "\n"
		return path, os.WriteFile(path, []byte(content), 0644)
	}
	// Same-day second reflection appends to the body, keeping one header.
	content := string(existing)
	if content[len(content)-1] != '\n' {
		content += "\n"
	}
	content += "\n" + body + "\n"
	return path, os.WriteFile(path, []byte(content), 0644)
}

// WriteSynthesis writes the standing-guidance artifact derived from
// reflections. It carries the same leash: written for review, never injected
// into the prompt automatically.
func (ms *MemoryStore) WriteSynthesis(content string) (string, error) {
	if err := os.MkdirAll(ms.dreamsDir(), 0755); err != nil {
		return "", err
	}
	path := filepath.Join(ms.dreamsDir(), "ALIGNMENT_SYNTHESIS.md")
	full := reflectionFrontmatter("synthesis", "ALIGNMENT_SYNTHESIS") + content + "\n"
	return path, os.WriteFile(path, []byte(full), 0644)
}
