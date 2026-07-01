package agent

import (
	"testing"
	"time"
)

func TestSteeringQueue_BasicEnqueueDequeue(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 10)

	msg := SteeringMessage{
		Content:    "test message",
		SessionKey: "session-1",
		Timestamp:  time.Now(),
	}

	if !q.Enqueue(msg) {
		t.Fatal("Expected enqueue to succeed")
	}

	if q.PendingCount("session-1") != 1 {
		t.Errorf("Expected 1 pending, got %d", q.PendingCount("session-1"))
	}

	msgs := q.Dequeue("session-1")
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "test message" {
		t.Errorf("Expected 'test message', got '%s'", msgs[0].Content)
	}

	if q.PendingCount("session-1") != 0 {
		t.Errorf("Expected 0 pending after dequeue, got %d", q.PendingCount("session-1"))
	}
}

func TestSteeringQueue_OneAtATime(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 10)

	q.Enqueue(SteeringMessage{Content: "msg1", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "msg2", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "msg3", SessionKey: "s1"})

	// Dequeue one at a time
	msgs := q.Dequeue("s1")
	if len(msgs) != 1 || msgs[0].Content != "msg1" {
		t.Errorf("Expected [msg1], got %v", msgs)
	}

	msgs = q.Dequeue("s1")
	if len(msgs) != 1 || msgs[0].Content != "msg2" {
		t.Errorf("Expected [msg2], got %v", msgs)
	}

	msgs = q.Dequeue("s1")
	if len(msgs) != 1 || msgs[0].Content != "msg3" {
		t.Errorf("Expected [msg3], got %v", msgs)
	}

	msgs = q.Dequeue("s1")
	if len(msgs) != 0 {
		t.Errorf("Expected empty, got %v", msgs)
	}
}

func TestSteeringQueue_All(t *testing.T) {
	q := NewSteeringQueue(SteeringAll, 10)

	q.Enqueue(SteeringMessage{Content: "msg1", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "msg2", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "msg3", SessionKey: "s1"})

	msgs := q.Dequeue("s1")
	if len(msgs) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "msg1" || msgs[1].Content != "msg2" || msgs[2].Content != "msg3" {
		t.Errorf("Unexpected messages: %v", msgs)
	}

	// Queue should be empty now
	msgs = q.Dequeue("s1")
	if len(msgs) != 0 {
		t.Errorf("Expected empty queue, got %v", msgs)
	}
}

func TestSteeringQueue_MaxSize(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 3)

	q.Enqueue(SteeringMessage{Content: "msg1", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "msg2", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "msg3", SessionKey: "s1"})

	// Queue is full, next enqueue should drop oldest
	if !q.Enqueue(SteeringMessage{Content: "msg4", SessionKey: "s1"}) {
		t.Fatal("Expected enqueue to succeed (with drop)")
	}

	msgs := q.Dequeue("s1")
	if len(msgs) != 1 || msgs[0].Content != "msg2" {
		t.Errorf("Expected [msg2] (oldest dropped), got %v", msgs)
	}
}

func TestSteeringQueue_SessionScoping(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 10)

	q.Enqueue(SteeringMessage{Content: "s1-msg", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "s2-msg", SessionKey: "s2"})

	msgs1 := q.Dequeue("s1")
	msgs2 := q.Dequeue("s2")

	if len(msgs1) != 1 || msgs1[0].Content != "s1-msg" {
		t.Errorf("Expected s1 message, got %v", msgs1)
	}
	if len(msgs2) != 1 || msgs2[0].Content != "s2-msg" {
		t.Errorf("Expected s2 message, got %v", msgs2)
	}
}

func TestSteeringQueue_TotalCount(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 10)

	q.Enqueue(SteeringMessage{Content: "a", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "b", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "c", SessionKey: "s2"})

	if q.TotalCount() != 3 {
		t.Errorf("Expected total count 3, got %d", q.TotalCount())
	}
}

func TestSteeringQueue_Clear(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 10)

	q.Enqueue(SteeringMessage{Content: "a", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "b", SessionKey: "s2"})

	q.Clear("s1")

	if q.PendingCount("s1") != 0 {
		t.Error("Expected s1 to be empty after clear")
	}
	if q.PendingCount("s2") != 1 {
		t.Error("Expected s2 to still have 1 message")
	}
}

func TestSteeringQueue_ClearAll(t *testing.T) {
	q := NewSteeringQueue(SteeringOneAtATime, 10)

	q.Enqueue(SteeringMessage{Content: "a", SessionKey: "s1"})
	q.Enqueue(SteeringMessage{Content: "b", SessionKey: "s2"})

	q.ClearAll()

	if q.TotalCount() != 0 {
		t.Errorf("Expected 0 total, got %d", q.TotalCount())
	}
}

func TestSteeringManager_Inject(t *testing.T) {
	sm := NewSteeringManager()

	msg := SteeringMessage{
		Content:    "correction",
		SessionKey: "session-1",
	}

	if !sm.Inject(msg) {
		t.Fatal("Expected inject to succeed")
	}

	if sm.PendingCount("session-1") != 1 {
		t.Errorf("Expected 1 pending, got %d", sm.PendingCount("session-1"))
	}

	msgs := sm.DrainPending("session-1")
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "correction" {
		t.Errorf("Expected 'correction', got '%s'", msgs[0].Content)
	}
}

func TestSteeringManager_Interrupt(t *testing.T) {
	sm := NewSteeringManager()

	sm.Interrupt("session-1")

	if !sm.CheckInterrupt("session-1") {
		t.Error("Expected interrupt flag to be set")
	}

	// Should be cleared after check
	if sm.CheckInterrupt("session-1") {
		t.Error("Expected interrupt flag to be cleared after check")
	}

	msgs := sm.DrainPending("session-1")
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 interrupt message, got %d", len(msgs))
	}
	if !msgs[0].IsInterrupt {
		t.Error("Expected message to be marked as interrupt")
	}
}

func TestSteeringManager_HardAbort(t *testing.T) {
	sm := NewSteeringManager()

	sm.HardAbort("session-1")

	msgs := sm.DrainPending("session-1")
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 abort message, got %d", len(msgs))
	}
	if !msgs[0].IsHardAbort {
		t.Error("Expected message to be marked as hard abort")
	}
}

func TestFormatForPrompt_Empty(t *testing.T) {
	result := FormatForPrompt(nil)
	if result != "" {
		t.Errorf("Expected empty string, got '%s'", result)
	}
}

func TestFormatForPrompt_WithMessages(t *testing.T) {
	msgs := []SteeringMessage{
		{Content: "correct this", IsInterrupt: false},
		{Content: "stop now", IsInterrupt: true},
		{Content: "abort everything", IsHardAbort: true},
	}

	result := FormatForPrompt(msgs)

	if result == "" {
		t.Fatal("Expected non-empty result")
	}
	if !contains(result, "correct this") {
		t.Error("Expected 'correct this' in result")
	}
	if !contains(result, "[INTERRUPT]") {
		t.Error("Expected [INTERRUPT] tag in result")
	}
	if !contains(result, "[ABORT]") {
		t.Error("Expected [ABORT] tag in result")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
