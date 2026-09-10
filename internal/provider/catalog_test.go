package provider

import "testing"

func TestCompatibleCatalogHasAtLeastFiftyProviders(t *testing.T) {
	if len(CompatibleProviderNames()) < 50 {
		t.Fatalf("expected at least 50 compatible providers, got %d", len(CompatibleProviderNames()))
	}
}

func TestDynamicCompatibleProviderRegistration(t *testing.T) {
	Configure("openrouter", "test-key", "https://example.com/v1")
	r := NewRegistry(nil, map[string]string{"openrouter": "test-model"})
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
