package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type RoutingRequest struct {
	Options           RequestOptions
	RequireStreaming  bool
	RequireToolCalling bool
	RequireVision     bool
	PreferredProvider string
	PreferredModel    string
	Candidates        []string
}

type RoutingCandidate struct {
	Provider     string
	Model        string
	Capabilities Capabilities
	Score        int
	Ready        bool
}

type RoutingDecision struct {
	Provider string
	Model    string
	Candidates []RoutingCandidate
}

func (r *Registry) Route(ctx context.Context, messages []Message, request RoutingRequest) (RoutingDecision, error) {
	names := request.Candidates
	if len(names) == 0 {
		names = r.fallback
	}
	if len(names) == 0 {
		return RoutingDecision{}, errors.New("no providers configured for routing")
	}

	candidates := make([]RoutingCandidate, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || name == "auto" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		p, err := r.Get(name)
		if err != nil {
			continue
		}
		capabilities := CapabilitiesOf(p)
		if request.RequireStreaming && !capabilities.Streaming {
			continue
		}
		if request.RequireToolCalling && !capabilities.ToolCalling {
			continue
		}
		if request.RequireVision && !capabilities.Vision {
			continue
		}

		options := request.Options
		if request.PreferredModel != "" {
			options.Model = request.PreferredModel
		}
		options.Model = r.model(name, options.Model)
		if options.Model == "" {
			continue
		}
		if !SupportsRequest(capabilities, options) {
			continue
		}
		routedMessages, routedOptions := adaptRequest(name, messages, options)
		if err := validateRequestBudget(name, routedMessages, routedOptions); err != nil {
			continue
		}
		if !r.ready(name) {
			continue
		}

		score := routeScore(name, capabilities, request)
		candidates = append(candidates, RoutingCandidate{
			Provider: name,
			Model: options.Model,
			Capabilities: capabilities,
			Score: score,
			Ready: true,
		})
	}

	if len(candidates) == 0 {
		return RoutingDecision{}, fmt.Errorf("no provider satisfies the requested capabilities and request budget")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Provider < candidates[j].Provider
	})

	return RoutingDecision{
		Provider: candidates[0].Provider,
		Model: candidates[0].Model,
		Candidates: candidates,
	}, nil
}

func routeScore(name string, capabilities Capabilities, request RoutingRequest) int {
	score := 0
	if strings.EqualFold(name, request.PreferredProvider) {
		score += 1000
	}
	if request.RequireStreaming && capabilities.Streaming {
		score += 200
	}
	if request.RequireToolCalling && capabilities.ToolCalling {
		score += 200
	}
	if request.RequireVision && capabilities.Vision {
		score += 200
	}
	if request.Options.JSONMode && capabilities.StructuredJSON {
		score += 150
	}
	score += minInt(capabilities.MaxOutputTokens/1000, 20) * 5
	score += minInt(capabilities.MaxInputChars/10000, 20) * 2
	return score
}

func (r *Registry) ready(name string) bool {
	r.limitMu.Lock()
	defer r.limitMu.Unlock()
	return !r.retryUntil[name].After(time.Now())
}
