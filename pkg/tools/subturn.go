package tools

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

const (
	DefaultSubTurnMaxDepth       = 3
	DefaultSubTurnMaxConcurrency = 5
	DefaultSubTurnTimeout        = 5 * time.Minute
	DefaultSubTurnMaxMessages    = 50
)

// SubTurnConfig configures a sub-turn execution.
type SubTurnConfig struct {
	ParentSessionKey string
	Channel          string
	ChatID           string
	Model            string
	SystemPrompt     string
	Tools            *ToolRegistry
	MaxIterations    int
	Timeout          time.Duration
	Async            bool
	MaxContextRunes  int
}

// SubTurnResult contains the result of a sub-turn.
type SubTurnResult struct {
	Content    string
	Iterations int
	Duration   time.Duration
	Depth      int
}

// SubTurnState tracks the state of a running sub-turn.
type SubTurnState struct {
	ID          string
	Config      SubTurnConfig
	Status      string // "running", "completed", "failed", "cancelled"
	Result      *SubTurnResult
	ParentID    string
	Depth       int
	CreatedAt   time.Time
	CompletedAt time.Time
}

// subTurnDepthKey is the context key for sub-turn depth.
type subTurnDepthKey struct{}

// SubTurnManager manages sub-turn execution with depth limits and concurrency control.
type SubTurnManager struct {
	provider     providers.LLMProvider
	mu           sync.RWMutex
	activeCount  int64
	nextID       int64
	states       map[string]*SubTurnState
	maxDepth     int
	maxConc      int
	timeout      time.Duration
	maxMessages  int
}

// NewSubTurnManager creates a new SubTurnManager.
func NewSubTurnManager(provider providers.LLMProvider) *SubTurnManager {
	return &SubTurnManager{
		provider:    provider,
		states:      make(map[string]*SubTurnState),
		maxDepth:    DefaultSubTurnMaxDepth,
		maxConc:     DefaultSubTurnMaxConcurrency,
		timeout:     DefaultSubTurnTimeout,
		maxMessages: DefaultSubTurnMaxMessages,
	}
}

// SetLimits configures the sub-turn manager limits.
func (stm *SubTurnManager) SetLimits(maxDepth, maxConcurrency int, timeout time.Duration) {
	stm.mu.Lock()
	defer stm.mu.Unlock()
	if maxDepth > 0 {
		stm.maxDepth = maxDepth
	}
	if maxConcurrency > 0 {
		stm.maxConc = maxConcurrency
	}
	if timeout > 0 {
		stm.timeout = timeout
	}
}

// WithSubTurnDepth adds depth information to a context.
func WithSubTurnDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subTurnDepthKey{}, depth)
}

// SubTurnDepth retrieves the sub-turn depth from a context.
func SubTurnDepth(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if depth, ok := ctx.Value(subTurnDepthKey{}).(int); ok {
		return depth
	}
	return 0
}

// acquireSlot tries to acquire a concurrency slot.
func (stm *SubTurnManager) acquireSlot() error {
	current := atomic.LoadInt64(&stm.activeCount)
	if int(current) >= stm.maxConc {
		return fmt.Errorf("sub-turn concurrency limit reached (%d)", stm.maxConc)
	}
	if !atomic.CompareAndSwapInt64(&stm.activeCount, current, current+1) {
		return fmt.Errorf("concurrent slot acquisition failed")
	}
	return nil
}

// releaseSlot releases a concurrency slot.
func (stm *SubTurnManager) releaseSlot() {
	atomic.AddInt64(&stm.activeCount, -1)
}

// ActiveCount returns the number of active sub-turns.
func (stm *SubTurnManager) ActiveCount() int {
	return int(atomic.LoadInt64(&stm.activeCount))
}

// Run executes a sub-turn synchronously.
func (stm *SubTurnManager) Run(ctx context.Context, config SubTurnConfig) (*SubTurnResult, error) {
	// Check depth first (before acquiring slot)
	currentDepth := SubTurnDepth(ctx)
	if currentDepth >= stm.maxDepth {
		return nil, fmt.Errorf("sub-turn depth %d exceeds max depth %d", currentDepth+1, stm.maxDepth)
	}

	// Acquire concurrency slot
	if err := stm.acquireSlot(); err != nil {
		return nil, err
	}
	defer stm.releaseSlot()

	// Generate ID
	id := fmt.Sprintf("subturn-%d", atomic.AddInt64(&stm.nextID, 1))

	// Create state
	stm.mu.Lock()
	state := &SubTurnState{
		ID:        id,
		Config:    config,
		Status:    "running",
		ParentID:  config.ParentSessionKey,
		Depth:     currentDepth + 1,
		CreatedAt: time.Now(),
	}
	stm.states[id] = state
	stm.mu.Unlock()

	// Set timeout
	timeout := stm.timeout
	if config.Timeout > 0 {
		timeout = config.Timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create child context with incremented depth
	childCtx := WithSubTurnDepth(runCtx, currentDepth+1)

	// Build messages
	messages := []providers.Message{
		{Role: "system", Content: config.SystemPrompt},
	}

	// Run the tool loop
	maxIter := config.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	tools := config.Tools
	if tools == nil {
		tools = NewToolRegistry()
	}

	result, err := RunToolLoop(childCtx, ToolLoopConfig{
		Provider:      stm.provider,
		Model:         config.Model,
		Tools:         tools,
		MaxIterations: maxIter,
		LLMOptions: map[string]any{
			"max_tokens":  4096,
			"temperature": 0.7,
		},
	}, messages, config.Channel, config.ChatID)

	duration := time.Since(state.CreatedAt)

	// Update state
	stm.mu.Lock()
	if err != nil {
		state.Status = "failed"
	} else {
		state.Status = "completed"
		state.Result = &SubTurnResult{
			Content:    result.Content,
			Iterations: result.Iterations,
			Duration:   duration,
			Depth:      currentDepth + 1,
		}
	}
	state.CompletedAt = time.Now()
	stm.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return state.Result, nil
}

// GetState returns the state of a sub-turn.
func (stm *SubTurnManager) GetState(id string) (*SubTurnState, bool) {
	stm.mu.RLock()
	defer stm.mu.RUnlock()
	state, ok := stm.states[id]
	if !ok {
		return nil, false
	}
	cp := *state
	return &cp, true
}

// ListStates returns all sub-turn states.
func (stm *SubTurnManager) ListStates() []*SubTurnState {
	stm.mu.RLock()
	defer stm.mu.RUnlock()
	result := make([]*SubTurnState, 0, len(stm.states))
	for _, s := range stm.states {
		cp := *s
		result = append(result, &cp)
	}
	return result
}

// EphemeralSessionStore is an in-memory session store for sub-turns.
// It auto-truncates to maxMessages and never persists to disk.
type EphemeralSessionStore struct {
	messages     map[string][]providers.Message
	summaries    map[string]string
	maxMessages  int
	mu           sync.RWMutex
}

// NewEphemeralSessionStore creates a new ephemeral session store.
func NewEphemeralSessionStore(maxMessages int) *EphemeralSessionStore {
	if maxMessages <= 0 {
		maxMessages = DefaultSubTurnMaxMessages
	}
	return &EphemeralSessionStore{
		messages:    make(map[string][]providers.Message),
		summaries:   make(map[string]string),
		maxMessages: maxMessages,
	}
}

func (es *EphemeralSessionStore) EnsureSession(key string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	if _, ok := es.messages[key]; !ok {
		es.messages[key] = make([]providers.Message, 0)
	}
}

func (es *EphemeralSessionStore) AddFullMessage(key string, msg providers.Message) {
	es.mu.Lock()
	defer es.mu.Unlock()
	if _, ok := es.messages[key]; !ok {
		es.messages[key] = make([]providers.Message, 0)
	}
	es.messages[key] = append(es.messages[key], msg)
	// Auto-truncate if over limit
	if len(es.messages[key]) > es.maxMessages {
		es.messages[key] = es.messages[key][len(es.messages[key])-es.maxMessages:]
	}
}

func (es *EphemeralSessionStore) GetHistory(key string) []providers.Message {
	es.mu.RLock()
	defer es.mu.RUnlock()
	msgs, ok := es.messages[key]
	if !ok {
		return nil
	}
	result := make([]providers.Message, len(msgs))
	copy(result, msgs)
	return result
}

func (es *EphemeralSessionStore) GetSummary(key string) string {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.summaries[key]
}

func (es *EphemeralSessionStore) SetSummary(key, summary string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.summaries[key] = summary
}

func (es *EphemeralSessionStore) TruncateHistory(key string, keepLast int) {
	es.mu.Lock()
	defer es.mu.Unlock()
	msgs, ok := es.messages[key]
	if !ok {
		return
	}
	if keepLast <= 0 {
		delete(es.messages, key)
		return
	}
	if len(msgs) > keepLast {
		es.messages[key] = msgs[len(msgs)-keepLast:]
	}
}

func (es *EphemeralSessionStore) SetHistory(key string, messages []providers.Message) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.messages[key] = messages
}

func (es *EphemeralSessionStore) Save(key string) error {
	return nil // No-op for ephemeral store
}

// DeleteSession removes all messages and the summary for a session.
func (es *EphemeralSessionStore) DeleteSession(key string) error {
	es.Clear(key)
	return nil
}

// Clear removes all messages for a session.
func (es *EphemeralSessionStore) Clear(key string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	delete(es.messages, key)
	delete(es.summaries, key)
}

// MessageCount returns the number of messages for a session.
func (es *EphemeralSessionStore) MessageCount(key string) int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.messages[key])
}

// SessionCount returns the number of sessions.
func (es *EphemeralSessionStore) SessionCount() int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.messages)
}

// Reset clears all sessions.
func (es *EphemeralSessionStore) Reset() {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.messages = make(map[string][]providers.Message)
	es.summaries = make(map[string]string)
}
