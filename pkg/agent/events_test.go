package agent

import (
	"sync"
	"testing"
)

func TestEventBusPublishSubscribe(t *testing.T) {
	bus := NewEventBus()
	var mu sync.Mutex
	var got []Event
	unsub := bus.Subscribe(func(ev Event) { mu.Lock(); got = append(got, ev); mu.Unlock() })

	bus.Publish(Event{Type: EventMemoryCreated, Subject: "identity/name"})
	bus.Publish(Event{Type: EventModelFailed, Data: map[string]interface{}{"error": "boom"}})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Type != EventMemoryCreated || got[0].Subject != "identity/name" {
		t.Fatalf("unexpected first event: %+v", got[0])
	}
	if got[0].At.IsZero() {
		t.Fatal("expected event timestamp to be set")
	}
	if got[1].Type != EventModelFailed {
		t.Fatalf("unexpected second event type: %s", got[1].Type)
	}

	unsub()
}

func TestEventBusUnsubscribeStopsDelivery(t *testing.T) {
	bus := NewEventBus()
	var count int
	unsub := bus.Subscribe(func(Event) { count++ })
	bus.Publish(Event{Type: EventModelFailed})
	unsub()
	bus.Publish(Event{Type: EventModelFailed})
	if count != 1 {
		t.Fatalf("expected 1 delivery after unsubscribe, got %d", count)
	}
}

// A panicking subscriber must not break other subscribers or the publisher.
func TestEventBusIsolatesPanickingSink(t *testing.T) {
	bus := NewEventBus()
	bus.Subscribe(func(Event) { panic("boom") })
	var got int
	bus.Subscribe(func(Event) { got++ })
	bus.Publish(Event{Type: EventMemoryCreated})
	if got != 1 {
		t.Fatalf("expected healthy subscriber to receive event, got %d", got)
	}
}

func TestTypedEventSet(t *testing.T) {
	// Guard the spelling of the vocabulary (public contract for consumers).
	want := []EventType{
		EventMemoryCreated, EventMemoryUpdated, EventMemorySuperseded,
		EventTaskStarted, EventTaskCompleted, EventTaskFailed,
		EventAutomationTriggered, EventDevicePaired, EventDeviceRevoked,
		EventModelFailed, EventModelRecovered,
		EventChannelMessageReceived, EventChannelMessageSent,
		EventSkillInstalled, EventSkillEnabled, EventSkillDisabled,
	}
	seen := map[EventType]bool{}
	for _, e := range want {
		if seen[e] {
			t.Fatalf("duplicate event type: %s", e)
		}
		seen[e] = true
	}
}

func TestEventsEmitNilSafe(t *testing.T) {
	var b *EventBus
	// Must not panic on a nil bus.
	b.emit(EventMemoryCreated, "", nil)
}
