package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/providers"
)

type subagentDepthKey struct{}

const DefaultSubagentTimeout = 5 * time.Minute

var subagentTimeout = DefaultSubagentTimeout

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

// OutputSchema defines the expected structure of a subagent's output.
type OutputSchema struct {
	Type      string              `json:"type"` // "text", "json", "markdown"
	Fields    []OutputSchemaField `json:"fields,omitempty"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

type OutputSchemaField struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "string", "number", "boolean", "array"
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// SubagentLogEntry represents a single log entry from a subagent.
type SubagentLogEntry struct {
	TaskID    string    `json:"task_id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`  // "info", "warn", "error", "tool"
	Source    string    `json:"source"` // "llm", "tool", "system"
	Message   string    `json:"message"`
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
	OutputSchema  *OutputSchema
	Logs          []SubagentLogEntry
	LogChannel    chan SubagentLogEntry // live log streaming
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
	// BrowserAuth, when set, governs subagent browser_* calls (see
	// ToolLoopConfig). Set by the embedding runtime; nil fails closed.
	BrowserAuth SubagentBrowserAuth
	// ConsequentialAuth, when set, governs subagent standalone
	// consequential tools. Set by the embedding runtime; nil fails closed.
	ConsequentialAuth SubagentConsequentialAuth
	// allowedCapabilities is the explicit capability scope delegated to
	// subtasks. Non-empty = deny-by-default. Empty = legacy unscoped (still
	// bounded by the actuator blocklist and the broker).
	allowedCapabilities []string
	// enqueue, when set, makes a spawn durable: the task is recorded as a
	// job and execution belongs to the runner that owns that record, not
	// to a goroutine started here. Nil keeps the old in-memory behaviour,
	// which is what an unwired test wants.
	enqueue func(SpawnRequest) (string, error)
}

// SpawnRequest is a background task the caller wants to outlive the
// process that asked for it.
type SpawnRequest struct {
	Task    string
	Label   string
	Channel string
	ChatID  string
	// SessionKey binds the recorded job to the conversation it came from,
	// so its state is visible where the owner will look for it and so an
	// approval reply in that conversation can find it again.
	SessionKey string
}

// DurableAttempt is one run of a recorded spawn: what to do, where earlier
// attempts got to, and the two callbacks that write what this attempt
// learns back to the record. Both may be nil.
type DurableAttempt struct {
	JobID      string
	Task       string
	Label      string
	Channel    string
	ChatID     string
	Resume     string
	Checkpoint func(line string)
	Evidence   func(text string)
}

// SetDurableSpawner hands spawns to a durable store. Nil restores the
// in-memory path.
func (sm *SubagentManager) SetDurableSpawner(fn func(SpawnRequest) (string, error)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.enqueue = fn
}

// IterationBudget is the tool-loop ceiling one attempt runs under. The
// job's progress is measured against it, so "how far through" means the
// same thing to the record as it does to the run.
func (sm *SubagentManager) IterationBudget() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.maxIterations
}

// SetDefaultModel updates the model subagents run on. A runtime model switch
// must reach them, or they keep calling the boot-time model (a second model to
// load, and a split brain about which one is active).
func (sm *SubagentManager) SetDefaultModel(model string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if strings.TrimSpace(model) != "" {
		sm.defaultModel = model
	}
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

// SetAllowedCapabilities sets the explicit capability scope for delegated
// subtasks. Non-empty is deny-by-default: a subtask may only use tools whose
// resolved capability is listed. The parent must not delegate capabilities
// it does not itself possess.
func (sm *SubagentManager) SetAllowedCapabilities(caps []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.allowedCapabilities = append([]string(nil), caps...)
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
	// Actuator/privileged primitives a subagent must never inherit just
	// because a generic surface exists. A subagent has no computer
	// authority, no third-party MCP authority, and no hardware/self-update
	// authority; those belong to a governed main-agent turn. Browser stays
	// available (research subagents legitimately use it) and is still
	// broker-gated on execution.
	for _, actuator := range []string{"hass", "i2c", "spi", "update", "networking"} {
		blocked[actuator] = struct{}{}
	}
	for _, name := range baseTools.List() {
		if strings.HasPrefix(name, "computer_") || strings.HasPrefix(name, "mcp_") {
			blocked[name] = struct{}{}
		}
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
	return sm.runLoopRecording(ctx, taskPrompt, originChannel, originChatID, label, nil)
}

// runLoopRecording is runLoop with an optional per-step recorder: a durable
// run leaves a trace as it goes, an in-memory one has nothing to resume
// from and so records nothing.
func (sm *SubagentManager) runLoopRecording(ctx context.Context, taskPrompt, originChannel, originChatID, label string, checkpoint func(string)) (*ToolLoopResult, error) {
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

	sm.mu.RLock()
	browserAuth := sm.BrowserAuth
	consequentialAuth := sm.ConsequentialAuth
	allowedCaps := sm.allowedCapabilities
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
		BrowserAuth:         browserAuth,
		ConsequentialAuth:   consequentialAuth,
		AllowedCapabilities: allowedCaps,
		Checkpoint:          checkpoint,
	}, messages, originChannel, originChatID)
}

// SpawnWithSchema spawns a subagent with an output schema for structured output.
func (sm *SubagentManager) SpawnWithSchema(ctx context.Context, task, label, originChannel, originChatID string, schema *OutputSchema, callback AsyncCallback) (string, error) {
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
	logCh := make(chan SubagentLogEntry, 100)
	subagentTask := &SubagentTask{
		ID:            taskID,
		Task:          task,
		Label:         label,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		Status:        "running",
		Created:       time.Now().UnixMilli(),
		OutputSchema:  schema,
		Logs:          make([]SubagentLogEntry, 0),
		LogChannel:    logCh,
	}
	sm.tasks[taskID] = subagentTask
	sm.mu.Unlock()

	go sm.runTask(childCtx, subagentTask, callback)

	if label != "" {
		return fmt.Sprintf("Spawned subagent '%s' for task: %s", label, task), nil
	}
	return fmt.Sprintf("Spawned subagent for task: %s", task), nil
}

// SubscribeLogs returns a channel that streams live logs for a specific task.
func (sm *SubagentManager) SubscribeLogs(taskID string) (<-chan SubagentLogEntry, error) {
	sm.mu.RLock()
	task, ok := sm.tasks[taskID]
	sm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}

	if task.LogChannel == nil {
		return nil, fmt.Errorf("log channel not available for task %q", taskID)
	}

	return task.LogChannel, nil
}

// GetTaskLogs returns all collected logs for a task.
func (sm *SubagentManager) GetTaskLogs(taskID string) ([]SubagentLogEntry, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	task, ok := sm.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}

	logs := make([]SubagentLogEntry, len(task.Logs))
	copy(logs, task.Logs)
	return logs, nil
}

// emitLog sends a log entry to the task's log channel and stores it.
func (sm *SubagentManager) emitLog(task *SubagentTask, level, source, message string) {
	entry := SubagentLogEntry{
		TaskID:    task.ID,
		Timestamp: time.Now(),
		Level:     level,
		Source:    source,
		Message:   message,
	}

	sm.mu.Lock()
	task.Logs = append(task.Logs, entry)
	logCh := task.LogChannel
	sm.mu.Unlock()

	if logCh != nil {
		select {
		case logCh <- entry:
		default:
			// Channel full, drop to avoid blocking
		}
	}
}

func (sm *SubagentManager) Spawn(ctx context.Context, task, label, originChannel, originChatID string, callback AsyncCallback) (string, error) {
	childCtx, _, err := sm.childContext(ctx)
	if err != nil {
		return "", err
	}

	// Durable first: record the task and hand execution to the runner.
	// The slot is not taken here — a queued job is not running work, and
	// holding a concurrency slot for something waiting its turn would
	// starve the runs that are actually going.
	sm.mu.RLock()
	enqueue := sm.enqueue
	sm.mu.RUnlock()
	if enqueue != nil {
		jobID, err := enqueue(SpawnRequest{Task: task, Label: label, Channel: originChannel, ChatID: originChatID,
			SessionKey: SessionKeyFromContext(ctx)})
		if err != nil {
			return "", err
		}
		sm.ensureTask(jobID, task, label, originChannel, originChatID, "queued")
		if label != "" {
			return fmt.Sprintf("Spawned subagent '%s' for task: %s (recorded as job %s; it survives a restart)", label, task, jobID), nil
		}
		return fmt.Sprintf("Spawned subagent for task: %s (recorded as job %s; it survives a restart)", task, jobID), nil
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

// ensureTask returns the in-memory record of a background run, creating it
// if neither side has seen it yet. It is idempotent on purpose: the
// spawner and the runner can arrive in either order (the runner may even
// start before Spawn has finished talking to the store) and they must share
// one record rather than hold two that disagree about what is running.
func (sm *SubagentManager) ensureTask(id, task, label, channel, chatID, status string) *SubagentTask {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if t, ok := sm.tasks[id]; ok {
		return t
	}
	t := &SubagentTask{
		ID:            id,
		Task:          task,
		Label:         label,
		OriginChannel: channel,
		OriginChatID:  chatID,
		Status:        status,
		Created:       time.Now().UnixMilli(),
	}
	sm.tasks[id] = t
	return t
}

// durableSubagentTimeout bounds one attempt of a job-backed run. It is far
// longer than an in-memory spawn's: the record outlives the process, so a
// run that runs out of time is a retry rather than work that disappeared.
const durableSubagentTimeout = 2 * time.Hour

// RunDurable executes one attempt of a recorded background task. The runner
// owns this call — it decides when to try, how long to wait, and when the
// budget is spent — and everything this learns is written back through the
// two callbacks, so what a restart finds is what actually happened rather
// than what this process happened to remember.
//
// It returns nil when the task is done, an *ApprovalWait when it stopped at
// something the owner has to allow, and any other error for a failure the
// runner may try again.
func (sm *SubagentManager) RunDurable(ctx context.Context, a DurableAttempt) error {
	if err := sm.acquireSlot(); err != nil {
		// A full concurrency budget is "later", not "no": the job waits
		// its turn instead of spending an attempt on a busy moment.
		return &AtCapacityError{Delay: 30 * time.Second}
	}
	defer sm.releaseSlot()

	task := sm.ensureTask(a.JobID, a.Task, a.Label, a.Channel, a.ChatID, "running")
	sm.mu.Lock()
	task.Status = "running"
	sm.mu.Unlock()
	sm.emitLog(task, "info", "system", "Background job attempt starting")

	prompt := a.Task
	if strings.TrimSpace(a.Resume) != "" {
		prompt = resumePrompt(a.Resume, a.Task)
	}

	runCtx, cancel := context.WithTimeout(ctx, durableSubagentTimeout)
	defer cancel()

	loopResult, err := sm.runLoopRecording(runCtx, prompt, a.Channel, a.ChatID, a.Label, a.Checkpoint)
	if err == nil && loopResult != nil && loopResult.NeedsApproval != "" {
		sm.setTaskState(task, "waiting", loopResult.NeedsApproval)
		sm.emitLog(task, "warn", "system", "Stopped for approval: "+loopResult.NeedsApproval)
		return &ApprovalWait{Reason: loopResult.NeedsApproval}
	}
	if err != nil {
		msg := "Error: " + err.Error()
		if runCtx.Err() == context.DeadlineExceeded {
			msg = "Ran out of time before it finished"
		}
		sm.setTaskState(task, "failed", msg)
		sm.emitLog(task, "error", "system", msg)
		return err
	}
	sm.setTaskState(task, "completed", loopResult.Content)
	sm.emitLog(task, "info", "system", fmt.Sprintf("Task completed in %d iterations", loopResult.Iterations))
	if a.Evidence != nil {
		a.Evidence(loopResult.Content)
	}
	return nil
}

// setTaskState records an outcome under the manager's lock; the task's
// result is read by surfaces that must never see a half-written string.
func (sm *SubagentManager) setTaskState(task *SubagentTask, status, result string) {
	sm.mu.Lock()
	task.Status = status
	task.Result = result
	sm.mu.Unlock()
}

// resumePrompt stitches what earlier attempts confirmed onto the task, so a
// restarted run is told where it got to instead of quietly starting again.
// The difference matters: re-doing work that already happened is how a
// resumed job ends up sending the same message twice.
func resumePrompt(resume, task string) string {
	return "You are continuing a task that was interrupted partway through. " +
		"These steps are already confirmed done — do not repeat them:\n" + resume +
		"\n\nCarry on from there and finish the task.\n\nTask:\n" + task
}

func (sm *SubagentManager) runTask(ctx context.Context, task *SubagentTask, callback AsyncCallback) {
	defer sm.releaseSlot()
	defer func() {
		if task.LogChannel != nil {
			close(task.LogChannel)
		}
	}()

	sm.emitLog(task, "info", "system", "Subagent task starting")
	task.Status = "running"
	task.Created = time.Now().UnixMilli()
	runCtx, cancel := context.WithTimeout(ctx, subagentTimeout)
	defer cancel()

	// Check if context is already cancelled before starting
	select {
	case <-runCtx.Done():
		sm.mu.Lock()
		task.Status = "cancelled"
		task.Result = "Task cancelled before execution"
		sm.mu.Unlock()
		sm.emitLog(task, "warn", "system", "Task cancelled before execution")
		return
	default:
	}

	sm.emitLog(task, "info", "system", fmt.Sprintf("Running with timeout: %v", subagentTimeout))
	loopResult, err := sm.runLoop(runCtx, task.Task, task.OriginChannel, task.OriginChatID, task.Label)

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
		if runCtx.Err() != nil {
			task.Status = "cancelled"
			task.Result = "Task cancelled during execution"
			if runCtx.Err() == context.DeadlineExceeded {
				task.Status = "failed"
				task.Result = "Task timed out"
			}
		}
		sm.emitLog(task, "error", "system", task.Result)
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
		sm.emitLog(task, "info", "system", fmt.Sprintf("Task completed in %d iterations", loopResult.Iterations))
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
	runCtx, cancel := context.WithTimeout(childCtx, subagentTimeout)
	defer cancel()
	return sm.runLoop(runCtx, task, originChannel, originChatID, label)
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
	return "Run a subagent on a task and WAIT for its result before you continue. Use for a bounded step you need the answer to now. For long work that should not block the conversation, use spawn instead."
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
