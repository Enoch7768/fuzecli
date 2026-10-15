package provider

import (
	"errors"
	"sync"
	"time"
)

type ProviderTelemetry struct {
	Provider         string        `json:"provider"`
	Requests         uint64        `json:"requests"`
	Successes        uint64        `json:"successes"`
	Failures         uint64        `json:"failures"`
	RateLimited      uint64        `json:"rate_limited"`
	QuotaExceeded    uint64        `json:"quota_exceeded"`
	Unauthorized     uint64        `json:"unauthorized"`
	Unavailable      uint64        `json:"unavailable"`
	BadRequests      uint64        `json:"bad_requests"`
	ModelNotFound    uint64        `json:"model_not_found"`
	UnknownErrors    uint64        `json:"unknown_errors"`
	StreamRequests   uint64        `json:"stream_requests"`
	PromptTokens     uint64        `json:"prompt_tokens"`
	CompletionTokens uint64        `json:"completion_tokens"`
	TotalTokens      uint64        `json:"total_tokens"`
	EstimatedTokens  uint64        `json:"estimated_tokens"`
	TotalCostUSD     float64       `json:"total_cost_usd"`
	TotalLatency     time.Duration `json:"total_latency"`
	LastLatency      time.Duration `json:"last_latency"`
	LastSuccess      time.Time     `json:"last_success"`
	LastFailure      time.Time     `json:"last_failure"`
	LastError        string        `json:"last_error"`
}

type TelemetryEvent struct {
	RequestID  string    `json:"request_id"`
	Time       time.Time `json:"time"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	PromptTokens uint64  `json:"prompt_tokens"`
	CompletionTokens uint64 `json:"completion_tokens"`
	TotalTokens uint64 `json:"total_tokens"`
	CostUSD float64 `json:"cost_usd"`
	Estimated bool `json:"estimated,omitempty"`
	LatencyMs  int64     `json:"latency_ms"`
	Streaming  bool      `json:"streaming"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	ErrorKind  string    `json:"error_kind,omitempty"`
}

type TelemetrySnapshot struct {
	Providers map[string]ProviderTelemetry `json:"providers"`
	Events    []TelemetryEvent             `json:"events"`
}

type Telemetry struct {
	mu      sync.RWMutex
	entries map[string]ProviderTelemetry
	events []TelemetryEvent
}

func NewTelemetry() *Telemetry {
	return &Telemetry{entries: make(map[string]ProviderTelemetry), events: make([]TelemetryEvent, 0, 500)}
}

func (t *Telemetry) Record(provider string, err error, usage Usage, latency time.Duration, streaming bool, model ...string) {
	t.RecordWithRequestID("", provider, err, usage, latency, streaming, model...)
}

func (t *Telemetry) RecordWithRequestID(requestID, provider string, err error, usage Usage, latency time.Duration, streaming bool, model ...string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	modelName := ""
	if len(model) > 0 { modelName = model[0] }
	entry := t.entries[provider]
	entry.Provider = provider
	entry.Requests++
	if streaming {
		entry.StreamRequests++
	}
	entry.PromptTokens += uint64(nonNegativeInt(usage.PromptTokens))
	entry.CompletionTokens += uint64(nonNegativeInt(usage.CompletionTokens))
	entry.TotalTokens += uint64(nonNegativeInt(usage.TotalTokens))
	if usage.Estimated { entry.EstimatedTokens += uint64(nonNegativeInt(usage.TotalTokens)) }
	if usage.CostUSD > 0 { entry.TotalCostUSD += usage.CostUSD }
	entry.TotalLatency += latency
	entry.LastLatency = latency
	event := TelemetryEvent{RequestID: requestID, Time: time.Now(), Provider: provider, Model: modelName, PromptTokens: uint64(nonNegativeInt(usage.PromptTokens)), CompletionTokens: uint64(nonNegativeInt(usage.CompletionTokens)), TotalTokens: uint64(nonNegativeInt(usage.TotalTokens)), CostUSD: maxNonNegativeFloat(usage.CostUSD), Estimated: usage.Estimated, LatencyMs: latency.Milliseconds(), Streaming: streaming, Success: err == nil}
	if err != nil {
		event.Error = err.Error()
		var providerErr *ProviderError
		if errors.As(err, &providerErr) { event.ErrorKind = string(providerErr.Kind) }
	}
	t.events = append(t.events, event)
	if len(t.events) > 500 { t.events = t.events[len(t.events)-500:] }

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

func maxNonNegativeFloat(value float64) float64 { if value < 0 { return 0 }; return value }

func nonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func (t *Telemetry) Snapshot() TelemetrySnapshot {
	if t == nil {
		return TelemetrySnapshot{Providers: map[string]ProviderTelemetry{}, Events: []TelemetryEvent{}}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	providers := make(map[string]ProviderTelemetry, len(t.entries))
	for name, entry := range t.entries {
		providers[name] = entry
	}
	events := append([]TelemetryEvent(nil), t.events...)
	return TelemetrySnapshot{Providers: providers, Events: events}
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
	t.events = make([]TelemetryEvent, 0, 500)
}
