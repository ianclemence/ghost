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
	return "Manage bot profiles: list, create, duplicate, delete, switch, set group/avatar, manage channels."
}

func (t *ProfileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "switch", "create", "duplicate", "delete", "current", "set_group", "set_avatar", "list_groups", "group_roster", "create_channel", "list_channels", "send_message", "read_history"},
				"description": "Action to perform",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Profile name",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Profile description (for create)",
			},
			"group": map[string]interface{}{
				"type":        "string",
				"description": "Group name (for set_group)",
			},
			"avatar_shape": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"circle", "squircle", "pill", "triangle", "hexagon", "cloud", "drop"},
				"description": "Avatar shape",
			},
			"avatar_color": map[string]interface{}{
				"type":        "string",
				"description": "Avatar hex color",
			},
			"channel_name": map[string]interface{}{
				"type":        "string",
				"description": "Channel name (for create_channel)",
			},
			"channel_topic": map[string]interface{}{
				"type":        "string",
				"description": "Channel topic",
			},
			"members": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Channel members (profile names)",
			},
			"channel_id": map[string]interface{}{
				"type":        "string",
				"description": "Channel ID (for send_message, read_history)",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Message content (for send_message)",
			},
			"sender": map[string]interface{}{
				"type":        "string",
				"description": "Sender profile name (for send_message)",
			},
			"max_lines": map[string]interface{}{
				"type":        "number",
				"description": "Max history lines to read (for read_history)",
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
	case "duplicate":
		name, _ := args["name"].(string)
		return t.duplicateProfile(name)
	case "delete":
		name, _ := args["name"].(string)
		return t.deleteProfile(name)
	case "set_group":
		name, _ := args["name"].(string)
		group, _ := args["group"].(string)
		return t.setGroup(name, group)
	case "set_avatar":
		name, _ := args["name"].(string)
		shape, _ := args["avatar_shape"].(string)
		color, _ := args["avatar_color"].(string)
		return t.setAvatar(name, shape, color)
	case "list_groups":
		return t.listGroups()
	case "group_roster":
		return t.groupRoster()
	case "create_channel":
		name, _ := args["channel_name"].(string)
		topic, _ := args["channel_topic"].(string)
		sender, _ := args["sender"].(string)
		members, _ := args["members"].([]interface{})
		return t.createChannel(name, topic, sender, members)
	case "list_channels":
		name, _ := args["name"].(string)
		return t.listChannels(name)
	case "send_message":
		channelID, _ := args["channel_id"].(string)
		sender, _ := args["sender"].(string)
		message, _ := args["message"].(string)
		return t.sendMessage(channelID, sender, message)
	case "read_history":
		channelID, _ := args["channel_id"].(string)
		maxLines := 50
		if ml, ok := args["max_lines"].(float64); ok && ml > 0 {
			maxLines = int(ml)
		}
		return t.readHistory(channelID, maxLines)
	default:
		return ErrorResult("unknown action")
	}
}

func (t *ProfileTool) listProfiles() *ToolResult {
	profileList, err := t.manager.List()
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list profiles: %v", err))
	}

	if len(profileList) == 0 {
		return UserResult("No profiles found.")
	}

	active := t.manager.ActiveProfile()
	var result []map[string]interface{}
	for _, p := range profileList {
		entry := map[string]interface{}{
			"name":        p.Name,
			"title":       p.Title,
			"description": p.Description,
			"group":       p.Group,
			"active":      p.Name == active,
		}
		if p.Avatar != nil {
			entry["avatar_shape"] = p.Avatar.Shape
			entry["avatar_color"] = p.Avatar.Color
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
		"title":       p.Title,
		"description": p.Description,
		"group":       p.Group,
		"ghost_home":  p.GhostHome,
		"workspace":   p.WorkspacePath(),
	}
	if p.Avatar != nil {
		result["avatar_shape"] = p.Avatar.Shape
		result["avatar_color"] = p.Avatar.Color
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

func (t *ProfileTool) duplicateProfile(name string) *ToolResult {
	if name == "" {
		return ErrorResult("source profile name is required")
	}

	newName := t.manager.UniqueName(name)
	p, err := t.manager.Duplicate(name, newName)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to duplicate profile: %v", err))
	}

	return UserResult(fmt.Sprintf("Duplicated '%s' → '%s'", name, p.Name))
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

func (t *ProfileTool) setGroup(name, group string) *ToolResult {
	if name == "" {
		return ErrorResult("profile name is required")
	}

	if err := t.manager.SetGroup(name, group); err != nil {
		return ErrorResult(fmt.Sprintf("failed to set group: %v", err))
	}

	if group == "" {
		return UserResult(fmt.Sprintf("Removed '%s' from group", name))
	}
	return UserResult(fmt.Sprintf("Moved '%s' to group '%s'", name, group))
}

func (t *ProfileTool) setAvatar(name, shape, color string) *ToolResult {
	if name == "" {
		return ErrorResult("profile name is required")
	}

	avatar := &profiles.Avatar{
		Shape: shape,
		Color: color,
	}

	if err := t.manager.UpdateAvatar(name, avatar); err != nil {
		return ErrorResult(fmt.Sprintf("failed to set avatar: %v", err))
	}

	return UserResult(fmt.Sprintf("Updated avatar for '%s': shape=%s, color=%s", name, shape, color))
}

func (t *ProfileTool) listGroups() *ToolResult {
	groups, err := t.manager.ListGroups()
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list groups: %v", err))
	}

	if len(groups) == 0 {
		return UserResult("No groups found.")
	}

	raw, _ := json.Marshal(groups)
	return UserResult(string(raw))
}

func (t *ProfileTool) groupRoster() *ToolResult {
	ungrouped, groups, err := t.manager.GroupRoster()
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to get group roster: %v", err))
	}

	result := map[string]interface{}{}

	if len(ungrouped) > 0 {
		var names []string
		for _, p := range ungrouped {
			names = append(names, p.Name)
		}
		result["ungrouped"] = names
	}

	for group, members := range groups {
		var names []string
		for _, p := range members {
			names = append(names, p.Name)
		}
		result[group] = names
	}

	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}

func (t *ProfileTool) createChannel(name, topic, sender string, membersIface []interface{}) *ToolResult {
	if name == "" {
		return ErrorResult("channel name is required")
	}
	if sender == "" {
		return ErrorResult("sender is required")
	}

	var members []string
	for _, m := range membersIface {
		if s, ok := m.(string); ok {
			members = append(members, s)
		}
	}

	if len(members) == 0 {
		members = []string{sender}
	}

	senderIsMember := false
	for _, m := range members {
		if m == sender {
			senderIsMember = true
			break
		}
	}
	if !senderIsMember {
		members = append(members, sender)
	}

	ch, err := t.manager.CreateChannel(name, topic, sender, members)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create channel: %v", err))
	}

	return UserResult(fmt.Sprintf("Created channel '%s' (id=%s, members=%v)", ch.Name, ch.ID, ch.Members))
}

func (t *ProfileTool) listChannels(name string) *ToolResult {
	if name == "" {
		return ErrorResult("profile name is required")
	}

	channels, err := t.manager.ListChannels(name)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list channels: %v", err))
	}

	if len(channels) == 0 {
		return UserResult(fmt.Sprintf("No channels for profile '%s'.", name))
	}

	var result []map[string]interface{}
	for _, ch := range channels {
		entry := map[string]interface{}{
			"id":      ch.ID,
			"name":    ch.Name,
			"topic":   ch.Topic,
			"members": ch.Members,
		}
		result = append(result, entry)
	}

	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}

func (t *ProfileTool) sendMessage(channelID, sender, message string) *ToolResult {
	if channelID == "" {
		return ErrorResult("channel_id is required")
	}
	if sender == "" {
		return ErrorResult("sender is required")
	}
	if message == "" {
		return ErrorResult("message is required")
	}

	if err := t.manager.SendChannelMessage(channelID, sender, message); err != nil {
		return ErrorResult(fmt.Sprintf("failed to send message: %v", err))
	}

	return UserResult(fmt.Sprintf("Message sent to channel '%s' by '%s'", channelID, sender))
}

func (t *ProfileTool) readHistory(channelID string, maxLines int) *ToolResult {
	if channelID == "" {
		return ErrorResult("channel_id is required")
	}

	messages, err := t.manager.ReadChannelHistory(channelID, maxLines)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read history: %v", err))
	}

	if len(messages) == 0 {
		return UserResult(fmt.Sprintf("No messages in channel '%s'.", channelID))
	}

	var result []map[string]interface{}
	for _, msg := range messages {
		entry := map[string]interface{}{
			"sender":    msg.Sender,
			"content":   msg.Content,
			"timestamp": msg.Timestamp.Format("15:04:05"),
		}
		result = append(result, entry)
	}

	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}
