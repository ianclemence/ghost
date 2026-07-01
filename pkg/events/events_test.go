package events

import (
	"context"
	"testing"
	"time"
)

func TestEventBus_PublishAndSubscribe(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name:       "test-sub",
		BufferSize: 10,
	})
	defer cancel()

	evt := NewEvent(KindAgentTurnStart, "agent")
	bus.Publish(context.Background(), evt)

	select {
	case received := <-ch:
		if received.Kind != KindAgentTurnStart {
			t.Errorf("Expected kind %s, got %s", KindAgentTurnStart, received.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for event")
	}
}

func TestEventBus_FilterByKind(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(OfKind(KindToolBefore, KindToolAfter), SubscriptionOptions{
		Name:       "tool-events",
		BufferSize: 10,
	})
	defer cancel()

	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))
	bus.Publish(context.Background(), NewEvent(KindToolBefore, "agent"))
	bus.Publish(context.Background(), NewEvent(KindToolAfter, "agent"))

	// Should only receive tool events
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for tool.before event")
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for tool.after event")
	}

	// Should not have more events
	select {
	case evt := <-ch:
		t.Errorf("Unexpected event: %s", evt.Kind)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_FilterByKindPrefix(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(KindPrefix("tool."), SubscriptionOptions{
		Name:       "tool-prefix",
		BufferSize: 10,
	})
	defer cancel()

	bus.Publish(context.Background(), NewEvent(KindToolBefore, "agent"))
	bus.Publish(context.Background(), NewEvent(KindToolError, "agent"))
	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))

	// Should receive 2 tool events
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	// No more tool events
	select {
	case evt := <-ch:
		t.Errorf("Unexpected event: %s", evt.Kind)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_FilterBySource(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(Source("agent"), SubscriptionOptions{
		Name:       "agent-source",
		BufferSize: 10,
	})
	defer cancel()

	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))
	bus.Publish(context.Background(), NewEvent(KindChannelMessageIn, "channel"))
	bus.Publish(context.Background(), NewEvent(KindAgentTurnEnd, "agent"))

	// Should receive 2 agent events
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case evt := <-ch:
		t.Errorf("Unexpected event from source: %s", evt.Source)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_FilterAnd(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(
		And(
			OfKind(KindToolBefore),
			Source("agent"),
		),
		SubscriptionOptions{
			Name:       "and-filter",
			BufferSize: 10,
		},
	)
	defer cancel()

	bus.Publish(context.Background(), NewEvent(KindToolBefore, "agent"))
	bus.Publish(context.Background(), NewEvent(KindToolBefore, "channel"))
	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))

	// Should receive only 1 event (kind=tool.before AND source=agent)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case evt := <-ch:
		t.Errorf("Unexpected event: %s from %s", evt.Kind, evt.Source)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_FilterOr(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(
		Or(
			OfKind(KindAgentTurnStart),
			OfKind(KindAgentTurnEnd),
		),
		SubscriptionOptions{
			Name:       "or-filter",
			BufferSize: 10,
		},
	)
	defer cancel()

	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))
	bus.Publish(context.Background(), NewEvent(KindToolBefore, "agent"))
	bus.Publish(context.Background(), NewEvent(KindAgentTurnEnd, "agent"))

	// Should receive 2 events (turn start and turn end)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case evt := <-ch:
		t.Errorf("Unexpected event: %s", evt.Kind)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_FilterNot(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(
		Not(OfKind(KindToolBefore)),
		SubscriptionOptions{
			Name:       "not-filter",
			BufferSize: 10,
		},
	)
	defer cancel()

	bus.Publish(context.Background(), NewEvent(KindToolBefore, "agent"))
	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))

	// Should receive only 1 event (not tool.before)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case evt := <-ch:
		t.Errorf("Unexpected event: %s", evt.Kind)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_PublishNonBlocking(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name:       "full-buffer",
		BufferSize: 1,
		Backpressure: DropNewest,
	})
	defer cancel()

	// Fill the buffer
	bus.PublishNonBlocking(NewEvent(KindAgentTurnStart, "agent"))

	// This should be dropped (buffer full)
	bus.PublishNonBlocking(NewEvent(KindAgentTurnEnd, "agent"))

	// Should have only 1 event
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case evt := <-ch:
		t.Errorf("Unexpected second event: %s", evt.Kind)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}

	stats := bus.Stats()
	if stats.TotalDropped == 0 {
		t.Error("Expected some dropped events")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus()

	ch1, cancel1 := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "sub1", BufferSize: 10,
	})
	defer cancel1()

	ch2, cancel2 := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "sub2", BufferSize: 10,
	})
	defer cancel2()

	evt := NewEvent(KindAgentTurnStart, "agent")
	bus.Publish(context.Background(), evt)

	// Both should receive
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("Sub1 timed out")
	}

	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("Sub2 timed out")
	}
}

func TestEventBus_CancelSubscription(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "cancellable", BufferSize: 10,
	})

	cancel()

	// After cancel, publishing should not send to this subscriber
	// (it's removed from the list)
	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))

	select {
	case <-ch:
		t.Error("Should not receive after cancel")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_Stats(t *testing.T) {
	bus := NewEventBus()

	_, cancel1 := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "s1", BufferSize: 10,
	})
	_, cancel2 := bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "s2", BufferSize: 10,
	})

	bus.Publish(context.Background(), NewEvent(KindAgentTurnStart, "agent"))
	bus.Publish(context.Background(), NewEvent(KindAgentTurnEnd, "agent"))

	stats := bus.Stats()
	if stats.TotalReceived != 2 {
		t.Errorf("Expected 2 received, got %d", stats.TotalReceived)
	}
	if stats.Subscriptions != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", stats.Subscriptions)
	}

	cancel2()
	cancel1()

	stats = bus.Stats()
	if stats.Subscriptions != 0 {
		t.Errorf("Expected 0 subscriptions after cancel, got %d", stats.Subscriptions)
	}
}

func TestEventBus_PriorityOrdering(t *testing.T) {
	bus := NewEventBus()

	// Subscribe with high priority first
	bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "high", BufferSize: 10, Priority: 100,
	})

	// Subscribe with low priority second
	bus.Subscribe(AlwaysMatch, SubscriptionOptions{
		Name: "low", BufferSize: 10, Priority: 1,
	})

	// Verify subs are sorted by priority
	bus.subMu.RLock()
	if len(bus.subs) != 2 {
		t.Fatalf("Expected 2 subs, got %d", len(bus.subs))
	}
	if bus.subs[0].name != "high" {
		t.Errorf("Expected high priority first in subs, got %s", bus.subs[0].name)
	}
	if bus.subs[1].name != "low" {
		t.Errorf("Expected low priority second in subs, got %s", bus.subs[1].name)
	}
	bus.subMu.RUnlock()
}

func TestEvent_SeverityString(t *testing.T) {
	if SeverityInfo.String() != "info" {
		t.Errorf("Expected 'info', got '%s'", SeverityInfo.String())
	}
	if SeverityWarn.String() != "warn" {
		t.Errorf("Expected 'warn', got '%s'", SeverityWarn.String())
	}
	if SeverityError.String() != "error" {
		t.Errorf("Expected 'error', got '%s'", SeverityError.String())
	}
}

func TestEvent_ScopeFilter(t *testing.T) {
	bus := NewEventBus()

	ch, cancel := bus.Subscribe(
		Scope(map[string]string{"agent_id": "agent-1"}),
		SubscriptionOptions{
			Name:       "scope-filter",
			BufferSize: 10,
		},
	)
	defer cancel()

	evt1 := NewEvent(KindAgentTurnStart, "agent")
	evt1.Scope = map[string]string{"agent_id": "agent-1"}

	evt2 := NewEvent(KindAgentTurnStart, "agent")
	evt2.Scope = map[string]string{"agent_id": "agent-2"}

	bus.Publish(context.Background(), evt1)
	bus.Publish(context.Background(), evt2)

	// Should receive only evt1
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}

	select {
	case evt := <-ch:
		t.Errorf("Unexpected event for agent-2: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
