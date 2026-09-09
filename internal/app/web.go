package app

import "context"

func (a *App) ChatRequest(ctx context.Context, prompt, providerName, model string) error {
	if providerName != "" {
		a.Config.DefaultProvider = providerName
	}

	if providerName != "" && model != "" {
		pc, ok := a.Config.Providers[providerName]
		if ok {
			pc.DefaultModel = model
			a.Config.Providers[providerName] = pc
		}
	}

	return a.chatTurn(ctx, prompt)
}
