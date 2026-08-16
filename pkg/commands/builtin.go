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
		sb.WriteString(fmt.Sprintf("- **%s**: %s", check.Name, status))
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
