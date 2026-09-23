package provider

import (
	"context"
	"testing"
)

type capabilityTestProvider struct{}

func (capabilityTestProvider) Name() string { return "capability-test" }
func (capabilityTestProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	return &Response{Content: "ok"}, nil
}
func (capabilityTestProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
func (capabilityTestProvider) ListModels(context.Context) ([]string, error) {
	return []string{"test"}, nil
}
func (capabilityTestProvider) Capabilities() Capabilities {
	return Capabilities{
		Streaming: true,
		StructuredJSON: true,
		ListModels: true,
		ToolCalling: true,
		Vision: true,
		MaxInputChars: 20000,
		MaxOutputTokens: 8000,
		JSONOutputTokens: 4000,
	}
}

func TestCapabilitiesOf(t *testing.T) {
	p := capabilityTestProvider{}
	got := CapabilitiesOf(p)
	if !got.Streaming || !got.StructuredJSON || !got.ListModels || !got.ToolCalling || !got.Vision {
		t.Fatalf("capabilities = %#v", got)
	}
	if got.MaxInputChars != 20000 || got.MaxOutputTokens != 8000 || got.JSONOutputTokens != 4000 {
		t.Fatalf("limits = %#v", got)
	}
}

func TestCapabilitiesOfLegacyProvider(t *testing.T) {
	p := conformanceProvider{name: "legacy"}
	got := CapabilitiesOf(p)
	if !got.Streaming || !got.ListModels {
		t.Fatalf("legacy defaults = %#v", got)
	}
}
