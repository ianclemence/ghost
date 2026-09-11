// Ghost - Ultra-lightweight personal AI agent
// Inspired by and based on GHOST: https://github.com/ianclemence/ghost
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/utils"
)

// ToolLoopConfig configures the tool execution loop.
type ToolLoopConfig struct {
	Provider      providers.LLMProvider
	Model         string
	Tools         *ToolRegistry
	MaxIterations int
	LLMOptions    map[string]any
	// BrowserAuth governs subagent browser calls. When set, browser_*
	// calls resolve through it: a returned binding executes under the
	// gate, a returned result replaces execution (approval wait or
	// denial). When nil, subagent browser calls are refused outright —
	// model-controlled execution without a gate is never silently
	// allowed.
	BrowserAuth SubagentBrowserAuth
	// ConsequentialAuth governs subagent standalone consequential tools
	// (exec, device io, scheduling, updates, image generation, messaging).
	// It returns a replacement result (an approval wait or denial) or nil
	// to allow execution. When nil, such tools are refused outright.
	ConsequentialAuth SubagentConsequentialAuth
}

// SubagentBrowserAuth authorizes one subagent browser call against the
// runtime (owner/context/session/task from the parent turn, permission
// broker, session binding). It returns the server-resolved binding to
// execute under, or a result that replaces execution when the call must
// wait or is denied.
type SubagentBrowserAuth func(ctx context.Context, tool string, args map[string]interface{}) (BrowserCall, *ToolResult)

// SubagentConsequentialAuth authorizes one subagent standalone
// consequential tool. nil means "allowed to execute"; a non-nil result
// replaces execution (approval wait or denial).
type SubagentConsequentialAuth func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult

// isBrowserToolName reports whether a tool name is a governed browser
// operation. Single predicate so every dispatch site agrees.
func isBrowserToolName(name string) bool {
	return len(name) > 8 && name[:8] == "browser_"
}

// isComputerToolName reports whether a tool name is a governed computer
// operation (refused in subagents — no subagent computer authority).
func isComputerToolName(name string) bool {
	return len(name) > 9 && name[:9] == "computer_"
}

// toolsFreeConsequential reports whether a tool is a standalone
// consequential operation from the audit table.
func toolsFreeConsequential(name string) bool {
	return IsFreeConsequentialTool(name)
}

// executeSubagentConsequential routes a subagent standalone-consequential
// tool through the authorizing hook. Without a hook there is no broker, so
// the call is refused — fail closed with a message the subagent can report.
func executeSubagentConsequential(ctx context.Context, config ToolLoopConfig, tc providers.ToolCall, channel, chatID string) *ToolResult {
	if config.ConsequentialAuth == nil {
		return ErrorResult("That operation is not authorized for a subagent. Nothing was run.")
	}
	replacement := config.ConsequentialAuth(ctx, tc.Name, tc.Arguments)
	if replacement != nil {
		return replacement
	}
	if config.Tools == nil {
		return ErrorResult("No tools available")
	}
	// Hook allowed: stamp the execution grant the registry requires for
	// primitives. The grant covers exactly this approved tool call.
	return config.Tools.ExecuteWithContext(GrantExec(ctx, tc.Name), tc.Name, tc.Arguments, channel, chatID, SessionKeyFromContext(ctx), nil)
}

// executeSubagentBrowser routes a subagent browser call through the
// authorizing hook when one is configured. Without a hook there is no
// gate, and without a gate model-controlled browser execution is
// refused — fail closed with a message the subagent can report.
func executeSubagentBrowser(ctx context.Context, config ToolLoopConfig, tc providers.ToolCall, channel, chatID string) *ToolResult {
	if config.BrowserAuth == nil {
		return ErrorResult("Browser use is not authorized for this subagent. Nothing was run.")
	}
	bag, replacement := config.BrowserAuth(ctx, tc.Name, tc.Arguments)
	if replacement != nil {
		return replacement
	}
	if config.Tools == nil {
		return ErrorResult("No tools available")
	}
	return config.Tools.ExecuteWithContext(GrantExec(WithBrowserCall(ctx, bag), tc.Name), tc.Name, tc.Arguments, channel, chatID, SessionKeyFromContext(ctx), nil)
}

// ToolLoopResult contains the result of running the tool loop.
type ToolLoopResult struct {
	Content    string
	Iterations int
}

// RunToolLoop executes the LLM + tool call iteration loop.
// This is the core agent logic that can be reused by both main agent and subagents.
func RunToolLoop(ctx context.Context, config ToolLoopConfig, messages []providers.Message, channel, chatID string) (*ToolLoopResult, error) {
	iteration := 0
	var finalContent string

	for iteration < config.MaxIterations {
		iteration++

		logger.DebugCF("toolloop", "LLM iteration",
			map[string]any{
				"iteration": iteration,
				"max":       config.MaxIterations,
			})

		// 1. Build tool definitions
		var providerToolDefs []providers.ToolDefinition
		if config.Tools != nil {
			providerToolDefs = config.Tools.ToProviderDefs()
		}

		// 2. Set default LLM options
		llmOpts := config.LLMOptions
		if llmOpts == nil {
			llmOpts = map[string]any{
				"max_tokens":  4096,
				"temperature": 0.7,
			}
		}

		// 3. Call LLM
		response, err := config.Provider.Chat(ctx, messages, providerToolDefs, config.Model, llmOpts)
		if err != nil {
			logger.ErrorCF("toolloop", "LLM call failed",
				map[string]any{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		// 4. If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("toolloop", "LLM response without tool calls (direct answer)",
				map[string]any{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		// 5. Log tool calls
		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("toolloop", "LLM requested tool calls",
			map[string]any{
				"tools":     toolNames,
				"count":     len(response.ToolCalls),
				"iteration": iteration,
			})

		// 6. Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}
		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		// 7. Execute tool calls
		for _, tc := range response.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			argsPreview := utils.Truncate(string(argsJSON), 200)
			logger.InfoCF("toolloop", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
				map[string]any{
					"tool":      tc.Name,
					"iteration": iteration,
				})

			// Execute tool (no async callback for subagents - they run independently)
			var toolResult *ToolResult
			if isComputerToolName(tc.Name) {
				// Computer control is a main-agent capability; a subagent
				// has no computer authority. Refusing makes the path
				// structurally impossible for the model to reach through a
				// subagent rather than creating an ungoverned alternate.
				toolResult = ErrorResult("Computer operations are not authorized for a subagent. Nothing was run.")
			} else if isBrowserToolName(tc.Name) {
				toolResult = executeSubagentBrowser(ctx, config, tc, channel, chatID)
			} else if toolsFreeConsequential(tc.Name) {
				toolResult = executeSubagentConsequential(ctx, config, tc, channel, chatID)
			} else if config.Tools != nil {
				toolResult = config.Tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, channel, chatID, "", nil)
			} else {
				toolResult = ErrorResult("No tools available")
			}

			// Determine content for LLM
			contentForLLM := toolResult.ForLLM
			if contentForLLM == "" && toolResult.Err != nil {
				contentForLLM = toolResult.Err.Error()
			}

			// Add tool result message
			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    contentForLLM,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
		}
	}

	return &ToolLoopResult{
		Content:    finalContent,
		Iterations: iteration,
	}, nil
}
