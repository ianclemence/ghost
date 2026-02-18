package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type ToolRegistry struct {
	tools   map[string]Tool
	schemas map[string]*jsonschema.Schema
	mu      sync.RWMutex
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:   make(map[string]Tool),
		schemas: make(map[string]*jsonschema.Schema),
	}
}

func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool

	// Compile schema for validation
	schemaMap := tool.Parameters()
	if schemaMap != nil {
		schemaJSON, err := json.Marshal(schemaMap)
		if err == nil {
			compiler := jsonschema.NewCompiler()
			if err := compiler.AddResource("schema.json", strings.NewReader(string(schemaJSON))); err == nil {
				if schema, err := compiler.Compile("schema.json"); err == nil {
					r.schemas[tool.Name()] = schema
				} else {
					logger.WarnCF("tool", "Failed to compile schema for tool validation", map[string]interface{}{
						"tool":  tool.Name(),
						"error": err.Error(),
					})
				}
			}
		}
	}
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) *ToolResult {
	return r.ExecuteWithContext(ctx, name, args, "", "", nil)
}

// ExecuteWithContext executes a tool with channel/chatID context and optional async callback.
// It also validates tool arguments against the tool's JSON schema.
func (r *ToolRegistry) ExecuteWithContext(ctx context.Context, name string, args map[string]interface{}, channel, chatID string, asyncCallback AsyncCallback) *ToolResult {
	logger.InfoCF("tool", "Tool execution started",
		map[string]interface{}{
			"tool": name,
			"args": args,
		})

	tool, ok := r.Get(name)
	if !ok {
		logger.ErrorCF("tool", "Tool not found",
			map[string]interface{}{
				"tool": name,
			})
		return ErrorResult(fmt.Sprintf("tool %q not found", name)).WithError(fmt.Errorf("tool not found"))
	}

	// Validate arguments
	r.mu.RLock()
	schema, hasSchema := r.schemas[name]
	r.mu.RUnlock()

	if hasSchema {
		if err := schema.Validate(args); err != nil {
			logger.WarnCF("tool", "Tool argument validation failed", map[string]interface{}{
				"tool":  name,
				"args":  args,
				"error": err.Error(),
			})

			var errMsg string
			if validationErr, ok := err.(*jsonschema.ValidationError); ok {
				errMsg = fmt.Sprintf("Invalid arguments for tool %q:\n%s", name, formatValidationError(validationErr))
			} else {
				errMsg = fmt.Sprintf("Invalid arguments for tool %q: %v", name, err)
			}

			return ErrorResult(errMsg).WithError(err)
		}
	}

	// If tool implements ContextualTool, set context
	if contextualTool, ok := tool.(ContextualTool); ok && channel != "" && chatID != "" {
		contextualTool.SetContext(channel, chatID)
	}

	// If tool implements AsyncTool and callback is provided, set callback
	if asyncTool, ok := tool.(AsyncTool); ok && asyncCallback != nil {
		asyncTool.SetCallback(asyncCallback)
		logger.DebugCF("tool", "Async callback injected",
			map[string]interface{}{
				"tool": name,
			})
	}

	start := time.Now()
	result := tool.Execute(ctx, args)
	duration := time.Since(start)

	// Log based on result type
	if result.IsError {
		logger.ErrorCF("tool", "Tool execution failed",
			map[string]interface{}{
				"tool":     name,
				"duration": duration.Milliseconds(),
				"error":    result.ForLLM,
			})
	} else if result.Async {
		logger.InfoCF("tool", "Tool started (async)",
			map[string]interface{}{
				"tool":     name,
				"duration": duration.Milliseconds(),
			})
	} else {
		logger.InfoCF("tool", "Tool execution completed",
			map[string]interface{}{
				"tool":          name,
				"duration_ms":   duration.Milliseconds(),
				"result_length": len(result.ForLLM),
			})
	}

	return result
}

func formatValidationError(ve *jsonschema.ValidationError) string {
	var msgs []string
	if len(ve.Causes) == 0 {
		msgs = append(msgs, fmt.Sprintf("- %s: %s", ve.InstanceLocation, ve.Message))
	} else {
		for _, cause := range ve.Causes {
			msgs = append(msgs, formatValidationError(cause))
		}
	}
	return strings.Join(msgs, "\n")
}

func (r *ToolRegistry) GetDefinitions() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]map[string]interface{}, 0, len(r.tools))
	for _, tool := range r.tools {
		definitions = append(definitions, ToolToSchema(tool))
	}
	return definitions
}

// ToProviderDefs converts tool definitions to provider-compatible format.
// This is the format expected by LLM provider APIs.
func (r *ToolRegistry) ToProviderDefs() []providers.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]providers.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		schema := ToolToSchema(tool)

		// Safely extract nested values with type checks
		fn, ok := schema["function"].(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]interface{})

		definitions = append(definitions, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionDefinition{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		})
	}
	return definitions
}

// List returns a list of all registered tool names.
func (r *ToolRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tools.
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// GetSummaries returns human-readable summaries of all registered tools.
// Returns a slice of "name - description" strings.
func (r *ToolRegistry) GetSummaries() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]string, 0, len(r.tools))
	for _, tool := range r.tools {
		summaries = append(summaries, fmt.Sprintf("- `%s` - %s", tool.Name(), tool.Description()))
	}
	return summaries
}
