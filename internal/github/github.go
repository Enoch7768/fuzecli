package github

import (
 "bytes"
 "context"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"
)

type Client struct{ Token, BaseURL string; HTTP *http.Client }
type Repository struct{ ID int64 `json:"id"`; Name string `json:"name"`; FullName string `json:"full_name"`; Description string `json:"description"`; HTMLURL string `json:"html_url"`; CloneURL string `json:"clone_url"`; SSHURL string `json:"ssh_url"`; DefaultBranch string `json:"default_branch"`; Private bool `json:"private"`; Archived bool `json:"archived"` }
type Branch struct{Name string `json:"name"`; SHA string `json:"sha"`}
type PullRequest struct{ Number int `json:"number"`; Title string `json:"title"`; Body string `json:"body"`; State string `json:"state"`; Draft bool `json:"draft"`; HTMLURL string `json:"html_url"`; Head struct{Ref,SHA string `json:"ref"`} `json:"head"`; Base struct{Ref,SHA string `json:"ref"`} `json:"base"`; User struct{Login string `json:"login"`} `json:"user"` }
type Comment struct{ ID int64 `json:"id"`; Body string `json:"body"`; HTMLURL string `json:"html_url"`; User struct{Login string `json:"login"`} `json:"user"`; CreatedAt time.Time `json:"created_at"`; UpdatedAt time.Time `json:"updated_at"` }
type WorkflowRun struct{ ID int64 `json:"id"`; Name string `json:"name"`; Status string `json:"status"`; Conclusion string `json:"conclusion"`; HTMLURL string `json:"html_url"`; HeadBranch string `json:"head_branch"`; RunNumber int `json:"run_number"`; CreatedAt,UpdatedAt time.Time `json:"created_at"` }
type User struct{Login string `json:"login"`; Name string `json:"name"`; Email string `json:"email"`; HTMLURL string `json:"html_url"`}
type Issue struct{Number int `json:"number"`; Title string `json:"title"`; Body string `json:"body"`; State string `json:"state"`; HTMLURL string `json:"html_url"`}
type CreateRepositoryRequest struct{Name string `json:"name"`; Description string `json:"description,omitempty"`; Private,AutoInit bool `json:"private"`}
type CreatePullRequestRequest struct{Title string `json:"title"`; Body string `json:"body,omitempty"`; Head string `json:"head"`; Base string `json:"base"`; Draft bool `json:"draft"`}
type ReviewRequest struct{Body string `json:"body"`; Event string `json:"event"`}

func New(token string)*Client{return &Client{Token:strings.TrimSpace(token),BaseURL:"https://api.github.com",HTTP:&http.Client{Timeout:30*time.Second}}}
func(c *Client)Authenticated()bool{return c!=nil&&strings.TrimSpace(c.Token)!=""}
func(c *Client)do(ctx context.Context,method,path string,body,out any)error{
 if !c.Authenticated(){return errors.New("GitHub is not authenticated; set GITHUB_TOKEN, GH_TOKEN, or run 'aicli github auth set <token>'")}
 var payload []byte; var err error
 if body!=nil{payload,err=json.Marshal(body);if err!=nil{return fmt.Errorf("encode GitHub request: %w",err)}}
 req,err:=http.NewRequestWithContext(ctx,method,strings.TrimRight(c.BaseURL,"/")+path,bytes.NewReader(payload));if err!=nil{return fmt.Errorf("create GitHub request: %w",err)}
 req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28");req.Header.Set("Authorization","Bearer "+c.Token);if body!=nil{req.Header.Set("Content-Type","application/json")}
 resp,err:=c.HTTP.Do(req);if err!=nil{return fmt.Errorf("GitHub request failed: %w",err)};defer resp.Body.Close()
 data,err:=io.ReadAll(io.LimitReader(resp.Body,8<<20));if err!=nil{return fmt.Errorf("read GitHub response: %w",err)}
 if resp.StatusCode<200||resp.StatusCode>=300{var e struct{Message string `json:"message"`};_ = json.Unmarshal(data,&e);m:=strings.TrimSpace(e.Message);if m==""{m=strings.TrimSpace(string(data))};if resp.StatusCode==401{return errors.New("GitHub authentication failed; check the token and its repository permissions")};if resp.StatusCode==403{return fmt.Errorf("GitHub permission denied: %s",m)};if resp.StatusCode==404{return fmt.Errorf("GitHub resource not found or not accessible: %s",m)};return fmt.Errorf("GitHub API error (%d): %s",resp.StatusCode,m)}
 if out!=nil&&len(data)>0{if err:=json.Unmarshal(data,out);err!=nil{return fmt.Errorf("decode GitHub response: %w",err)}};return nil
}
func(c *Client)Profile(ctx context.Context)(User,error){var v User;err:=c.do(ctx,http.MethodGet,"/user",nil,&v);return v,err}
func(c *Client)Repositories(ctx context.Context,page,perPage int)([]Repository,error){if page<1{page=1};if perPage<=0||perPage>100{perPage=100};var v []Repository;err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/user/repos?affiliation=owner,collaborator,organization_member&sort=updated&per_page=%d&page=%d",perPage,page),nil,&v);return v,err}
func(c *Client)Repository(ctx context.Context,full string)(Repository,error){var v Repository;err:=c.do(ctx,http.MethodGet,"/repos/"+pathPart(full),nil,&v);return v,err}
func(c *Client)CreateRepository(ctx context.Context,v CreateRepositoryRequest)(Repository,error){v.Name=strings.TrimSpace(v.Name);if v.Name==""{return Repository{},errors.New("repository name is required")};if strings.ContainsAny(v.Name,"/\\"){return Repository{},errors.New("repository name cannot contain path separators")};var out Repository;err:=c.do(ctx,http.MethodPost,"/user/repos",v,&out);return out,err}
func(c *Client)Branches(ctx context.Context,full string)([]Branch,error){var raw []struct{Name string `json:"name"`;Commit struct{SHA string `json:"sha"`} `json:"commit"`};err:=c.do(ctx,http.MethodGet,"/repos/"+pathPart(full)+"/branches?per_page=100",nil,&raw);if err!=nil{return nil,err};out:=make([]Branch,0,len(raw));for _,v:=range raw{out=append(out,Branch{Name:v.Name,SHA:v.Commit.SHA})};return out,nil}
func(c *Client)CreateBranch(ctx context.Context,full,name,from string)error{if strings.TrimSpace(name)==""{return errors.New("branch name is required")};if strings.TrimSpace(from)==""{r,err:=c.Repository(ctx,full);if err!=nil{return err};from=r.DefaultBranch};var ref struct{Object struct{SHA string `json:"sha"`} `json:"object"`};if err:=c.do(ctx,http.MethodGet,"/repos/"+pathPart(full)+"/git/ref/heads/"+pathPart(from),nil,&ref);err!=nil{return fmt.Errorf("resolve branch %q: %w",from,err)};return c.do(ctx,http.MethodPost,"/repos/"+pathPart(full)+"/git/refs",map[string]string{"ref":"refs/heads/"+name,"sha":ref.Object.SHA},nil)}
func(c *Client)PullRequests(ctx context.Context,full,state string)([]PullRequest,error){if state==""{state="open"};var v []PullRequest;err:=c.do(ctx,http.MethodGet,"/repos/"+pathPart(full)+"/pulls?state="+urlQuery(state)+"&per_page=100",nil,&v);return v,err}
func(c *Client)CreatePullRequest(ctx context.Context,full string,v CreatePullRequestRequest)(PullRequest,error){var out PullRequest;err:=c.do(ctx,http.MethodPost,"/repos/"+pathPart(full)+"/pulls",v,&out);return out,err}
func(c *Client)PullRequest(ctx context.Context,full string,n int)(PullRequest,error){var v PullRequest;err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/repos/%s/pulls/%d",pathPart(full),n),nil,&v);return v,err}
func(c *Client)Comments(ctx context.Context,full string,n int)([]Comment,error){var v []Comment;err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100",pathPart(full),n),nil,&v);return v,err}
func(c *Client)AddComment(ctx context.Context,full string,n int,body string)(Comment,error){if strings.TrimSpace(body)==""{return Comment{},errors.New("comment body is required")};var v Comment;err:=c.do(ctx,http.MethodPost,fmt.Sprintf("/repos/%s/issues/%d/comments",pathPart(full),n),map[string]string{"body":body},&v);return v,err}
func(c *Client)Review(ctx context.Context,full string,n int,v ReviewRequest)error{v.Event=strings.ToUpper(strings.TrimSpace(v.Event));switch v.Event{case"APPROVE","COMMENT","REQUEST_CHANGES":default:return errors.New("review event must be APPROVE, COMMENT, or REQUEST_CHANGES")};return c.do(ctx,http.MethodPost,fmt.Sprintf("/repos/%s/pulls/%d/reviews",pathPart(full),n),v,nil)}
func(c *Client)WorkflowRuns(ctx context.Context,full string)([]WorkflowRun,error){var v struct{WorkflowRuns []WorkflowRun `json:"workflow_runs"`};err:=c.do(ctx,http.MethodGet,"/repos/"+pathPart(full)+"/actions/runs?per_page=100",nil,&v);return v.WorkflowRuns,err}
func(c *Client)RerunWorkflow(ctx context.Context,full string,id int64)error{return c.do(ctx,http.MethodPost,fmt.Sprintf("/repos/%s/actions/runs/%d/rerun",pathPart(full),id),nil,nil)}
func(c *Client)WorkflowJobs(ctx context.Context,full string,id int64)(any,error){var v any;err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=100",pathPart(full),id),nil,&v);return v,err}
func(c *Client)Issues(ctx context.Context,full,state string)([]Issue,error){if state==""{state="open"};var v []Issue;err:=c.do(ctx,http.MethodGet,"/repos/"+pathPart(full)+"/issues?state="+urlQuery(state)+"&per_page=100",nil,&v);if err!=nil{return nil,err};out:=v[:0];for _,x:=range v{out=append(out,x)};return out,nil}
func(c *Client)CreateIssue(ctx context.Context,full,title,body string)(Issue,error){var v Issue;err:=c.do(ctx,http.MethodPost,"/repos/"+pathPart(full)+"/issues",map[string]string{"title":title,"body":body},&v);return v,err}

func LocalRemote(root string)(string,error){out,err:=exec.Command("git","-C",root,"remote","get-url","origin").Output();if err!=nil{return "",errors.New("this workspace has no Git origin remote")};return strings.TrimSpace(string(out)),nil}
func Pull(ctx context.Context,root,remote,branch,token string)(string,error){if err:=ensureGit(root);err!=nil{return "",err};args:=[]string{"pull","--ff-only"};if strings.TrimSpace(remote)!=""{args=append(args,remote)};if strings.TrimSpace(branch)!=""{args=append(args,branch)};return runGit(ctx,root,token,args...)}
func Push(ctx context.Context,root,remote,branch,token string)(string,error){if err:=ensureGit(root);err!=nil{return "",err};args:=[]string{"push","-u"};if strings.TrimSpace(remote)!=""{args=append(args,remote)};if strings.TrimSpace(branch)!=""{args=append(args,branch)}else{args=append(args,"origin","HEAD")};return runGit(ctx,root,token,args...)}
func Clone(ctx context.Context,url,destination,token string)(string,error){if strings.TrimSpace(url)==""||strings.TrimSpace(destination)==""{return "",errors.New("clone URL and destination are required")};if _,err:=os.Stat(destination);err==nil{return "",fmt.Errorf("destination already exists: %s",destination)}else if !os.IsNotExist(err){return "",err};if err:=os.MkdirAll(filepath.Dir(destination),0755);err!=nil{return "",err};return runGit(ctx,filepath.Dir(destination),token,"clone",url,destination)}
func ensureGit(root string)error{info,err:=os.Stat(filepath.Join(root,".git"));if err!=nil||!info.IsDir(){return errors.New("workspace is not a Git repository")};return nil}
func runGit(ctx context.Context,root,token string,args ...string)(string,error){cmd:=exec.CommandContext(ctx,"git",args...);cmd.Dir=root;cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");if strings.TrimSpace(token)!=""{remote,_:=LocalRemote(root);if strings.HasPrefix(strings.ToLower(remote),"https://github.com/"){auth:=base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token));cmd.Env=append(cmd.Env,"GIT_CONFIG_COUNT=1","GIT_CONFIG_KEY_0=http.extraheader","GIT_CONFIG_VALUE_0=Authorization: Basic "+auth)}};var out,er bytes.Buffer;cmd.Stdout=&out;cmd.Stderr=&er;err:=cmd.Run();s:=strings.TrimSpace(out.String());if e:=strings.TrimSpace(er.String());e!=""{if s!=""{s+="\n"};s+=e};if err!=nil{return s,fmt.Errorf("git %s failed: %s",strings.Join(args," "),s)};return s,nil}
func pathPart(v string)string{return strings.Trim(strings.TrimSpace(v),"/")}
func urlQuery(v string)string{return strings.ReplaceAll(strings.TrimSpace(v)," ","%20")}
