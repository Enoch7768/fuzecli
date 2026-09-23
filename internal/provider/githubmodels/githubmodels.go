package githubmodels

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("github-models",apiKey,baseURL,"GITHUB_TOKEN",20000,3000,2500)}
