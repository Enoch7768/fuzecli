package platform

import("os";"path/filepath";"sort";"strings")

func LoadRules(root string)([]Rule,error){return loadMarkdownDirectory(filepath.Join(root,".aicli","rules"))}
func LoadSkills(root string)([]Skill,error){
 dir:=filepath.Join(root,".aicli","skills");entries,err:=os.ReadDir(dir);if os.IsNotExist(err){return nil,nil};if err!=nil{return nil,err}
 var skills []Skill
 for _,entry:=range entries{if !entry.IsDir(){continue};path:=filepath.Join(dir,entry.Name());data,err:=os.ReadFile(filepath.Join(path,"SKILL.md"));if os.IsNotExist(err){continue};if err!=nil{return nil,err};skills=append(skills,Skill{Name:entry.Name(),Path:filepath.ToSlash(filepath.Join(".aicli","skills",entry.Name(),"SKILL.md")),Content:string(data)})}
 sort.Slice(skills,func(i,j int)bool{return skills[i].Name<skills[j].Name});return skills,nil
}
func loadMarkdownDirectory(dir string)([]Rule,error){
 entries,err:=os.ReadDir(dir);if os.IsNotExist(err){return nil,nil};if err!=nil{return nil,err};var rules []Rule
 for _,entry:=range entries{if entry.IsDir()||!strings.HasSuffix(strings.ToLower(entry.Name()),".md"){continue};data,err:=os.ReadFile(filepath.Join(dir,entry.Name()));if err!=nil{return nil,err};rules=append(rules,Rule{Name:strings.TrimSuffix(entry.Name(),filepath.Ext(entry.Name())),Content:string(data)})}
 sort.Slice(rules,func(i,j int)bool{return rules[i].Name<rules[j].Name});return rules,nil
}
