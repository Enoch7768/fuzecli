package platform

import (
 "os"
 "path/filepath"
 "strings"
 "github.com/Enoch7768/fuzecli/internal/repository"
)

func BuildGraph(root string)(ContextGraph,error){
 index,err:=repository.Build(root);if err!=nil{return ContextGraph{},err}
 graph:=ContextGraph{}
 for _,file:=range index.Files{
  graph.Nodes=append(graph.Nodes,GraphNode{Path:file.Path,Language:file.Language,Symbols:append([]string(nil),file.Symbols...),Imports:append([]string(nil),file.Imports...)})
  for _,imp:=range file.Imports{
   if target:=resolveImport(root,file.Path,imp);target!=""{graph.Edges=append(graph.Edges,GraphEdge{From:file.Path,To:target,Kind:"imports"})}
  }
 }
 return graph,nil
}
func resolveImport(root,from,imp string)string{
 imp=strings.TrimSpace(imp);if imp==""{return ""}
 base:=filepath.Dir(filepath.Join(root,filepath.FromSlash(from)))
 candidates:=[]string{imp,imp+".go",imp+".ts",imp+".tsx",imp+".js",imp+".jsx",imp+".py",filepath.Join(imp,"index.ts"),filepath.Join(imp,"index.js")}
 for _,candidate:=range candidates{
  path:=candidate;if !filepath.IsAbs(path){path=filepath.Join(base,filepath.FromSlash(candidate))}
  if info,err:=os.Stat(path);err==nil&&!info.IsDir(){rel,_:=filepath.Rel(root,path);return filepath.ToSlash(rel)}
 }
 return ""
}
