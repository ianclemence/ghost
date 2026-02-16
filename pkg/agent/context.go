package agent

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/tools"
)

type ContextBuilder struct {
	workspace    string
	skillsLoader *skills.SkillsLoader
	memory       *MemoryStore
	tools        *tools.ToolRegistry // Direct reference to tool registry
}

func getGlobalConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".Ghost")
}

func NewContextBuilder(workspace string) *ContextBuilder {
	// builtin skills: skills directory in current project
	// Use the skills/ directory under the current working directory
	wd, _ := os.Getwd()
	builtinSkillsDir := filepath.Join(wd, "skills")
	globalSkillsDir := filepath.Join(getGlobalConfigDir(), "skills")

	return &ContextBuilder{
		workspace:    workspace,
		skillsLoader: skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir),
		memory:       NewMemoryStore(workspace),
	}
}

// SetToolsRegistry sets the tools registry for dynamic tool summary generation.
func (cb *ContextBuilder) SetToolsRegistry(registry *tools.ToolRegistry) {
	cb.tools = registry
}

func (cb *ContextBuilder) getIdentity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspacePath, _ := filepath.Abs(filepath.Join(cb.workspace))
	runtime := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())

	// Build tools section dynamically
	toolsSection := cb.buildToolsSection()

	return fmt.Sprintf(`# Ghost 👻

You are Ghost, a helpful AI assistant.

## Current Time
%s

## Runtime
%s

## Workspace
Your workspace is at: %s
- Memory: %s/memory/MEMORY.md
- Daily Notes: %s/memory/YYYYMM/YYYYMMDD.md
- Skills: %s/skills/{skill-name}/SKILL.md

%s

## Important Rules

1. **ALWAYS use tools** - When you need to perform an action (schedule reminders, send messages, execute commands, etc.), you MUST call the appropriate tool. Do NOT just say you'll do it or pretend to do it.

2. **Be helpful and accurate** - When using tools, briefly explain what you're doing.

3. **Memory** - When remembering something, write to %s/memory/MEMORY.md`,
		now, runtime, workspacePath, workspacePath, workspacePath, workspacePath, toolsSection, workspacePath)
}

func (cb *ContextBuilder) buildToolsSection() string {
	if cb.tools == nil {
		return ""
	}

	summaries := cb.tools.GetSummaries()
	if len(summaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("**CRITICAL**: You MUST use tools to perform actions. Do NOT pretend to execute commands or schedule tasks.\n\n")
	sb.WriteString("You have access to the following tools:\n\n")
	for _, s := range summaries {
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (cb *ContextBuilder) BuildSystemPrompt() string {
	parts := []string{}

	// Core identity section
	parts = append(parts, cb.getIdentity())

	// Bootstrap files
	bootstrapContent := cb.LoadBootstrapFiles()
	if bootstrapContent != "" {
		parts = append(parts, bootstrapContent)
	}

	// Skills - show summary, AI can read full content with read_file tool
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

%s`, skillsSummary))
	}

	// Memory context
	memoryContext := cb.memory.GetMemoryContext()
	if memoryContext != "" {
		parts = append(parts, "# Memory\n\n"+memoryContext)
	}

	// Join with "---" separator
	return strings.Join(parts, "\n\n---\n\n")
}

func (cb *ContextBuilder) LoadBootstrapFiles() string {
	bootstrapFiles := []string{
		"AGENTS.md",
		"SOUL.md",
		"USER.md",
		"IDENTITY.md",
	}

	var result string
	for _, filename := range bootstrapFiles {
		filePath := filepath.Join(cb.workspace, filename)
		if data, err := os.ReadFile(filePath); err == nil {
			result += fmt.Sprintf("## %s\n\n%s\n\n", filename, string(data))
		}
	}

	return result
}

func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID string) []providers.Message {
	messages := []providers.Message{}

	systemPrompt := cb.BuildSystemPrompt()

	// Add Current Session info if provided
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}

	// Log system prompt summary for debugging (debug mode only)
	logger.DebugCF("agent", "System prompt built",
		map[string]interface{}{
			"total_chars":   len(systemPrompt),
			"total_lines":   strings.Count(systemPrompt, "\n") + 1,
			"section_count": strings.Count(systemPrompt, "\n\n---\n\n") + 1,
		})

	// Log preview of system prompt (avoid logging huge content)
	preview := systemPrompt
	if len(preview) > 500 {
		preview = preview[:500] + "... (truncated)"
	}
	logger.DebugCF("agent", "System prompt preview",
		map[string]interface{}{
			"preview": preview,
		})

	if summary != "" {
		systemPrompt += "\n\n## Summary of Previous Conversation\n\n" + summary
	}

	//This fix prevents the session memory from LLM failure due to elimination of toolu_IDs required from LLM
	// --- INICIO DEL FIX ---
	//Diegox-17
	for len(history) > 0 && (history[0].Role == "tool") {
		logger.DebugCF("agent", "Removing orphaned tool message from history to prevent LLM error",
			map[string]interface{}{"role": history[0].Role})
		history = history[1:]
	}
	//Diegox-17
	// --- FIN DEL FIX ---

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	messages = append(messages, history...)

	// Construct user message
	userMsg := providers.Message{
		Role:    "user",
		Content: currentMessage,
	}

	if len(media) > 0 {
		contentParts := []providers.ContentPart{
			{
				Type: "text",
				Text: currentMessage,
			},
		}

		for _, path := range media {
			data, err := os.ReadFile(path)
			if err != nil {
				logger.ErrorCF("agent", "Failed to read media file", map[string]interface{}{"path": path, "error": err})
				continue
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			ext := strings.ToLower(filepath.Ext(path))

			var mimeType string
			var mediaType string // "image", "video", "audio", "unknown"

			switch ext {
			case ".png":
				mimeType = "image/png"
				mediaType = "image"
			case ".jpg", ".jpeg":
				mimeType = "image/jpeg"
				mediaType = "image"
			case ".webp":
				mimeType = "image/webp"
				mediaType = "image"
			case ".gif":
				mimeType = "image/gif"
				mediaType = "image"
			case ".mp4", ".mov", ".avi", ".webm":
				mimeType = "video/mp4"
				mediaType = "video"
			case ".mp3":
				mimeType = "audio/mpeg"
				mediaType = "audio"
			case ".wav":
				mimeType = "audio/wav"
				mediaType = "audio"
			case ".ogg":
				mimeType = "audio/ogg"
				mediaType = "audio"
			default:
				// Fallback or ignore
				logger.WarnCF("agent", "Unsupported media type", map[string]interface{}{"path": path, "ext": ext})
				continue
			}

			if mediaType == "image" {
				contentParts = append(contentParts, providers.ContentPart{
					Type: "image_url",
					ImageURL: &providers.ImageURL{
						URL: fmt.Sprintf("data:%s;base64,%s", mimeType, encoded),
					},
				})
			} else if mediaType == "video" || mediaType == "audio" {
				// Some providers (like Kimi) might accept audio via video_url or have specific handling
				// For now, we use video_url as a generic container for non-image media if supported
				contentParts = append(contentParts, providers.ContentPart{
					Type: "video_url",
					VideoURL: &providers.VideoURL{
						URL: fmt.Sprintf("data:%s;base64,%s", mimeType, encoded),
					},
				})
			}
		}
		userMsg.MultiContent = contentParts
	}

	messages = append(messages, userMsg)

	return messages
}

func (cb *ContextBuilder) AddToolResult(messages []providers.Message, toolCallID, toolName, result string) []providers.Message {
	messages = append(messages, providers.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
	})
	return messages
}

func (cb *ContextBuilder) AddAssistantMessage(messages []providers.Message, content string, toolCalls []map[string]interface{}) []providers.Message {
	msg := providers.Message{
		Role:    "assistant",
		Content: content,
	}
	// Always add assistant message, whether or not it has tool calls
	messages = append(messages, msg)
	return messages
}

func (cb *ContextBuilder) loadSkills() string {
	allSkills := cb.skillsLoader.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	var skillNames []string
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}

	content := cb.skillsLoader.LoadSkillsForContext(skillNames)
	if content == "" {
		return ""
	}

	return "# Skill Definitions\n\n" + content
}

// GetSkillsInfo returns information about loaded skills.
func (cb *ContextBuilder) GetSkillsInfo() map[string]interface{} {
	allSkills := cb.skillsLoader.ListSkills()
	skillNames := make([]string, 0, len(allSkills))
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}
	return map[string]interface{}{
		"total":     len(allSkills),
		"available": len(allSkills),
		"names":     skillNames,
	}
}
