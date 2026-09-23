package provider

import (
	"errors"
	"testing"
	"time"
)

func TestTelemetryRecordsSuccessAndUsage(t *testing.T) {
	telemetry := NewTelemetry()
	telemetry.Record("test", nil, Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}, 250*time.Millisecond, false)

	entry, ok := telemetry.Provider("test")
	if !ok {
		t.Fatal("expected provider telemetry")
	}
	if entry.Requests != 1 || entry.Successes != 1 || entry.Failures != 0 {
		t.Fatalf("unexpected counters: %#v", entry)
	}
	if entry.PromptTokens != 10 || entry.CompletionTokens != 20 || entry.TotalTokens != 30 {
		t.Fatalf("unexpected usage: %#v", entry)
	}
	if entry.LastLatency != 250*time.Millisecond {
		t.Fatalf("last latency = %v", entry.LastLatency)
	}
}

func TestTelemetryClassifiesProviderErrors(t *testing.T) {
	telemetry := NewTelemetry()
	telemetry.Record("test", &ProviderError{Kind: ErrorRateLimited, Provider: "test", Message: "slow"}, Usage{}, time.Second, true)
	telemetry.Record("test", &ProviderError{Kind: ErrorModelNotFound, Provider: "test", Message: "missing"}, Usage{}, time.Second, false)
	telemetry.Record("test", errors.New("unknown"), Usage{}, time.Second, false)

	entry, ok := telemetry.Provider("test")
	if !ok {
		t.Fatal("expected provider telemetry")
	}
	if entry.Requests != 3 || entry.Failures != 3 || entry.Successes != 0 {
		t.Fatalf("unexpected counters: %#v", entry)
	}
	if entry.RateLimited != 1 || entry.ModelNotFound != 1 || entry.UnknownErrors != 1 {
		t.Fatalf("unexpected error counters: %#v", entry)
	}
	if entry.StreamRequests != 1 {
		t.Fatalf("stream requests = %d, want 1", entry.StreamRequests)
	}
}

func TestTelemetrySnapshotIsIndependent(t *testing.T) {
	telemetry := NewTelemetry()
	telemetry.Record("test", nil, Usage{}, 10*time.Millisecond, false)

	snapshot := telemetry.Snapshot()
	snapshot.Providers["test"] = ProviderTelemetry{Provider: "changed"}

	entry, ok := telemetry.Provider("test")
	if !ok || entry.Provider != "test" {
		t.Fatal("snapshot mutated live telemetry")
	}

	telemetry.Reset()
	if _, ok := telemetry.Provider("test"); ok {
		t.Fatal("telemetry was not reset")
	}
}
