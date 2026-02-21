package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

type PicoLMProvider struct {
	binaryPath string
	modelPath  string
	threads    int
	context    int
	template   string
}

func NewPicoLMProvider(cfg config.PicoLMProviderConfig) (*PicoLMProvider, error) {
	// Expand home directory in paths if needed
	binaryPath := expandPath(cfg.BinaryPath)
	modelPath := expandPath(cfg.ModelPath)

	if binaryPath == "" {
		return nil, fmt.Errorf("picolm binary path is required")
	}
	if modelPath == "" {
		return nil, fmt.Errorf("picolm model path is required")
	}

	// Verify binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("picolm binary not found at: %s", binaryPath)
	}

	// Verify model exists
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("picolm model not found at: %s", modelPath)
	}

	return &PicoLMProvider{
		binaryPath: binaryPath,
		modelPath:  modelPath,
		threads:    cfg.Threads,
		context:    cfg.Context,
		template:   cfg.Template,
	}, nil
}

func (p *PicoLMProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	// 1. Format prompt
	prompt := p.formatPrompt(messages)

	// 2. Prepare command arguments
	args := []string{p.modelPath}

	// Basic options
	if p.threads > 0 {
		args = append(args, "-j", fmt.Sprintf("%d", p.threads))
	}
	if p.context > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", p.context))
	}

	// Generation options from map
	if maxTokens, ok := options["max_tokens"].(int); ok && maxTokens > 0 {
		args = append(args, "-n", fmt.Sprintf("%d", maxTokens))
	} else {
		args = append(args, "-n", "256") // Default
	}

	if temp, ok := options["temperature"].(float64); ok {
		args = append(args, "-t", fmt.Sprintf("%.2f", temp))
	}

	// Tool calling mode
	useJSON := len(tools) > 0
	if useJSON {
		args = append(args, "--json")
		// Append tool instructions to system prompt or user prompt if not already present
		// Ideally the prompt formatter handles this, but for now we rely on the prompt having instructions.
		// However, with --json, PicoLM might expect a specific schema.
		// For now, we just pass --json as per docs.
	}

	// Prompt (passed via stdin to avoid shell limit issues, though docs show -p flag)
	// Docs say: echo "..." | ./picolm ...
	// But they also show -p. Stdin is safer for large prompts.
	// However, the binary might expect -p for the prompt argument.
	// Docs: " -p <prompt> Input prompt (or pipe via stdin)"
	// So we can pipe it.

	cmd := exec.CommandContext(ctx, p.binaryPath, args...)
	
	// Set up stdin for prompt
	cmd.Stdin = strings.NewReader(prompt)

	// Capture stdout/stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.DebugCF("picolm", "Running PicoLM", map[string]interface{}{
		"binary": p.binaryPath,
		"args":   args,
		"prompt_len": len(prompt),
	})

	// Run
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("picolm execution failed: %w (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()
	// Clean up output: sometimes local LLMs output the prompt too, or extra newlines.
	// PicoLM docs example shows just the completion.
	output = strings.TrimSpace(output)

	resp := &LLMResponse{
		Content: output,
	}

	// Parse JSON if needed
	if useJSON {
		// Attempt to parse tool calls
		// Expected format: {"tool_calls": [...]} or just generic JSON
		// If the model output valid JSON, we try to parse it.
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(output), &parsed); err == nil {
			// Check for tool_calls
			if tcRaw, ok := parsed["tool_calls"]; ok {
				// Re-marshal to struct
				tcBytes, _ := json.Marshal(tcRaw)
				var toolCalls []ToolCall
				if err := json.Unmarshal(tcBytes, &toolCalls); err == nil {
					resp.ToolCalls = toolCalls
					// If successful, clear content to avoid double processing
					resp.Content = "" 
				}
			}
		} else {
			// If JSON parsing fails, just return raw content. 
			// The agent loop might handle text fallback.
			logger.WarnCF("picolm", "Failed to parse JSON output", map[string]interface{}{
				"output": output,
				"error":  err.Error(),
			})
		}
	}

	return resp, nil
}

func (p *PicoLMProvider) GetDefaultModel() string {
	return "picolm-local"
}

// formatPrompt converts messages to a prompt string based on the template.
func (p *PicoLMProvider) formatPrompt(messages []Message) string {
	var sb strings.Builder

	// Default to ChatML
	// <|user|>
	// Message</s>
	// <|assistant|>
	
	for _, msg := range messages {
		role := msg.Role
		content := msg.Content

		// Handle tool results
		if role == "tool" {
			role = "user" // Treat tool output as user input in ChatML usually, or specific role
			content = fmt.Sprintf("Tool %s output: %s", msg.ToolCallID, content)
		}

		sb.WriteString("<|")
		sb.WriteString(role)
		sb.WriteString("|>\n")
		sb.WriteString(content)
		sb.WriteString("</s>\n")
	}

	sb.WriteString("<|assistant|>\n")
	return sb.String()
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "$HOME") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[5:])
		}
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if len(path) == 1 {
				return home
			}
			return filepath.Join(home, path[1:])
		}
	}
	return os.ExpandEnv(path)
}
