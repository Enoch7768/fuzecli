package friendli

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("friendli",apiKey,baseURL,"FRIENDLI_TOKEN",22000,3500,3000)}
