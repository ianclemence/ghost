package tools

import (
	"context"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestSubTurnManager_BasicLimits(t *testing.T) {
	mgr := NewSubTurnManager(nil)

	if mgr.ActiveCount() != 0 {
		t.Errorf("Expected 0 active, got %d", mgr.ActiveCount())
	}

	mgr.SetLimits(5, 3, 10*time.Minute)

	mgr.mu.RLock()
	if mgr.maxDepth != 5 {
		t.Errorf("Expected maxDepth 5, got %d", mgr.maxDepth)
	}
	if mgr.maxConc != 3 {
		t.Errorf("Expected maxConc 3, got %d", mgr.maxConc)
	}
	mgr.mu.RUnlock()
}

func TestSubTurnManager_DepthTracking(t *testing.T) {
	ctx := context.Background()

	// No depth initially
	if SubTurnDepth(ctx) != 0 {
		t.Errorf("Expected depth 0, got %d", SubTurnDepth(ctx))
	}

	// Add depth
	ctx1 := WithSubTurnDepth(ctx, 0)
	if SubTurnDepth(ctx1) != 0 {
		t.Errorf("Expected depth 0, got %d", SubTurnDepth(ctx1))
	}

	// Nested depth
	ctx2 := WithSubTurnDepth(ctx1, 1)
	if SubTurnDepth(ctx2) != 1 {
		t.Errorf("Expected depth 1, got %d", SubTurnDepth(ctx2))
	}
}

func TestSubTurnManager_ConcurrencySlots(t *testing.T) {
	mgr := NewSubTurnManager(nil)
	mgr.SetLimits(3, 2, time.Minute)

	// Acquire first slot
	if err := mgr.acquireSlot(); err != nil {
		t.Fatalf("First acquire failed: %v", err)
	}
	if mgr.ActiveCount() != 1 {
		t.Errorf("Expected 1 active, got %d", mgr.ActiveCount())
	}

	// Acquire second slot
	if err := mgr.acquireSlot(); err != nil {
		t.Fatalf("Second acquire failed: %v", err)
	}
	if mgr.ActiveCount() != 2 {
		t.Errorf("Expected 2 active, got %d", mgr.ActiveCount())
	}

	// Third should fail
	if err := mgr.acquireSlot(); err == nil {
		t.Error("Expected third acquire to fail")
	}

	// Release one
	mgr.releaseSlot()
	if mgr.ActiveCount() != 1 {
		t.Errorf("Expected 1 active after release, got %d", mgr.ActiveCount())
	}

	// Now third should work
	if err := mgr.acquireSlot(); err != nil {
		t.Fatalf("Third acquire after release failed: %v", err)
	}

	mgr.releaseSlot()
	mgr.releaseSlot()
}

func TestSubTurnManager_GetAndListStates(t *testing.T) {
	mgr := NewSubTurnManager(nil)

	// Initially empty
	states := mgr.ListStates()
	if len(states) != 0 {
		t.Errorf("Expected 0 states, got %d", len(states))
	}

	// Add a state manually
	mgr.mu.Lock()
	mgr.states["test-1"] = &SubTurnState{
		ID:     "test-1",
		Status: "running",
		Depth:  1,
	}
	mgr.mu.Unlock()

	// Get state
	state, ok := mgr.GetState("test-1")
	if !ok {
		t.Fatal("Expected to find state")
	}
	if state.ID != "test-1" {
		t.Errorf("Expected ID 'test-1', got '%s'", state.ID)
	}

	// List should have 1
	states = mgr.ListStates()
	if len(states) != 1 {
		t.Errorf("Expected 1 state, got %d", len(states))
	}
}

func TestEphemeralSessionStore_BasicOperations(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	store.EnsureSession("s1")

	// Add messages
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "hello"})
	store.AddFullMessage("s1", providers.Message{Role: "assistant", Content: "hi"})

	history := store.GetHistory("s1")
	if len(history) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("Expected 'hello', got '%s'", history[0].Content)
	}

	// Summary
	store.SetSummary("s1", "test summary")
	if store.GetSummary("s1") != "test summary" {
		t.Error("Expected 'test summary'")
	}

	// Message count
	if store.MessageCount("s1") != 2 {
		t.Errorf("Expected 2, got %d", store.MessageCount("s1"))
	}
}

func TestEphemeralSessionStore_AutoTruncate(t *testing.T) {
	store := NewEphemeralSessionStore(3)

	store.EnsureSession("s1")

	for i := 0; i < 5; i++ {
		store.AddFullMessage("s1", providers.Message{Role: "user", Content: "msg"})
	}

	if store.MessageCount("s1") != 3 {
		t.Errorf("Expected auto-truncate to 3, got %d", store.MessageCount("s1"))
	}
}

func TestEphemeralSessionStore_TruncateHistory(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	store.EnsureSession("s1")
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "a"})
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "b"})
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "c"})

	store.TruncateHistory("s1", 2)

	if store.MessageCount("s1") != 2 {
		t.Errorf("Expected 2 after truncate, got %d", store.MessageCount("s1"))
	}
}

func TestEphemeralSessionStore_TruncateAll(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	store.EnsureSession("s1")
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "a"})

	store.TruncateHistory("s1", 0)

	if store.MessageCount("s1") != 0 {
		t.Errorf("Expected 0 after truncate all, got %d", store.MessageCount("s1"))
	}
}

func TestEphemeralSessionStore_Clear(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	store.EnsureSession("s1")
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "a"})
	store.SetSummary("s1", "summary")

	store.Clear("s1")

	if store.MessageCount("s1") != 0 {
		t.Error("Expected 0 messages after clear")
	}
	if store.GetSummary("s1") != "" {
		t.Error("Expected empty summary after clear")
	}
}

func TestEphemeralSessionStore_Reset(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	store.EnsureSession("s1")
	store.EnsureSession("s2")
	store.AddFullMessage("s1", providers.Message{Role: "user", Content: "a"})
	store.AddFullMessage("s2", providers.Message{Role: "user", Content: "b"})

	store.Reset()

	if store.SessionCount() != 0 {
		t.Errorf("Expected 0 sessions after reset, got %d", store.SessionCount())
	}
}

func TestEphemeralSessionStore_SessionCount(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	if store.SessionCount() != 0 {
		t.Errorf("Expected 0, got %d", store.SessionCount())
	}

	store.EnsureSession("s1")
	store.EnsureSession("s2")

	if store.SessionCount() != 2 {
		t.Errorf("Expected 2, got %d", store.SessionCount())
	}
}

func TestEphemeralSessionStore_SetHistory(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	store.SetHistory("s1", msgs)

	history := store.GetHistory("s1")
	if len(history) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(history))
	}
}

func TestEphemeralSessionStore_GetHistory_Empty(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	history := store.GetHistory("nonexistent")
	if history != nil {
		t.Errorf("Expected nil, got %v", history)
	}
}

func TestEphemeralSessionStore_Save_NoOp(t *testing.T) {
	store := NewEphemeralSessionStore(10)

	// Save should not error
	if err := store.Save("s1"); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestSubTurnManager_DepthExceeded(t *testing.T) {
	mgr := NewSubTurnManager(nil)
	mgr.SetLimits(2, 5, time.Minute)

	// Create context at max depth (depth 2, maxDepth 2, so next would be 3 > 2)
	ctx := WithSubTurnDepth(context.Background(), 1) // depth 1
	ctx = WithSubTurnDepth(ctx, 2)                    // depth 2

	// This should fail because depth would be 3 > maxDepth 2
	config := SubTurnConfig{
		SystemPrompt: "test",
	}

	_, err := mgr.Run(ctx, config)
	if err == nil {
		t.Error("Expected depth exceeded error")
	}
}

func TestSubTurnManager_DefaultLimits(t *testing.T) {
	mgr := NewSubTurnManager(nil)

	mgr.mu.RLock()
	if mgr.maxDepth != DefaultSubTurnMaxDepth {
		t.Errorf("Expected default maxDepth %d, got %d", DefaultSubTurnMaxDepth, mgr.maxDepth)
	}
	if mgr.maxConc != DefaultSubTurnMaxConcurrency {
		t.Errorf("Expected default maxConc %d, got %d", DefaultSubTurnMaxConcurrency, mgr.maxConc)
	}
	mgr.mu.RUnlock()
}
