package app

import (
	"context"
	"fmt"
	"strings"
)

// SessionWelcome builds the welcome message displayed when an interactive
// terminal session starts.
func (a *App) SessionWelcome(ctx context.Context, providerName, model string) (string, error) {
	if a.Store == nil {
		return "", fmt.Errorf("workspace not initialized; run aicli init")
	}

	name, mdl, err := a.ProviderAndModel(providerName, model)
	if err != nil {
		return "", err
	}

	state, err := a.Store.LoadState()
	if err != nil {
		return "", err
	}

	if state.ActiveProvider != "" {
		name = state.ActiveProvider
	}
	if state.ActiveModel != "" {
		mdl = state.ActiveModel
	}

	history, err := a.Store.History(20)
	if err != nil {
		return "", err
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	var b strings.Builder
	b.WriteString("\x1b[1;38;5;117mWelcome back to FuzeCLI\x1b[0m\n")
	b.WriteString("\x1b[38;5;244m────────────────────────────────────────────────────────────\x1b[0m\n")
	fmt.Fprintf(&b, "\x1b[38;5;111mProvider\x1b[0m  %s\n", name)
	fmt.Fprintf(&b, "\x1b[38;5;111mModel\x1b[0m     %s\n", mdl)
	fmt.Fprintf(&b, "\x1b[38;5;111mWorkspace\x1b[0m %s\n", a.Store.Root)

	if len(history) > 0 {
		fmt.Fprintf(&b, "\x1b[38;5;244mMemory\x1b[0m    %d recent messages available\n", len(history))
	} else {
		b.WriteString("\x1b[38;5;244mMemory\x1b[0m    No previous conversation\n")
	}

	return b.String(), nil
}
