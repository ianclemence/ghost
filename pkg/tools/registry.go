package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/redact"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

type ToolRegistry struct {
	tools             map[string]Tool
	schemas           map[string]*jsonschema.Schema
	hiddenTools       map[string]time.Time
	channelToolPolicy map[string]map[string]bool
	sessionToolPolicy map[string]map[string]bool
	// verifySink observes world-state verification outcomes (tool name,
	// latency, verification error or nil). The context carries the calling
	// session and trajectory for correlation. Set by the agent runtime;
	// nil = no observation.
	verifySink func(ctx context.Context, tool string, latencyMs int64, verifyErr error)
	mu         sync.RWMutex
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:             make(map[string]Tool),
		schemas:           make(map[string]*jsonschema.Schema),
		hiddenTools:       make(map[string]time.Time),
		channelToolPolicy: make(map[string]map[string]bool),
		sessionToolPolicy: make(map[string]map[string]bool),
	}
}

// SetVerifySink installs an observer for VerifiableTool world-state
// verification outcomes. The runtime bridges these to the canonical event
// stream (verification.completed/failed on the turn's trajectory).
func (r *ToolRegistry) SetVerifySink(fn func(ctx context.Context, tool string, latencyMs int64, verifyErr error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.verifySink = fn
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

func (r *ToolRegistry) RegisterHidden(tool Tool, ttl time.Duration) {
	r.Register(tool)
	r.mu.Lock()
	defer r.mu.Unlock()
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	r.hiddenTools[tool.Name()] = time.Now().Add(ttl)
}

func (r *ToolRegistry) Promote(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.hiddenTools, name)
}

func (r *ToolRegistry) SetToolEnabledForChannel(channel, tool string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	channel = strings.ToLower(strings.TrimSpace(channel))
	if _, ok := r.channelToolPolicy[channel]; !ok {
		r.channelToolPolicy[channel] = map[string]bool{}
	}
	r.channelToolPolicy[channel][tool] = enabled
}

func (r *ToolRegistry) SetToolEnabledForSession(sessionKey, tool string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sessionKey = strings.TrimSpace(sessionKey)
	if _, ok := r.sessionToolPolicy[sessionKey]; !ok {
		r.sessionToolPolicy[sessionKey] = map[string]bool{}
	}
	r.sessionToolPolicy[sessionKey][tool] = enabled
}

func (r *ToolRegistry) isAllowed(name, channel, sessionKey string) bool {
	if until, ok := r.hiddenTools[name]; ok {
		if time.Now().Before(until) {
			return false
		}
		delete(r.hiddenTools, name)
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionPolicy, ok := r.sessionToolPolicy[sessionKey]; ok {
		if allowed, exists := sessionPolicy[name]; exists {
			return allowed
		}
	}
	if channelPolicy, ok := r.channelToolPolicy[channel]; ok {
		if allowed, exists := channelPolicy[name]; exists {
			return allowed
		}
	}
	return true
}

func (r *ToolRegistry) isVisible(name string) bool {
	if until, ok := r.hiddenTools[name]; ok {
		if time.Now().Before(until) {
			return false
		}
		delete(r.hiddenTools, name)
	}
	return true
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) *ToolResult {
	return r.ExecuteWithContext(ctx, name, args, "", "", "", nil)
}

// ExecuteWithContext executes a tool with channel/chatID context and optional async callback.
// It also validates tool arguments against the tool's JSON schema.
func (r *ToolRegistry) ExecuteWithContext(ctx context.Context, name string, args map[string]interface{}, channel, chatID, sessionKey string, asyncCallback AsyncCallback) *ToolResult {
	// The logger does not redact; args are model input that may contain
	// secret-shaped content (typed credentials, keys), so the registry
	// redacts before anything leaves the core. Tool execution itself uses
	// the ORIGINAL args — redaction is log-only.
	logger.InfoCF("tool", "Tool execution started",
		map[string]interface{}{
			"tool": name,
			"args": redact.Any(args),
		})

	tool, ok := r.Get(name)
	if !ok {
		logger.ErrorCF("tool", "Tool not found",
			map[string]interface{}{
				"tool": name,
			})
		return ErrorResult(fmt.Sprintf("tool %q not found", name)).WithError(fmt.Errorf("tool not found"))
	}
	r.mu.Lock()
	allowed := r.isAllowed(name, channel, sessionKey)
	r.mu.Unlock()
	if !allowed {
		return ErrorResult(fmt.Sprintf("tool %q is disabled for this channel/session", name))
	}

	// Default-deny execution policy for primitives: exec, sandbox, update,
	// hardware actuators, browser/computer families, and third-party MCP
	// servers run only with a turn-scoped grant stamped by an authorization
	// boundary (broker approval, computer/browser gate, approved resume, or
	// a durable-automation executor). Anything else is refused loudly —
	// never executed, never silently skipped.
	if GrantRequired(name) && !execGranted(ctx, name) {
		logger.ErrorCF("tool", "Execution denied: no grant",
			map[string]interface{}{
				"tool": name,
			})
		return ErrorResult(fmt.Sprintf("tool %q is denied by execution policy: no execution grant for this turn (authorization boundary bypass suspected)", name))
	}

	// Validate arguments
	r.mu.RLock()
	schema, hasSchema := r.schemas[name]
	r.mu.RUnlock()

	if hasSchema {
		if err := schema.Validate(args); err != nil {
			logger.WarnCF("tool", "Tool argument validation failed", map[string]interface{}{
				"tool":  name,
				"args":  redact.Any(args),
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

	// Carry the calling session for session-scoped enforcement inside
	// tools (memory visibility). Server-side value, never model input.
	ctx = WithSessionKey(ctx, sessionKey)

	// If tool implements AsyncTool and callback is provided, set callback
	if asyncTool, ok := tool.(AsyncTool); ok && asyncCallback != nil {
		asyncTool.SetCallback(asyncCallback)
		logger.DebugCF("tool", "Async callback injected",
			map[string]interface{}{
				"tool": name,
			})
	}

	start := time.Now()
	result := executeWithReliability(ctx, tool, args)
	duration := time.Since(start)

	// Phase 3 — Verify: a successful, non-async execution of a VerifiableTool
	// isn't proof the outcome actually happened. Confirm it and, if verification
	// fails, surface a real failure so the agent recovers instead of trusting a
	// false positive. Proportional: only tools that opt in are verified.
	if !result.IsError && !result.Async {
		if verifiable, ok := tool.(VerifiableTool); ok {
			vStart := time.Now()
			verr := verifiable.Verify(ctx, args)
			r.mu.RLock()
			sink := r.verifySink
			r.mu.RUnlock()
			if sink != nil {
				sink(ctx, name, time.Since(vStart).Milliseconds(), verr)
			}
			if verr != nil {
				result = ErrorResult(fmt.Sprintf("tool %q ran but verification failed: %s", name, verr.Error())).WithError(verr)
				logger.WarnCF("tool", "verification failed", map[string]interface{}{"tool": name, "error": verr.Error()})
			}
		}
	}

	// Normalize the outcome into a structured observation before returning:
	// callers (agent loop → trajectory) record status/error-class/retryability
	// without parsing prose.
	if result != nil {
		obs := NewObservation(name, result)
		result.Obs = &obs
	}

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
	for name, tool := range r.tools {
		if !r.isVisible(name) {
			continue
		}
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
	for name, tool := range r.tools {
		if !r.isVisible(name) {
			continue
		}
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
// RegisteredNames returns every registered tool name, including hidden
// tools (hidden tools are still model-reachable when a committed
// capability or profile promotes them, so governance audits must see them).
func (r *ToolRegistry) RegisteredNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *ToolRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		if !r.isVisible(name) {
			continue
		}
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
	for name, tool := range r.tools {
		if !r.isVisible(name) {
			continue
		}
		summaries = append(summaries, fmt.Sprintf("- `%s` - %s", tool.Name(), tool.Description()))
	}
	return summaries
}
