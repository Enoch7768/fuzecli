package generation

import (
	"context"
	"fmt"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

// JSONNormalizerProvider is kept as a stable identifier for diagnostics and
// compatibility. Normalization itself uses the real configured Gemini provider
// from the application registry rather than a synthetic provider name.
const JSONNormalizerProvider = "gemini-normalizer"

func normalizeWithConfiguredProvider(ctx context.Context, raw string, parseErr error) (ChatResponse, error) {
	registry := provider.NewRegistry(nil, map[string]string{"gemini": "gemini-2.5-flash"})
	return normalizeWithRegistry(ctx, raw, parseErr, registry)
}

func normalizeWithRegistry(ctx context.Context, raw string, parseErr error, registry *provider.Registry) (ChatResponse, error) {
	if registry == nil {
		return ChatResponse{}, fmt.Errorf("JSON normalizer unavailable: provider registry is nil")
	}
	engine := &Engine{Registry: registry}
	return engine.normalizeChatResponse(ctx, raw, parseErr)
}

// normalizerCandidates deliberately prefers Gemini because it is FuzeCLI's
// structured-JSON repair engine. Auto is a safe fallback when Gemini is not
// configured or temporarily unavailable.
func normalizerCandidates(registry *provider.Registry) []string {
	if registry == nil {
		return nil
	}
	candidates := make([]string, 0, 2)
	if _, err := registry.Get("gemini"); err == nil {
		candidates = append(candidates, "gemini")
	}
	if _, err := registry.Get("auto"); err == nil {
		candidates = append(candidates, "auto")
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
