package scheduler

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/purav/microgravity/internal/events"
	"github.com/robfig/cron/v3"
)

type TimerCondition string

const (
	ConditionNever TimerCondition = "never"
	ConditionAny   TimerCondition = "any"
)

type TimerEntry struct {
	ID        int
	Condition TimerCondition
	SenderID  string
	Timer     *time.Timer
}

type Scheduler struct {
	cron     *cron.Cron
	timers   map[int]*TimerEntry
	nextID   int
	eventBus *events.EventBus
	mu       sync.Mutex
}

func New() *Scheduler {
	c := cron.New()
	c.Start()
	return &Scheduler{
		cron:   c,
		timers: make(map[int]*TimerEntry),
		nextID: 1,
	}
}

func (s *Scheduler) SetEventBus(eb *events.EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = eb
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.timers {
		if entry.Timer != nil {
			entry.Timer.Stop()
		}
	}
}

func (s *Scheduler) ScheduleTimer(duration time.Duration, message string, condition TimerCondition, targetSenderID string, notify chan<- string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++

	if condition == "" {
		condition = ConditionNever
	}

	entry := &TimerEntry{
		ID:        id,
		Condition: condition,
		SenderID:  targetSenderID,
	}

	timer := time.AfterFunc(duration, func() {
		s.mu.Lock()
		eb := s.eventBus
		delete(s.timers, id)
		s.mu.Unlock()

		select {
		case notify <- message:
		default:
		}

		if eb != nil {
			alert := fmt.Sprintf(`[Message] timestamp=%s sender=scheduler priority=MESSAGE_PRIORITY_HIGH content=%s`, time.Now().UTC().Format(time.RFC3339), message)
			eb.Publish("scheduler_alerts", alert)
		}
	})

	entry.Timer = timer
	s.timers[id] = entry
	return id
}

func (s *Scheduler) CancelTimer(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.timers[id]
	if !exists {
		return false
	}
	if entry.Timer != nil {
		entry.Timer.Stop()
	}
	delete(s.timers, id)
	return true
}

func (s *Scheduler) OnMessageReceived(senderID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cancelled := 0
	for id, entry := range s.timers {
		shouldCancel := false
		if entry.Condition == ConditionAny {
			shouldCancel = true
		} else if entry.SenderID == senderID || string(entry.Condition) == senderID {
			shouldCancel = true
		}

		if shouldCancel {
			if entry.Timer != nil {
				entry.Timer.Stop()
			}
			delete(s.timers, id)
			cancelled++
		}
	}
	return cancelled
}

func (s *Scheduler) ScheduleCron(expr string, message string, maxIterations int, notify chan<- string) (cron.EntryID, error) {
	var iterations atomic.Int32
	return s.cron.AddFunc(expr, func() {
		if maxIterations > 0 && int(iterations.Add(1)) > maxIterations {
			return
		}
		select {
		case notify <- message:
		default:
		}

		s.mu.Lock()
		eb := s.eventBus
		s.mu.Unlock()
		if eb != nil {
			alert := fmt.Sprintf(`[Message] timestamp=%s sender=scheduler priority=MESSAGE_PRIORITY_HIGH content=%s`, time.Now().UTC().Format(time.RFC3339), message)
			eb.Publish("scheduler_alerts", alert)
		}
	})
}
