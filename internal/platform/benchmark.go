package platform

import("context";"time";"github.com/Enoch7768/fuzecli/internal/provider")
type BenchmarkFunc func(context.Context,string,provider.RequestOptions)(provider.Response,error)
func RunBenchmark(ctx context.Context,task,model string,candidates []string,run BenchmarkFunc)[]BenchmarkResult{out:=make([]BenchmarkResult,0,len(candidates));for _,name:=range candidates{start:=time.Now();resp,err:=run(ctx,name,provider.RequestOptions{Model:model,Temperature:0,MaxTokens:8000});r:=BenchmarkResult{Task:task,Provider:name,Model:model,Latency:time.Since(start)};if err==nil{r.Success=true;r.Tokens=uint64(resp.Usage.TotalTokens)};out=append(out,r)};return out}
