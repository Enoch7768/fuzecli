package stepfun

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("stepfun",apiKey,baseURL,"STEPFUN_API_KEY",22000,3500,3000)}
