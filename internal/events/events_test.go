package events

import (
	"testing"
	"time"
)

func TestEventBus(t *testing.T) {
	bus := NewEventBus()

	ch, _ := bus.Subscribe("agent-123")

	go func() {
		bus.Publish("agent-123", "Hello from subagent")
	}()

	select {
	case msg := <-ch:
		if msg != "Hello from subagent" {
			t.Errorf("Expected 'Hello from subagent', got '%v'", msg)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()

	ch, id := bus.Subscribe("agent-456")
	bus.Unsubscribe("agent-456", id)

	// Should not block or panic when publishing to unsubscribed topic
	bus.Publish("agent-456", "Hello")

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Should not receive message after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
