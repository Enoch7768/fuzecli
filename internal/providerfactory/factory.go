package providerfactory

import (
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/provider/ai21"
	"github.com/Enoch7768/fuzecli/internal/provider/alephalpha"
	"github.com/Enoch7768/fuzecli/internal/provider/baseten"
	"github.com/Enoch7768/fuzecli/internal/provider/baichuan"
	"github.com/Enoch7768/fuzecli/internal/provider/cerebras"
	"github.com/Enoch7768/fuzecli/internal/provider/cloudflare"
	"github.com/Enoch7768/fuzecli/internal/provider/cohere"
	"github.com/Enoch7768/fuzecli/internal/provider/custom1"
	"github.com/Enoch7768/fuzecli/internal/provider/custom2"
	"github.com/Enoch7768/fuzecli/internal/provider/custom3"
	"github.com/Enoch7768/fuzecli/internal/provider/dashscope"
	"github.com/Enoch7768/fuzecli/internal/provider/databricks"
	"github.com/Enoch7768/fuzecli/internal/provider/deepinfra"
	"github.com/Enoch7768/fuzecli/internal/provider/deepseek"
	"github.com/Enoch7768/fuzecli/internal/provider/featherless"
	"github.com/Enoch7768/fuzecli/internal/provider/fireworks"
	"github.com/Enoch7768/fuzecli/internal/provider/friendli"
	"github.com/Enoch7768/fuzecli/internal/provider/githubmodels"
	"github.com/Enoch7768/fuzecli/internal/provider/hyperbolic"
	inferencenet "github.com/Enoch7768/fuzecli/internal/provider/inference-net"
	"github.com/Enoch7768/fuzecli/internal/provider/inworld"
	"github.com/Enoch7768/fuzecli/internal/provider/jan"
	"github.com/Enoch7768/fuzecli/internal/provider/lambda"
	"github.com/Enoch7768/fuzecli/internal/provider/lepton"
	"github.com/Enoch7768/fuzecli/internal/provider/litellm"
	"github.com/Enoch7768/fuzecli/internal/provider/lmstudio"
	"github.com/Enoch7768/fuzecli/internal/provider/minimax"
	"github.com/Enoch7768/fuzecli/internal/provider/mistral"
	"github.com/Enoch7768/fuzecli/internal/provider/modelscope"
	"github.com/Enoch7768/fuzecli/internal/provider/moonshot"
	"github.com/Enoch7768/fuzecli/internal/provider/nebius"
	"github.com/Enoch7768/fuzecli/internal/provider/novelai"
	"github.com/Enoch7768/fuzecli/internal/provider/novita"
	"github.com/Enoch7768/fuzecli/internal/provider/nvidia"
	"github.com/Enoch7768/fuzecli/internal/provider/ollama"
	"github.com/Enoch7768/fuzecli/internal/provider/openaicompatible"
	"github.com/Enoch7768/fuzecli/internal/provider/openrouter"
	"github.com/Enoch7768/fuzecli/internal/provider/perplexity"
	"github.com/Enoch7768/fuzecli/internal/provider/portkey"
	"github.com/Enoch7768/fuzecli/internal/provider/predibase"
	"github.com/Enoch7768/fuzecli/internal/provider/sambanova"
	"github.com/Enoch7768/fuzecli/internal/provider/scaleway"
	"github.com/Enoch7768/fuzecli/internal/provider/siliconflow"
	"github.com/Enoch7768/fuzecli/internal/provider/stepfun"
	"github.com/Enoch7768/fuzecli/internal/provider/textgenerationinference"
	"github.com/Enoch7768/fuzecli/internal/provider/together"
	"github.com/Enoch7768/fuzecli/internal/provider/togetherai"
	"github.com/Enoch7768/fuzecli/internal/provider/vllm"
	"github.com/Enoch7768/fuzecli/internal/provider/volcengine"
	"github.com/Enoch7768/fuzecli/internal/provider/xai"
	"github.com/Enoch7768/fuzecli/internal/provider/yi"
	"github.com/Enoch7768/fuzecli/internal/provider/zhipu"
)

func New(c config.Config) []provider.Provider {
	p := func(name string) config.ProviderConfig { return c.Providers[name] }
	return []provider.Provider{
		openrouter.New(p("openrouter").APIKey, p("openrouter").BaseURL),
		together.New(p("together").APIKey, p("together").BaseURL),
		fireworks.New(p("fireworks").APIKey, p("fireworks").BaseURL),
		cerebras.New(p("cerebras").APIKey, p("cerebras").BaseURL),
		mistral.New(p("mistral").APIKey, p("mistral").BaseURL),
		deepseek.New(p("deepseek").APIKey, p("deepseek").BaseURL),
		xai.New(p("xai").APIKey, p("xai").BaseURL),
		perplexity.New(p("perplexity").APIKey, p("perplexity").BaseURL),
		sambanova.New(p("sambanova").APIKey, p("sambanova").BaseURL),
		nvidia.New(p("nvidia").APIKey, p("nvidia").BaseURL),
		deepinfra.New(p("deepinfra").APIKey, p("deepinfra").BaseURL),
		hyperbolic.New(p("hyperbolic").APIKey, p("hyperbolic").BaseURL),
		novita.New(p("novita").APIKey, p("novita").BaseURL),
		siliconflow.New(p("siliconflow").APIKey, p("siliconflow").BaseURL),
		moonshot.New(p("moonshot").APIKey, p("moonshot").BaseURL),
		zhipu.New(p("zhipu").APIKey, p("zhipu").BaseURL),
		dashscope.New(p("dashscope").APIKey, p("dashscope").BaseURL),
		baichuan.New(p("baichuan").APIKey, p("baichuan").BaseURL),
		minimax.New(p("minimax").APIKey, p("minimax").BaseURL),
		volcengine.New(p("volcengine").APIKey, p("volcengine").BaseURL),
		modelscope.New(p("modelscope").APIKey, p("modelscope").BaseURL),
		yi.New(p("yi").APIKey, p("yi").BaseURL),
		stepfun.New(p("stepfun").APIKey, p("stepfun").BaseURL),
		cohere.New(p("cohere").APIKey, p("cohere").BaseURL),
		githubmodels.New(p("github-models").APIKey, p("github-models").BaseURL),
		friendli.New(p("friendli").APIKey, p("friendli").BaseURL),
		baseten.New(p("baseten").APIKey, p("baseten").BaseURL),
		lambda.New(p("lambda").APIKey, p("lambda").BaseURL),
		nebius.New(p("nebius").APIKey, p("nebius").BaseURL),
		scaleway.New(p("scaleway").APIKey, p("scaleway").BaseURL),
		cloudflare.New(p("cloudflare").APIKey, p("cloudflare").BaseURL),
		predibase.New(p("predibase").APIKey, p("predibase").BaseURL),
		lepton.New(p("lepton").APIKey, p("lepton").BaseURL),
		featherless.New(p("featherless").APIKey, p("featherless").BaseURL),
		inferencenet.New(p("inference-net").APIKey, p("inference-net").BaseURL),
		portkey.New(p("portkey").APIKey, p("portkey").BaseURL),
		databricks.New(p("databricks").APIKey, p("databricks").BaseURL),
		azureopenai.New(p("azure-openai").APIKey, p("azure-openai").BaseURL),
		ollama.New(p("ollama").APIKey, p("ollama").BaseURL),
		vllm.New(p("vllm").APIKey, p("vllm").BaseURL),
		textgenerationinference.New(p("textgenerationinference").APIKey, p("textgenerationinference").BaseURL),
		lmstudio.New(p("lmstudio").APIKey, p("lmstudio").BaseURL),
		jan.New(p("jan").APIKey, p("jan").BaseURL),
		litellm.New(p("litellm").APIKey, p("litellm").BaseURL),
		ai21.New(p("ai21").APIKey, p("ai21").BaseURL),
		alephalpha.New(p("alephalpha").APIKey, p("alephalpha").BaseURL),
		inworld.New(p("inworld").APIKey, p("inworld").BaseURL),
		novelai.New(p("novelai").APIKey, p("novelai").BaseURL),
		togetherai.New(p("togetherai").APIKey, p("togetherai").BaseURL),
		openaicompatible.New(p("openaicompatible").APIKey, p("openaicompatible").BaseURL),
		custom1.New(p("custom1").APIKey, p("custom1").BaseURL),
		custom2.New(p("custom2").APIKey, p("custom2").BaseURL),
		custom3.New(p("custom3").APIKey, p("custom3").BaseURL),
	}
}
