package agent

import (
	"sync"
	"time"
)

type Trace struct {
	mu     sync.RWMutex
	events []TaskEvent
	limit  int
}

func NewTrace(limit int) *Trace {
	if limit <= 0 {
		limit = 512
	}
	return &Trace{limit: limit}
}

func (t *Trace) Add(event TaskEvent) {
	if t == nil {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
	if len(t.events) > t.limit {
		t.events = append([]TaskEvent(nil), t.events[len(t.events)-t.limit:]...)
	}
}

func (t *Trace) Events() []TaskEvent {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]TaskEvent, len(t.events))
	copy(out, t.events)
	return out
}
