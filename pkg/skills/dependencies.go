package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Prerequisites struct {
	Commands []string `json:"commands"`
}

type DependencyCheckResult struct {
	Skill     string
	Missing   []string
	Available []string
}

type DependencyReport struct {
	Results []DependencyCheckResult
}

var commandCache = make(map[string]bool)
var commandPathCache = make(map[string]string)

func init() {
	commandCache["__cache_initialized__"] = true
}

func CheckSkillDependencies(workspace string) *DependencyReport {
	loader := NewSkillsLoader(workspace, "", "")
	skills := loader.ListSkills()

	report := &DependencyReport{
		Results: make([]DependencyCheckResult, 0),
	}

	for _, skill := range skills {
		prereqs := parsePrerequisites(skill.Path)
		if len(prereqs.Commands) == 0 {
			continue
		}

		result := DependencyCheckResult{
			Skill:     skill.Name,
			Missing:   []string{},
			Available: []string{},
		}

		for _, cmd := range prereqs.Commands {
			if cmd == "" {
				continue
			}
			if isCommandAvailable(cmd) {
				result.Available = append(result.Available, cmd)
			} else {
				result.Missing = append(result.Missing, cmd)
			}
		}

		if len(result.Missing) > 0 || len(result.Available) > 0 {
			report.Results = append(report.Results, result)
		}
	}

	return report
}

func CheckSkillDependenciesForSkill(skillName, workspace string) *DependencyCheckResult {
	loader := NewSkillsLoader(workspace, "", "")
	skillPath := findSkillPath(skillName, loader)
	if skillPath == "" {
		return nil
	}

	prereqs := parsePrerequisites(skillPath)
	result := &DependencyCheckResult{
		Skill:     skillName,
		Missing:   []string{},
		Available: []string{},
	}

	for _, cmd := range prereqs.Commands {
		if cmd == "" {
			continue
		}
		if isCommandAvailable(cmd) {
			result.Available = append(result.Available, cmd)
		} else {
			result.Missing = append(result.Missing, cmd)
		}
	}

	return result
}

func (r *DependencyReport) HasMissing() bool {
	for _, res := range r.Results {
		if len(res.Missing) > 0 {
			return true
		}
	}
	return false
}

func (r *DependencyReport) Summary() string {
	if len(r.Results) == 0 {
		return "All skills have their dependencies satisfied."
	}

	var buf bytes.Buffer
	hasAnyMissing := false

	for _, res := range r.Results {
		if len(res.Missing) > 0 {
			hasAnyMissing = true
			buf.WriteString(fmt.Sprintf("⚠ %s: missing %v\n", res.Skill, res.Missing))
		}
	}

	if !hasAnyMissing {
		return "All skill dependencies are satisfied."
	}

	buf.WriteString("\nRun `ghost doctor` for detailed installation instructions.")
	return buf.String()
}

func (r *DependencyReport) DetailedReport() string {
	if len(r.Results) == 0 {
		return "All skills have their dependencies satisfied."
	}

	var buf bytes.Buffer
	buf.WriteString("# Skill Dependency Report\n\n")

	allGood := true
	for _, res := range r.Results {
		if len(res.Missing) > 0 {
			allGood = false
			buf.WriteString(fmt.Sprintf("## %s\n", res.Skill))
			buf.WriteString(fmt.Sprintf("- **Missing**: `%s`\n", strings.Join(res.Missing, "`, `")))
			if len(res.Available) > 0 {
				buf.WriteString(fmt.Sprintf("- **Available**: `%s`\n", strings.Join(res.Available, "`, `")))
			}
			buf.WriteString("\n")
		}
	}

	if allGood {
		return "All skill dependencies are satisfied."
	}

	buf.WriteString("## Installation Quick Reference\n\n")
	buf.WriteString("| Command | Install |\n")
	buf.WriteString("|---------|---------|\n")

	installHints := map[string]string{
		"python":        "`pip install <package>` or `apt-get install python3`",
		"curl":          "`apt-get install curl` or `winget install curl`",
		"git":           "`apt-get install git` or `winget install Git`",
		"gcalcli":       "`pip install gcalcli`",
		"adb":           "`winget install Google.PlatformTools` (Windows) or `apt-get install adb` (Linux)",
		"nmap":          "`apt-get install nmap` or download from nmap.org",
		"ffmpeg":        "`apt-get install ffmpeg` or `winget install ffmpeg`",
		"tmux":          "`apt-get install tmux`",
		"spotify":       "Spotify CLI wrapper (platform-specific)",
		"nano-pdf":      "`uv pip install nano-pdf` or `pip install nano-pdf`",
		"himalaya":      "See https://github.com/pimalaya/himalaya",
		"speedtest-cli": "`pip install speedtest-cli`",
		"ddgs":          "`pip install ddgs`",
		"i2cdetect":     "`apt-get install i2c-tools`",
		"i2cget":        "`apt-get install i2c-tools`",
	}

	for _, res := range r.Results {
		for _, cmd := range res.Missing {
			if hint, ok := installHints[cmd]; ok {
				buf.WriteString(fmt.Sprintf("| `%s` | %s |\n", cmd, hint))
			} else {
				buf.WriteString(fmt.Sprintf("| `%s` | Install via package manager or see skill documentation |\n", cmd))
			}
		}
	}

	return buf.String()
}

func parsePrerequisites(skillPath string) Prerequisites {
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return Prerequisites{}
	}

	frontmatter := extractFrontmatter(string(content))
	if frontmatter == "" {
		return Prerequisites{}
	}

	var data struct {
		Prerequisites Prerequisites `json:"prerequisites"`
	}

	if err := json.Unmarshal([]byte(frontmatter), &data); err == nil {
		return data.Prerequisites
	}

	yamlPrereqs := parsePrerequisitesFromYAML(frontmatter)
	return yamlPrereqs
}

func parsePrerequisitesFromYAML(content string) Prerequisites {
	prereqs := Prerequisites{}

	lines := strings.Split(content, "\n")
	inPrereqs := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if !inPrereqs {
			if strings.HasPrefix(trimmed, "prerequisites:") {
				inPrereqs = true
			}
			continue
		}

		// Stop at the next top-level key (a line with no leading whitespace)
		// that is not part of the prerequisites block.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			if strings.HasPrefix(trimmed, "commands:") {
				// commands: written without indentation — still process it.
			} else {
				break
			}
		}

		if strings.HasPrefix(trimmed, "commands:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "commands:"))
			if strings.HasPrefix(value, "[") {
				// Inline list: commands: [curl, python]
				value = strings.Trim(value, "[] \t")
				for _, c := range strings.Split(value, ",") {
					c = strings.TrimSpace(strings.Trim(c, "\"'"))
					if c != "" {
						prereqs.Commands = append(prereqs.Commands, c)
					}
				}
			} else if value == "" {
				// Block list:
				//   commands:
				//     - curl
				//     - python
				for j := i + 1; j < len(lines); j++ {
					itemLine := lines[j]
					item := strings.TrimSpace(itemLine)
					if item == "" {
						continue
					}
					// Stop when a new, non-indented key or closure appears.
					if itemLine[0] != ' ' && itemLine[0] != '\t' {
						break
					}
					if !strings.HasPrefix(item, "-") {
						break
					}
					item = strings.TrimSpace(strings.Trim(strings.TrimPrefix(item, "-"), " \t\"'"))
					if item != "" {
						prereqs.Commands = append(prereqs.Commands, item)
					}
				}
			} else {
				// Single value or a comma-separated list after the colon.
				for _, c := range strings.Split(value, ",") {
					c = strings.TrimSpace(strings.Trim(c, "\"'"))
					if c != "" {
						prereqs.Commands = append(prereqs.Commands, c)
					}
				}
			}
		}
	}

	return prereqs
}

func extractFrontmatter(content string) string {
	re := regexp.MustCompile(`(?s)^---\n(.*?)\n---`)
	matches := re.FindStringSubmatch(content)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func findSkillPath(skillName string, loader *SkillsLoader) string {
	skills := loader.ListSkills()
	for _, s := range skills {
		if s.Name == skillName {
			return s.Path
		}
	}
	return ""
}

func isCommandAvailable(cmd string) bool {
	if val, ok := commandCache[cmd]; ok {
		return val
	}

	_, err := exec.LookPath(cmd)
	available := err == nil
	commandCache[cmd] = available
	return available
}

func GetCommandPath(cmd string) string {
	if path, ok := commandPathCache[cmd]; ok {
		return path
	}

	if path, err := exec.LookPath(cmd); err == nil {
		absPath, _ := filepath.Abs(path)
		commandPathCache[cmd] = absPath
		return absPath
	}

	commandPathCache[cmd] = ""
	return ""
}
