package ollama

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("ollama",apiKey,baseURL,"OLLAMA_API_KEY",10000,2200,1800)}
