package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/purav/microgravity/internal/events"
)

func TestScheduleTimer(t *testing.T) {
	s := New()
	defer s.Stop()
	ch := make(chan string, 1)

	s.ScheduleTimer(10*time.Millisecond, "timer fired", ConditionNever, "", ch)

	select {
	case msg := <-ch:
		if msg != "timer fired" {
			t.Errorf("expected 'timer fired', got %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for timer")
	}
}

func TestTimerConditionsAndEarlyCancel(t *testing.T) {
	s := New()
	defer s.Stop()
	ch := make(chan string, 1)

	// Timer 1: ConditionAny -> should be cancelled when any message arrives
	id1 := s.ScheduleTimer(500*time.Millisecond, "timer 1", ConditionAny, "", ch)

	// Timer 2: Specific Sender -> should be cancelled when "sub-123" arrives
	id2 := s.ScheduleTimer(500*time.Millisecond, "timer 2", ConditionNever, "sub-123", ch)

	// Timer 3: ConditionNever -> should NOT be cancelled by messages
	id3 := s.ScheduleTimer(20*time.Millisecond, "timer 3", ConditionNever, "", ch)

	cancelled := s.OnMessageReceived("sub-123")
	if cancelled != 2 {
		t.Errorf("expected 2 timers cancelled by OnMessageReceived, got %d", cancelled)
	}

	if s.CancelTimer(id1) {
		t.Errorf("expected timer 1 to already be cancelled")
	}
	if s.CancelTimer(id2) {
		t.Errorf("expected timer 2 to already be cancelled")
	}

	select {
	case msg := <-ch:
		if msg != "timer 3" {
			t.Errorf("expected 'timer 3', got %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected timer 3 to fire")
	}
	_ = id3
}

func TestScheduleCron(t *testing.T) {
	s := New()
	defer s.Stop()
	ch := make(chan string, 1)

	_, err := s.ScheduleCron("invalid cron", "msg", 0, ch)
	if err == nil {
		t.Fatal("expected error on invalid cron expression")
	}

	id, err := s.ScheduleCron("* * * * *", "msg", 0, ch)
	if err != nil {
		t.Fatalf("unexpected error scheduling valid cron: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero entry ID")
	}

	entry := s.cron.Entry(id)
	entry.Job.Run()

	select {
	case msg := <-ch:
		if msg != "msg" {
			t.Errorf("expected 'msg', got %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for cron notification")
	}
}

func TestScheduler_EventBusPublishing(t *testing.T) {
	s := New()
	defer s.Stop()
	eb := events.NewEventBus()
	s.SetEventBus(eb)

	alertCh, subID := eb.Subscribe("scheduler_alerts")
	defer eb.Unsubscribe("scheduler_alerts", subID)

	notifyCh := make(chan string, 1)

	// 1. Timer event bus publishing
	s.ScheduleTimer(15*time.Millisecond, "autonomous reminder check", ConditionNever, "", notifyCh)

	select {
	case alert := <-alertCh:
		if !strings.Contains(alert, "autonomous reminder check") {
			t.Errorf("expected alert to contain message, got: %s", alert)
		}
		if !strings.Contains(alert, "sender=scheduler") {
			t.Errorf("expected alert to specify sender=scheduler, got: %s", alert)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for scheduler_alerts timer event")
	}

	// 2. Cron event bus publishing
	cronID, err := s.ScheduleCron("* * * * *", "recurring health check", 1, notifyCh)
	if err != nil {
		t.Fatalf("ScheduleCron failed: %v", err)
	}
	entry := s.cron.Entry(cronID)
	entry.Job.Run()

	select {
	case alert := <-alertCh:
		if !strings.Contains(alert, "recurring health check") {
			t.Errorf("expected alert to contain cron message, got: %s", alert)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for scheduler_alerts cron event")
	}
}
