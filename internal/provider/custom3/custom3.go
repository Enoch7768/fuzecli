package custom3

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("custom-3",apiKey,baseURL,"FUZECLI_CUSTOM_3_API_KEY",20000,3000,2500)}
