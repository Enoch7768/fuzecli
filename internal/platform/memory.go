package platform

import ("encoding/json"; "errors"; "os"; "path/filepath"; "time")

func memoryPath(root string) string { return filepath.Join(root, ".aicli", "memory.json") }
func LoadMemory(root string) (ProjectMemory,error) {
 data,err:=os.ReadFile(memoryPath(root)); if errors.Is(err,os.ErrNotExist){return ProjectMemory{},nil}; if err!=nil{return ProjectMemory{},err}
 var m ProjectMemory; if err:=json.Unmarshal(data,&m);err!=nil{return ProjectMemory{},err}; return m,nil
}
func SaveMemory(root string,m ProjectMemory) error {
 m.UpdatedAt=time.Now().UTC(); if err:=os.MkdirAll(filepath.Dir(memoryPath(root)),0700);err!=nil{return err}
 data,err:=json.MarshalIndent(m,"","  ");if err!=nil{return err}; tmp:=memoryPath(root)+".tmp"
 if err:=os.WriteFile(tmp,data,0600);err!=nil{return err}; return os.Rename(tmp,memoryPath(root))
}
func UpdateMemory(root string,update func(*ProjectMemory)) error { m,err:=LoadMemory(root);if err!=nil{return err};update(&m);return SaveMemory(root,m) }
