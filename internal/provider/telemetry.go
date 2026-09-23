package provider

import (
	"errors"
	"sync"
	"time"
)

type ProviderTelemetry struct {
	Provider            string
	Requests            uint64
	Successes           uint64
	Failures            uint64
	RateLimited         uint64
	QuotaExceeded       uint64
	Unauthorized        uint64
	Unavailable         uint64
	BadRequests         uint64
	ModelNotFound       uint64
	UnknownErrors       uint64
	StreamRequests      uint64
	PromptTokens        uint64
	CompletionTokens    uint64
	TotalTokens         uint64
	TotalLatency        time.Duration
	LastLatency         time.Duration
	LastSuccess         time.Time
	LastFailure         time.Time
	LastError           string
}

type TelemetrySnapshot struct {
	Providers map[string]ProviderTelemetry
}

type Telemetry struct {
	mu      sync.RWMutex
	entries map[string]ProviderTelemetry
}

func NewTelemetry() *Telemetry {
	return &Telemetry{entries: make(map[string]ProviderTelemetry)}
}

func (t *Telemetry) Record(provider string, err error, usage Usage, latency time.Duration, streaming bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.entries[provider]
	entry.Provider = provider
	entry.Requests++
	if streaming {
		entry.StreamRequests++
	}
	entry.PromptTokens += uint64(nonNegativeInt(usage.PromptTokens))
	entry.CompletionTokens += uint64(nonNegativeInt(usage.CompletionTokens))
	entry.TotalTokens += uint64(nonNegativeInt(usage.TotalTokens))
	entry.TotalLatency += latency
	entry.LastLatency = latency

	if err == nil {
		entry.Successes++
		entry.LastSuccess = time.Now()
		entry.LastError = ""
		t.entries[provider] = entry
		return
	}

	entry.Failures++
	entry.LastFailure = time.Now()
	entry.LastError = err.Error()
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.Kind {
		case ErrorRateLimited:
			entry.RateLimited++
		case ErrorQuotaExceeded:
			entry.QuotaExceeded++
		case ErrorUnauthorized:
			entry.Unauthorized++
		case ErrorProviderUnavailable, ErrorOverloaded:
			entry.Unavailable++
		case ErrorBadRequest:
			entry.BadRequests++
		case ErrorModelNotFound:
			entry.ModelNotFound++
		default:
			entry.UnknownErrors++
		}
	} else {
		entry.UnknownErrors++
	}
	t.entries[provider] = entry
}

func nonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func (t *Telemetry) Snapshot() TelemetrySnapshot {
	if t == nil {
		return TelemetrySnapshot{Providers: map[string]ProviderTelemetry{}}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	providers := make(map[string]ProviderTelemetry, len(t.entries))
	for name, entry := range t.entries {
		providers[name] = entry
	}
	return TelemetrySnapshot{Providers: providers}
}

func (t *Telemetry) Provider(provider string) (ProviderTelemetry, bool) {
	if t == nil {
		return ProviderTelemetry{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	entry, ok := t.entries[provider]
	return entry, ok
}

func (t *Telemetry) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = make(map[string]ProviderTelemetry)
}
