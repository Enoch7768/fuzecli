package provider

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type compatibleSpec struct {
	Name              string
	BaseURL           string
	EnvKey            string
	MinInterval       time.Duration
	MaxInputChars     int
	MaxOutputTokens   int
	JSONOutputTokens  int
}

type configuredProvider struct { APIKey string; BaseURL string }

var configuredMu sync.RWMutex
var configured = map[string]configuredProvider{}

var compatibleCatalog = []compatibleSpec{
	{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", EnvKey: "OPENROUTER_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "together", BaseURL: "https://api.together.xyz/v1", EnvKey: "TOGETHER_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1", EnvKey: "FIREWORKS_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "cerebras", BaseURL: "https://api.cerebras.ai/v1", EnvKey: "CEREBRAS_API_KEY", MinInterval: 8 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "mistral", BaseURL: "https://api.mistral.ai/v1", EnvKey: "MISTRAL_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", EnvKey: "DEEPSEEK_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 24000, MaxOutputTokens: 4000, JSONOutputTokens: 3500},
	{Name: "xai", BaseURL: "https://api.x.ai/v1", EnvKey: "XAI_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 24000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "perplexity", BaseURL: "https://api.perplexity.ai", EnvKey: "PERPLEXITY_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "sambanova", BaseURL: "https://api.sambanova.ai/v1", EnvKey: "SAMBANOVA_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "nvidia", BaseURL: "https://integrate.api.nvidia.com/v1", EnvKey: "NVIDIA_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "deepinfra", BaseURL: "https://api.deepinfra.com/v1/openai", EnvKey: "DEEPINFRA_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "hyperbolic", BaseURL: "https://api.hyperbolic.xyz/v1", EnvKey: "HYPERBOLIC_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "novita", BaseURL: "https://api.novita.ai/v3/openai", EnvKey: "NOVITA_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "siliconflow", BaseURL: "https://api.siliconflow.com/v1", EnvKey: "SILICONFLOW_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "moonshot", BaseURL: "https://api.moonshot.cn/v1", EnvKey: "MOONSHOT_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "zhipu", BaseURL: "https://open.bigmodel.cn/api/paas/v4", EnvKey: "ZHIPU_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "dashscope", BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", EnvKey: "DASHSCOPE_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "baichuan", BaseURL: "https://api.baichuan-ai.com/v1", EnvKey: "BAICHUAN_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "minimax", BaseURL: "https://api.minimax.io/v1", EnvKey: "MINIMAX_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "volcengine", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", EnvKey: "VOLCENGINE_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "modelscope", BaseURL: "https://api-inference.modelscope.cn/v1", EnvKey: "MODELSCOPE_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "yi", BaseURL: "https://api.lingyiwanwu.com/v1", EnvKey: "YI_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "stepfun", BaseURL: "https://api.stepfun.com/v1", EnvKey: "STEPFUN_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "cohere", BaseURL: "https://api.cohere.com/compatibility/v1", EnvKey: "COHERE_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "github-models", BaseURL: "https://models.inference.ai.azure.com", EnvKey: "GITHUB_TOKEN", MinInterval: 4 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "friendli", BaseURL: "https://api.friendli.ai/serverless/v1", EnvKey: "FRIENDLI_TOKEN", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "baseten", BaseURL: "https://inference.baseten.co/v1", EnvKey: "BASETEN_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "lambda", BaseURL: "https://api.lambdal.ai/v1", EnvKey: "LAMBDA_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "nebius", BaseURL: "https://api.tokenfactory.nebius.ai/v1", EnvKey: "NEBIUS_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "scaleway", BaseURL: "https://api.scaleway.ai/v1", EnvKey: "SCALEWAY_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "cloudflare", BaseURL: "", EnvKey: "CLOUDFLARE_API_TOKEN", MinInterval: 5 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 2500, JSONOutputTokens: 2200},
	{Name: "predibase", BaseURL: "", EnvKey: "PREDIBASE_API_TOKEN", MinInterval: 5 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 2500, JSONOutputTokens: 2200},
	{Name: "lepton", BaseURL: "", EnvKey: "LEPTON_API_TOKEN", MinInterval: 5 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 2500, JSONOutputTokens: 2200},
	{Name: "featherless", BaseURL: "https://api.featherless.ai/v1", EnvKey: "FEATHERLESS_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "inference-net", BaseURL: "https://api.inference.net/v1", EnvKey: "INFERENCE_NET_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "portkey", BaseURL: "https://api.portkey.ai/v1", EnvKey: "PORTKEY_API_KEY", MinInterval: 4 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "databricks", BaseURL: "", EnvKey: "DATABRICKS_TOKEN", MinInterval: 5 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "azure-openai", BaseURL: "", EnvKey: "AZURE_OPENAI_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "ollama", BaseURL: "http://127.0.0.1:11434/v1", EnvKey: "OLLAMA_API_KEY", MinInterval: 500 * time.Millisecond, MaxInputChars: 10000, MaxOutputTokens: 2200, JSONOutputTokens: 1800},
	{Name: "vllm", BaseURL: "http://127.0.0.1:8000/v1", EnvKey: "VLLM_API_KEY", MinInterval: 500 * time.Millisecond, MaxInputChars: 12000, MaxOutputTokens: 2400, JSONOutputTokens: 2000},
	{Name: "text-generation-inference", BaseURL: "http://127.0.0.1:8080/v1", EnvKey: "TGI_API_KEY", MinInterval: 500 * time.Millisecond, MaxInputChars: 12000, MaxOutputTokens: 2400, JSONOutputTokens: 2000},
	{Name: "lmstudio", BaseURL: "http://127.0.0.1:1234/v1", EnvKey: "LMSTUDIO_API_KEY", MinInterval: 500 * time.Millisecond, MaxInputChars: 10000, MaxOutputTokens: 2200, JSONOutputTokens: 1800},
	{Name: "jan", BaseURL: "http://127.0.0.1:1337/v1", EnvKey: "JAN_API_KEY", MinInterval: 500 * time.Millisecond, MaxInputChars: 10000, MaxOutputTokens: 2200, JSONOutputTokens: 1800},
	{Name: "litellm", BaseURL: "http://127.0.0.1:4000/v1", EnvKey: "LITELLM_API_KEY", MinInterval: 1 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "ai21", BaseURL: "https://api.ai21.com/studio/v1", EnvKey: "AI21_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "aleph-alpha", BaseURL: "https://api.aleph-alpha.com", EnvKey: "ALEPH_ALPHA_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "inworld", BaseURL: "https://api.inworld.ai/llm/v1", EnvKey: "INWORLD_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 18000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "novelai", BaseURL: "", EnvKey: "NOVELAI_API_KEY", MinInterval: 5 * time.Second, MaxInputChars: 16000, MaxOutputTokens: 2500, JSONOutputTokens: 2200},
	{Name: "togetherai", BaseURL: "https://api.together.xyz/v1", EnvKey: "TOGETHER_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 22000, MaxOutputTokens: 3500, JSONOutputTokens: 3000},
	{Name: "openai-compatible", BaseURL: "", EnvKey: "FUZECLI_COMPATIBLE_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "custom-1", BaseURL: "", EnvKey: "FUZECLI_CUSTOM_1_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "custom-2", BaseURL: "", EnvKey: "FUZECLI_CUSTOM_2_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "custom-3", BaseURL: "", EnvKey: "FUZECLI_CUSTOM_3_API_KEY", MinInterval: 3 * time.Second, MaxInputChars: 20000, MaxOutputTokens: 3000, JSONOutputTokens: 2500},
	{Name: "gemini-normalizer", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", EnvKey: "FUZECLI_GEMINI_NORMALIZER_API_KEY", MinInterval: 2 * time.Second, MaxInputChars: 30000, MaxOutputTokens: 12000, JSONOutputTokens: 12000},
}

func Configure(name, apiKey, baseURL string) { configuredMu.Lock(); configured[strings.ToLower(strings.TrimSpace(name))] = configuredProvider{APIKey: apiKey, BaseURL: baseURL}; configuredMu.Unlock() }
func configuredFor(name string) configuredProvider { configuredMu.RLock(); cfg := configured[strings.ToLower(name)]; configuredMu.RUnlock(); return cfg }
func compatibleSpecFor(name string) (compatibleSpec, bool) { for _, spec := range compatibleCatalog { if strings.EqualFold(spec.Name, name) { return spec, true } }; return compatibleSpec{}, false }
func CompatibleProviderNames() []string { out := make([]string, 0, len(compatibleCatalog)); for _, spec := range compatibleCatalog { out = append(out, spec.Name) }; sort.Strings(out); return out }
func NewAllCompatibleProviders(resolve func(string) (string, string)) []Provider { providers := make([]Provider, 0, len(compatibleCatalog)); for _, spec := range compatibleCatalog { apiKey, baseURL := "", ""; if resolve != nil { apiKey, baseURL = resolve(spec.Name) }; providers = append(providers, newCompatibleProvider(spec, configuredProvider{APIKey: apiKey, BaseURL: baseURL})) }; return providers }
func ProviderPolicy(name string) compatibleSpec { if spec, ok := compatibleSpecFor(name); ok { return spec }; return compatibleSpec{Name: name, MinInterval: 3 * time.Second, MaxInputChars: 16000, MaxOutputTokens: 2600, JSONOutputTokens: 2000} }
