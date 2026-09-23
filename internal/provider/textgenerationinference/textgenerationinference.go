package textgenerationinference

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("text-generation-inference",apiKey,baseURL,"TGI_API_KEY",12000,2400,2000)}
