package platform

import("context";"errors";"github.com/Enoch7768/fuzecli/internal/provider")

type Manager struct{MemoryRoot string;Workspaces *WorkspaceManager;Jobs *JobRunner}
func NewManager(root string)*Manager{return &Manager{MemoryRoot:root,Workspaces:NewWorkspaceManager(),Jobs:NewJobRunner()}}
func(m *Manager)Context(ctx context.Context,prompt string,rules []Rule,skills []Skill)string{if m==nil{return ""};memory,_:=LoadMemory(m.MemoryRoot);out:="";if memory.Architecture!=""{out+="\nProject architecture:\n"+memory.Architecture};if len(memory.Decisions)>0{out+="\nProject decisions:\n- "+join(memory.Decisions)};if len(memory.Conventions)>0{out+="\nProject conventions:\n- "+join(memory.Conventions)};if len(rules)>0{out+="\nProject rules:\n";for _,r:=range rules{out+="\n["+r.Name+"]\n"+r.Content}};if len(skills)>0{out+="\nAvailable skills:\n";for _,s:=range skills{out+="\n["+s.Name+"]\n"+s.Content}};_ = ctx;_ = prompt;return out}
func(m *Manager)Validate()error{if m==nil{return errors.New("platform manager is nil")};if m.MemoryRoot==""{return errors.New("platform manager has no workspace root")};return nil}
func join(values []string)string{if len(values)==0{return ""};out:=values[0];for _,v:=range values[1:]{out+="\n- "+v};return out}
var _ provider.Provider
