package skills

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type SkillMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Schedule    string `json:"schedule,omitempty"`
	// RequiresBins lists external binaries the skill's fallback path
	// needs (parsed from `commands: [...]`, nested or top-level).
	RequiresBins []string `json:"requires_bins,omitempty"`
	// RequiresEnv lists env vars the fallback path needs (top-level
	// `requires_env: [...]`). Preferred-path tools check their own
	// credentials at runtime; these gate only the fallback.
	RequiresEnv []string `json:"requires_env,omitempty"`
	// RequiresCapabilities lists the Ghost capabilities the skill needs
	// (`requires: [calendar.read, web.search]`). This is a REQUEST, never a
	// grant: Ghost's normal capability/permission system still decides
	// whether each capability may be used. A skill cannot grant authority.
	RequiresCapabilities []string `json:"requires_capabilities,omitempty"`
}

type SkillInfo struct {
	Name                 string   `json:"name"`
	Path                 string   `json:"path"`
	Source               string   `json:"source"`
	Description          string   `json:"description"`
	Schedule             string   `json:"schedule,omitempty"`
	RequiresBins         []string `json:"requires_bins,omitempty"`
	RequiresEnv          []string `json:"requires_env,omitempty"`
	RequiresCapabilities []string `json:"requires_capabilities,omitempty"`
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

// ListSkills returns 1-level skills only: workspace/skills/<name>/SKILL.md
// (plus flat workflows/*.md and global/builtin overrides). Nested containers
// (github/*, productivity/*, research/*, software-development/*,
// email/himalaya) have no top-level SKILL.md and are intentionally never
// exposed — they are Tier 3 contributor docs, not live capabilities.
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
									info.RequiresBins = metadata.RequiresBins
									info.RequiresEnv = metadata.RequiresEnv
									info.RequiresCapabilities = metadata.RequiresCapabilities
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
							info.RequiresBins = metadata.RequiresBins
							info.RequiresEnv = metadata.RequiresEnv
							info.RequiresCapabilities = metadata.RequiresCapabilities
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
							info.RequiresBins = metadata.RequiresBins
							info.RequiresEnv = metadata.RequiresEnv
							info.RequiresCapabilities = metadata.RequiresCapabilities
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
							info.RequiresBins = metadata.RequiresBins
							info.RequiresEnv = metadata.RequiresEnv
							info.RequiresCapabilities = metadata.RequiresCapabilities
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
	return sl.BuildSkillsSummaryBudget(SkillsSummaryBudgetChars)
}

// SkillsSummaryBudgetChars caps the <skills> prompt index (~4 chars/token,
// so 12000 chars ≈ 3000 tokens). Capability discovery must not grow
// without bound as skills accumulate: every skill ships on every prompt.
const SkillsSummaryBudgetChars = 12000

// BuildSkillsSummaryBudget renders the compact skill index in stable
// (name-sorted) order, truncated to maxChars. Overflow is reported with a
// recovery path (list_dir on workspace/skills), never silently dropped.
func (sl *SkillsLoader) BuildSkillsSummaryBudget(maxChars int) string {
	allSkills := sl.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}
	sort.Slice(allSkills, func(i, j int) bool { return allSkills[i].Name < allSkills[j].Name })

	// Compact index: name + a short intent + the trigger phrases, so the model
	// can route quickly without reading a long description for every skill
	// (the full SKILL.md is read only once a skill is chosen).
	var lines []string
	lines = append(lines, "<skills>")
	included := 0
	for _, s := range allSkills {
		intent, triggers := compactSkill(s.Description)
		block := []string{
			"  <skill>",
			fmt.Sprintf("    <name>%s</name>", escapeXML(s.Name)),
			fmt.Sprintf("    <intent>%s</intent>", escapeXML(intent)),
			fmt.Sprintf("    <triggers>%s</triggers>", escapeXML(triggers)),
			fmt.Sprintf("    <location>%s</location>", escapeXML(s.Path)),
		}
		// Requirement gating: when a skill's declared fallback needs a
		// binary/env this machine lacks, say so up front. The preferred
		// tool path may still work; this only stops the model from
		// walking into a fallback it cannot run.
		if note := requirementNote(s); note != "" {
			block = append(block, fmt.Sprintf("    <fallback>%s</fallback>", escapeXML(note)))
		}
		// Declared capability requirements are a REQUEST, not a grant:
		// Ghost's normal capability/permission system still authorizes each
		// one. Surfaced so the model knows what the skill needs.
		if len(s.RequiresCapabilities) > 0 {
			block = append(block, fmt.Sprintf("    <requires>%s</requires>", escapeXML(strings.Join(s.RequiresCapabilities, ", "))))
		}
		block = append(block, "  </skill>")
		if maxChars > 0 && included > 0 {
			proj := 0
			for _, l := range lines {
				proj += len(l) + 1
			}
			for _, l := range block {
				proj += len(l) + 1
			}
			if proj > maxChars {
				break
			}
		}
		lines = append(lines, block...)
		included++
	}
	if included < len(allSkills) {
		lines = append(lines, fmt.Sprintf("  <!-- +%d more skills installed (index budget %d chars). Browse workspace/skills with list_dir to discover the rest. -->", len(allSkills)-included, maxChars))
	}
	lines = append(lines, "</skills>")

	return strings.Join(lines, "\n")
}

// requirementsCache memoizes binary/env presence: prompt builds run per
// turn and must not stat PATH dozens of times each.
var requirementsCache = struct {
	sync.Mutex
	at   time.Time
	bins map[string]bool
}{bins: map[string]bool{}}

// CheckRequirements reports which of the skill's declared fallback
// requirements are unmet on this machine: missing binaries (first word of
// each entry — flags are not binaries) and empty env vars. Results cache
// for a minute.
func CheckRequirements(info SkillInfo) (missingBins, missingEnv []string) {
	requirementsCache.Lock()
	if time.Since(requirementsCache.at) >= time.Minute {
		requirementsCache.bins = map[string]bool{}
		requirementsCache.at = time.Now()
	}
	for _, entry := range info.RequiresBins {
		bin := entry
		if f := strings.Fields(bin); len(f) > 0 {
			bin = f[0]
		}
		present, ok := requirementsCache.bins[bin]
		if !ok {
			_, err := exec.LookPath(bin)
			present = err == nil
			requirementsCache.bins[bin] = present
		}
		if !present {
			missingBins = append(missingBins, bin)
		}
	}
	requirementsCache.Unlock()
	for _, key := range info.RequiresEnv {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missingEnv = append(missingEnv, key)
		}
	}
	return missingBins, missingEnv
}

// requirementNote renders the one-line fallback availability note for a
// skill, or "" when the fallback's requirements are satisfied (or none are
// declared). Kept short so it fits the prompt index budget.
func requirementNote(info SkillInfo) string {
	missingBins, missingEnv := CheckRequirements(info)
	if len(missingBins) == 0 && len(missingEnv) == 0 {
		return ""
	}
	var parts []string
	if len(missingBins) > 0 {
		parts = append(parts, "missing binary "+strings.Join(missingBins, ", "))
	}
	if len(missingEnv) > 0 {
		parts = append(parts, "missing env "+strings.Join(missingEnv, ", "))
	}
	return "fallback unavailable (" + strings.Join(parts, "; ") + "); use the preferred tool path"
}

func (sl *SkillsLoader) Version() string {
	skills := sl.ListSkills()
	h := fnv.New64a()
	fmt.Fprintf(h, "n=%d;", len(skills))
	names := make([]string, 0, len(skills))
	byName := make(map[string]SkillInfo, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
		byName[s.Name] = s
	}
	sort.Strings(names)
	for _, n := range names {
		var mtime int64
		if fi, err := os.Stat(byName[n].Path); err == nil {
			mtime = fi.ModTime().UnixNano()
		}
		fmt.Fprintf(h, "%s:%d;", n, mtime)
	}
	return fmt.Sprintf("%x", h.Sum64())
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
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Schedule     string   `json:"schedule"`
		RequiresBins []string `json:"requires_bins"`
		RequiresEnv  []string `json:"requires_env"`
	}
	if err := json.Unmarshal([]byte(frontmatter), &jsonMeta); err == nil {
		return &SkillMetadata{
			Name:         jsonMeta.Name,
			Description:  jsonMeta.Description,
			Schedule:     jsonMeta.Schedule,
			RequiresBins: jsonMeta.RequiresBins,
			RequiresEnv:  jsonMeta.RequiresEnv,
		}
	}

	// Fall back to simple YAML parsing
	yamlMeta := sl.parseSimpleYAML(frontmatter)
	return &SkillMetadata{
		Name:                 yamlMeta["name"],
		Description:          yamlMeta["description"],
		Schedule:             yamlMeta["schedule"],
		RequiresBins:         parseListField(yamlMeta, "commands", "requires_bins", "bins"),
		RequiresEnv:          parseListField(yamlMeta, "requires_env", "env"),
		RequiresCapabilities: parseListField(yamlMeta, "requires", "requires_capabilities"),
	}
}

// parseListField reads a bracket list (`[a, b]`) from the first present of
// the candidate flat keys. Nested `prerequisites: commands: [...]` surfaces
// as a flat `commands:` line under the simple parser, so both house style
// and top-level keys resolve.
func parseListField(m map[string]string, keys ...string) []string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "[")
		v = strings.TrimSuffix(v, "]")
		var out []string
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(strings.Trim(part, "\"'")); p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
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
