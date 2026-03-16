package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/providers"
)

type subagentDepthKey struct{}

func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subagentDepthKey{}, depth)
}

func SubagentDepth(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if depth, ok := ctx.Value(subagentDepthKey{}).(int); ok {
		return depth
	}
	return 0
}

type SubagentPolicy struct {
	MaxDepth          int      `json:"max_depth"`
	MaxConcurrency    int      `json:"max_concurrency"`
	BlockedTools      []string `json:"blocked_tools"`
	AllowMessageWrite bool     `json:"allow_message_write"`
}

var DefaultSubagentPolicy = SubagentPolicy{
	MaxDepth:          1,
	MaxConcurrency:    2,
	BlockedTools:      []string{"subagent", "delegate", "spawn_agent", "spawn", "message_write", "message"},
	AllowMessageWrite: false,
}

type SubagentTask struct {
	ID            string
	Task          string
	Label         string
	OriginChannel string
	OriginChatID  string
	Status        string
	Result        string
	Created       int64
}

type SubagentManager struct {
	tasks         map[string]*SubagentTask
	mu            sync.RWMutex
	provider      providers.LLMProvider
	defaultModel  string
	bus           *bus.MessageBus
	workspace     string
	tools         *ToolRegistry
	maxIterations int
	nextID        int
	policy        SubagentPolicy
	activeCount   int
}

func NewSubagentManager(provider providers.LLMProvider, defaultModel, workspace string, bus *bus.MessageBus) *SubagentManager {
	return &SubagentManager{
		tasks:         make(map[string]*SubagentTask),
		provider:      provider,
		defaultModel:  defaultModel,
		bus:           bus,
		workspace:     workspace,
		tools:         NewToolRegistry(),
		maxIterations: 10,
		nextID:        1,
		policy:        DefaultSubagentPolicy,
	}
}

// SetTools sets the tool registry for subagent execution.
// If not set, subagent will have access to the provided tools.
func (sm *SubagentManager) SetTools(tools *ToolRegistry) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tools = tools
}

func (sm *SubagentManager) SetPolicy(policy SubagentPolicy) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.policy = policy
}

// RegisterTool registers a tool for subagent execution.
func (sm *SubagentManager) RegisterTool(tool Tool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tools.Register(tool)
}

func (sm *SubagentManager) currentDepth(ctx context.Context) int {
	return SubagentDepth(ctx)
}

func (sm *SubagentManager) childContext(ctx context.Context) (context.Context, int, error) {
	sm.mu.RLock()
	maxDepth := sm.policy.MaxDepth
	sm.mu.RUnlock()
	depth := sm.currentDepth(ctx) + 1
	if depth > maxDepth {
		return nil, depth, fmt.Errorf("subagent depth %d exceeds max depth %d", depth, maxDepth)
	}
	return WithSubagentDepth(ctx, depth), depth, nil
}

func (sm *SubagentManager) acquireSlot() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.activeCount >= sm.policy.MaxConcurrency {
		return fmt.Errorf("subagent concurrency limit reached (%d)", sm.policy.MaxConcurrency)
	}
	sm.activeCount++
	return nil
}

func (sm *SubagentManager) releaseSlot() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.activeCount > 0 {
		sm.activeCount--
	}
}

func (sm *SubagentManager) filteredTools() *ToolRegistry {
	sm.mu.RLock()
	baseTools := sm.tools
	policy := sm.policy
	sm.mu.RUnlock()

	if baseTools == nil {
		return NewToolRegistry()
	}

	blocked := map[string]struct{}{}
	for _, alwaysBlocked := range []string{"subagent", "spawn", "delegate", "spawn_agent"} {
		blocked[alwaysBlocked] = struct{}{}
	}
	for _, name := range policy.BlockedTools {
		blocked[name] = struct{}{}
	}
	if !policy.AllowMessageWrite {
		blocked["message"] = struct{}{}
		blocked["message_write"] = struct{}{}
	}

	filtered := NewToolRegistry()
	for _, name := range baseTools.List() {
		if _, isBlocked := blocked[name]; isBlocked {
			continue
		}
		if tool, ok := baseTools.Get(name); ok {
			filtered.Register(tool)
		}
	}
	return filtered
}

func (sm *SubagentManager) runLoop(ctx context.Context, taskPrompt, originChannel, originChatID, label string) (*ToolLoopResult, error) {
	messages := []providers.Message{
		{
			Role:    "system",
			Content: "You are a subagent. Complete the task independently and return only the final result.",
		},
		{
			Role:    "user",
			Content: taskPrompt,
		},
	}

	sm.mu.RLock()
	maxIter := sm.maxIterations
	sm.mu.RUnlock()

	return RunToolLoop(ctx, ToolLoopConfig{
		Provider:      sm.provider,
		Model:         sm.defaultModel,
		Tools:         sm.filteredTools(),
		MaxIterations: maxIter,
		LLMOptions: map[string]any{
			"max_tokens":  4096,
			"temperature": 0.7,
		},
	}, messages, originChannel, originChatID)
}

func (sm *SubagentManager) Spawn(ctx context.Context, task, label, originChannel, originChatID string, callback AsyncCallback) (string, error) {
	childCtx, _, err := sm.childContext(ctx)
	if err != nil {
		return "", err
	}
	if err := sm.acquireSlot(); err != nil {
		return "", err
	}

	sm.mu.Lock()
	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++
	subagentTask := &SubagentTask{
		ID:            taskID,
		Task:          task,
		Label:         label,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		Status:        "running",
		Created:       time.Now().UnixMilli(),
	}
	sm.tasks[taskID] = subagentTask
	sm.mu.Unlock()

	go sm.runTask(childCtx, subagentTask, callback)

	if label != "" {
		return fmt.Sprintf("Spawned subagent '%s' for task: %s", label, task), nil
	}
	return fmt.Sprintf("Spawned subagent for task: %s", task), nil
}

func (sm *SubagentManager) runTask(ctx context.Context, task *SubagentTask, callback AsyncCallback) {
	defer sm.releaseSlot()
	task.Status = "running"
	task.Created = time.Now().UnixMilli()

	// Check if context is already cancelled before starting
	select {
	case <-ctx.Done():
		sm.mu.Lock()
		task.Status = "cancelled"
		task.Result = "Task cancelled before execution"
		sm.mu.Unlock()
		return
	default:
	}

	loopResult, err := sm.runLoop(ctx, task.Task, task.OriginChannel, task.OriginChatID, task.Label)

	sm.mu.Lock()
	var result *ToolResult
	defer func() {
		sm.mu.Unlock()
		// Call callback if provided and result is set
		if callback != nil && result != nil {
			callback(ctx, result)
		}
	}()

	if err != nil {
		task.Status = "failed"
		task.Result = fmt.Sprintf("Error: %v", err)
		// Check if it was cancelled
		if ctx.Err() != nil {
			task.Status = "cancelled"
			task.Result = "Task cancelled during execution"
		}
		result = &ToolResult{
			ForLLM:  task.Result,
			ForUser: "",
			Silent:  false,
			IsError: true,
			Async:   false,
			Err:     err,
		}
	} else {
		task.Status = "completed"
		task.Result = loopResult.Content
		result = &ToolResult{
			ForLLM:  fmt.Sprintf("Subagent '%s' completed (iterations: %d): %s", task.Label, loopResult.Iterations, loopResult.Content),
			ForUser: loopResult.Content,
			Silent:  false,
			IsError: false,
			Async:   false,
		}
	}

	// Send announce message back to main agent
	if sm.bus != nil {
		announceContent := fmt.Sprintf("Task '%s' completed.\n\nResult:\n%s", task.Label, task.Result)
		sm.bus.PublishInbound(bus.InboundMessage{
			Channel:  "system",
			SenderID: fmt.Sprintf("subagent:%s", task.ID),
			// Format: "original_channel:original_chat_id" for routing back
			ChatID:  fmt.Sprintf("%s:%s", task.OriginChannel, task.OriginChatID),
			Content: announceContent,
		})
	}
}

func (sm *SubagentManager) RunSync(ctx context.Context, task, label, originChannel, originChatID string) (*ToolLoopResult, error) {
	childCtx, _, err := sm.childContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := sm.acquireSlot(); err != nil {
		return nil, err
	}
	defer sm.releaseSlot()
	return sm.runLoop(childCtx, task, originChannel, originChatID, label)
}

func (sm *SubagentManager) GetTask(taskID string) (*SubagentTask, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	task, ok := sm.tasks[taskID]
	return task, ok
}

func (sm *SubagentManager) ListTasks() []*SubagentTask {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	tasks := make([]*SubagentTask, 0, len(sm.tasks))
	for _, task := range sm.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// SubagentTool executes a subagent task synchronously and returns the result.
// Unlike SpawnTool which runs tasks asynchronously, SubagentTool waits for completion
// and returns the result directly in the ToolResult.
type SubagentTool struct {
	manager       *SubagentManager
	originChannel string
	originChatID  string
}

func NewSubagentTool(manager *SubagentManager) *SubagentTool {
	return &SubagentTool{
		manager:       manager,
		originChannel: "cli",
		originChatID:  "direct",
	}
}

func (t *SubagentTool) Name() string {
	return "subagent"
}

func (t *SubagentTool) Description() string {
	return "Execute a subagent task synchronously and return the result. Use this for delegating specific tasks to an independent agent instance. Returns execution summary to user and full details to LLM."
}

func (t *SubagentTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The task for subagent to complete",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Optional short label for the task (for display)",
			},
		},
		"required": []string{"task"},
	}
}

func (t *SubagentTool) SetContext(channel, chatID string) {
	t.originChannel = channel
	t.originChatID = chatID
}

func (t *SubagentTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	task, ok := args["task"].(string)
	if !ok {
		return ErrorResult("task is required").WithError(fmt.Errorf("task parameter is required"))
	}

	label, _ := args["label"].(string)

	if t.manager == nil {
		return ErrorResult("Subagent manager not configured").WithError(fmt.Errorf("manager is nil"))
	}

	loopResult, err := t.manager.RunSync(ctx, task, label, t.originChannel, t.originChatID)

	if err != nil {
		return ErrorResult(fmt.Sprintf("Subagent execution failed: %v", err)).WithError(err)
	}

	// ForUser: Brief summary for user (truncated if too long)
	userContent := loopResult.Content
	maxUserLen := 500
	if len(userContent) > maxUserLen {
		userContent = userContent[:maxUserLen] + "..."
	}

	// ForLLM: Full execution details
	labelStr := label
	if labelStr == "" {
		labelStr = "(unnamed)"
	}
	llmContent := fmt.Sprintf("Subagent task completed:\nLabel: %s\nIterations: %d\nResult: %s",
		labelStr, loopResult.Iterations, loopResult.Content)

	return &ToolResult{
		ForLLM:  llmContent,
		ForUser: userContent,
		Silent:  false,
		IsError: false,
		Async:   false,
	}
}
