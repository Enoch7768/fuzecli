package provider_test

import (
	"testing"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/providerfactory"
)

func TestCompatibleCatalogHasAtLeastFiftyProviders(t *testing.T) {
	if len(provider.CompatibleProviderNames()) < 50 {
		t.Fatalf("expected at least 50 compatible providers, got %d", len(provider.CompatibleProviderNames()))
	}
}

func TestAzureOpenAIIsRemovedFromCatalog(t *testing.T) {
	for _, name := range provider.CompatibleProviderNames() {
		if name == "azure-openai" {
			t.Fatal("azure-openai must not be present in the compatible provider catalog")
		}
	}
}

func TestDynamicCompatibleProviderRegistration(t *testing.T) {
	c := config.Default()
	providers := providerfactory.New(c)
	if len(providers) < 50 {
		t.Fatalf("expected at least 50 provider implementations, got %d", len(providers))
	}

	r := provider.NewRegistry(nil, map[string]string{"openrouter": "test-model"}, providers...)
	p, err := r.Get("openrouter")
	if err != nil {
		t.Fatalf("Get(openrouter): %v", err)
	}
	if p.Name() != "openrouter" {
		t.Fatalf("expected provider name openrouter, got %s", p.Name())
	}
}
