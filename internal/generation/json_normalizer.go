package generation

import (
	"context"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

const JSONNormalizerProvider = "gemini-normalizer"

func normalizeWithConfiguredProvider(ctx context.Context, raw string, parseErr error) (ChatResponse, error) {
	registry := provider.NewRegistry([]string{JSONNormalizerProvider}, map[string]string{JSONNormalizerProvider: "gemini-2.5-flash"})
	engine := &Engine{Registry: registry}
	return engine.normalizeChatResponse(ctx, raw, parseErr)
}
