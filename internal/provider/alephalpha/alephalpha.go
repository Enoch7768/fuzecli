package alephalpha

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("alephalpha",apiKey,baseURL,"ALEPH_ALPHA_API_KEY",18000,3000,2500)}
