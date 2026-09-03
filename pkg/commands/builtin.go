package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
			Aliases:     []string{"/new"},
			Description: "Clear current session history (or /clear all --yes for full chat history)",
			Usage:       "/clear [all] [--yes]",
			Handler:     clearHandler,
		},
		{
			Name:        "/reset",
			Aliases:     []string{"/factory-reset"},
			Description: "Factory reset Ghost — clear chats, memory, automations, context (keeps secrets & paired devices by default)",
			Usage:       "/reset [all|chats|memory|automations|context|devices] [--yes] [--include-secrets] [--include-devices]",
			Handler:     resetHandler,
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
			Name:        "/doctor",
			Aliases:     []string{"/health"},
			Description: "Run read-only Ghost diagnostics checks",
			Handler:     doctorHandler,
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
		{
			Name:        "/loop",
			Aliases:     []string{"/repeat"},
			Description: "Re-run a prompt on an interval. Usage: /loop [interval] <prompt>",
			Usage:       "/loop [5m|1h|30s] <prompt>",
			Handler:     loopHandler,
		},
		{
			Name:        "/loops",
			Description: "List active loops",
			Handler:     loopsHandler,
		},
		{
			Name:        "/stoploop",
			Description: "Stop a loop. Usage: /stoploop <job_id>",
			Usage:       "/stoploop <job_id>",
			Handler:     stoploopHandler,
		},
		{
			Name:        "/personality",
			Aliases:     []string{"/person"},
			Description: "Switch or list AI personalities",
			Usage:       "/personality [name]",
			Handler:     personalityHandler,
		},
		{
			Name:        "/model",
			Description: "Switch AI model or show current model",
			Usage:       "/model [provider:model]",
			Handler:     modelHandler,
		},
		{
			Name:        "/usage",
			Description: "Show current session token usage and stats",
			Handler:     usageHandler,
		},
		{
			Name:        "/compress",
			Description: "Force compress current conversation context",
			Handler:     compressHandler,
		},
		{
			Name:        "/context",
			Description: "Show your current Personal Context — what Ghost believes about you",
			Usage:       "/context [kind|subject|predicate] [--verbose]",
			Handler:     contextHandler,
		},
		{
			Name:        "/forget",
			Description: "Forget Personal Context entries or delete a session's conversation evidence",
			Usage:       "/forget <predicate | suffix | topic> | /forget everything about <topic> | /forget session <session-id>",
			Handler:     forgetHandler,
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

func getWorkspaceDir() string {
	if v := strings.TrimSpace(os.Getenv("GHOST_WORKSPACE_DIR")); v != "" {
		return v
	}
	return "workspace"
}

func listWorkspaceSkills() ([]string, error) {
	root := filepath.Join(getWorkspaceDir(), "skills")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	skills := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, dir.Name(), "SKILL.md")); err == nil {
			skills = append(skills, dir.Name())
		}
	}
	sort.Strings(skills)
	return skills, nil
}

func statusHandler(ctx context.Context, req Request, rt *Runtime) error {
	var sb strings.Builder
	sb.WriteString("### Ghost System Status\n\n")

	// Ghost Version
	sb.WriteString(fmt.Sprintf("**Ghost Version**: %s\n", "dev")) // You might want to pass version through runtime
	sb.WriteString(fmt.Sprintf("**Go Version**: %s\n", runtime.Version()))
	sb.WriteString(fmt.Sprintf("**OS/Arch**: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("**CPUs**: %d\n", runtime.NumCPU()))

	// Memory Usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	sb.WriteString(fmt.Sprintf("**Memory**: %v MB (Alloc) / %v MB (Sys)\n", m.Alloc/1024/1024, m.Sys/1024/1024))
	sb.WriteString(fmt.Sprintf("**Goroutines**: %d\n", runtime.NumGoroutine()))

	// Uptime (approximate via tool if available, or just omit if too complex without global start time)
	// We could use uptime command if available
	if rt != nil && rt.Tools != nil {
		if tool, ok := rt.Tools.Get("exec"); ok {
			res := tool.Execute(ctx, map[string]interface{}{
				"command": "uptime",
			})
			if res.Err == nil {
				sb.WriteString(fmt.Sprintf("**Uptime**: %s\n", strings.TrimSpace(res.ForLLM)))
			}
		}
	}

	return req.Reply(sb.String())
}

func doctorHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.Doctor == nil {
		return req.Reply("Doctor diagnostics are unavailable.")
	}
	results := rt.Doctor.RunAll(ctx)
	var sb strings.Builder
	sb.WriteString("### Ghost Doctor\n\n")
	for _, check := range results {
		status := strings.ToUpper(check.Status)
		if check.Status == "" {
			status = "UNKNOWN"
		}
		display := check.Label
		if display == "" {
			display = check.Name
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s", display, status))
		if check.Latency > 0 {
			sb.WriteString(fmt.Sprintf(" (%dms)", check.Latency))
		}
		if check.Message != "" {
			sb.WriteString(fmt.Sprintf(" — %s", check.Message))
		}
		sb.WriteString("\n")
	}
	return req.Reply(sb.String())
}

func skillsHandler(ctx context.Context, req Request, rt *Runtime) error {
	skills, err := listWorkspaceSkills()
	if err != nil {
		return req.Reply(fmt.Sprintf("Failed to list skills: %v", err))
	}
	if len(skills) == 0 {
		return req.Reply("No installed skills found in workspace/skills.")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Ghost Skills (%d)\n\n", len(skills)))
	for _, name := range skills {
		sb.WriteString(fmt.Sprintf("- `%s`\n", name))
	}
	return req.Reply(sb.String())
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
	toolNames := rt.Tools.List()
	if len(toolNames) == 0 {
		return req.Reply("No tools are currently loaded.")
	}
	sort.Strings(toolNames)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Ghost Tools (%d)\n\n", len(toolNames)))
	for _, t := range toolNames {
		if tool, ok := rt.Tools.Get(t); ok {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", t, tool.Description()))
		}
	}
	return req.Reply(sb.String())
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
	text := strings.TrimSpace(req.Text)
	fields := strings.Fields(text)
	// /clear all --yes clears all chat history (all sessions)
	if len(fields) >= 2 && strings.ToLower(fields[1]) == "all" {
		if !hasFlag(fields, "--yes") {
			return req.Reply("This will delete ALL chat history (all sessions). Add `--yes` to confirm: `/clear all --yes`")
		}
		if rt == nil || rt.Sessions == nil {
			return req.Reply("Session manager unavailable.")
		}
		ws := workspaceForRuntime(rt)
		if err := clearAllChats(ws, rt); err != nil {
			return req.Reply(fmt.Sprintf("Failed to clear chats: %v", err))
		}
		return req.Reply("All chat history cleared (all sessions). Ghost is ready for a fresh conversation.")
	}
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

func personalityHandler(ctx context.Context, req Request, rt *Runtime) error {
	args := strings.Fields(req.Text)
	if len(args) < 2 {
		list := []string{
			"default", "hacker", "creative", "teacher", "minimal",
		}
		var sb strings.Builder
		sb.WriteString("### Available Personalities\n\n")
		for _, name := range list {
			sb.WriteString(fmt.Sprintf("- `%s`\n", name))
		}
		sb.WriteString("\nUsage: /personality <name>")
		return req.Reply(sb.String())
	}
	name := args[1]
	if err := rt.SetPersonality(name); err != nil {
		return req.Reply(fmt.Sprintf("Failed to set personality: %v", err))
	}
	return req.Reply(fmt.Sprintf("Personality set to **%s**", name))
}

func modelHandler(ctx context.Context, req Request, rt *Runtime) error {
	args := strings.Fields(req.Text)
	if len(args) < 2 {
		var sb strings.Builder
		cur := "default"
		if rt.CurrentModel != nil {
			cur = rt.CurrentModel()
		} else if rt.Model != "" {
			cur = rt.Model
		}
		sb.WriteString(fmt.Sprintf("Current model: `%s`\n", cur))
		if len(rt.ModelPresets) > 0 {
			sb.WriteString("\n**Available presets:**\n")
			for _, p := range rt.ModelPresets {
				sb.WriteString(fmt.Sprintf("- `%s`\n", p))
			}
			sb.WriteString("\nUsage: `/model <preset>` or `/model <provider:model>`")
		} else {
			sb.WriteString("\nUsage: `/model <provider:model>`")
		}
		return req.Reply(sb.String())
	}

	target := args[1]
	// If it matches a named preset, resolve it to provider:model.
	if rt.ModelPresets != nil {
		for _, p := range rt.ModelPresets {
			if p == target {
				target = p
				break
			}
		}
	}
	// Prefer the injected setter so the change is persisted and takes effect live.
	if rt.SetActiveModel != nil {
		if err := rt.SetActiveModel(target); err != nil {
			return req.Reply(fmt.Sprintf("Failed to set model: %v", err))
		}
		return req.Reply(fmt.Sprintf("Model set to `%s`", target))
	}
	// Fallback: in-memory only (legacy behaviour).
	if err := rt.SetModel(target); err != nil {
		return req.Reply(fmt.Sprintf("Failed to set model: %v", err))
	}
	return req.Reply(fmt.Sprintf("Model set to `%s`", target))
}

func usageHandler(ctx context.Context, req Request, rt *Runtime) error {
	stats := rt.GetSessionStats(req.SessionKey)
	var sb strings.Builder
	sb.WriteString("### Session Usage\n\n")
	if stats.Messages > 0 {
		sb.WriteString(fmt.Sprintf("- **Messages**: %d\n", stats.Messages))
	}
	if stats.TotalTokens > 0 {
		sb.WriteString(fmt.Sprintf("- **Total tokens**: %d\n", stats.TotalTokens))
	}
	if stats.ToolCalls > 0 {
		sb.WriteString(fmt.Sprintf("- **Tool calls**: %d\n", stats.ToolCalls))
	}
	if stats.SummaryTokens > 0 {
		sb.WriteString(fmt.Sprintf("- **Summary tokens**: %d\n", stats.SummaryTokens))
	}
	if sb.String() == "### Session Usage\n\n" {
		sb.WriteString("No usage data available for this session.")
	}
	return req.Reply(sb.String())
}

func compressHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt.Sessions == nil {
		return req.Reply("Session manager unavailable.")
	}
	rt.Sessions.ClearHistory(req.SessionKey)
	return req.Reply("Session compressed. History cleared and summary will be generated on next message.")
}
