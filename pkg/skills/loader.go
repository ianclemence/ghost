package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SkillMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Schedule    string `json:"schedule,omitempty"`
}

type SkillInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Schedule    string `json:"schedule,omitempty"`
}

type SkillsLoader struct {
	workspace       string
	workspaceSkills string // workspace skills (é¡¹ç›®çº§åˆ«)
	globalSkills    string // å…¨å±€ skills (~/.GHOST/skills)
	builtinSkills   string // å†…ç½® skills
}

func NewSkillsLoader(workspace string, globalSkills string, builtinSkills string) *SkillsLoader {
	return &SkillsLoader{
		workspace:       workspace,
		workspaceSkills: filepath.Join(workspace, "skills"),
		globalSkills:    globalSkills, // ~/.GHOST/skills
		builtinSkills:   builtinSkills,
	}
}

func (sl *SkillsLoader) ListSkills() []SkillInfo {
	skills := make([]SkillInfo, 0)

	if sl.workspaceSkills != "" {
		if dirs, err := os.ReadDir(sl.workspaceSkills); err == nil {
			for _, dir := range dirs {
				if dir.IsDir() && dir.Name() == "workflows" {
					// Load flat .md files from workflows directory
					workflowsDir := filepath.Join(sl.workspaceSkills, "workflows")
					if wfiles, err := os.ReadDir(workflowsDir); err == nil {
						for _, f := range wfiles {
							if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
								skillName := strings.TrimSuffix(f.Name(), ".md")
								skillFile := filepath.Join(workflowsDir, f.Name())
								info := SkillInfo{
									Name:   skillName,
									Path:   skillFile,
									Source: "workspace",
								}
								metadata := sl.getSkillMetadata(skillFile)
								if metadata != nil {
									info.Description = metadata.Description
									info.Schedule = metadata.Schedule
								}
								// Only append if there isn't one already added with same name?
								// Just append.
								skills = append(skills, info)
							}
						}
					}
					continue
				}

				if dir.IsDir() {
					skillFile := filepath.Join(sl.workspaceSkills, dir.Name(), "SKILL.md")
					if _, err := os.Stat(skillFile); err == nil {
						info := SkillInfo{
							Name:   dir.Name(),
							Path:   skillFile,
							Source: "workspace",
						}
						metadata := sl.getSkillMetadata(skillFile)
						if metadata != nil {
							info.Description = metadata.Description
							info.Schedule = metadata.Schedule
						}
						skills = append(skills, info)
					}
				}
			}
		}
	}

	// å…¨å±€ skills (~/.GHOST/skills) - è¢« workspace skills è¦†ç›–
	if sl.globalSkills != "" {
		if dirs, err := os.ReadDir(sl.globalSkills); err == nil {
			for _, dir := range dirs {
				if dir.IsDir() {
					skillFile := filepath.Join(sl.globalSkills, dir.Name(), "SKILL.md")
					if _, err := os.Stat(skillFile); err == nil {
						// æ£€æŸ¥æ˜¯å¦å·²è¢« workspace skills è¦†ç›–
						exists := false
						for _, s := range skills {
							if s.Name == dir.Name() && s.Source == "workspace" {
								exists = true
								break
							}
						}
						if exists {
							continue
						}

						info := SkillInfo{
							Name:   dir.Name(),
							Path:   skillFile,
							Source: "global",
						}
						metadata := sl.getSkillMetadata(skillFile)
						if metadata != nil {
							info.Description = metadata.Description
						}
						skills = append(skills, info)
					}
				}
			}
		}
	}

	if sl.builtinSkills != "" {
		if dirs, err := os.ReadDir(sl.builtinSkills); err == nil {
			for _, dir := range dirs {
				if dir.IsDir() {
					skillFile := filepath.Join(sl.builtinSkills, dir.Name(), "SKILL.md")
					if _, err := os.Stat(skillFile); err == nil {
						// æ£€æŸ¥æ˜¯å¦å·²è¢« workspace æˆ– global skills è¦†ç›–
						exists := false
						for _, s := range skills {
							if s.Name == dir.Name() && (s.Source == "workspace" || s.Source == "global") {
								exists = true
								break
							}
						}
						if exists {
							continue
						}

						info := SkillInfo{
							Name:   dir.Name(),
							Path:   skillFile,
							Source: "builtin",
						}
						metadata := sl.getSkillMetadata(skillFile)
						if metadata != nil {
							info.Description = metadata.Description
						}
						skills = append(skills, info)
					}
				}
			}
		}
	}

	return skills
}

func (sl *SkillsLoader) LoadSkill(name string) (string, bool) {
	// 1. ä¼˜å…ˆä»Ž workspace skills åŠ è½½ï¼ˆé¡¹ç›®çº§åˆ«ï¼‰
	if sl.workspaceSkills != "" {
		// First try as a flat workflow file
		workflowFile := filepath.Join(sl.workspaceSkills, "workflows", name+".md")
		if content, err := os.ReadFile(workflowFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}

		// Then try as a directory
		skillFile := filepath.Join(sl.workspaceSkills, name, "SKILL.md")
		if content, err := os.ReadFile(skillFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}
	}

	// 2. å…¶æ¬¡ä»Žå…¨å±€ skills åŠ è½½ (~/.GHOST/skills)
	if sl.globalSkills != "" {
		skillFile := filepath.Join(sl.globalSkills, name, "SKILL.md")
		if content, err := os.ReadFile(skillFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}
	}

	// 3. æœ€åŽä»Žå†…ç½® skills åŠ è½½
	if sl.builtinSkills != "" {
		skillFile := filepath.Join(sl.builtinSkills, name, "SKILL.md")
		if content, err := os.ReadFile(skillFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}
	}

	return "", false
}

func (sl *SkillsLoader) LoadSkillsForContext(skillNames []string) string {
	if len(skillNames) == 0 {
		return ""
	}

	var parts []string
	for _, name := range skillNames {
		content, ok := sl.LoadSkill(name)
		if ok {
			parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", name, content))
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}

func (sl *SkillsLoader) BuildSkillsSummary() string {
	allSkills := sl.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	// Compact index: name + a short intent + the trigger phrases, so the model
	// can route quickly without reading a long description for every skill
	// (the full SKILL.md is read only once a skill is chosen).
	var lines []string
	lines = append(lines, "<skills>")
	for _, s := range allSkills {
		intent, triggers := compactSkill(s.Description)
		lines = append(lines, "  <skill>")
		lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapeXML(s.Name)))
		lines = append(lines, fmt.Sprintf("    <intent>%s</intent>", escapeXML(intent)))
		lines = append(lines, fmt.Sprintf("    <triggers>%s</triggers>", escapeXML(triggers)))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", escapeXML(s.Path)))
		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")

	return strings.Join(lines, "\n")
}

// compactSkill reduces a skill description to a short intent plus the quoted
// trigger phrases it contains ("Invoke when the user says 'X', 'Y'"). This keeps
// the <skills> index small while giving the model the routing signal it needs.
func compactSkill(desc string) (intent, triggers string) {
	desc = strings.TrimSpace(desc)
	lower := strings.ToLower(desc)

	// Collect every quoted phrase — these are the trigger/example phrases the
	// description lists regardless of whether it says "Invoke when" or "Invoke for".
	var phrases []string
	seen := map[string]bool{}
	for _, q := range quoteRe.FindAllString(desc, -1) {
		p := strings.Trim(strings.TrimSpace(q), "\"'")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		phrases = append(phrases, p)
	}
	triggers = strings.Join(phrases, ", ")

	// Intent: everything before the invoke clause; otherwise the first sentence.
	if idx := strings.Index(lower, "invoke"); idx > 0 {
		intent = strings.TrimSpace(desc[:idx])
	} else {
		intent = desc
		if pos := strings.Index(desc, "."); pos > 0 {
			intent = strings.TrimSpace(desc[:pos])
		}
	}
	if intent == "" {
		intent = "Skill"
	}

	// Bound the intent length so a single skill can't blow up the index.
	intent = truncateRunes(intent, 140)
	return intent, strings.TrimSpace(triggers)
}

func (sl *SkillsLoader) getSkillMetadata(skillPath string) *SkillMetadata {
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return nil
	}

	frontmatter := sl.extractFrontmatter(string(content))
	if frontmatter == "" {
		return &SkillMetadata{
			Name: filepath.Base(filepath.Dir(skillPath)),
		}
	}

	// Try JSON first (for backward compatibility)
	var jsonMeta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schedule    string `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(frontmatter), &jsonMeta); err == nil {
		return &SkillMetadata{
			Name:        jsonMeta.Name,
			Description: jsonMeta.Description,
			Schedule:    jsonMeta.Schedule,
		}
	}

	// Fall back to simple YAML parsing
	yamlMeta := sl.parseSimpleYAML(frontmatter)
	return &SkillMetadata{
		Name:        yamlMeta["name"],
		Description: yamlMeta["description"],
		Schedule:    yamlMeta["schedule"],
	}
}

// parseSimpleYAML parses simple key: value YAML format
// Example: name: github\n description: "..."
func (sl *SkillsLoader) parseSimpleYAML(content string) map[string]string {
	result := make(map[string]string)

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, "\"'")
			result[key] = value
		}
	}

	return result
}

func (sl *SkillsLoader) extractFrontmatter(content string) string {
	// (?s) enables DOTALL mode so . matches newlines
	// Match first ---, capture everything until next --- on its own line
	re := regexp.MustCompile(`(?s)^---\n(.*)\n---`)
	match := re.FindStringSubmatch(content)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func (sl *SkillsLoader) stripFrontmatter(content string) string {
	re := regexp.MustCompile(`^---\n.*?\n---\n`)
	return re.ReplaceAllString(content, "")
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// quoteRe matches quoted phrases ('x' or "x") used as trigger examples in
// skill descriptions.
var quoteRe = regexp.MustCompile(`["'][^"']+["']`)

// truncateRunes shortens s to at most n runes, appending an ellipsis if cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
