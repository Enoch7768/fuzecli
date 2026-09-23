package novelai

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("novelai",apiKey,baseURL,"NOVELAI_API_KEY",16000,2500,2200)}
