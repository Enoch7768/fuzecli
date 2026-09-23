package generation

import (
	"context"
	"fmt"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

// JSONNormalizerProvider is the dedicated Gemini-backed repair provider.
// It is configured independently from the user's normal chat provider so a
// malformed structured response can still be repaired reliably.
const JSONNormalizerProvider = "gemini-normalizer"

func normalizeWithConfiguredProvider(ctx context.Context, raw string, parseErr error) (ChatResponse, error) {
	registry := provider.NewRegistry(nil, map[string]string{JSONNormalizerProvider: "gemini-3.6-flash"})
	return normalizeWithRegistry(ctx, raw, parseErr, registry)
}

func normalizeWithRegistry(ctx context.Context, raw string, parseErr error, registry *provider.Registry) (ChatResponse, error) {
	if registry == nil {
		return ChatResponse{}, fmt.Errorf("JSON normalizer unavailable: provider registry is nil")
	}
	engine := &Engine{Registry: registry}
	return engine.normalizeChatResponse(ctx, raw, parseErr)
}

// normalizerCandidates prefers the dedicated Gemini normalizer. The ordinary
// Gemini provider is retained as a compatibility fallback, followed by auto
// mode when the application has one configured.
func normalizerCandidates(registry *provider.Registry) []string {
	if registry == nil {
		return nil
	}
	candidates := make([]string, 0, 3)
	for _, name := range []string{"gemini", "auto"} {
		if _, err := registry.Get(name); err == nil {
			candidates = append(candidates, name)
		}
	}
	return candidates
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
