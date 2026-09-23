package provider

import (
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider/openrouter"
)

func TestCompatibleCatalogHasAtLeastFiftyProviders(t *testing.T) {
	if len(CompatibleProviderNames()) < 50 {
		t.Fatalf("expected at least 50 compatible providers, got %d", len(CompatibleProviderNames()))
	}
}

func TestProviderRegistration(t *testing.T) {
	r := NewRegistry(nil, map[string]string{"openrouter": "test-model"}, openrouter.New("test-key", "https://example.com/v1"))
	p, err := r.Get("openrouter")
	if err != nil {
		t.Fatalf("Get(openrouter): %v", err)
	}
	if p.Name() != "openrouter" {
		t.Fatalf("expected provider name openrouter, got %s", p.Name())
	}
	if _, err := r.Get("unknown-provider"); err == nil {
		t.Fatal("expected unknown provider error")
	}
}
