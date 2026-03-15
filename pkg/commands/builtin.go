package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func DefaultDefinitions() []Definition {
	return []Definition{
		{
			Name:        "/help",
			Description: "Show help and tool list",
			Handler:     helpHandler,
		},
		{
			Name:        "/think",
			Description: "Enable deep reasoning mode",
			Usage:       "/think <message>",
			Handler:     thinkHandler,
		},
		{
			Name:        "/clear",
			Aliases:     []string{"/reset"},
			Description: "Archive current session history",
			Handler:     clearHandler,
		},
		{
			Name:        "/remind",
			Description: "Set a reminder (e.g. /remind buy milk in 10m)",
			Usage:       "/remind <message> in <time>",
			Handler:     remindHandler,
		},
		{
			Name:        "/status",
			Description: "Show Pi system status and Ghost version",
			Handler:     statusHandler,
		},
		{
			Name:        "/skills",
			Description: "List all installed skills in your workspace/skills directory",
			Handler:     skillsHandler,
		},
		{
			Name:        "/install",
			Description: "Installs a new skill from a GitHub URL or local path",
			Usage:       "/install <url>",
			Handler:     installHandler,
		},
		{
			Name:        "/tools",
			Description: "Shows the specific JSON schemas for all loaded tools",
			Handler:     toolsHandler,
		},
	}
}

func getGhostBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "ghost"
	}
	return exe
}

func statusHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Status is unavailable.")
	}
	tool, ok := rt.Tools.Get("exec")
	if !ok {
		return req.Reply("Shell execution is unavailable.")
	}
	
	// Get system info directly via shell
	res := tool.Execute(ctx, map[string]interface{}{
		"command": "uptime && df -h / && free -h",
	})
	
	var sb strings.Builder
	sb.WriteString("### Ghost Pi Status\n\n")
	
	if res.Err == nil {
		sb.WriteString("```\n")
		sb.WriteString(res.ForLLM)
		sb.WriteString("\n```\n")
	} else {
		sb.WriteString(fmt.Sprintf("> System info error: %v\n", res.Err))
	}

	// Also try to get ghost version info
	ghostRes := tool.Execute(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s version", getGhostBinary()),
	})
	if ghostRes.Err == nil {
		sb.WriteString("\n**Ghost Version:**\n")
		sb.WriteString("```\n")
		sb.WriteString(ghostRes.ForLLM)
		sb.WriteString("\n```\n")
	}

	return req.Reply(sb.String())
}

func skillsHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Skills are unavailable.")
	}
	tool, ok := rt.Tools.Get("exec")
	if !ok {
		return req.Reply("Shell execution is unavailable.")
	}
	res := tool.Execute(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s skills list", getGhostBinary()),
	})
	if res.Err != nil {
		return req.Reply(fmt.Sprintf("Failed to list skills: %v", res.Err))
	}
	
	// Prepend a hidden marker so the agent or UI knows this is informational
	// and should not be saved to long-term chat history if possible.
	return req.Reply(fmt.Sprintf("### Ghost Skills\n\n```\n%s\n```\n", res.ForLLM))
}

func installHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Skills are unavailable.")
	}
	args := strings.Fields(req.Text)
	if len(args) < 2 {
		return req.Reply("Usage: /install <url>")
	}
	url := args[1]
	tool, ok := rt.Tools.Get("exec")
	if !ok {
		return req.Reply("Shell execution is unavailable.")
	}
	res := tool.Execute(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s skills install %s", getGhostBinary(), url),
	})
	if res.Err != nil {
		return req.Reply(fmt.Sprintf("Failed to install skill: %v", res.Err))
	}
	return req.Reply(fmt.Sprintf("### Ghost Skill Install\n\n```\n%s\n```\n", res.ForLLM))
}

func toolsHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Tools are unavailable.")
	}
	tool, ok := rt.Tools.Get("exec")
	if !ok {
		return req.Reply("Shell execution is unavailable.")
	}
	res := tool.Execute(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s agent --list-tools", getGhostBinary()),
	})
	if res.Err != nil {
		return req.Reply(fmt.Sprintf("Failed to list tools: %v", res.Err))
	}
	return req.Reply(fmt.Sprintf("### Ghost Tools\n\n```\n%s\n```\n", res.ForLLM))
}

func helpHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Commands == nil {
		return req.Reply("Help is unavailable.")
	}
	var sb strings.Builder
	sb.WriteString("### Ghost Help\n\n")
	sb.WriteString("**Slash Commands:**\n")
	defs := rt.Commands.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	for _, def := range defs {
		desc := def.Description
		if desc == "" {
			desc = "No description"
		}
		sb.WriteString(fmt.Sprintf("- `%s`: %s\n", def.Name, desc))
	}
	if rt.Tools != nil {
		sb.WriteString("\n**Available Tools:**\n")
		toolNames := rt.Tools.List()
		sort.Strings(toolNames)
		for _, t := range toolNames {
			if tool, ok := rt.Tools.Get(t); ok {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t, tool.Description()))
			}
		}
	}
	return req.Reply(sb.String())
}

func clearHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Sessions == nil {
		return req.Reply("Session manager unavailable.")
	}
	rt.Sessions.ClearHistory(req.SessionKey)
	return req.Reply("Session history cleared.")
}

func thinkHandler(ctx context.Context, req Request, rt *Runtime) error {
	if strings.TrimSpace(req.Text) == "/think" {
		return req.Reply("Usage: /think <message>")
	}
	return nil
}

func remindHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Tools == nil {
		return req.Reply("Reminder tool unavailable.")
	}
	args := strings.Fields(req.Text)
	if len(args) < 4 {
		return req.Reply("Usage: /remind <message> in <time> (e.g., /remind coffee in 5m)")
	}
	inIdx := -1
	for i := len(args) - 1; i >= 0; i-- {
		if args[i] == "in" {
			inIdx = i
			break
		}
	}
	if inIdx <= 1 || inIdx >= len(args)-1 {
		return req.Reply("Usage: /remind <message> in <time> (e.g., /remind coffee in 5m)")
	}
	message := strings.Join(args[1:inIdx], " ")
	timeStr := args[inIdx+1]
	duration, err := time.ParseDuration(timeStr)
	if err != nil {
		duration, err = time.ParseDuration(timeStr + "s")
		if err != nil {
			return req.Reply("Invalid time format. Use 10s, 5m, 1h, etc.")
		}
	}
	tool, ok := rt.Tools.Get("cron")
	if !ok {
		return req.Reply("Cron tool not available.")
	}
	if ct, ok := tool.(interface {
		SetContext(channel, chatID string)
	}); ok {
		ct.SetContext(req.Channel, req.ChatID)
	}
	res := tool.Execute(ctx, map[string]interface{}{
		"action":     "add",
		"message":    message,
		"at_seconds": duration.Seconds(),
		"deliver":    true,
	})
	if res.Err != nil {
		return req.Reply(fmt.Sprintf("Failed to set reminder: %v", res.Err))
	}
	return req.Reply(fmt.Sprintf("Reminder set: '%s' in %s", message, duration.String()))
}
