package providerfactory

import (
	"testing"

	"github.com/Enoch7768/fuzecli/internal/config"
)

func TestFactoryRegistersCompatibleProviders(t *testing.T) {
	providers := New(config.Default())
	seen := map[string]bool{}
	for _, p := range providers {
		seen[p.Name()] = true
	}
	for _, name := range []string{"openrouter", "together", "fireworks", "inference-net", "custom-1", "custom-2", "custom-3"} {
		if !seen[name] {
			t.Fatalf("provider factory did not register %q", name)
		}
	}
}
