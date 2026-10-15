package provider

import (
	"context"
	"encoding/json"
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


func TestRegistryEstimatesMissingUsage(t *testing.T) {
	p := emptyUsageProvider{}
	registry := NewRegistry([]string{"test"}, map[string]string{"test": "test-model"}, p)
	response, err := registry.Send(context.Background(), "test", []Message{{Role: "user", Content: "hello there"}}, RequestOptions{})
	if err != nil { t.Fatal(err) }
	if response.Usage.PromptTokens <= 0 || response.Usage.CompletionTokens <= 0 || response.Usage.TotalTokens != response.Usage.PromptTokens+response.Usage.CompletionTokens {
		t.Fatalf("missing or inconsistent usage estimate: %#v", response.Usage)
	}
	if !response.Usage.Estimated { t.Fatalf("expected estimated usage when provider omits it: %#v", response.Usage) }
	entry, ok := registry.ProviderTelemetry("test")
	if !ok || entry.TotalTokens == 0 || entry.EstimatedTokens != entry.TotalTokens {
		t.Fatalf("estimated usage not reflected in telemetry: %#v", entry)
	}
}

func TestProviderTelemetryUsesWebJSONFieldNames(t *testing.T) {
	telemetry := NewTelemetry()
	telemetry.Record("test", nil, Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7, Estimated: true}, 10, false)
	data, err := json.Marshal(telemetry.Snapshot())
	if err != nil { t.Fatal(err) }
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil { t.Fatal(err) }
	providers, ok := decoded["providers"].(map[string]any)
	if !ok { t.Fatalf("missing providers object: %s", data) }
	entry, ok := providers["test"].(map[string]any)
	if !ok || entry["total_tokens"] != float64(7) || entry["estimated_tokens"] != float64(7) || entry["prompt_tokens"] != float64(3) {
		t.Fatalf("web telemetry field names/values incorrect: %s", data)
	}
}


type emptyUsageProvider struct{}

func (emptyUsageProvider) Name() string { return "test" }
func (emptyUsageProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (emptyUsageProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	return &Response{Content: "A generated answer with several words.", Model: "test-model"}, nil
}
func (emptyUsageProvider) Stream(ctx context.Context, _ []Message, _ RequestOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Delta: "A generated answer with several words."}
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
