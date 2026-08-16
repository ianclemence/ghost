package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ianclemence/ghost/pkg/profiles"
)

type ProfileTool struct {
	manager *profiles.Manager
}

func NewProfileTool(manager *profiles.Manager) *ProfileTool {
	return &ProfileTool{manager: manager}
}

func (t *ProfileTool) Name() string {
	return "profile"
}

func (t *ProfileTool) Description() string {
	return "Switch between bot profiles. Each profile has its own workspace, skills, and configuration."
}

func (t *ProfileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "switch", "create", "delete", "current"},
				"description": "Action to perform",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Profile name (for switch/create/delete)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Profile description (for create)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ProfileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)

	switch action {
	case "list":
		return t.listProfiles()
	case "current":
		return t.currentProfile()
	case "switch":
		name, _ := args["name"].(string)
		return t.switchProfile(name)
	case "create":
		name, _ := args["name"].(string)
		desc, _ := args["description"].(string)
		return t.createProfile(name, desc)
	case "delete":
		name, _ := args["name"].(string)
		return t.deleteProfile(name)
	default:
		return ErrorResult("unknown action")
	}
}

func (t *ProfileTool) listProfiles() *ToolResult {
	profiles, err := t.manager.List()
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list profiles: %v", err))
	}

	if len(profiles) == 0 {
		return UserResult("No profiles found. Create one with: profile create <name>")
	}

	active := t.manager.ActiveProfile()
	var result []map[string]interface{}
	for _, p := range profiles {
		entry := map[string]interface{}{
			"name":        p.Name,
			"description": p.Description,
			"active":      p.Name == active,
		}
		result = append(result, entry)
	}

	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}

func (t *ProfileTool) currentProfile() *ToolResult {
	active := t.manager.ActiveProfile()
	p, err := t.manager.Get(active)
	if err != nil {
		return UserResult(fmt.Sprintf("Active profile: %s (not found)", active))
	}

	result := map[string]interface{}{
		"name":        p.Name,
		"description": p.Description,
		"ghost_home":  p.GhostHome,
		"workspace":   p.WorkspacePath(),
	}
	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}

func (t *ProfileTool) switchProfile(name string) *ToolResult {
	if name == "" {
		return ErrorResult("profile name is required")
	}

	p, err := t.manager.Get(name)
	if err != nil {
		return ErrorResult(fmt.Sprintf("profile not found: %v", err))
	}

	if err := t.manager.SetActive(name); err != nil {
		return ErrorResult(fmt.Sprintf("failed to switch profile: %v", err))
	}

	return UserResult(fmt.Sprintf("Switched to profile '%s' (%s)", p.Name, p.Description))
}

func (t *ProfileTool) createProfile(name, description string) *ToolResult {
	if name == "" {
		return ErrorResult("profile name is required")
	}

	p, err := t.manager.Create(name, description, nil)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create profile: %v", err))
	}

	return UserResult(fmt.Sprintf("Created profile '%s' at %s", p.Name, p.GhostHome))
}

func (t *ProfileTool) deleteProfile(name string) *ToolResult {
	if name == "" {
		return ErrorResult("profile name is required")
	}

	if err := t.manager.Delete(name); err != nil {
		return ErrorResult(fmt.Sprintf("failed to delete profile: %v", err))
	}

	return UserResult(fmt.Sprintf("Deleted profile '%s'", name))
}
