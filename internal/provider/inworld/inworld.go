package inworld

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("inworld",apiKey,baseURL,"INWORLD_API_KEY",18000,3000,2500)}
