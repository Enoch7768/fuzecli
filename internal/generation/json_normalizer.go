package generation

import (
	"context"
	"fmt"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

// JSONNormalizerProvider is the dedicated Groq-backed repair provider.
// It is configured independently from the user's normal chat provider so a
// malformed structured response can still be repaired reliably.
const JSONNormalizerProvider = "groq"

func normalizeWithConfiguredProvider(ctx context.Context, raw string, parseErr error) (ChatResponse, error) {
	registry := provider.NewRegistry(nil, map[string]string{JSONNormalizerProvider: "openai/gpt-oss-20b"})
	return normalizeWithRegistry(ctx, raw, parseErr, registry)
}

func normalizeWithRegistry(ctx context.Context, raw string, parseErr error, registry *provider.Registry) (ChatResponse, error) {
	if registry == nil {
		return ChatResponse{}, fmt.Errorf("JSON normalizer unavailable: provider registry is nil")
	}
	engine := &Engine{Registry: registry}
	return engine.normalizeChatResponse(ctx, raw, parseErr)
}

// normalizerCandidates uses Groq as the dedicated JSON repair provider.
func normalizerCandidates(registry *provider.Registry) []string {
	if registry == nil {
		return nil
	}
	if _, err := registry.Get("groq"); err != nil {
		return nil
	}
	return []string{"groq"}
}

func normalizeFailureSummary(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, strings.TrimSpace(err.Error()))
		}
	}
	if len(parts) == 0 {
		return "no normalizer provider was available"
	}
	return strings.Join(parts, "; ")
}
