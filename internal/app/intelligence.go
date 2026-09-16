package app

import (
	"fmt"

	"github.com/Enoch7768/fuzecli/internal/intelligence"
)


func (a *App) BuildCodeIndex() (*intelligence.Index, error) {
	if a.Store == nil {
		return nil, fmt.Errorf("workspace not initialized; run aicli init")
	}
	return intelligence.Build(a.Store.Root)
}

func (a *App) SearchCode(query string, limit int) ([]intelligence.Match, error) {
	idx, err := a.BuildCodeIndex()
	if err != nil {
		return nil, err
	}
	return idx.Search(query, limit)
}

// FindCodeSymbols finds functions, classes and types by name, kind or path.
func (a *App) FindCodeSymbols(query string, limit int) ([]intelligence.Symbol, error) {
	idx, err := a.BuildCodeIndex()
	if err != nil {
		return nil, err
	}
	return idx.FindSymbols(query, limit), nil
}
