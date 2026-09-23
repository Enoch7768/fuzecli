package cohere

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("cohere",apiKey,baseURL,"COHERE_API_KEY",20000,3000,2500)}
