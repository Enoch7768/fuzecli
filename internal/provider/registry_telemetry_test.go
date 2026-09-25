package provider

import (
	"context"
	"testing"
)

func TestRegistryRecordsTelemetry(t *testing.T) {
	p := conformanceProvider{name: "test"}
	registry := NewRegistry([]string{"test"}, map[string]string{"test": "test-model"}, p)

	if _, err := registry.Send(context.Background(), "test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.ProviderTelemetry("test")
	if !ok {
		t.Fatal("expected provider telemetry")
	}
	if entry.Requests != 1 || entry.Successes != 1 || entry.Failures != 0 {
		t.Fatalf("unexpected send telemetry: %#v", entry)
	}
	if entry.TotalTokens != 2 {
		t.Fatalf("total tokens = %d, want 2", entry.TotalTokens)
	}

	stream, err := registry.Stream(context.Background(), "test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	entry, ok = registry.ProviderTelemetry("test")
	if !ok {
		t.Fatal("expected provider telemetry after stream")
	}
	if entry.Requests != 2 || entry.Successes != 2 || entry.StreamRequests != 1 {
		t.Fatalf("unexpected combined telemetry: %#v", entry)
	}
}

func TestRegistryTelemetryReset(t *testing.T) {
	p := conformanceProvider{name: "test"}
	registry := NewRegistry([]string{"test"}, map[string]string{"test": "test-model"}, p)

	if _, err := registry.Send(context.Background(), "test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	registry.ResetTelemetry()
	if _, ok := registry.ProviderTelemetry("test"); ok {
		t.Fatal("telemetry was not reset")
	}
}


func TestRegistryTelemetrySinkReceivesExactEvent(t *testing.T) {
	p := conformanceProvider{name: "test"}
	registry := NewRegistry([]string{"test"}, map[string]string{"test": "test-model"}, p)
	var events []TelemetryEvent
	registry.SetTelemetrySink(func(event TelemetryEvent) {
		events = append(events, event)
	})

	if _, err := registry.Send(context.Background(), "test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{RequestID: "req-exact"}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].RequestID != "req-exact" || events[0].Provider != "test" || events[0].Model != "test-model" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
	if events[0].TotalTokens != 2 || !events[0].Success {
		t.Fatalf("unexpected usage/success: %#v", events[0])
	}
}
