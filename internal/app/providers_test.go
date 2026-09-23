package app

import (
	"testing"

	"github.com/Enoch7768/fuzecli/internal/config"
)

func TestProviderConstructionIsConsistent(t *testing.T) {
	c := config.Default()
	app := &App{Config: c}

	expected := buildProviders(c)
	app.ReloadProviders()

	expectedNames := make(map[string]bool, len(expected))
	for _, p := range expected {
		expectedNames[p.Name()] = true
	}

	for name := range expectedNames {
		if _, err := app.Registry.Get(name); err != nil {
			t.Fatalf("reload registry missing provider %q: %v", name, err)
		}
	}

	if _, err := app.Registry.Get("azure-openai"); err == nil {
		t.Fatal("reload registry must not contain azure-openai")
	}
}
