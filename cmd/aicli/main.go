package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"strconv"

	"github.com/Enoch7768/fuzecli/internal/api"
	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/github"
	"github.com/Enoch7768/fuzecli/internal/mcpserver"
	"github.com/Enoch7768/fuzecli/internal/profile"
)

var version = "Revision 2.4"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "\nFuzeCLI could not complete that request.")
		fmt.Fprintln(os.Stderr, "Reason:", err)
		fmt.Fprintln(os.Stderr, "\nUseful next steps:")
		fmt.Fprintln(os.Stderr, "  aicli debug     Show a safe developer diagnostic report")
		fmt.Fprintln(os.Stderr, "  aicli doctor    Diagnose provider/workspace problems")
		fmt.Fprintln(os.Stderr, "  aicli setup     Configure your provider and model")
		fmt.Fprintln(os.Stderr, "  aicli --help   Show all commands")
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return entryScreen()
	}
	switch args[0] {
	case "version", "--version":
		fmt.Println("FuzeCLI", version)
		return nil
	case "init":
		return initCommand()
	case "setup":
		return setupCommand()
	case "config":
		return configCommand(args[1:])
	case "models":
		return modelsCommand(args[1:])
	case "profile":
		return profileCommand(args[1:])
	case "history":
		return historyCommand()
	case "ask":
		return askCommand(args[1:])
	case "chat":
		return chatCommand(args[1:])
	case "api":
		return apiCommand(args[1:])
	case "app":
		return appCommand(args[1:])
	case "apikey":
		return apiKeyCommand(args[1:])
	case "github":
		return githubCommand(args[1:])
	case "mcp":
		return mcpCommand()
	case "doctor":
		return doctorCommand()
	case "debug":
		return debugCommand()
	case "snapshot":
		return snapshotCommand()
	case "restore":
		return restoreCommand(args[1:])
	case "status":
		return gitStatusCommand()
	case "diff":
		return gitDiffCommand()
	case "help", "--help", "-h":
		return usage()
	default:
		return fmt.Errorf("unknown command %q; run 'aicli --help' for available commands", args[0])
	}
}

func entryScreen() error {
	initialized := workspaceInitialized(".")
	fmt.Println()
	fmt.Println("\x1b[1;38;5;117m  F U Z E C L I\x1b[0m")
	fmt.Println("\x1b[38;5;244m  AI coding workspace for developers\x1b[0m")
	fmt.Println("\x1b[38;5;239m  ────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Println()
	fmt.Println("  Build, inspect, debug and change your project from one terminal.")
	fmt.Println("  You stay in control: generated commands are not executed automatically,")
	fmt.Println("  workspace paths are validated, and changes can be reviewed or recovered.")
	fmt.Println()

	if !initialized {
		fmt.Println("\x1b[1;38;5;111m  FIRST RUN\x1b[0m")
		fmt.Println("  Start here — initialize this project and configure FuzeCLI:")
		fmt.Println()
		fmt.Println("    \x1b[1maicli init\x1b[0m")
		fmt.Println()
		fmt.Println("  Then use:")
		fmt.Println("    aicli chat       Interactive AI coding session")
		fmt.Println("    aicli doctor     Verify your environment")
		fmt.Println("    aicli debug      Safe troubleshooting report")
	} else {
		fmt.Println("\x1b[1;38;5;111m  READY\x1b[0m  This workspace is already initialized.")
		fmt.Println()
		fmt.Println("    aicli chat       Start an interactive coding session")
		fmt.Println("    aicli ask \"...\"   Run a one-shot request")
		fmt.Println("    aicli doctor     Check your environment")
		fmt.Println("    aicli debug      Safe troubleshooting report")
		fmt.Println()
		fmt.Println("  Need to reconfigure? Run 'aicli setup'.")
	}
	fmt.Println()
	fmt.Println("  Tip: run 'aicli --help' for the complete command reference.")
	return nil
}

func initCommand() error {
	root, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	already := workspaceInitialized(root)

	fmt.Println()
	fmt.Println("\x1b[1;38;5;117mFUZECLI INIT\x1b[0m")
	fmt.Printf("  Workspace: %s\n", root)

	if already {
		fmt.Println("  \x1b[38;5;111m✓\x1b[0m Workspace already initialized.")
		fmt.Println("  Nothing was reset or deleted.")
		fmt.Println()
		fmt.Println("  If you want to change provider/model settings, run:")
		fmt.Println("    aicli setup")
		fmt.Println("  Otherwise, you're ready:")
		fmt.Println("    aicli chat")
		return nil
	}

	if err := app.InitWorkspace(root); err != nil {
		return fmt.Errorf("initialize workspace: %w", err)
	}
	fmt.Println("  \x1b[38;5;111m✓\x1b[0m Workspace initialized.")
	fmt.Println("  Next, configure your AI provider.")
	fmt.Println()
	return setupCommand()
}

func setupCommand() error {
	reader := bufio.NewReader(os.Stdin)
	current, err := config.Load()
	if err != nil {
		return err
	}

	fmt.Println("\n\x1b[1;38;5;117mFuzeCLI SETUP\x1b[0m")
	fmt.Println("\x1b[38;5;244mConfigure the provider FuzeCLI should use by default.\x1b[0m")
	fmt.Println("\x1b[38;5;244mYour API key is stored locally in the FuzeCLI config directory.\x1b[0m")
	fmt.Println()

	fmt.Printf("Provider [%s] (gemini/openai/groq/anthropic/llamacpp): ", current.DefaultProvider)
	providerName, err := readSetupLine(reader)
	if err != nil {
		return err
	}
	if providerName == "" {
		providerName = current.DefaultProvider
	}
	if _, ok := current.Providers[providerName]; !ok {
		return fmt.Errorf("unknown provider %q; choose gemini, openai, groq, anthropic, or llamacpp", providerName)
	}

	providerConfig := current.Providers[providerName]
	fmt.Printf("Model [%s]: ", providerConfig.DefaultModel)
	model, err := readSetupLine(reader)
	if err != nil {
		return err
	}
	if model != "" {
		providerConfig.DefaultModel = model
	}

	if providerName != "llamacpp" {
		fmt.Print("API key (leave blank to keep the current key): ")
		key, err := readSetupLine(reader)
		if err != nil {
			return err
		}
		if key != "" {
			providerConfig.APIKey = key
		}
	}

	current.Providers[providerName] = providerConfig
	current.DefaultProvider = providerName
	if err := config.Save(current); err != nil {
		return err
	}
	if !workspaceInitialized(".") {
		if err := app.InitWorkspace("."); err != nil {
			return err
		}
	}

	fmt.Println("\n\x1b[38;5;111m✓ Setup saved.\x1b[0m")
	fmt.Printf("  Provider: %s\n  Model:    %s\n", providerName, providerConfig.DefaultModel)
	fmt.Println("\nNext steps:")
	fmt.Println("  aicli doctor   Verify the provider and workspace")
	fmt.Println("  aicli chat     Start coding with FuzeCLI")
	return nil
}

func readSetupLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func askCommand(args []string) error {
	var providerName, model string
	var yes, safe bool
	var promptParts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --provider")
			}
			providerName = args[i+1]
			i++
		case "--model":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --model")
			}
			model = args[i+1]
			i++
		case "--yes":
			yes = true
		case "--safe":
			safe = true
		case "--help", "-h":
			fmt.Println("Usage: aicli ask \"prompt\" [--provider name|auto] [--model name] [--yes] [--safe]")
			return nil
		default:
			promptParts = append(promptParts, args[i])
		}
	}
	prompt, err := app.ReadPrompt(promptParts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("ask requires a prompt or stdin input")
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	a.SetSafeMode(safe)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.RunProfileExtraction(ctx)
	}()
	_, err = a.Ask(context.Background(), prompt, providerName, model, yes)
	return err
}

func chatCommand(args []string) error {
	var yes, safe bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			yes = true
		case "--safe":
			safe = true
		case "--help", "-h":
			fmt.Println("Usage: aicli chat [--yes] [--safe]")
			fmt.Println("Inside chat: /file, /provider, /model, /status, /clear, /help, /exit")
			return nil
		default:
			return fmt.Errorf("unknown chat option %q", args[i])
		}
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	a.SetSafeMode(safe)
	return a.TerminalChat(context.Background(), yes)
}

func debugCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return a.Debug()
}

func apiCommand(args []string) error {
	addr := "127.0.0.1:8787"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --addr")
			}
			addr = args[i+1]
			i++
		case "--help", "-h":
			fmt.Println("Usage: aicli api [--addr 127.0.0.1:8787]")
			return nil
		default:
			return fmt.Errorf("unknown api option %q", args[i])
		}
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	server := api.NewServer(api.NewService(a), api.TokenFromEnvironment())
	fmt.Printf("FuzeCLI API listening on http://%s\n", addr)
	return server.ListenAndServe(addr)
}

func appCommand(args []string) error {
	addr := "127.0.0.1:8787"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --addr")
			}
			addr = args[i+1]
			i++
		case "--help", "-h":
			fmt.Println("Usage: aicli app [--addr 127.0.0.1:8787]")
			return nil
		default:
			return fmt.Errorf("unknown app option %q", args[i])
		}
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	server := api.NewServer(api.NewService(a), api.TokenFromEnvironment())
	fmt.Printf("FuzeCLI web app: http://%s\n", addr)
	return server.ListenAndServe(addr)
}

func apiKeyCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: aicli apikey set <provider> <key>")
		fmt.Println("       aicli apikey clear <provider>")
		fmt.Println("       aicli apikey status")
		return nil
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: aicli apikey set <provider> <key>")
		}
		if err := config.SetAPIKey(args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("API key saved for %s.\n", args[1])
		return nil
	case "clear":
		if len(args) != 2 {
			return fmt.Errorf("usage: aicli apikey clear <provider>")
		}
		if err := config.ClearAPIKey(args[1]); err != nil {
			return err
		}
		fmt.Printf("API key cleared for %s.\n", args[1])
		return nil
	case "status":
		status, err := config.APIKeyStatus()
		if err != nil {
			return err
		}
		for _, name := range []string{"gemini", "openai", "groq", "anthropic"} {
			if status[name] {
				fmt.Printf("%s: configured\n", name)
			} else {
				fmt.Printf("%s: not configured\n", name)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown apikey command %q", args[0])
	}
}

func githubCommand(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("Usage: aicli github auth set|status|clear")
		fmt.Println("       aicli github repo list|create|info|clone")
		fmt.Println("       aicli github pull [remote] [branch]")
		fmt.Println("       aicli github push [remote] [branch]")
		fmt.Println("       aicli github branch list|create <name> [from]")
		fmt.Println("       aicli github pr list|create|view|comments|comment|review")
		fmt.Println("       aicli github issue list|create")
		fmt.Println("       aicli github actions list|rerun|jobs")
		return nil
	}
	token, err := config.GitHubToken()
	if err != nil { return err }
	client := github.New(token)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	switch args[0] {
	case "auth":
		if len(args) < 2 { return fmt.Errorf("usage: aicli github auth set|status|clear") }
		switch args[1] {
		case "set":
			if len(args) != 3 || strings.TrimSpace(args[2]) == "" { return fmt.Errorf("usage: aicli github auth set <token>") }
			if err := config.SetGitHubToken(args[2]); err != nil { return err }
			fmt.Println("GitHub token saved locally.")
			return nil
		case "clear":
			if err := config.ClearGitHubToken(); err != nil { return err }
			fmt.Println("GitHub token cleared.")
			return nil
		case "status":
			if !client.Authenticated() { fmt.Println("GitHub: not authenticated"); return nil }
			user, err := client.Profile(ctx); if err != nil { return err }
			fmt.Printf("GitHub: authenticated as %s\n", user.Login)
			return nil
		default:
			return fmt.Errorf("unknown github auth command %q", args[1])
		}
	case "repo":
		if len(args) < 2 { return fmt.Errorf("usage: aicli github repo list|create|info|clone") }
		switch args[1] {
		case "list":
			repos, err := client.Repositories(ctx, 1, 100); if err != nil { return err }
			for _, r := range repos { fmt.Printf("%s\t%s\t%s\n", r.FullName, r.DefaultBranch, map[bool]string{true:"private",false:"public"}[r.Private]) }
			return nil
		case "create":
			if len(args) < 3 { return fmt.Errorf("usage: aicli github repo create <name> [--private] [--description text] [--init]") }
			req := github.CreateRepositoryRequest{Name: args[2]}
			for i:=3;i<len(args);i++ { switch args[i] { case "--private": req.Private=true; case "--init": req.AutoInit=true; case "--description": if i+1>=len(args){return fmt.Errorf("missing description")}; req.Description=args[i+1]; i++; default:return fmt.Errorf("unknown repo option %q",args[i]) } }
			r, err := client.CreateRepository(ctx, req); if err != nil { return err }
			fmt.Printf("Created %s\n%s\n", r.FullName, r.HTMLURL)
			return nil
		case "info":
			if len(args)!=3{return fmt.Errorf("usage: aicli github repo info <owner/name>")}
			r,err:=client.Repository(ctx,args[2]);if err!=nil{return err}
			fmt.Printf("%s\nDefault branch: %s\nVisibility: %s\nClone: %s\n",r.FullName,r.DefaultBranch,map[bool]string{true:"private",false:"public"}[r.Private],r.CloneURL)
			return nil
		case "clone":
			if len(args)<3||len(args)>4{return fmt.Errorf("usage: aicli github repo clone <owner/name> [destination]")}
			r,err:=client.Repository(ctx,args[2]);if err!=nil{return err}; dest:=r.Name;if len(args)==4{dest=args[3]}
			out,err:=github.Clone(ctx,r.CloneURL,dest,token);if err!=nil{return err};fmt.Println(out);return nil
		}
	case "pull","push":
		if len(args)>3{return fmt.Errorf("usage: aicli github %s [remote] [branch]",args[0])}
		remote,branch:="","";if len(args)>1{remote=args[1]};if len(args)>2{branch=args[2]}
		var out string
		if args[0]=="pull"{out,err=github.Pull(ctx,".",remote,branch,token)}else{out,err=github.Push(ctx,".",remote,branch,token)}
		if err!=nil{return err};fmt.Println(out);return nil
	case "branch":
		if len(args)<2{return fmt.Errorf("usage: aicli github branch list|create <name> [from]")}
		if args[1]=="list"{full,err:=githubRepoFromOrigin();if err!=nil{return err};bs,err:=client.Branches(ctx,full);if err!=nil{return err};for _,b:=range bs{fmt.Printf("%s\t%s\n",b.Name,b.SHA)};return nil}
		if args[1]=="create"{if len(args)<3||len(args)>4{return fmt.Errorf("usage: aicli github branch create <name> [from]")};full,err:=githubRepoFromOrigin();if err!=nil{return err};from:="";if len(args)==4{from=args[3]};if err:=client.CreateBranch(ctx,full,args[2],from);err!=nil{return err};fmt.Println("Branch created:",args[2]);return nil}
	case "pr":
		if len(args)<2{return fmt.Errorf("usage: aicli github pr list|create|view|comments|comment|review")}
		full,numberErr:=githubRepoFromOrigin();if numberErr!=nil{return numberErr}
		switch args[1]{
		case "list":state:="open";if len(args)==3{state=args[2]};prs,err:=client.PullRequests(ctx,full,state);if err!=nil{return err};for _,p:=range prs{fmt.Printf("#%d %s [%s] %s\n",p.Number,p.Title,p.State,p.HTMLURL)};return nil
		case "create":if len(args)<5{return fmt.Errorf("usage: aicli github pr create <head> <base> <title> [body]")};body:="";if len(args)>5{body=strings.Join(args[5:]," ")};p,err:=client.CreatePullRequest(ctx,full,github.CreatePullRequestRequest{Head:args[2],Base:args[3],Title:args[4],Body:body});if err!=nil{return err};fmt.Printf("#%d %s\n%s\n",p.Number,p.Title,p.HTMLURL);return nil
		case "view","comments","comment","review":
			if len(args)<3{return fmt.Errorf("pull request number is required")};n,e:=strconv.Atoi(args[2]);if e!=nil{return fmt.Errorf("invalid pull request number")};if args[1]=="view"{p,err:=client.PullRequest(ctx,full,n);if err!=nil{return err};fmt.Printf("#%d %s\n%s\n%s -> %s\n",p.Number,p.Title,p.HTMLURL,p.Head.Ref,p.Base.Ref);return nil};if args[1]=="comments"{cs,err:=client.Comments(ctx,full,n);if err!=nil{return err};for _,c:=range cs{fmt.Printf("%s: %s\n",c.User.Login,c.Body)};return nil};if args[1]=="comment"{if len(args)<4{return fmt.Errorf("usage: aicli github pr comment <number> <body>")};_,err:=client.AddComment(ctx,full,n,strings.Join(args[3:]," "));return err};if len(args)<4{return fmt.Errorf("usage: aicli github pr review <number> <approve|comment|request_changes> [body]")};event:=strings.ToUpper(strings.ReplaceAll(args[3],"-","_"));body:="";if len(args)>4{body=strings.Join(args[4:]," ")};return client.Review(ctx,full,n,github.ReviewRequest{Event:event,Body:body})
		}
	case "issue":
		if len(args)<2{return fmt.Errorf("usage: aicli github issue list|create")}
		full,err:=githubRepoFromOrigin();if err!=nil{return err};if args[1]=="list"{issues,err:=client.Issues(ctx,full,"open");if err!=nil{return err};for _,i:=range issues{fmt.Printf("#%d %s %s\n",i.Number,i.Title,i.HTMLURL)};return nil};if args[1]=="create"{if len(args)<3{return fmt.Errorf("usage: aicli github issue create <title> [body]")};body:="";if len(args)>3{body=strings.Join(args[3:]," ")};i,err:=client.CreateIssue(ctx,full,args[2],body);if err!=nil{return err};fmt.Printf("#%d %s\n",i.Number,i.HTMLURL);return nil}
	case "actions":
		if len(args)<2{return fmt.Errorf("usage: aicli github actions list|rerun|jobs")}
		full,err:=githubRepoFromOrigin();if err!=nil{return err};if args[1]=="list"{runs,err:=client.WorkflowRuns(ctx,full);if err!=nil{return err};for _,r:=range runs{fmt.Printf("%d #%d %s [%s/%s] %s\n",r.ID,r.RunNumber,r.Name,r.Status,r.Conclusion,r.HTMLURL)};return nil};if len(args)<3{return fmt.Errorf("workflow run id is required")};id,e:=strconv.ParseInt(args[2],10,64);if e!=nil{return fmt.Errorf("invalid workflow run id")};if args[1]=="rerun"{return client.RerunWorkflow(ctx,full,id)};if args[1]=="jobs"{v,err:=client.WorkflowJobs(ctx,full,id);if err!=nil{return err};b,_:=json.MarshalIndent(v,"","  ");fmt.Println(string(b));return nil}
	}
	return fmt.Errorf("unknown github command %q",args[0])
}

func githubRepoFromOrigin()(string,error){
	remote,err:=github.LocalRemote(".");if err!=nil{return "",err}
	value:=strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(remote),".git"),"/")
	if strings.HasPrefix(value,"git@github.com:"){value=strings.TrimPrefix(value,"git@github.com:")}else if i:=strings.Index(value,"github.com/");i>=0{value=value[i+len("github.com/"):]}else{return "",fmt.Errorf("origin is not a GitHub repository")}
	parts:=strings.Split(strings.Trim(value,"/"),"/");if len(parts)!=2{return "",fmt.Errorf("could not determine GitHub repository from origin")}
	return parts[0]+"/"+parts[1],nil
}

func mcpCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return mcpserver.Run(context.Background(), a)
}


func doctorCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return a.Doctor()
}

func snapshotCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	paths, err := a.Store.Touched()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no touched files are available for a snapshot")
	}
	snapshot, err := a.Store.CreateSnapshot(paths)
	if err != nil {
		return err
	}
	fmt.Println("Snapshot created:", snapshot)
	return nil
}

func restoreCommand(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: aicli restore [snapshot.zip]")
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	snapshot := ""
	if len(args) == 1 {
		snapshot = args[0]
	} else {
		snapshot, err = a.Store.LatestSnapshot()
		if err != nil {
			return err
		}
	}
	paths, err := a.Store.RestoreSnapshot(snapshot)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %d file(s) from %s\n", len(paths), snapshot)
	return nil
}

func gitStatusCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return app.GitStatus(a.Store.Root)
}

func gitDiffCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return app.GitDiff(a.Store.Root)
}

func modelsCommand(args []string) error {
	providerName := "gemini"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --provider")
			}
			providerName = args[i+1]
			i++
		case "--help", "-h":
			fmt.Println("Usage: aicli models [--provider name]")
			return nil
		default:
			return fmt.Errorf("unknown models option %q", args[i])
		}
	}
	if providerName == "auto" {
		return fmt.Errorf("models requires a specific provider")
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	models, err := a.Registry.ListModels(context.Background(), providerName)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		return nil
	}
	fmt.Printf("%s models:\n", providerName)
	for _, model := range models {
		fmt.Println("  " + model)
	}
	return nil
}

func historyCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return app.ShowHistory(".")
}

func configCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("config requires set or show")
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: aicli config set <key> <value>")
		}
		return config.Set(args[1], args[2])
	case "show":
		c, err := config.Load()
		if err != nil {
			return err
		}
		names := []string{"openai", "gemini", "groq", "anthropic", "llamacpp"}
		fmt.Println("Default provider:", c.DefaultProvider)
		if token, err := config.GitHubToken(); err == nil { if token != "" { fmt.Println("GitHub token: <configured>") } else { fmt.Println("GitHub token: <not set>") } }
		for _, name := range names {
			if p, ok := c.Providers[name]; ok {
				key := "<not set>"
				if p.APIKey != "" {
					key = "<configured>"
				}
				fmt.Printf("%s: model=%s api_key=%s\n", name, p.DefaultModel, key)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func profileCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("profile requires show or extract")
	}
	switch args[0] {
	case "show":
		p, err := profile.Load()
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case "extract":
		a, err := app.Load()
		if err != nil {
			return err
		}
		defer a.Close()
		if err := a.AttachWorkspace("."); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.RunProfileExtraction(ctx)
		return nil
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func workspaceInitialized(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".aicli", "session.db"))
	return err == nil && !info.IsDir()
}

func usage() error {
	fmt.Println(`FuzeCLI - AI coding workspace for developers

Getting started:
  aicli init                         Initialize this project and run setup
  aicli setup                        Reconfigure provider/model
  aicli chat                         Start an interactive coding session (--safe for destructive-change blocking)
  aicli ask "prompt"                 Run a one-shot AI request (--safe for destructive-change blocking)

Developer tools:
  aicli debug                        Safe diagnostic report for troubleshooting
  aicli doctor                       Check provider and workspace health
  aicli status                       Inspect Git/workspace status
  aicli diff                         Review current changes
  aicli models --provider gemini     List available models
  aicli history                      View recent conversation history
  aicli snapshot                     Create a local recovery snapshot
  aicli restore [snapshot.zip]       Restore a snapshot
  aicli profile show|extract         Inspect or refresh developer profile

Integration:
  aicli app [--addr host:port]       Start the polished local web app
  aicli api [--addr host:port]       Start the local API server
  aicli apikey set <provider> <key>  Save a provider API key
  aicli github ...                   GitHub repos, Git sync, pull requests, issues, comments, and Actions
  aicli apikey status                Show provider key status
  aicli apikey clear <provider>      Remove a provider API key
  aicli mcp                          Start the MCP server

Configuration:
  aicli config show
  aicli config set <key> <value>
  aicli provider setup

Chat commands:
  /file, /file <path>, /file list, /file clear
  /provider <name>, /model <name>
  /status, /clear, /help, /exit

Safety:
  Workspace context respects .aicliignore and secret-file protections.
  Generated shell commands are informational and are not executed automatically.
  Snapshots provide explicit local recovery points.

First run recommendation:
  aicli init

Already initialized?
  aicli init is safe and idempotent; it will not reset your project.
  Use aicli setup when you want to change your AI configuration.
  
Caution
  FuzeCLI(AiCli) is not responsible for any of the AI providers misebehaviours or errors. 
  So for errors concerning AI, Please contact your AI provider. 
  While this CLI can work on free commands for maximum perfomance, we recommend that you pay for the subscription to avoid errors concerning tokens, usage limits, tpm, and rate limits. `)
	return nil
}
