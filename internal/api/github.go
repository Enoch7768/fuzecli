package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/github"
)

func (s *Server) githubRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/github")
	if path == "" || path == "/" {
		s.githubStatus(w, r)
		return
	}
	switch path {
	case "/status":
		s.githubStatus(w, r)
	case "/profile":
		s.githubProfile(w, r)
	case "/auth":
		s.githubAuth(w, r)
	case "/auth/login":
		s.githubAuthLogin(w, r)
	case "/auth/login/status":
		s.githubAuthLoginStatus(w, r)
	case "/repos":
		s.githubRepos(w, r)
	case "/repo":
		s.githubRepo(w, r)
	case "/sync":
		s.githubSync(w, r)
	case "/branches":
		s.githubBranches(w, r)
	case "/prs":
		s.githubPRs(w, r)
	case "/pr/checks":
		s.githubPRChecks(w, r)
	case "/comments":
		s.githubComments(w, r)
	case "/review":
		s.githubReview(w, r)
	case "/actions":
		s.githubActions(w, r)
	case "/issues":
		s.githubIssues(w, r)
	default:
		writeError(w, http.StatusNotFound, "GitHub endpoint not found")
	}
}

func (s *Server) githubClient() (*github.Client, error) {
	token, err := config.GitHubToken()
	if err != nil { return nil, err }
	configured, err := config.GitHubTokenConfigured()
	if err != nil { return nil, err }

	// An explicit Studio disconnect is respected. Otherwise, an existing gh
	// session takes precedence over legacy PAT configuration so the CLI and
	// Studio consistently use the same durable GitHub identity.
	if !(configured && strings.TrimSpace(token) == "") {
		if _, lookErr := exec.LookPath("gh"); lookErr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			statusErr := exec.CommandContext(ctx, "gh", "auth", "status", "--hostname", "github.com").Run()
			cancel()
			if statusErr == nil {
				if configured && strings.TrimSpace(token) != "" {
					if err := config.ClearGitHubToken(); err != nil { return nil, err }
				}
				token = ""
				configured = false
			}
		}
	}
	client := github.New(token)
	client.DisableCLI = configured && strings.TrimSpace(token) == ""
	return client, nil
}

func (s *Server) githubStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	c, err := s.githubClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read GitHub connection settings")
		return
	}
	configured, err := config.GitHubTokenConfigured()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read GitHub connection settings")
		return
	}
authenticated := c.Authenticated()
	out := map[string]any{"authenticated": authenticated, "remote": "", "auth_mode": "gh"}
	if configured && strings.TrimSpace(c.Token) == "" {
		out["auth_mode"] = "disconnected"
	} else if strings.TrimSpace(c.Token) != "" {
		out["auth_mode"] = "token"
	}
	if remote, e := github.LocalRemote(s.service.Root()); e == nil {
		out["remote"] = remote
	}
	s.githubAuthMu.Lock()
	out["login_running"] = s.githubAuthRunning
	out["login_message"] = s.githubAuthMessage
	out["login_error"] = s.githubAuthError
	s.githubAuthMu.Unlock()
	if !authenticated {
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	user, err := c.Profile(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "GitHub authentication exists but the profile could not be verified")
		return
	}
	out["user"] = user.Login
	out["profile_url"] = user.HTMLURL
	out["profile"] = user
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) githubProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	c, err := s.githubClient()
	if err != nil { writeError(w, http.StatusInternalServerError, "could not initialize GitHub client"); return }
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second); defer cancel()
	user, err := c.Profile(ctx)
	if err != nil { writeError(w, http.StatusBadGateway, err.Error()); return }
	writeJSON(w, http.StatusOK, map[string]any{"profile": user})
}

func (s *Server) githubAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "GitHub CLI sign-in is available only from the local machine")
		return
	}
	client, clientErr := s.githubClient()
	if clientErr == nil && client.Authenticated() {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		user, profileErr := client.Profile(ctx)
		if profileErr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"running": false, "authenticated": true, "user": user.Login, "message": "Already connected as " + user.Login + ". Your existing GitHub CLI session is being reused."})
			return
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub CLI is not installed. Install it from https://cli.github.com, reopen your terminal, and retry.")
		return
	}
	s.githubAuthMu.Lock()
	if s.githubAuthRunning {
		s.githubAuthMu.Unlock()
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "message": "GitHub sign-in is already running."})
		return
	}
	s.githubAuthRunning = true
	s.githubAuthMessage = "Waiting for GitHub browser sign-in. Complete the flow in the terminal running FuzeCLI."
	s.githubAuthError = ""
	s.githubAuthMu.Unlock()
	go s.runGitHubCLILogin()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"running": true,
		"message": "GitHub sign-in started. Complete the browser flow in the terminal running FuzeCLI, then return here.",
	})
}

func (s *Server) runGitHubCLILogin() {
	setState := func(message, failure string) {
		s.githubAuthMu.Lock()
		s.githubAuthRunning = false
		s.githubAuthMessage = message
		s.githubAuthError = failure
		s.githubAuthMu.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			setState("GitHub sign-in timed out. Start sign-in again.", "The GitHub CLI sign-in timed out.")
		} else {
			setState("GitHub sign-in failed. Check the terminal running FuzeCLI for details.", "GitHub CLI sign-in failed.")
		}
		return
	}
	setup := exec.CommandContext(ctx, "gh", "auth", "setup-git", "--hostname", "github.com")
	setup.Stdin = os.Stdin
	setup.Stdout = os.Stdout
	setup.Stderr = os.Stderr
	if err := setup.Run(); err != nil {
		setState("GitHub account signed in, but Git credential setup failed.", "Run 'gh auth setup-git --hostname github.com' in your terminal.")
		return
	}
	if err := config.ClearGitHubToken(); err != nil {
		setState("GitHub signed in, but FuzeCLI could not switch to GitHub CLI authentication.", "Could not update FuzeCLI GitHub settings.")
		return
	}
	client := github.New("")
	profileCtx, profileCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer profileCancel()
	user, err := client.Profile(profileCtx)
	if err != nil {
		setState("GitHub sign-in finished, but FuzeCLI could not verify the account.", "Run 'gh auth status --hostname github.com' and retry.")
		return
	}
	setState("Connected as "+user.Login+".", "")
}

func (s *Server) githubAuthLoginStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.githubAuthMu.Lock()
	out := map[string]any{
		"running": s.githubAuthRunning,
		"message": s.githubAuthMessage,
		"error": s.githubAuthError,
	}
	s.githubAuthMu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) githubAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed; use the GitHub CLI sign-in button")
		return
	}
	s.githubAuthMu.Lock()
	defer s.githubAuthMu.Unlock()
	if s.githubAuthRunning {
		writeError(w, http.StatusConflict, "wait for GitHub sign-in to finish before disconnecting")
		return
	}
	if err := config.DisconnectGitHub(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not disconnect GitHub in FuzeCLI")
		return
	}
	s.githubAuthMessage = "GitHub disconnected from Studio."
	s.githubAuthError = ""
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
 if r.Method!=http.MethodPost{writeError(w,405,"method not allowed");return};c,err:=s.githubClient();if err!=nil{writeError(w,500,err.Error());return};if c.DisableCLI&&strings.TrimSpace(c.Token)==""{writeError(w,http.StatusUnauthorized,"GitHub is disconnected in Studio");return};r.Body=http.MaxBytesReader(w,r.Body,16<<10);var req struct{Operation string `json:"operation"`; Remote string `json:"remote"`; Branch string `json:"branch"`};if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{writeError(w,400,"invalid request");return};if req.Operation!="pull"&&req.Operation!="push"{writeError(w,400,"operation must be pull or push");return};ctx,cancel:=context.WithTimeout(r.Context(),5*time.Minute);defer cancel();var out string;if req.Operation=="pull"{out,err=github.Pull(ctx,s.service.Root(),req.Remote,req.Branch,c.Token)}else{out,err=github.Push(ctx,s.service.Root(),req.Remote,req.Branch,c.Token)};if err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]any{"ok":true,"output":out})
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

func (s *Server) githubPRChecks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	c, err := s.githubClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not initialize GitHub client")
		return
	}
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" {
		repo, err = githubRepoFromRemote(s.service.Root())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	number, err := strconv.Atoi(r.URL.Query().Get("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "a positive pull request number is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	summary, err := c.PullRequestChecks(ctx, repo, number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
