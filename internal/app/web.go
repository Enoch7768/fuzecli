package app

import "context"

func (a *App) ChatRequest(ctx context.Context, prompt string) error {
	return a.chatTurn(ctx, prompt)
}
