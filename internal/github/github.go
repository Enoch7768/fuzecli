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

type Client struct{ Token, BaseURL string; HTTP *http.Client; DisableCLI bool }
type Repository struct{ ID int64 `json:"id"`; Name string `json:"name"`; FullName string `json:"full_name"`; Description string `json:"description"`; HTMLURL string `json:"html_url"`; CloneURL string `json:"clone_url"`; SSHURL string `json:"ssh_url"`; DefaultBranch string `json:"default_branch"`; Private bool `json:"private"`; Archived bool `json:"archived"` }
type Branch struct{Name string `json:"name"`; SHA string `json:"sha"`}
type PullRequest struct{ Number int `json:"number"`; Title string `json:"title"`; Body string `json:"body"`; State string `json:"state"`; Draft bool `json:"draft"`; HTMLURL string `json:"html_url"`; Head struct{Ref string `json:"ref"`; SHA string `json:"sha"`} `json:"head"`; Base struct{Ref string `json:"ref"`; SHA string `json:"sha"`} `json:"base"`; User struct{Login string `json:"login"`} `json:"user"` }
type Comment struct{ ID int64 `json:"id"`; Body string `json:"body"`; HTMLURL string `json:"html_url"`; User struct{Login string `json:"login"`} `json:"user"`; CreatedAt time.Time `json:"created_at"`; UpdatedAt time.Time `json:"updated_at"` }
type WorkflowRun struct{ ID int64 `json:"id"`; Name string `json:"name"`; Status string `json:"status"`; Conclusion string `json:"conclusion"`; HTMLURL string `json:"html_url"`; HeadBranch string `json:"head_branch"`; RunNumber int `json:"run_number"`; CreatedAt time.Time `json:"created_at"`; UpdatedAt time.Time `json:"updated_at"` }
type CheckRun struct { ID int64 `json:"id"`; Name string `json:"name"`; Status string `json:"status"`; Conclusion string `json:"conclusion"`; DetailsURL string `json:"details_url"` }
type CommitStatus struct { Context string `json:"context"`; State string `json:"state"`; Description string `json:"description"`; TargetURL string `json:"target_url"` }
type PullRequestChecksSummary struct { PullRequest PullRequest `json:"pull_request"`; HeadSHA string `json:"head_sha"`; State string `json:"state"`; Checks []CheckRun `json:"checks"`; Statuses []CommitStatus `json:"statuses"` }
type User struct{Login string `json:"login"`; Name string `json:"name"`; Email string `json:"email"`; HTMLURL string `json:"html_url"`; AvatarURL string `json:"avatar_url"`; Bio string `json:"bio"`; Company string `json:"company"`; Location string `json:"location"`; PublicRepos int `json:"public_repos"`; Followers int `json:"followers"`; Following int `json:"following"`}
type Issue struct{Number int `json:"number"`; Title string `json:"title"`; Body string `json:"body"`; State string `json:"state"`; HTMLURL string `json:"html_url"`}
type CreateRepositoryRequest struct{Name string `json:"name"`; Description string `json:"description,omitempty"`; Private bool `json:"private"`; AutoInit bool `json:"auto_init"`}
type CreatePullRequestRequest struct{Title string `json:"title"`; Body string `json:"body,omitempty"`; Head string `json:"head"`; Base string `json:"base"`; Draft bool `json:"draft"`}
type ReviewRequest struct{Body string `json:"body"`; Event string `json:"event"`}

func New(token string) *Client {
	return &Client{Token: strings.TrimSpace(token), BaseURL: "https://api.github.com", HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Authenticated() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.Token) != "" {
		return true
	}
	if c.DisableCLI {
		return false
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "gh", "auth", "status", "--hostname", "github.com").Run() == nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c == nil { return errors.New("GitHub client is not configured") }
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil { return fmt.Errorf("encode GitHub request: %w", err) }
	}
	if strings.TrimSpace(c.Token) == "" {
		if c.DisableCLI { return errors.New("GitHub is disconnected; connect an account in the GitHub panel") }
		if _, err := exec.LookPath("gh"); err != nil {
			return errors.New("GitHub is not authenticated; run 'aicli github auth login' or set GITHUB_TOKEN")
		}
		endpoint := strings.TrimLeft(path, "/")
		args := []string{"api", "--hostname", "github.com", "--method", method}
		if len(payload) > 0 { args = append(args, "--input", "-") }
		args = append(args, endpoint)
		cmd := exec.CommandContext(ctx, "gh", args...)
		if len(payload) > 0 { cmd.Stdin = bytes.NewReader(payload) }
		data, runErr := cmd.Output()
		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				message := strings.TrimSpace(string(exitErr.Stderr))
				if message != "" { return fmt.Errorf("GitHub CLI request failed: %s", message) }
			}
			return fmt.Errorf("GitHub CLI request failed: %w", runErr)
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil { return fmt.Errorf("decode GitHub response: %w", err) }
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(payload))
	if err != nil { return fmt.Errorf("create GitHub request: %w", err) }
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil { req.Header.Set("Content-Type", "application/json") }
	resp, err := c.HTTP.Do(req)
	if err != nil { return fmt.Errorf("GitHub request failed: %w", err) }
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil { return fmt.Errorf("read GitHub response: %w", err) }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct { Message string `json:"message"` }
		_ = json.Unmarshal(data, &e)
		m := strings.TrimSpace(e.Message)
		if m == "" { m = strings.TrimSpace(string(data)) }
		if resp.StatusCode == 401 { return errors.New("GitHub authentication failed; check the token and its repository permissions") }
		if resp.StatusCode == 403 { return fmt.Errorf("GitHub permission denied: %s", m) }
		if resp.StatusCode == 404 { return fmt.Errorf("GitHub resource not found or not accessible: %s", m) }
		return fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, m)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil { return fmt.Errorf("decode GitHub response: %w", err) }
	}
	return nil
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
func runGit(ctx context.Context,root,token string,args ...string)(string,error){cmd:=exec.CommandContext(ctx,"git",args...);cmd.Dir=root;cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");if strings.TrimSpace(token)!=""{remote:="";if len(args)>1&&args[0]=="clone"{remote=args[1]}else{remote,_=LocalRemote(root)};if strings.HasPrefix(strings.ToLower(strings.TrimSpace(remote)),"https://github.com/"){auth:=base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token));cmd.Env=append(cmd.Env,"GIT_CONFIG_COUNT=1","GIT_CONFIG_KEY_0=http.extraheader","GIT_CONFIG_VALUE_0=Authorization: Basic "+auth)}};var out,er bytes.Buffer;cmd.Stdout=&out;cmd.Stderr=&er;err:=cmd.Run();s:=strings.TrimSpace(out.String());if e:=strings.TrimSpace(er.String());e!=""{if s!=""{s+="\n"};s+=e};if err!=nil{return s,fmt.Errorf("git %s failed: %s",strings.Join(args," "),s)};return s,nil}
func pathPart(v string)string{return strings.Trim(strings.TrimSpace(v),"/")}
func urlQuery(v string)string{return strings.ReplaceAll(strings.TrimSpace(v)," ","%20")}


func (c *Client) PullRequestChecks(ctx context.Context, full string, number int) (PullRequestChecksSummary, error) {
	if number <= 0 {
		return PullRequestChecksSummary{}, errors.New("pull request number must be positive")
	}
	pr, err := c.PullRequest(ctx, full, number)
	if err != nil {
		return PullRequestChecksSummary{}, err
	}
	if strings.TrimSpace(pr.Head.SHA) == "" {
		return PullRequestChecksSummary{}, errors.New("pull request has no head commit SHA")
	}
	base := "/repos/" + pathPart(full) + "/commits/" + pathPart(pr.Head.SHA)
	var checks struct { CheckRuns []CheckRun `json:"check_runs"` }
	if err := c.do(ctx, http.MethodGet, base+"/check-runs?per_page=100", nil, &checks); err != nil {
		return PullRequestChecksSummary{}, fmt.Errorf("load pull request checks: %w", err)
	}
	var status struct {
		State string `json:"state"`
		Statuses []CommitStatus `json:"statuses"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/status", nil, &status); err != nil {
		return PullRequestChecksSummary{}, fmt.Errorf("load commit statuses: %w", err)
	}
	summary := PullRequestChecksSummary{PullRequest: pr, HeadSHA: pr.Head.SHA, Checks: checks.CheckRuns, Statuses: status.Statuses, State: "neutral"}
	pending, failed, passed := strings.EqualFold(status.State, "pending"), false, strings.EqualFold(status.State, "success")
	for _, item := range checks.CheckRuns {
		switch strings.ToLower(item.Status) {
		case "queued", "in_progress", "waiting", "requested", "pending":
			pending = true
		case "completed":
			switch strings.ToLower(item.Conclusion) {
			case "failure", "timed_out", "cancelled", "action_required", "startup_failure", "stale":
				failed = true
			case "success", "neutral", "skipped":
				passed = true
			}
		}
	}
	for _, item := range status.Statuses {
		switch strings.ToLower(item.State) {
		case "pending", "expected":
			pending = true
		case "failure", "error":
			failed = true
		case "success":
			passed = true
		}
	}
	switch {
	case failed:
		summary.State = "failure"
	case pending:
		summary.State = "pending"
	case passed:
		summary.State = "success"
	}
	return summary, nil
}

type UpdateIssueRequest struct {
	Title string `json:"title,omitempty"`
	Body string `json:"body,omitempty"`
	State string `json:"state,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

func (c *Client) UpdateIssue(ctx context.Context, full string, number int, update UpdateIssueRequest) (Issue, error) {
	if number <= 0 { return Issue{}, errors.New("issue number must be positive") }
	if strings.TrimSpace(update.Title)=="" && update.Body=="" && update.State=="" && update.Labels==nil && update.Assignees==nil {
		return Issue{}, errors.New("at least one issue field must be updated")
	}
	if update.State!="" && update.State!="open" && update.State!="closed" { return Issue{}, errors.New("issue state must be open or closed") }
	var out Issue
	err:=c.do(ctx,http.MethodPatch,fmt.Sprintf("/repos/%s/issues/%d",pathPart(full),number),update,&out)
	return out,err
}

func (c *Client) MergePullRequest(ctx context.Context, full string, number int, method, expectedHeadSHA string) (map[string]any,error) {
	if number<=0{return nil,errors.New("pull request number must be positive")}
	switch method {case "merge","squash","rebase":default:return nil,errors.New("merge method must be merge, squash, or rebase")}
	body:=map[string]string{"merge_method":method}
	if strings.TrimSpace(expectedHeadSHA)!="" {body["sha"]=strings.TrimSpace(expectedHeadSHA)}
	var out map[string]any
	err:=c.do(ctx,http.MethodPut,fmt.Sprintf("/repos/%s/pulls/%d/merge",pathPart(full),number),body,&out)
	return out,err
}

func (c *Client) BranchesPage(ctx context.Context, full string, page, perPage int) ([]Branch,error) {
	if page<1 { page=1 }; if perPage<1||perPage>100 { perPage=100 }
	var raw []struct { Name string `json:"name"`; Commit struct { SHA string `json:"sha"` } `json:"commit"` }
	err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/repos/%s/branches?per_page=%d&page=%d",pathPart(full),perPage,page),nil,&raw)
	if err!=nil{return nil,err};out:=make([]Branch,0,len(raw));for _,v:=range raw{out=append(out,Branch{Name:v.Name,SHA:v.Commit.SHA})};return out,nil
}
func (c *Client) WorkflowRunsPage(ctx context.Context, full string, page, perPage int) ([]WorkflowRun,error) {
	if page<1{page=1};if perPage<1||perPage>100{perPage=30}
	var v struct{ WorkflowRuns []WorkflowRun `json:"workflow_runs"` }
	err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/repos/%s/actions/runs?per_page=%d&page=%d",pathPart(full),perPage,page),nil,&v)
	return v.WorkflowRuns,err
}
func (c *Client) WorkflowJobsPage(ctx context.Context, full string, id int64, page, perPage int) (any,error) {
	if page<1{page=1};if perPage<1||perPage>100{perPage=30}
	var v any
	err:=c.do(ctx,http.MethodGet,fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=%d&page=%d",pathPart(full),id,perPage,page),nil,&v)
	return v,err
}
func (c *Client) CreateIssueDetailed(ctx context.Context, full,title,body string,labels,assignees []string)(Issue,error) {
	payload:=map[string]any{"title":title,"body":body}
	if labels!=nil{payload["labels"]=labels};if assignees!=nil{payload["assignees"]=assignees}
	var out Issue;err:=c.do(ctx,http.MethodPost,"/repos/"+pathPart(full)+"/issues",payload,&out);return out,err
}
