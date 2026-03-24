package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/logger"
)

// SkillManageTool allows the agent to autonomously create, update, and delete
// skills — turning successful approaches into reusable procedural knowledge.
// Inspired by Hermes Agent's skill_manager_tool.
type SkillManageTool struct {
	workspace string // Ghost workspace root (contains skills/ directory)
}

func NewSkillManageTool(workspace string) *SkillManageTool {
	return &SkillManageTool{
		workspace: workspace,
	}
}

func (t *SkillManageTool) Name() string {
	return "skill_manage"
}

func (t *SkillManageTool) Description() string {
	return `Manage skills (create, update, delete). Skills are your procedural memory — reusable approaches for recurring task types. New skills go to workspace/skills/<name>/SKILL.md.

Actions: create (full SKILL.md with frontmatter), patch (targeted find-and-replace), delete.

Create when: complex task succeeded (5+ tool calls), errors were overcome, user-corrected approach worked, non-trivial workflow discovered, or user asks you to remember a procedure.
Update when: instructions are stale/wrong, missing steps or pitfalls found during use. If you used a skill and hit issues not covered by it, patch it immediately.

After difficult/iterative tasks, offer to save as a skill. Skip for simple one-offs. Confirm with user before creating/deleting.

Good skills have: trigger conditions, numbered steps with exact commands, pitfalls section, verification steps.`
}

func (t *SkillManageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"create", "patch", "delete"},
				"description": "The action to perform.",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Skill name (lowercase, hyphens/underscores, max 64 chars). Must match an existing skill for patch/delete.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Full SKILL.md content (YAML frontmatter + markdown body). Required for 'create'.",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "Text to find in SKILL.md (required for 'patch'). Must be unique.",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "Replacement text (required for 'patch'). Can be empty string to delete matched text.",
			},
		},
		"required": []string{"action", "name"},
	}
}

// Validation constants
var validNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

const maxNameLength = 64

func (t *SkillManageTool) skillsDir() string {
	return filepath.Join(t.workspace, "skills")
}

func (t *SkillManageTool) findSkillDir(name string) (string, bool) {
	skillDir := filepath.Join(t.skillsDir(), name)
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillFile); err == nil {
		return skillDir, true
	}
	return "", false
}

func (t *SkillManageTool) validateName(name string) string {
	if name == "" {
		return "Skill name is required."
	}
	if len(name) > maxNameLength {
		return fmt.Sprintf("Skill name exceeds %d characters.", maxNameLength)
	}
	if !validNameRe.MatchString(name) {
		return "Invalid skill name. Use lowercase letters, numbers, hyphens, dots, and underscores. Must start with a letter or digit."
	}
	return ""
}

func (t *SkillManageTool) validateFrontmatter(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "Content cannot be empty."
	}
	if !strings.HasPrefix(content, "---") {
		return "SKILL.md must start with YAML frontmatter (---). Example:\n---\nname: my-skill\ndescription: What this skill does\n---\n\n# Instructions..."
	}

	// Find closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "SKILL.md frontmatter is not closed. Ensure you have a closing '---' line."
	}

	// Parse the YAML frontmatter (simple key: value)
	yamlContent := rest[:idx]
	hasName := false
	hasDescription := false
	for _, line := range strings.Split(yamlContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			if key == "name" {
				hasName = true
			}
			if key == "description" {
				hasDescription = true
			}
		}
	}

	if !hasName {
		return "Frontmatter must include 'name' field."
	}
	if !hasDescription {
		return "Frontmatter must include 'description' field."
	}

	// Check for body after frontmatter
	body := strings.TrimSpace(rest[idx+4:])
	if body == "" {
		return "SKILL.md must have content after the frontmatter (instructions, procedures, etc.)."
	}

	return ""
}

func (t *SkillManageTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)

	if action == "" {
		return ErrorResult("action is required. Use: create, patch, delete")
	}

	if errMsg := t.validateName(name); errMsg != "" {
		return ErrorResult(errMsg)
	}

	switch action {
	case "create":
		return t.createSkill(name, args)
	case "patch":
		return t.patchSkill(name, args)
	case "delete":
		return t.deleteSkill(name)
	default:
		return ErrorResult(fmt.Sprintf("Unknown action '%s'. Use: create, patch, delete", action))
	}
}

func (t *SkillManageTool) createSkill(name string, args map[string]interface{}) *ToolResult {
	content, _ := args["content"].(string)
	if content == "" {
		return ErrorResult("content is required for 'create'. Provide the full SKILL.md text (YAML frontmatter + markdown body).")
	}

	// Validate frontmatter
	if errMsg := t.validateFrontmatter(content); errMsg != "" {
		return ErrorResult(errMsg)
	}

	// Check for name collision
	if _, exists := t.findSkillDir(name); exists {
		return ErrorResult(fmt.Sprintf("A skill named '%s' already exists. Use action='patch' to update it.", name))
	}

	// Create directory and write SKILL.md
	skillDir := filepath.Join(t.skillsDir(), name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to create skill directory: %v", err))
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
		// Clean up on failure
		os.RemoveAll(skillDir)
		return ErrorResult(fmt.Sprintf("Failed to write SKILL.md: %v", err))
	}

	logger.InfoCF("skill_manage", "Skill created", map[string]interface{}{
		"name": name,
		"path": skillFile,
	})

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Skill '%s' created successfully.", name),
		"path":    skillFile,
		"hint":    "The skill will be available in the next session or when skills are reloaded.",
	}
	jsonResult, _ := json.Marshal(result)
	return NewToolResult(string(jsonResult))
}

func (t *SkillManageTool) patchSkill(name string, args map[string]interface{}) *ToolResult {
	oldString, _ := args["old_string"].(string)
	newString, hasNew := args["new_string"].(string)

	if oldString == "" {
		return ErrorResult("old_string is required for 'patch'. Provide the text to find in SKILL.md.")
	}
	if !hasNew {
		return ErrorResult("new_string is required for 'patch'. Use empty string to delete matched text.")
	}

	// Find the skill
	skillDir, exists := t.findSkillDir(name)
	if !exists {
		return ErrorResult(fmt.Sprintf("Skill '%s' not found.", name))
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to read SKILL.md: %v", err))
	}

	original := string(content)

	// Check for match
	count := strings.Count(original, oldString)
	if count == 0 {
		preview := original
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		return ErrorResult(fmt.Sprintf("old_string not found in SKILL.md.\n\nFile preview:\n%s", preview))
	}
	if count > 1 {
		return ErrorResult(fmt.Sprintf("old_string matched %d times. Provide more surrounding context to make the match unique.", count))
	}

	// Apply the patch
	patched := strings.Replace(original, oldString, newString, 1)

	// Validate the result still has valid frontmatter
	if errMsg := t.validateFrontmatter(patched); errMsg != "" {
		return ErrorResult(fmt.Sprintf("Patch would break SKILL.md structure: %s", errMsg))
	}

	// Write the patched file
	if err := os.WriteFile(skillFile, []byte(patched), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to write patched SKILL.md: %v", err))
	}

	logger.InfoCF("skill_manage", "Skill patched", map[string]interface{}{
		"name": name,
	})

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Skill '%s' patched successfully (1 replacement).", name),
	}
	jsonResult, _ := json.Marshal(result)
	return NewToolResult(string(jsonResult))
}

func (t *SkillManageTool) deleteSkill(name string) *ToolResult {
	skillDir, exists := t.findSkillDir(name)
	if !exists {
		return ErrorResult(fmt.Sprintf("Skill '%s' not found.", name))
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to delete skill: %v", err))
	}

	logger.InfoCF("skill_manage", "Skill deleted", map[string]interface{}{
		"name": name,
	})

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Skill '%s' deleted.", name),
	}
	jsonResult, _ := json.Marshal(result)
	return NewToolResult(string(jsonResult))
}
