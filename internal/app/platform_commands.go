package app

import(
 "context"
 "fmt"
 "github.com/Enoch7768/fuzecli/internal/platform"
)

func(a *App)StartBackground(ctx context.Context,prompt string,mode platform.Mode)(platform.BackgroundJob,error){
 if a.Platform==nil{return platform.BackgroundJob{},fmt.Errorf("developer platform is not initialized")}
 return a.Platform.Jobs.Start(ctx,prompt,func(ctx context.Context,progress func(string))(string,error){
  progress("planning")
  plan,err:=a.Ask(ctx,prompt,"","",true);if err!=nil{return "",err}
  progress("verified changes")
  return fmt.Sprintf("%d file(s) changed",len(plan.Files)),nil
 })
}
func(a *App)PlatformSnapshot()(map[string]any,error){
 if a.Platform==nil{return nil,fmt.Errorf("developer platform is not initialized")}
 memory,err:=platform.LoadMemory(a.Store.Root);if err!=nil{return nil,err}
 rules,_:=platform.LoadRules(a.Store.Root);skills,_:=platform.LoadSkills(a.Store.Root);graph,err:=platform.BuildGraph(a.Store.Root);if err!=nil{return nil,err}
 return map[string]any{"memory":memory,"rules":rules,"skills":skills,"graph":graph,"workspaces":a.Platform.Workspaces.List(),"jobs":a.Platform.Jobs.List()},nil
}
