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
