package vllm

import("github.com/Enoch7768/fuzecli/internal/provider";"github.com/Enoch7768/fuzecli/internal/provider/catalogruntime")
func New(apiKey,baseURL string) provider.Provider{return catalogruntime.New("vllm",apiKey,baseURL,"VLLM_API_KEY",12000,2400,2000)}
