package platform

import("context";"os";"path/filepath";"testing";"time")

func TestMemoryRoundTrip(t *testing.T){root:=t.TempDir();want:=ProjectMemory{Architecture:"service graph",Decisions:[]string{"transactions"}};if err:=SaveMemory(root,want);err!=nil{t.Fatal(err)};got,err:=LoadMemory(root);if err!=nil{t.Fatal(err)};if got.Architecture!=want.Architecture||len(got.Decisions)!=1{t.Fatalf("unexpected memory: %+v",got)}}
func TestWorkspaceManager(t *testing.T){root:=t.TempDir();m:=NewWorkspaceManager();if _,err:=m.Add("demo",root);err!=nil{t.Fatal(err)};if len(m.List())!=1{t.Fatal("workspace was not added")};m.Remove("demo");if len(m.List())!=0{t.Fatal("workspace was not removed")}}
func TestReviewDiff(t *testing.T){review:=ReviewDiff(`+ api_key = "1234567890123456"`);if review.Passed{t.Fatal("secret finding should fail review")}}
func TestJobRunner(t *testing.T){r:=NewJobRunner();job,err:=r.Start(context.Background(),"test",func(ctx context.Context,progress func(string))(string,error){progress("done");return "ok",nil});if err!=nil{t.Fatal(err)};deadline:=time.Now().Add(time.Second);for{got,ok:=r.Get(job.ID);if ok&&got.Status==JobSucceeded{if got.Result!="ok"{t.Fatal(got.Result)};return};if time.Now().After(deadline){t.Fatal("job did not finish")};time.Sleep(10*time.Millisecond)}}
func TestProof(t *testing.T){root:=t.TempDir();proof,err:=SaveProof(root,Proof{Goal:"test"});if err!=nil{t.Fatal(err)};if _,err:=os.Stat(filepath.Join(root,".aicli","proofs",proof.ID+".json"));err!=nil{t.Fatal(err)}}
