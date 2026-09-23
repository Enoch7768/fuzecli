package provider

import (
	"context"
	"testing"
	"time"
)

type routingProvider struct {
	name string
	cap  Capabilities
}

func (p routingProvider) Name() string { return p.name }

func (p routingProvider) Capabilities() Capabilities { return p.cap }

func (p routingProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	return &Response{ProviderName: p.name, Content: "ok"}, nil
}

func (p routingProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (p routingProvider) ListModels(context.Context) ([]string, error) {
	return []string{"test-model"}, nil
}

func TestRegistryRoutePrefersRequestedProvider(t *testing.T) {
	registry := NewRegistry(
		[]string{"small", "strong"},
		map[string]string{"small": "small-model", "strong": "strong-model"},
		routingProvider{name: "small", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 20000, MaxOutputTokens: 2000, JSONOutputTokens: 1500}},
		routingProvider{name: "strong", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 100000, MaxOutputTokens: 16000, JSONOutputTokens: 12000}},
	)

	decision, err := registry.Route(context.Background(), []Message{{Role: "user", Content: "hello"}}, RoutingRequest{
		Options: RequestOptions{JSONMode: true},
		PreferredProvider: "small",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Provider != "small" {
		t.Fatalf("expected preferred provider small, got %q", decision.Provider)
	}
}

func TestRegistryRouteFiltersCapabilities(t *testing.T) {
	registry := NewRegistry(
		[]string{"text-only", "vision"},
		map[string]string{"text-only": "text-model", "vision": "vision-model"},
		routingProvider{name: "text-only", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 20000, MaxOutputTokens: 2000, JSONOutputTokens: 1500}},
		routingProvider{name: "vision", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, Vision: true, MaxInputChars: 100000, MaxOutputTokens: 16000, JSONOutputTokens: 12000}},
	)

	decision, err := registry.Route(context.Background(), []Message{{Role: "user", Content: "inspect image"}}, RoutingRequest{
		RequireVision: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Provider != "vision" {
		t.Fatalf("expected vision provider, got %q", decision.Provider)
	}
}

func TestRegistryRouteSkipsUnavailableProvider(t *testing.T) {
	registry := NewRegistry(
		[]string{"first", "second"},
		map[string]string{"first": "first-model", "second": "second-model"},
		routingProvider{name: "first", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 20000, MaxOutputTokens: 2000, JSONOutputTokens: 1500}},
		routingProvider{name: "second", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 100000, MaxOutputTokens: 16000, JSONOutputTokens: 12000}},
	)

	registry.limitMu.Lock()
	registry.retryUntil["first"] = timeNow().Add(time.Hour)
	registry.limitMu.Unlock()

	decision, err := registry.Route(context.Background(), []Message{{Role: "user", Content: "hello"}}, RoutingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Provider != "second" {
		t.Fatalf("expected second provider, got %q", decision.Provider)
	}
}

func TestRegistryRouteRejectsUnsupportedRequest(t *testing.T) {
	registry := NewRegistry(
		[]string{"json-off"},
		map[string]string{"json-off": "text-model"},
		routingProvider{name: "json-off", cap: Capabilities{Streaming: true, StructuredJSON: false, ListModels: true, MaxInputChars: 20000, MaxOutputTokens: 2000, JSONOutputTokens: 0}},
	)

	_, err := registry.Route(context.Background(), []Message{{Role: "user", Content: "json"}}, RoutingRequest{
		Options: RequestOptions{JSONMode: true},
	})
	if err == nil {
		t.Fatal("expected routing failure")
	}
}

func timeNow() time.Time {
	return time.Now()
}

func TestRegistryRouteUsesTelemetryHealth(t *testing.T) {
	registry := NewRegistry(
		[]string{"slow", "healthy"},
		map[string]string{"slow": "slow-model", "healthy": "healthy-model"},
		routingProvider{name: "slow", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 100000, MaxOutputTokens: 16000, JSONOutputTokens: 12000}},
		routingProvider{name: "healthy", cap: Capabilities{Streaming: true, StructuredJSON: true, ListModels: true, MaxInputChars: 100000, MaxOutputTokens: 16000, JSONOutputTokens: 12000}},
	)

	for i := 0; i < 8; i++ {
		registry.telemetry.Record("slow", ErrProviderUnavailable, Usage{}, time.Second, false)
	}
	for i := 0; i < 8; i++ {
		registry.telemetry.Record("healthy", nil, Usage{}, 10*time.Millisecond, false)
	}

	decision, err := registry.Route(context.Background(), []Message{{Role: "user", Content: "hello"}}, RoutingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Provider != "healthy" {
		t.Fatalf("expected telemetry-healthy provider, got %q", decision.Provider)
	}
}
