package generation

import (
	"context"
	"sync"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

var normalizerState struct {
	sync.RWMutex
	registry *provider.Registry
}

func ConfigureJSONNormalizer(registry *provider.Registry) {
	normalizerState.Lock()
	normalizerState.registry = registry
	normalizerState.Unlock()
}

func configuredJSONNormalizer() *provider.Registry {
	normalizerState.RLock()
	defer normalizerState.RUnlock()
	return normalizerState.registry
}

func normalizeWithConfiguredProvider(ctx context.Context, raw string, parseErr error) (ChatResponse, error) {
	registry := configuredJSONNormalizer()
	if registry == nil {
		return ChatResponse{}, parseErr
	}
	engine := &Engine{Registry: registry}
	return engine.normalizeChatResponse(ctx, raw, parseErr)
}
