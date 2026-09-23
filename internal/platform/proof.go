package platform

import("encoding/json";"errors";"os";"path/filepath";"time";"github.com/google/uuid")

func SaveProof(root string,proof Proof)(Proof,error){if proof.ID==""{proof.ID=uuid.NewString()};if proof.CreatedAt.IsZero(){proof.CreatedAt=time.Now().UTC()};dir:=filepath.Join(root,".aicli","proofs");if err:=os.MkdirAll(dir,0700);err!=nil{return Proof{},err};data,err:=json.MarshalIndent(proof,"","  ");if err!=nil{return Proof{},err};if err:=os.WriteFile(filepath.Join(dir,proof.ID+".json"),data,0600);err!=nil{return Proof{},err};return proof,nil}
func LoadProof(root,id string)(Proof,error){if id==""{return Proof{},errors.New("proof id is required")};data,err:=os.ReadFile(filepath.Join(root,".aicli","proofs",id+".json"));if err!=nil{return Proof{},err};var proof Proof;if err:=json.Unmarshal(data,&proof);err!=nil{return Proof{},err};return proof,nil}
