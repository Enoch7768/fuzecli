package custom2

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("custom-2",apiKey,baseURL,"FUZECLI_CUSTOM_2_API_KEY",20000,3000,2500)}
