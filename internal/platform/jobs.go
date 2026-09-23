package platform

import("context";"errors";"sync";"time";"github.com/google/uuid")

type JobRunner struct{mu sync.RWMutex;jobs map[string]*BackgroundJob}
func NewJobRunner()*JobRunner{return &JobRunner{jobs:map[string]*BackgroundJob{}}}
func(r *JobRunner)Start(ctx context.Context,prompt string,work func(context.Context,func(string))(string,error))(BackgroundJob,error){
 if r==nil{return BackgroundJob{},errors.New("job runner is nil")}
 job:=&BackgroundJob{ID:uuid.NewString(),Prompt:prompt,Status:JobQueued,StartedAt:time.Now().UTC()};r.mu.Lock();r.jobs[job.ID]=job;r.mu.Unlock()
 go func(){r.mu.Lock();job.Status=JobRunning;r.mu.Unlock();result,err:=work(ctx,func(message string){r.mu.Lock();job.Progress=append(job.Progress,message);r.mu.Unlock()});now:=time.Now().UTC();r.mu.Lock();defer r.mu.Unlock();job.FinishedAt=&now;if err!=nil{job.Status=JobFailed;job.Error=err.Error();return};job.Status=JobSucceeded;job.Result=result}()
 return *job,nil
}
func(r *JobRunner)Get(id string)(BackgroundJob,bool){r.mu.RLock();defer r.mu.RUnlock();job,ok:=r.jobs[id];if !ok{return BackgroundJob{},false};copy:=*job;copy.Progress=append([]string(nil),job.Progress...);return copy,true}
func(r *JobRunner)List()[]BackgroundJob{r.mu.RLock();defer r.mu.RUnlock();out:=make([]BackgroundJob,0,len(r.jobs));for _,job:=range r.jobs{copy:=*job;copy.Progress=append([]string(nil),job.Progress...);out=append(out,copy)};return out}
