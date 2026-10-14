package api

import (
 "context"
 "errors"
 "fmt"
 "io"
 "encoding/json"
 "net/http"
 "net/url"
 "os"
 "strconv"
 "strings"
 "time"

 "github.com/Enoch7768/fuzecli/internal/config"
 "github.com/Enoch7768/fuzecli/internal/github"
)

func(s *Server) githubRoutes(w http.ResponseWriter,r *http.Request){
 path:=strings.TrimPrefix(r.URL.Path,"/v1/github")
 if path==""||path=="/"{s.githubStatus(w,r);return}
 switch {
 case path=="/status":s.githubStatus(w,r)
 case path=="/auth":s.githubAuth(w,r)
 case path=="/auth/device/start":s.githubDeviceStart(w,r)
 case path=="/auth/device/poll":s.githubDevicePoll(w,r)
 case path=="/repos":s.githubRepos(w,r)
 case path=="/repo":s.githubRepo(w,r)
 case path=="/sync":s.githubSync(w,r)
 case path=="/branches":s.githubBranches(w,r)
 case path=="/prs":s.githubPRs(w,r)
 case path=="/comments":s.githubComments(w,r)
 case path=="/review":s.githubReview(w,r)
 case path=="/actions":s.githubActions(w,r)
 case path=="/issues":s.githubIssues(w,r)
 default:writeError(w,http.StatusNotFound,"GitHub endpoint not found")
 }
}
func(s *Server) githubClient()( *github.Client,error){token,err:=config.GitHubToken();if err!=nil{return nil,err};client:=github.New(token);configured,err:=config.GitHubTokenConfigured();if err!=nil{return nil,err};client.DisableCLI=configured&&strings.TrimSpace(token)=="";return client,nil}
func(s *Server) githubStatus(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodGet{writeError(w,405,"method not allowed");return}
 c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return}
 out:=map[string]any{"authenticated":c.Authenticated(),"remote":""}
 if remote,e:=github.LocalRemote(s.service.Root());e==nil{out["remote"]=remote}
 if !c.Authenticated(){writeJSON(w,200,out);return}
 ctx,cancel:=context.WithTimeout(r.Context(),10*time.Second);defer cancel()
 user,e:=c.Profile(ctx);if e!=nil{writeError(w,502,e.Error());return}
 out["user"]=user.Login;out["profile_url"]=user.HTMLURL
 writeJSON(w,200,out)
}

const githubDeviceBaseURL = "https://github.com/login/device"
const githubOAuthDeviceURL = "https://github.com/login/device/code"
const githubOAuthTokenURL = "https://github.com/login/oauth/access_token"

func githubOAuthClientID() string {
	return strings.TrimSpace(os.Getenv("FUZECLI_GITHUB_CLIENT_ID"))
}

func (s *Server) githubDeviceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	clientID := githubOAuthClientID()
	if clientID == "" {
		writeError(w, http.StatusServiceUnavailable, "GitHub OAuth is not configured. Set FUZECLI_GITHUB_CLIENT_ID to the Client ID of a GitHub OAuth App with Device Flow enabled, then restart FuzeCLI.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	form := strings.NewReader(url.Values{"client_id": {clientID}, "scope": {"repo workflow read:user"}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubOAuthDeviceURL, form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not prepare GitHub authorization")
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not contact GitHub authorization service")
		return
	}
	defer resp.Body.Close()
	var result struct {
		DeviceCode string `json:"device_code"`
		UserCode string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn int `json:"expires_in"`
		Interval int `json:"interval"`
		Error string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		writeError(w, http.StatusBadGateway, "GitHub returned an invalid authorization response")
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.DeviceCode == "" || result.UserCode == "" {
		message := result.ErrorDescription
		if message == "" { message = result.Error }
		if message == "" { message = "GitHub could not start device authorization" }
		writeError(w, http.StatusBadGateway, message)
		return
	}
	if result.Interval < 1 { result.Interval = 5 }
	if result.ExpiresIn < 1 { result.ExpiresIn = 900 }
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": result.DeviceCode,
		"user_code": result.UserCode,
		"verification_uri": result.VerificationURI,
		"expires_in": result.ExpiresIn,
		"interval": result.Interval,
	})
}

func (s *Server) githubDevicePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var body struct { DeviceCode string `json:"device_code"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.DeviceCode) == "" {
		writeError(w, http.StatusBadRequest, "device authorization code is required")
		return
	}
	clientID := githubOAuthClientID()
	if clientID == "" {
		writeError(w, http.StatusServiceUnavailable, "GitHub OAuth is not configured")
		return
	}
	form := strings.NewReader(url.Values{"client_id": {clientID}, "device_code": {body.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}.Encode())
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubOAuthTokenURL, form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not prepare token exchange")
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not contact GitHub token service")
		return
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType string `json:"token_type"`
		Scope string `json:"scope"`
		Error string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		writeError(w, http.StatusBadGateway, "GitHub returned an invalid token response")
		return
	}
	if result.AccessToken == "" {
		switch result.Error {
		case "authorization_pending":
			writeJSON(w, http.StatusAccepted, map[string]any{"pending": true, "interval": 5})
		case "slow_down":
			writeJSON(w, http.StatusAccepted, map[string]any{"pending": true, "slow_down": true, "interval": 10})
		case "expired_token":
			writeJSON(w, http.StatusGone, map[string]any{"message": "GitHub device code expired. Start authorization again."})
		case "access_denied":
			writeJSON(w, http.StatusForbidden, map[string]any{"message": "GitHub authorization was denied."})
		default:
			message := result.ErrorDescription
			if message == "" { message = result.Error }
			if message == "" { message = "GitHub authorization has not completed." }
			writeJSON(w, http.StatusAccepted, map[string]any{"pending": true, "message": message})
		}
		return
	}
	client := github.New(result.AccessToken)
	profileCtx, profileCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer profileCancel()
	user, err := client.Profile(profileCtx)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("GitHub issued a token, but profile verification failed: %v", err))
		return
	}
	if err := config.SetGitHubToken(result.AccessToken); err != nil {
		writeError(w, http.StatusInternalServerError, "could not securely save GitHub authorization")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "login": user.Login, "profile_url": user.HTMLURL})
}

func(s *Server) githubAuth(w http.ResponseWriter,r *http.Request){
	if r.Method == http.MethodDelete {
		if err := config.ClearGitHubToken(); err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeError(w, http.StatusGone, "Personal access token authentication has been removed. Use /v1/github/auth/device/start to connect through GitHub OAuth.")
}
func(s *Server) githubRepos(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodGet{writeError(w,405,"method not allowed");return};c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel();v,err:=c.Repositories(ctx,1,100);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"repositories":v})
}
func(s *Server) githubRepo(w http.ResponseWriter,r *http.Request){
 c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel()
 if r.Method==http.MethodPost{r.Body=http.MaxBytesReader(w,r.Body,32<<10);var req github.CreateRepositoryRequest;if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};v,err:=c.CreateRepository(ctx,req);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,201,map[string]any{"repository":v});return}
 if r.Method!=http.MethodGet{writeError(w,405,"method not allowed");return};full:=strings.TrimSpace(r.URL.Query().Get("name"));if full==""{writeError(w,400,"repository name is required");return};v,err:=c.Repository(ctx,full);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"repository":v})
}
func(s *Server) githubSync(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodPost{writeError(w,405,"method not allowed");return};c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};r.Body=http.MaxBytesReader(w,r.Body,16<<10);var req struct{Operation string `json:"operation"`; Remote string `json:"remote"`; Branch string `json:"branch"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};if req.Operation!="pull"&&req.Operation!="push"{writeError(w,400,"operation must be pull or push");return};ctx,cancel:=context.WithTimeout(r.Context(),5*time.Minute);defer cancel();var out string;if req.Operation=="pull"{out,err=github.Pull(ctx,s.service.Root(),req.Remote,req.Branch,c.Token)}else{out,err=github.Push(ctx,s.service.Root(),req.Remote,req.Branch,c.Token)};if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"ok":true,"output":out})
}
func(s *Server) githubBranches(w http.ResponseWriter,r *http.Request){
 c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};full:=strings.TrimSpace(r.URL.Query().Get("repo"));if full==""{full,err=githubRepoFromRemote(s.service.Root());if err!=nil{writeError(w,400,err.Error());return}}
 ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel()
 if r.Method==http.MethodPost{r.Body=http.MaxBytesReader(w,r.Body,16<<10);var req struct{Name string `json:"name"`; From string `json:"from"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};if err:=c.CreateBranch(ctx,full,req.Name,req.From);err!=nil{writeError(w,502,err.Error());return};writeJSON(w,201,map[string]any{"ok":true,"name":req.Name});return}
 if r.Method!=http.MethodGet{writeError(w,405,"method not allowed");return};v,err:=c.Branches(ctx,full);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"branches":v,"repo":full})
}
func(s *Server) githubPRs(w http.ResponseWriter,r *http.Request){
 c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};full:=strings.TrimSpace(r.URL.Query().Get("repo"));if full==""{full,err=githubRepoFromRemote(s.service.Root());if err!=nil{writeError(w,400,err.Error());return}};ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel()
 if r.Method==http.MethodPost{r.Body=http.MaxBytesReader(w,r.Body,64<<10);var req github.CreatePullRequestRequest;if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};v,err:=c.CreatePullRequest(ctx,full,req);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,201,map[string]any{"pull_request":v});return}
 state:=r.URL.Query().Get("state");v,err:=c.PullRequests(ctx,full,state);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"pull_requests":v,"repo":full})
}
func(s *Server) githubComments(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodGet&&r.Method!=http.MethodPost{writeError(w,405,"method not allowed");return};c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};full:=strings.TrimSpace(r.URL.Query().Get("repo"));n,_:=strconv.Atoi(r.URL.Query().Get("number"));if full==""{full,err=githubRepoFromRemote(s.service.Root())};if err!=nil||n<=0{writeError(w,400,"repo and pull request number are required");return};ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel()
 if r.Method==http.MethodPost{r.Body=http.MaxBytesReader(w,r.Body,32<<10);var req struct{Body string `json:"body"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};v,err:=c.AddComment(ctx,full,n,req.Body);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,201,map[string]any{"comment":v});return}
 v,err:=c.Comments(ctx,full,n);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"comments":v})
}
func(s *Server) githubReview(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodPost{writeError(w,405,"method not allowed");return};c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};r.Body=http.MaxBytesReader(w,r.Body,32<<10);var req struct{Repo string `json:"repo"`;Number int `json:"number"`;Body string `json:"body"`;Event string `json:"event"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};if req.Repo==""{req.Repo,err=githubRepoFromRemote(s.service.Root())};if err!=nil||req.Number<=0{writeError(w,400,"repo and pull request number are required");return};ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel();if err:=c.Review(ctx,req.Repo,req.Number,github.ReviewRequest{Body:req.Body,Event:req.Event});err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"ok":true})
}
func(s *Server) githubActions(w http.ResponseWriter,r *http.Request){
 c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};full:=r.URL.Query().Get("repo");if full==""{full,err=githubRepoFromRemote(s.service.Root())};if err!=nil{writeError(w,400,err.Error());return};ctx,cancel:=context.WithTimeout(r.Context(),30*time.Second);defer cancel();if r.Method==http.MethodPost{var req struct{RunID int64 `json:"run_id"`;Operation string `json:"operation"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};if req.Operation=="rerun"{if err:=c.RerunWorkflow(ctx,full,req.RunID);err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"ok":true});return};if req.Operation=="jobs"{v,err:=c.WorkflowJobs(ctx,full,req.RunID);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,v);return};writeError(w,400,"unknown Actions operation");return};v,err:=c.WorkflowRuns(ctx,full);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"runs":v})
}
func(s *Server) githubIssues(w http.ResponseWriter,r *http.Request){
 c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};full:=r.URL.Query().Get("repo");if full==""{full,err=githubRepoFromRemote(s.service.Root())};if err!=nil{writeError(w,400,err.Error());return};ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second);defer cancel();if r.Method==http.MethodPost{var req struct{Title string `json:"title"`;Body string `json:"body"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};v,err:=c.CreateIssue(ctx,full,req.Title,req.Body);if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,201,map[string]any{"issue":v});return};v,err:=c.Issues(ctx,full,r.URL.Query().Get("state"));if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"issues":v})
}
func githubRepoFromRemote(root string)(string,error){remote,err:=github.LocalRemote(root);if err!=nil{return "",err};v:=strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(remote),".git"),"/");if i:=strings.Index(v,"github.com/");i>=0{v=v[i+len("github.com/"):]}else if strings.HasPrefix(v,"git@github.com:"){v=strings.TrimPrefix(v,"git@github.com:")}else{return "",errors.New("origin is not a GitHub repository")};p:=strings.Split(strings.Trim(v,"/"),"/");if len(p)!=2{return "",errors.New("could not determine GitHub repository from origin")};return p[0]+"/"+p[1],nil}
