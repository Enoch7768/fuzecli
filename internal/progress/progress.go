package progress

import (
	"sync"
	"time"
)

type Event struct {
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	Project         string    `json:"project"`
	TotalFiles      int       `json:"total_files"`
	CompletedFiles  int       `json:"completed_files"`
	CurrentFile     string    `json:"current_file"`
	BatchNumber     int       `json:"batch_number"`
	TotalBatches    int       `json:"total_batches"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Message         string    `json:"message"`
	ErrorTitle      string    `json:"error_title,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	ErrorRecovery  string    `json:"error_recovery,omitempty"`
	ErrorTechnical string    `json:"error_technical,omitempty"`
	RetryAfter      int       `json:"retry_after,omitempty"`
	HTTPStatus      int       `json:"http_status,omitempty"`
	ElapsedMillis   int64     `json:"elapsed_millis"`
	Timestamp       time.Time `json:"timestamp"`
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	last        Event
}

func New() *Hub {
	return &Hub{subscribers: make(map[chan Event]struct{})}
}

func (h *Hub) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	h.mu.Lock()
	h.last = event
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}

	return ch, cancel
}

func (h *Hub) Last() Event {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.last
}
