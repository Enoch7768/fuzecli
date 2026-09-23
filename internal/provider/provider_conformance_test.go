package provider_test

import (
  "testing"

  "github.com/Enoch7768/fuzecli/internal/config"
  "github.com/Enoch7768/fuzecli/internal/provider"
  "github.com/Enoch7768/fuzecli/internal/providerfactory"
)

func TestRegisteredProvidersConformToProviderContract(t *testing.T) {
  providers := providerfactory.New(config.Config{Providers: map[string]config.ProviderConfig{}})

  if len(providers) == 0 { t.Fatal("provider factory returned no providers") }

  seen := make(map[string]struct{}, len(providers))
  for _, p := range providers {
    if p == nil { t.Fatal("provider factory returned a nil provider") }
    var _ provider.Provider = p
    name := p.Name()
    if name == "" { t.Fatal("provider has an empty name") }
    if _, exists := seen[name]; exists { t.Fatalf("duplicate provider name: %q", name) }
    seen[name] = struct{}{}
    capabilities := provider.CapabilitiesOf(p)
    if capabilities.MaxInputChars < 0 { t.Fatalf("provider %q reports a negative max input size", name) }
    if capabilities.MaxOutputTokens < 0 { t.Fatalf("provider %q reports a negative max output size", name) }
    if capabilities.JSONOutputTokens < 0 { t.Fatalf("provider %q reports a negative JSON output size", name) }
    if capabilities.JSONOutputTokens > 0 && capabilities.MaxOutputTokens > 0 && capabilities.JSONOutputTokens > capabilities.MaxOutputTokens { t.Fatalf("provider %q reports JSON output capacity above max output capacity", name) }
  }
}

func TestProviderContractIsUsableByRegistry(t *testing.T) {
  providers := providerfactory.New(config.Config{Providers: map[string]config.ProviderConfig{}})
  registry := provider.NewRegistry(nil, nil, providers...)

  for _, p := range providers {
    got, err := registry.Get(p.Name())
    if err != nil { t.Fatalf("registry lookup failed for %q: %v", p.Name(), err) }
    if got != p { t.Fatalf("registry returned a different provider instance for %q", p.Name()) }
    capabilities, err := registry.Capabilities(p.Name())
    if err != nil { t.Fatalf("capability lookup failed for %q: %v", p.Name(), err) }
    if capabilities.JSONOutputTokens > 0 && capabilities.MaxOutputTokens > 0 && capabilities.JSONOutputTokens > capabilities.MaxOutputTokens { t.Fatalf("registry exposed invalid JSON output capacity for %q", p.Name()) }
  }
}
