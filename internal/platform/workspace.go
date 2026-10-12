package platform

import ("os";"path/filepath";"strings";"sync";"sort")

type WorkspaceManager struct{mu sync.RWMutex;workspaces map[string]Workspace}
func NewWorkspaceManager()*WorkspaceManager{return &WorkspaceManager{workspaces:map[string]Workspace{}}}
func(m *WorkspaceManager)Add(name,root string)(Workspace,error){root,err:=filepath.Abs(root);if err!=nil{return Workspace{},err};info,err:=os.Stat(root);if err!=nil||!info.IsDir(){return Workspace{},os.ErrNotExist};name=strings.TrimSpace(name);if name==""{name=filepath.Base(root)};w:=Workspace{Name:name,Root:root};m.mu.Lock();m.workspaces[name]=w;m.mu.Unlock();return w,nil}
func(m *WorkspaceManager)Remove(name string){m.mu.Lock();delete(m.workspaces,name);m.mu.Unlock()}
func(m *WorkspaceManager)List()[]Workspace{m.mu.RLock();defer m.mu.RUnlock();out:=make([]Workspace,0,len(m.workspaces));for _,w:=range m.workspaces{out=append(out,w)};sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name});return out}
