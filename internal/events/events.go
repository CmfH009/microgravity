package events

import (
	"sync"
)

type EventBus struct {
	subscribers map[string]map[int]chan string
	nextID      int
	mu          sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]map[int]chan string),
	}
}

func (eb *EventBus) Subscribe(topic string) (<-chan string, int) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if _, ok := eb.subscribers[topic]; !ok {
		eb.subscribers[topic] = make(map[int]chan string)
	}

	ch := make(chan string, 100)
	id := eb.nextID
	eb.nextID++
	eb.subscribers[topic][id] = ch
	return ch, id
}

func (eb *EventBus) Unsubscribe(topic string, id int) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if subs, ok := eb.subscribers[topic]; ok {
		if ch, exists := subs[id]; exists {
			close(ch)
			delete(subs, id)
		}
		if len(subs) == 0 {
			delete(eb.subscribers, topic)
		}
	}
}

func (eb *EventBus) Publish(topic string, message string) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if subs, ok := eb.subscribers[topic]; ok {
		for _, ch := range subs {
			select {
			case ch <- message:
			default:
			}
		}
	}
}
