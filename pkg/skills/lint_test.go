package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillFrontmatterHygiene guards the product invariants the Skills UI
// and prompt depend on: every bundled skill has name+description, the name
// matches its directory, and no absolute paths or secret shapes leak into
// user-visible descriptions. Routing triggers ("Invoke when...") stay in
// frontmatter intentionally — the Web UI strips them via humanizeSkillDesc.
func TestSkillFrontmatterHygiene(t *testing.T) {
	roots := []string{"../../workspace/skills", "../../cmd/ghost/workspace/skills"}
	checked := 0
	for _, root := range roots {
		dirs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			mdPath := filepath.Join(root, d.Name(), "SKILL.md")
			data, err := os.ReadFile(mdPath)
			if err != nil {
				continue // containers without top-level SKILL.md are Tier 3 docs
			}
			checked++
			text := string(data)
			name, desc := parseFrontmatterNameDesc(text)
			if name == "" {
				t.Errorf("%s: missing name in frontmatter", mdPath)
			} else if name != d.Name() {
				t.Errorf("%s: frontmatter name %q != directory %q", mdPath, name, d.Name())
			}
			if desc == "" {
				t.Errorf("%s: missing description in frontmatter", mdPath)
			}
			for _, leak := range []string{"/var/lib/", "/home/", ".env", "API_KEY=", "Bearer ", "gcalcli oauth"} {
				if strings.Contains(text, leak) && !strings.Contains(text, "Integrations") {
					// Allow setup sections to mention tools, but descriptions must not leak paths/secrets.
					if strings.Contains(desc, leak) {
						t.Errorf("%s: description leaks %q", mdPath, leak)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no skills checked")
	}
}

func parseFrontmatterNameDesc(text string) (name, desc string) {
	rest := strings.TrimSpace(text)
	if !strings.HasPrefix(rest, "---") {
		return "", ""
	}
	end := strings.Index(rest[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(rest[3:3+end], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimPrefix(line, "name:"), " \"'")
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.Trim(strings.TrimPrefix(line, "description:"), " \"'")
		}
	}
	return name, desc
}
