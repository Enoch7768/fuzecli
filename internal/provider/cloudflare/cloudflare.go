package cloudflare

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("cloudflare",apiKey,baseURL,"CLOUDFLARE_API_TOKEN",18000,2500,2200)}
