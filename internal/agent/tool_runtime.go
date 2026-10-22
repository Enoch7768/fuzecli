package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/github"
	gitlocal "github.com/Enoch7768/fuzecli/internal/git"
	"github.com/Enoch7768/fuzecli/internal/repository"
	"github.com/Enoch7768/fuzecli/internal/security"
	"github.com/Enoch7768/fuzecli/internal/verify"
	"github.com/Enoch7768/fuzecli/internal/workspace"
)

type ToolRuntime struct {
	Root string
	Store *workspace.Store
	Index *repository.Index
	Capabilities security.CapabilitySet
	// Approve must return nil only after an actual user-approved mutation.
	// A model-supplied argument is never proof of approval.
	Approve func(context.Context, string, json.RawMessage) error
}

func NewFuzeCLIToolRegistry(runtime ToolRuntime) (*ToolRegistry, error) {
	if strings.TrimSpace(runtime.Root) == "" { return nil, errors.New("workspace root is required") }
	root, err := filepath.Abs(runtime.Root); if err != nil { return nil, err }; runtime.Root = root
	if runtime.Capabilities == nil { runtime.Capabilities = security.SafeCapabilities() }
	registry := NewToolRegistry()
	for _, definition := range FuzeCLIToolDefinitions() {
		def := definition
		handler := func(ctx context.Context, raw json.RawMessage) (any, error) { return runtime.execute(ctx, def.Name, raw) }
		if err := registry.Register(def, handler); err != nil { return nil, err }
	}
	return registry, nil
}

func (rt ToolRuntime) execute(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var a map[string]any
	if err := json.Unmarshal(raw, &a); err != nil { return nil, err }
	if strings.HasPrefix(name,"github.") {
		if repositoryName:=stringArg(a,"repository");repositoryName!=""&&!validRepositoryIdentifier(repositoryName){return nil,errors.New("repository must use the full owner/name format")}
	}
	switch name {
	case "workspace.list_files": return rt.listFiles(a)
	case "workspace.read_file": return rt.readFile(a)
	case "workspace.search_code": return rt.searchCode(ctx, a)
	case "workspace.find_symbols": return rt.findSymbols(a)
	case "workspace.apply_edits": return rt.applyEdits(ctx, raw, a)
	case "project.health": return rt.projectHealth(a)
	case "terminal.run": return rt.terminalRun(ctx, a)
	case "verify.run": return rt.verifyRun(ctx, a)
	case "git.status": return rt.gitStatus(a)
	case "git.diff": return rt.gitDiff(a)
	case "git.commit": return rt.gitCommit(ctx, a, raw)
	case "github.auth": return rt.githubAuth(ctx, a)
	case "github.repositories": return rt.githubRepositories(ctx, a)
	case "github.branches": return rt.githubBranches(ctx, a, raw)
	case "github.sync": return rt.githubSync(ctx, a, raw)
	case "github.pull_requests": return rt.githubPullRequests(ctx, a, raw)
	case "github.issues": return rt.githubIssues(ctx, a, raw)
	case "github.actions": return rt.githubActions(ctx, a, raw)
	case "context.get": return rt.contextGet(a)
	case "context.checkpoint": return rt.checkpoint(a)
	default: return nil, fmt.Errorf("tool handler not implemented: %s", name)
	}
}

func (rt ToolRuntime) approve(ctx context.Context, name string, raw json.RawMessage) error {
	if rt.Approve == nil { return fmt.Errorf("%s requires an explicit user-approval handler; no mutation was performed", name) }
	return rt.Approve(ctx, name, raw)
}

func (rt ToolRuntime) resolve(rel string, allowDelete bool) (string, error) {
	if err := security.ValidateWorkspacePath(rt.Root, rel, allowDelete); err != nil { return "", err }
	path, err := generation.Resolve(rt.Root, rel); if err != nil { return "", err }
	normalized := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(rel)), "./")
	if normalized == ".git" || strings.HasPrefix(normalized, ".git/") || normalized == ".aicli" || strings.HasPrefix(normalized, ".aicli/") {
		return "", fmt.Errorf("protected workspace path %q", rel)
	}
	policy, err := workspace.LoadIgnorePolicy(rt.Root); if err != nil { return "", err }
	if policy.Ignored(filepath.ToSlash(rel)) { return "", fmt.Errorf("workspace access policy blocks %q", rel) }
	return path, nil
}

func (rt ToolRuntime) listFiles(a map[string]any) (any, error) {
	prefix, _ := a["prefix"].(string); limit := intArg(a,"limit",200)
	if prefix != "" { if _,err:=rt.resolve(prefix,false);err!=nil { // A prefix may name a directory; path checks still apply.
		if err := security.ValidateWorkspacePath(rt.Root,prefix,false); err != nil { return nil,err }
	} }
	policy,err:=workspace.LoadIgnorePolicy(rt.Root); if err!=nil{return nil,err}
	var files []string
	err=filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,walkErr error)error{
		if walkErr!=nil{return walkErr};if path==rt.Root{return nil}
		rel,e:=filepath.Rel(rt.Root,path);if e!=nil{return e};rel=filepath.ToSlash(rel)
		if rel==".git"||strings.HasPrefix(rel,".git/")||rel==".aicli"||strings.HasPrefix(rel,".aicli/")||policy.Ignored(rel){if d.IsDir(){return filepath.SkipDir};return nil}
		if d.IsDir(){return nil}
		if prefix!=""&&rel!=strings.TrimSuffix(filepath.ToSlash(prefix),"/")&&!strings.HasPrefix(rel,strings.TrimSuffix(filepath.ToSlash(prefix),"/")+"/"){return nil}
		files=append(files,rel);if len(files)>=limit{return errToolWalkLimit}
		return nil
	})
	if err!=nil&&!errors.Is(err,errToolWalkLimit){return nil,err}
	sort.Strings(files);return map[string]any{"files":files,"truncated":errors.Is(err,errToolWalkLimit),"count":len(files)},nil
}
var errToolWalkLimit=errors.New("file listing limit reached")

func (rt ToolRuntime) readFile(a map[string]any)(any,error){
	rel:=stringArg(a,"path");path,err:=rt.resolve(rel,false);if err!=nil{return nil,err}
	data,err:=os.ReadFile(path);if err!=nil{return nil,err};if len(data)>2<<20{return nil,errors.New("file exceeds the 2 MiB read limit")}
	if !json.Valid(json.RawMessage(`"`+strings.ReplaceAll(strings.ReplaceAll(string(data),`\`,`\\`),"\"","\"" )+`"`)) { /* UTF-8 is checked below without altering file content. */ }
	if !isUTF8Text(data){return nil,errors.New("file is not UTF-8 text")}
	lines:=strings.Split(string(data),"\n");start:=intArg(a,"start_line",1);end:=intArg(a,"end_line",len(lines));if start<1||end<start{return nil,errors.New("invalid line range")};if start>len(lines){return map[string]any{"path":filepath.ToSlash(rel),"content":"","start_line":start,"end_line":end,"total_lines":len(lines)},nil};if end>len(lines){end=len(lines)}
	return map[string]any{"path":filepath.ToSlash(rel),"content":strings.Join(lines[start-1:end],"\n"),"start_line":start,"end_line":end,"total_lines":len(lines)},nil
}
func isUTF8Text(data []byte) bool { return !bytes.Contains(data, []byte{0}) && utf8.Valid(data) }

func (rt ToolRuntime) searchCode(ctx context.Context,a map[string]any)(any,error){
	query:=stringArg(a,"query");max:=intArg(a,"max_results",50);caseSensitive:=boolArg(a,"case_sensitive",false);isRegex:=boolArg(a,"is_regex",false)
	var re *regexp.Regexp;var err error
	if isRegex {pattern:=query;if !caseSensitive{pattern="(?i)"+pattern};re,err=regexp.Compile(pattern);if err!=nil{return nil,fmt.Errorf("invalid regex: %w",err)}}
	paths:=stringArray(a["paths"]);pathSet:=map[string]bool{};for _,p:=range paths{if _,e:=rt.resolve(p,false);e!=nil{return nil,e};pathSet[filepath.ToSlash(p)]=true}
	policy,err:=workspace.LoadIgnorePolicy(rt.Root);if err!=nil{return nil,err}
	type match struct{Path string `json:"path"`;Line int `json:"line"`;Text string `json:"text"`}
	out:=make([]match,0,max);truncated:=false
	err=filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,walkErr error)error{
		if walkErr!=nil{return walkErr};if path==rt.Root{return nil}
		rel,e:=filepath.Rel(rt.Root,path);if e!=nil{return e};rel=filepath.ToSlash(rel)
		if d.IsDir(){if rel==".git"||strings.HasPrefix(rel,".git/")||rel==".aicli"||strings.HasPrefix(rel,".aicli/")||policy.Ignored(rel){return filepath.SkipDir};return nil}
		if policy.Ignored(rel)||len(pathSet)>0&&!matchesPathSet(rel,pathSet){return nil}
		if e:=ctx.Err();e!=nil{return e}
		info,e:=d.Info();if e!=nil||info.Size()>2<<20{return nil}
		data,e:=os.ReadFile(path);if e!=nil||!isUTF8Text(data){return nil}
		sc:=bufio.NewScanner(bytes.NewReader(data));sc.Buffer(make([]byte,4096),1<<20);line:=0
		for sc.Scan(){line++;text:=sc.Text();hay,needle:=text,query;if !caseSensitive{hay=strings.ToLower(hay);needle=strings.ToLower(needle)}
			found:=strings.Contains(hay,needle);if re!=nil{found=re.MatchString(text)}
			if found{if len(text)>500{text=text[:500]};out=append(out,match{rel,line,text});if len(out)>=max{truncated=true;return errToolWalkLimit}}
		};return sc.Err()
	})
	if err!=nil&&!errors.Is(err,errToolWalkLimit){return nil,err};return map[string]any{"matches":out,"truncated":truncated,"count":len(out)},nil
}
func matchesPathSet(rel string,set map[string]bool)bool{for p:=range set{if rel==p||strings.HasPrefix(rel,strings.TrimSuffix(p,"/")+"/"){return true}};return false}

func (rt ToolRuntime) findSymbols(a map[string]any)(any,error){
	if rt.Index==nil{return nil,errors.New("workspace symbol index is unavailable")}
	query:=strings.ToLower(stringArg(a,"query"));kind:=stringArg(a,"kind");if kind==""{kind="any"}
	max:=intArg(a,"max_results",30);pathFilter:=filepath.ToSlash(stringArg(a,"path"))
	if pathFilter!=""{if _,err:=rt.resolve(pathFilter,false);err!=nil{return nil,err}}
	type symbol struct{Name string `json:"name"`;Path string `json:"path"`;Language string `json:"language"`;Kind string `json:"kind"`}
	out:=[]symbol{}
	for _,f:=range rt.Index.Files{
		if pathFilter!=""&&f.Path!=pathFilter{continue}
		if f.Language!="go"{
			if kind!="any"{continue}
			for _,name:=range f.Symbols{if strings.Contains(strings.ToLower(name),query){out=append(out,symbol{name,f.Path,f.Language,"unknown"})}}
			continue
		}
		full:=filepath.Join(rt.Root,filepath.FromSlash(f.Path))
		parsed,_:=parser.ParseFile(token.NewFileSet(),full,nil,parser.ParseComments|parser.AllErrors)
		if parsed==nil{continue}
		found:=make([]symbol,0,len(f.Symbols))
		for _,decl:=range parsed.Decls{
			switch d:=decl.(type){
			case *ast.FuncDecl:
				k:="function";if d.Recv!=nil{k="method"}
				found=append(found,symbol{d.Name.Name,f.Path,f.Language,k})
			case *ast.GenDecl:
				for _,spec:=range d.Specs{
					switch item:=spec.(type){
					case *ast.TypeSpec:
						k:="type";found=append(found,symbol{item.Name.Name,f.Path,f.Language,k})
						if kind=="class"{found[len(found)-1].Kind="class"}
					case *ast.ValueSpec:
						k:="variable";if d.Tok==token.CONST{k="constant"}
						for _,name:=range item.Names{found=append(found,symbol{name.Name,f.Path,f.Language,k})}
					}
				}
			}
		}
		for _,entry:=range found{
			if !strings.Contains(strings.ToLower(entry.Name),query){continue}
			if kind!="any"&&entry.Kind!=kind{continue}
			out=append(out,entry)
			if len(out)>=max{return map[string]any{"symbols":out,"truncated":true,"indexed_languages":indexedLanguages(rt.Index)},nil}
		}
	}
	return map[string]any{"symbols":out,"truncated":false,"indexed_languages":indexedLanguages(rt.Index)},nil
}
func indexedLanguages(i *repository.Index)[]string{m:=map[string]bool{};for _,f:=range i.Files{m[f.Language]=true};o:=[]string{};for k:=range m{o=append(o,k)};sort.Strings(o);return o}

func (rt ToolRuntime) applyEdits(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	items,_:=a["files"].([]any);plan:=generation.Plan{}
	for _,item:=range items{f,_:=item.(map[string]any);path:=stringArg(f,"path");action:=stringArg(f,"action");if _,err:=rt.resolve(path,action=="delete");err!=nil{return nil,err};if action=="delete"{if err:=rt.Capabilities.Require(security.DeleteFile);err!=nil{return nil,err}};plan.Files=append(plan.Files,generation.FileChange{Path:path,Action:action,Content:stringArg(f,"content")})}
	verifyRequested:=boolArg(a,"verify",true)
	verifierAvailable:=false
	for _,manifest:=range []string{"go.mod","package.json","composer.json","requirements.txt","pyproject.toml"}{if _,e:=os.Stat(filepath.Join(rt.Root,manifest));e==nil{verifierAvailable=true;break}}
	var verifyFn func()VerificationResult
	if verifyRequested&&verifierAvailable {
		verifyFn=func()VerificationResult{paths:=[]string{};for _,f:=range plan.Files{paths=append(paths,f.Path)};v,e:=verify.Detect(rt.Root,paths);if e!=nil{return VerificationResult{Output:e.Error()}};return VerificationResult{Passed:v.Passed,Output:v.Output}}
	}
	executor:=Executor{Root:rt.Root,Capabilities:rt.Capabilities}
	result,err:=executor.Execute(plan,verifyFn)
	if err!=nil{return map[string]any{"applied":false,"changed":result.Changed,"verification":result.Output},err}
	if verifyRequested&&!verifierAvailable{result.Output="Verification skipped: no supported project manifest was found."}
	if rt.Index!=nil{_ = rt.Index.Refresh()};if rt.Store!=nil{_ = rt.Store.MarkTouched(result.Changed);_ = rt.Store.RefreshHashes(result.Changed)}
	return map[string]any{"applied":true,"changed":result.Changed,"verified":verifyRequested&&verifierAvailable,"verification":result.Output},nil
}

func (rt ToolRuntime) projectHealth(a map[string]any)(any,error){
	out:=map[string]any{"workspace":rt.Root,"manifests":[]string{}}
	for _,name:=range []string{"go.mod","package.json","composer.json","pyproject.toml","requirements.txt","Cargo.toml","pom.xml","Makefile"}{if _,e:=os.Stat(filepath.Join(rt.Root,name));e==nil{out["manifests"]=append(out["manifests"].([]string),name)}}
	if boolArg(a,"include_runtimes",true){runtimes:=map[string]string{};for _,name:=range []string{"go","node","npm","php","python","python3","cargo","java","make"}{if p,e:=exec.LookPath(name);e==nil{runtimes[name]=p}};out["runtimes"]=runtimes}
	if boolArg(a,"include_git",true){s,e:=gitlocal.StatusOf(rt.Root);if e==nil{out["git"]=s}else{out["git_error"]=e.Error()}}
	out["read_only"]=true
	return out,nil
}

func (rt ToolRuntime) terminalRun(ctx context.Context,a map[string]any)(any,error){
	exe:=stringArg(a,"executable");args:=stringArray(a["args"]);if err:=rt.Capabilities.Require(security.RunBuild);err!=nil&&exe!="go"&&exe!="npm"&&exe!="node"&&exe!="python"&&exe!="python3"&&exe!="php"&&exe!="make"&&exe!="cargo"{return nil,err}
	if exe=="php"&&len(args)==2&&args[0]=="-l"{resolved,err:=rt.resolve(args[1],false);if err!=nil{return nil,err};args[1]=resolved}
	decision:=security.Authorize(security.Command{Name:exe,Args:args});if !decision.Allowed{return nil,fmt.Errorf("command blocked by security policy: %s",decision.Reason)}
	cwd:=rt.Root;if rel:=stringArg(a,"cwd");rel!=""{p,e:=rt.resolve(rel,false);if e!=nil{return nil,e};info,e:=os.Stat(p);if e!=nil||!info.IsDir(){return nil,errors.New("cwd must be an existing workspace directory")};cwd=p}
	timeout:=time.Duration(intArg(a,"timeout_seconds",60))*time.Second;cctx,cancel:=context.WithTimeout(ctx,timeout);defer cancel()
	cmd:=exec.CommandContext(cctx,exe,args...);cmd.Dir=cwd;cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");var stdout,stderr limitedBuffer;stdout.limit=64<<10;stderr.limit=64<<10;cmd.Stdout=&stdout;cmd.Stderr=&stderr
	err:=cmd.Run();exit:=0;if err!=nil{var ee *exec.ExitError;if errors.As(err,&ee){exit=ee.ExitCode()}else if cctx.Err()==nil{return nil,fmt.Errorf("run command: %w",err)}else{exit=-1}}
	return map[string]any{"exit_code":exit,"stdout":stdout.String(),"stderr":stderr.String(),"timed_out":errors.Is(cctx.Err(),context.DeadlineExceeded),"error":errorString(err)},nil
}
type limitedBuffer struct{bytes.Buffer;limit int}
func(b *limitedBuffer)Write(p []byte)(int,error){n:=len(p);remaining:=b.limit-b.Len();if remaining>0{if remaining>len(p){remaining=len(p)};_,_=b.Buffer.Write(p[:remaining])};return n,nil}
func errorString(err error)string{if err==nil{return ""};return err.Error()}

func (rt ToolRuntime) verifyRun(ctx context.Context,a map[string]any)(any,error){
	checks:=stringArray(a["checks"]);timeout:=time.Duration(intArg(a,"timeout_seconds",120))*time.Second;requested:=stringArray(a["paths"]);validated:=make([]string,0,len(requested))
	for _,path:=range requested{resolved,err:=rt.resolve(path,false);if err!=nil{return nil,fmt.Errorf("verification path %q: %w",path,err)};info,err:=os.Stat(resolved);if err!=nil{return nil,err};if !info.IsDir()&&!info.Mode().IsRegular(){return nil,fmt.Errorf("verification path %q is not a regular file or directory",path)};validated=append(validated,filepath.ToSlash(path))}
	results:=[]map[string]any{}
	for _,check:=range checks{
		if err:=ctx.Err();err!=nil{return nil,err}
		exe,args,err:=checkCommand(rt.Root,check,validated)
		entry:=map[string]any{"check":check,"status":"skipped","exit_code":-1,"duration_ms":int64(0),"diagnostics":[]any{}}
		if len(validated)>0 {entry["requested_paths"]=validated}
		if err!=nil{entry["diagnostic"]=err.Error();results=append(results,entry);continue}
		if len(validated)>0&&exe!="go"&&exe!="gofmt"{entry["scope"]="project-wide; the manifest script does not support path-scoped execution"}else if len(validated)>0{entry["scope"]="requested paths"}else{entry["scope"]="project"}
		if _,e:=exec.LookPath(exe);e!=nil{entry["diagnostic"]=exe+" is not installed";results=append(results,entry);continue}
		decision:=security.Authorize(security.Command{Name:exe,Args:args});if !decision.Allowed{entry["diagnostic"]="blocked by security policy: "+decision.Reason;results=append(results,entry);continue}
		cctx,cancel:=context.WithTimeout(ctx,timeout);start:=time.Now();cmd:=exec.CommandContext(cctx,exe,args...);cmd.Dir=rt.Root;var out limitedBuffer;out.limit=256<<10;cmd.Stdout=&out;cmd.Stderr=&out;runErr:=cmd.Run();cancel();entry["duration_ms"]=time.Since(start).Milliseconds();entry["exit_code"]=0;entry["status"]="passed"
		if runErr!=nil{entry["status"]="failed";var ee *exec.ExitError;if errors.As(runErr,&ee){entry["exit_code"]=ee.ExitCode()}else{entry["exit_code"]=-1};entry["diagnostic"]=out.String()}else if check=="format"&&exe=="gofmt"&&strings.TrimSpace(out.String())!=""{entry["status"]="failed";entry["exit_code"]=1;entry["diagnostic"]=out.String()}else{entry["diagnostic"]=out.String()}
		results=append(results,entry)
	}
	return map[string]any{"checks":results,"all_passed":allPassed(results),"count":len(results)},nil
}
func checkCommand(root,check string,paths []string)(string,[]string,error){
	has:=func(name string)bool{_,err:=os.Stat(filepath.Join(root,name));return err==nil}
	goPackages:=[]string{"./..."}
	if len(paths)>0 {goPackages=packagePaths(root,paths)}
	switch check{
	case "format":
		if has("go.mod"){args:=[]string{"-l"};if len(paths)==0{return "gofmt",[]string{"-l","."},nil};files,err:=goFilesForPaths(root,paths);if err!=nil{return "",nil,err};if len(files)==0{return "",nil,errors.New("no Go files found in the requested paths")};return "gofmt",append(args,files...),nil}
		if has("package.json"){return "npm",[]string{"run","format:check"},nil}
		return "",nil,errors.New("no supported format-check command detected")
	case "lint":
		if has("go.mod"){return "go",append([]string{"vet"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"run","lint"},nil}
		return "",nil,errors.New("no supported lint command detected")
	case "test":
		if has("go.mod"){return "go",append([]string{"test"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"test"},nil}
		return "",nil,errors.New("no supported test command detected")
	case "build":
		if has("go.mod"){return "go",append([]string{"build"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"run","build"},nil}
		return "",nil,errors.New("no supported build command detected")
	case "typecheck":
		if has("go.mod"){return "go",append([]string{"test","-run","^$"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"run","typecheck"},nil}
		return "",nil,errors.New("no supported typecheck command detected")
	default:return "",nil,fmt.Errorf("unsupported check %q",check)
	}
}
func packagePaths(root string,paths []string)[]string{
	seen:=map[string]bool{};out:=[]string{}
	for _,rel:=range paths{full:=filepath.Join(root,filepath.FromSlash(rel));info,err:=os.Stat(full);if err!=nil{continue};dir:=full;if !info.IsDir(){dir=filepath.Dir(full)};relative,err:=filepath.Rel(root,dir);if err!=nil{continue};pkg:="./"+filepath.ToSlash(relative);if relative=="."{pkg="./..."};if !seen[pkg]{seen[pkg]=true;out=append(out,pkg)}}
	sort.Strings(out);if len(out)==0{return []string{"./..."}};return out
}
func goFilesForPaths(root string,paths []string)([]string,error){
	var files []string;seen:=map[string]bool{}
	for _,rel:=range paths{full:=filepath.Join(root,filepath.FromSlash(rel));info,err:=os.Stat(full);if err!=nil{return nil,err};if info.Mode().IsRegular(){if filepath.Ext(full)==".go"{seen[full]=true};continue}
		err=filepath.WalkDir(full,func(path string,d os.DirEntry,e error)error{if e!=nil{return e};if d.IsDir()&&path!=full&&(d.Name()==".git"||d.Name()==".aicli"||d.Name()=="vendor"){return filepath.SkipDir};if d.IsDir(){return nil};if filepath.Ext(path)==".go"{seen[path]=true};return nil});if err!=nil{return nil,err}
	}
	for path:=range seen{rel,err:=filepath.Rel(root,path);if err!=nil{return nil,err};files=append(files,filepath.ToSlash(rel))};sort.Strings(files);return files,nil
}
func allPassed(results []map[string]any)bool{if len(results)==0{return false};for _,r:=range results{if r["status"]!="passed"{return false}};return true}

func (rt ToolRuntime) gitStatus(a map[string]any)(any,error){s,e:=gitlocal.StatusOf(rt.Root);if e!=nil{return nil,e};if !boolArg(a,"include_untracked",true){filtered:=s.Files[:0];for _,f:=range s.Files{if f.Index!='?'&&f.Worktree!='?'{filtered=append(filtered,f)}};s.Files=filtered;s.Clean=len(filtered)==0};return s,nil}
func (rt ToolRuntime) gitDiff(a map[string]any)(any,error){
	path:=stringArg(a,"path")
	if path!=""{if _,e:=rt.resolve(path,false);e!=nil{return nil,e}}
	args:=[]string{"diff","-U"+strconv.Itoa(intArg(a,"context_lines",3))}
	if boolArg(a,"staged",false){args=append(args,"--cached")}
	if path!=""{args=append(args,"--",path)}else{args=append(args,"--")}
	diff,e:=runGitOutput(context.Background(),rt.Root,args...);if e!=nil{return nil,e}
	return map[string]any{"diff":truncate(diff,256<<10),"truncated":len(diff)>256<<10},nil
}
func ctxOrBackground()context.Context{return context.Background()}
func diffArgs(staged bool,path string)[]string{args:=[]string{"diff"};if staged{args=append(args,"--cached")};return append(args,"--",path)}
func runGitOutput(ctx context.Context,root string,args ...string)(string,error){cmd:=exec.CommandContext(ctx,"git",args...);cmd.Dir=root;b,e:=cmd.CombinedOutput();if e!=nil{return "",fmt.Errorf("git %v: %s",args,strings.TrimSpace(string(b)))};return string(b),nil}
func (rt ToolRuntime) gitCommit(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	if e:=rt.Capabilities.Require(security.RunGit);e!=nil{return nil,e}
	head,e:=runGitOutput(ctx,rt.Root,"rev-parse","HEAD");if e!=nil{return nil,e};if strings.TrimSpace(head)!=stringArg(a,"expected_head"){return nil,errors.New("repository HEAD changed; refresh status and try again")}
	paths:=stringArray(a["paths"]);for _,p:=range paths{if _,e:=rt.resolve(p,true);e!=nil{return nil,e}}
	status,e:=gitlocal.StatusOf(rt.Root);if e!=nil{return nil,e};allowed:=map[string]bool{};for _,p:=range paths{allowed[filepath.ToSlash(p)]=true};for _,f:=range status.Files{if !allowed[f.Path]{continue};if f.Index!=' '&&f.Index!='?'{return nil,fmt.Errorf("selected path %s already has staged changes; refusing to commit unrelated index content",f.Path)}}
	addArgs:=append([]string{"add","--"},paths...);cmd:=exec.CommandContext(ctx,"git",addArgs...);cmd.Dir=rt.Root;if out,e:=cmd.CombinedOutput();e!=nil{return nil,fmt.Errorf("git add failed: %s",strings.TrimSpace(string(out)))}
	commitArgs:=append([]string{"commit","-m",stringArg(a,"message"),"--"},paths...);cmd=exec.CommandContext(ctx,"git",commitArgs...);cmd.Dir=rt.Root;out,e:=cmd.CombinedOutput();if e!=nil{return nil,fmt.Errorf("git commit failed: %s",strings.TrimSpace(string(out)))}
	newHead,e:=runGitOutput(ctx,rt.Root,"rev-parse","HEAD");if e!=nil{return nil,e};return map[string]any{"committed":true,"head":strings.TrimSpace(newHead),"output":strings.TrimSpace(string(out))},nil
}

func (rt ToolRuntime) githubClient()( *github.Client,error){token,e:=config.GitHubToken();if e!=nil{return nil,e};configured,e:=config.GitHubTokenConfigured();if e!=nil{return nil,e};c:=github.New(token);c.DisableCLI=configured&&strings.TrimSpace(token)=="";if !c.Authenticated(){return nil,errors.New("GitHub is not authenticated; connect with the supported GitHub browser/device login first")};return c,nil}
func (rt ToolRuntime) githubAuth(ctx context.Context,a map[string]any)(any,error){
	action:=stringArg(a,"action")
	switch action{
	case "status": c,e:=rt.githubClient();if e!=nil{return map[string]any{"authenticated":false,"message":e.Error()},nil};u,e:=c.Profile(ctx);if e!=nil{return nil,e};return map[string]any{"authenticated":true,"user":u.Login,"profile_url":u.HTMLURL},nil
	case "login_start":
		if _,err:=exec.LookPath("gh");err!=nil{return nil,errors.New("GitHub CLI (gh) is required for browser sign-in")}
		cctx,cancel:=context.WithTimeout(ctx,120*time.Second);defer cancel()
		cmd:=exec.CommandContext(cctx,"gh","auth","login","--web","--hostname","github.com");var out limitedBuffer;out.limit=16<<10;cmd.Stdout=&out;cmd.Stderr=&out
		if err:=cmd.Run();err!=nil{return map[string]any{"started":false,"output":out.String(),"timed_out":errors.Is(cctx.Err(),context.DeadlineExceeded)},fmt.Errorf("GitHub browser sign-in did not complete: %w",err)}
		return map[string]any{"started":true,"authenticated":true,"output":out.String()},nil
	case "login_status":
		if _,err:=exec.LookPath("gh");err!=nil{return map[string]any{"authenticated":false,"message":"GitHub CLI is not installed"},nil}
		cctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();cmd:=exec.CommandContext(cctx,"gh","auth","status","--hostname","github.com");out,err:=cmd.CombinedOutput()
		return map[string]any{"authenticated":err==nil,"output":truncate(string(out),4000)},nil
	case "disconnect":
		if err:=config.ClearGitHubToken();err!=nil{return nil,err}
		return map[string]any{"disconnected":true,"note":"Stored Studio token cleared. This does not revoke or log out the underlying GitHub CLI session."},nil
	default:return nil,errors.New("unsupported GitHub auth action")
	}
}
func (rt ToolRuntime) githubRepositories(ctx context.Context,a map[string]any)(any,error){c,e:=rt.githubClient();if e!=nil{return nil,e};if stringArg(a,"action")=="get"{return c.Repository(ctx,stringArg(a,"repository"))};return c.Repositories(ctx,intArg(a,"page",1),intArg(a,"per_page",30))}
func (rt ToolRuntime) githubBranches(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){c,e:=rt.githubClient();if e!=nil{return nil,e};repo:=stringArg(a,"repository");if stringArg(a,"action")=="list"{return c.BranchesPage(ctx,repo,intArg(a,"page",1),100)};if e=c.CreateBranch(ctx,repo,stringArg(a,"branch"),stringArg(a,"base"));e!=nil{return nil,e};return map[string]any{"created":true,"branch":stringArg(a,"branch")},nil}
func (rt ToolRuntime) githubSync(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	op:=stringArg(a,"operation");if op=="push"{}
	c,e:=rt.githubClient();if e!=nil{return nil,e};if e=rt.Capabilities.Require(security.RunGit);e!=nil{return nil,e}
	status,e:=gitlocal.StatusOf(rt.Root);if e!=nil{return nil,e};if !status.Clean{return nil,errors.New("sync refused: commit or stash local changes first")}
	head,e:=runGitOutput(ctx,rt.Root,"rev-parse","HEAD");if e!=nil{return nil,e};if expected:=stringArg(a,"expected_head");expected!=""&&strings.TrimSpace(head)!=expected{return nil,errors.New("local HEAD changed; refresh repository status")}
	remote:=stringArg(a,"remote");if remote==""{remote="origin"};if !regexp.MustCompile(` + "`^[A-Za-z0-9_.-]+package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/github"
	gitlocal "github.com/Enoch7768/fuzecli/internal/git"
	"github.com/Enoch7768/fuzecli/internal/repository"
	"github.com/Enoch7768/fuzecli/internal/security"
	"github.com/Enoch7768/fuzecli/internal/verify"
	"github.com/Enoch7768/fuzecli/internal/workspace"
)

type ToolRuntime struct {
	Root string
	Store *workspace.Store
	Index *repository.Index
	Capabilities security.CapabilitySet
	// Approve must return nil only after an actual user-approved mutation.
	// A model-supplied argument is never proof of approval.
	Approve func(context.Context, string, json.RawMessage) error
}

func NewFuzeCLIToolRegistry(runtime ToolRuntime) (*ToolRegistry, error) {
	if strings.TrimSpace(runtime.Root) == "" { return nil, errors.New("workspace root is required") }
	root, err := filepath.Abs(runtime.Root); if err != nil { return nil, err }; runtime.Root = root
	if runtime.Capabilities == nil { runtime.Capabilities = security.SafeCapabilities() }
	registry := NewToolRegistry()
	for _, definition := range FuzeCLIToolDefinitions() {
		def := definition
		handler := func(ctx context.Context, raw json.RawMessage) (any, error) { return runtime.execute(ctx, def.Name, raw) }
		if err := registry.Register(def, handler); err != nil { return nil, err }
	}
	return registry, nil
}

func (rt ToolRuntime) execute(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var a map[string]any
	if err := json.Unmarshal(raw, &a); err != nil { return nil, err }
	if strings.HasPrefix(name,"github.") {
		if repositoryName:=stringArg(a,"repository");repositoryName!=""&&!validRepositoryIdentifier(repositoryName){return nil,errors.New("repository must use the full owner/name format")}
	}
	switch name {
	case "workspace.list_files": return rt.listFiles(a)
	case "workspace.read_file": return rt.readFile(a)
	case "workspace.search_code": return rt.searchCode(ctx, a)
	case "workspace.find_symbols": return rt.findSymbols(a)
	case "workspace.apply_edits": return rt.applyEdits(ctx, raw, a)
	case "project.health": return rt.projectHealth(a)
	case "terminal.run": return rt.terminalRun(ctx, a)
	case "verify.run": return rt.verifyRun(ctx, a)
	case "git.status": return rt.gitStatus(a)
	case "git.diff": return rt.gitDiff(a)
	case "git.commit": return rt.gitCommit(ctx, a, raw)
	case "github.auth": return rt.githubAuth(ctx, a)
	case "github.repositories": return rt.githubRepositories(ctx, a)
	case "github.branches": return rt.githubBranches(ctx, a, raw)
	case "github.sync": return rt.githubSync(ctx, a, raw)
	case "github.pull_requests": return rt.githubPullRequests(ctx, a, raw)
	case "github.issues": return rt.githubIssues(ctx, a, raw)
	case "github.actions": return rt.githubActions(ctx, a, raw)
	case "context.get": return rt.contextGet(a)
	case "context.checkpoint": return rt.checkpoint(a)
	default: return nil, fmt.Errorf("tool handler not implemented: %s", name)
	}
}

func (rt ToolRuntime) approve(ctx context.Context, name string, raw json.RawMessage) error {
	if rt.Approve == nil { return fmt.Errorf("%s requires an explicit user-approval handler; no mutation was performed", name) }
	return rt.Approve(ctx, name, raw)
}

func (rt ToolRuntime) resolve(rel string, allowDelete bool) (string, error) {
	if err := security.ValidateWorkspacePath(rt.Root, rel, allowDelete); err != nil { return "", err }
	path, err := generation.Resolve(rt.Root, rel); if err != nil { return "", err }
	normalized := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(rel)), "./")
	if normalized == ".git" || strings.HasPrefix(normalized, ".git/") || normalized == ".aicli" || strings.HasPrefix(normalized, ".aicli/") {
		return "", fmt.Errorf("protected workspace path %q", rel)
	}
	policy, err := workspace.LoadIgnorePolicy(rt.Root); if err != nil { return "", err }
	if policy.Ignored(filepath.ToSlash(rel)) { return "", fmt.Errorf("workspace access policy blocks %q", rel) }
	return path, nil
}

func (rt ToolRuntime) listFiles(a map[string]any) (any, error) {
	prefix, _ := a["prefix"].(string); limit := intArg(a,"limit",200)
	if prefix != "" { if _,err:=rt.resolve(prefix,false);err!=nil { // A prefix may name a directory; path checks still apply.
		if err := security.ValidateWorkspacePath(rt.Root,prefix,false); err != nil { return nil,err }
	} }
	policy,err:=workspace.LoadIgnorePolicy(rt.Root); if err!=nil{return nil,err}
	var files []string
	err=filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,walkErr error)error{
		if walkErr!=nil{return walkErr};if path==rt.Root{return nil}
		rel,e:=filepath.Rel(rt.Root,path);if e!=nil{return e};rel=filepath.ToSlash(rel)
		if rel==".git"||strings.HasPrefix(rel,".git/")||rel==".aicli"||strings.HasPrefix(rel,".aicli/")||policy.Ignored(rel){if d.IsDir(){return filepath.SkipDir};return nil}
		if d.IsDir(){return nil}
		if prefix!=""&&rel!=strings.TrimSuffix(filepath.ToSlash(prefix),"/")&&!strings.HasPrefix(rel,strings.TrimSuffix(filepath.ToSlash(prefix),"/")+"/"){return nil}
		files=append(files,rel);if len(files)>=limit{return errToolWalkLimit}
		return nil
	})
	if err!=nil&&!errors.Is(err,errToolWalkLimit){return nil,err}
	sort.Strings(files);return map[string]any{"files":files,"truncated":errors.Is(err,errToolWalkLimit),"count":len(files)},nil
}
var errToolWalkLimit=errors.New("file listing limit reached")

func (rt ToolRuntime) readFile(a map[string]any)(any,error){
	rel:=stringArg(a,"path");path,err:=rt.resolve(rel,false);if err!=nil{return nil,err}
	data,err:=os.ReadFile(path);if err!=nil{return nil,err};if len(data)>2<<20{return nil,errors.New("file exceeds the 2 MiB read limit")}
	if !json.Valid(json.RawMessage(`"`+strings.ReplaceAll(strings.ReplaceAll(string(data),`\`,`\\`),"\"","\"" )+`"`)) { /* UTF-8 is checked below without altering file content. */ }
	if !isUTF8Text(data){return nil,errors.New("file is not UTF-8 text")}
	lines:=strings.Split(string(data),"\n");start:=intArg(a,"start_line",1);end:=intArg(a,"end_line",len(lines));if start<1||end<start{return nil,errors.New("invalid line range")};if start>len(lines){return map[string]any{"path":filepath.ToSlash(rel),"content":"","start_line":start,"end_line":end,"total_lines":len(lines)},nil};if end>len(lines){end=len(lines)}
	return map[string]any{"path":filepath.ToSlash(rel),"content":strings.Join(lines[start-1:end],"\n"),"start_line":start,"end_line":end,"total_lines":len(lines)},nil
}
func isUTF8Text(data []byte) bool { return !bytes.Contains(data, []byte{0}) && utf8.Valid(data) }

func (rt ToolRuntime) searchCode(ctx context.Context,a map[string]any)(any,error){
	query:=stringArg(a,"query");max:=intArg(a,"max_results",50);caseSensitive:=boolArg(a,"case_sensitive",false);isRegex:=boolArg(a,"is_regex",false)
	var re *regexp.Regexp;var err error
	if isRegex {pattern:=query;if !caseSensitive{pattern="(?i)"+pattern};re,err=regexp.Compile(pattern);if err!=nil{return nil,fmt.Errorf("invalid regex: %w",err)}}
	paths:=stringArray(a["paths"]);pathSet:=map[string]bool{};for _,p:=range paths{if _,e:=rt.resolve(p,false);e!=nil{return nil,e};pathSet[filepath.ToSlash(p)]=true}
	policy,err:=workspace.LoadIgnorePolicy(rt.Root);if err!=nil{return nil,err}
	type match struct{Path string `json:"path"`;Line int `json:"line"`;Text string `json:"text"`}
	out:=make([]match,0,max);truncated:=false
	err=filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,walkErr error)error{
		if walkErr!=nil{return walkErr};if path==rt.Root{return nil}
		rel,e:=filepath.Rel(rt.Root,path);if e!=nil{return e};rel=filepath.ToSlash(rel)
		if d.IsDir(){if rel==".git"||strings.HasPrefix(rel,".git/")||rel==".aicli"||strings.HasPrefix(rel,".aicli/")||policy.Ignored(rel){return filepath.SkipDir};return nil}
		if policy.Ignored(rel)||len(pathSet)>0&&!matchesPathSet(rel,pathSet){return nil}
		if e:=ctx.Err();e!=nil{return e}
		info,e:=d.Info();if e!=nil||info.Size()>2<<20{return nil}
		data,e:=os.ReadFile(path);if e!=nil||!isUTF8Text(data){return nil}
		sc:=bufio.NewScanner(bytes.NewReader(data));sc.Buffer(make([]byte,4096),1<<20);line:=0
		for sc.Scan(){line++;text:=sc.Text();hay,needle:=text,query;if !caseSensitive{hay=strings.ToLower(hay);needle=strings.ToLower(needle)}
			found:=strings.Contains(hay,needle);if re!=nil{found=re.MatchString(text)}
			if found{if len(text)>500{text=text[:500]};out=append(out,match{rel,line,text});if len(out)>=max{truncated=true;return errToolWalkLimit}}
		};return sc.Err()
	})
	if err!=nil&&!errors.Is(err,errToolWalkLimit){return nil,err};return map[string]any{"matches":out,"truncated":truncated,"count":len(out)},nil
}
func matchesPathSet(rel string,set map[string]bool)bool{for p:=range set{if rel==p||strings.HasPrefix(rel,strings.TrimSuffix(p,"/")+"/"){return true}};return false}

func (rt ToolRuntime) findSymbols(a map[string]any)(any,error){
	if rt.Index==nil{return nil,errors.New("workspace symbol index is unavailable")}
	query:=strings.ToLower(stringArg(a,"query"));kind:=stringArg(a,"kind");if kind==""{kind="any"}
	max:=intArg(a,"max_results",30);pathFilter:=filepath.ToSlash(stringArg(a,"path"))
	if pathFilter!=""{if _,err:=rt.resolve(pathFilter,false);err!=nil{return nil,err}}
	type symbol struct{Name string `json:"name"`;Path string `json:"path"`;Language string `json:"language"`;Kind string `json:"kind"`}
	out:=[]symbol{}
	for _,f:=range rt.Index.Files{
		if pathFilter!=""&&f.Path!=pathFilter{continue}
		if f.Language!="go"{
			if kind!="any"{continue}
			for _,name:=range f.Symbols{if strings.Contains(strings.ToLower(name),query){out=append(out,symbol{name,f.Path,f.Language,"unknown"})}}
			continue
		}
		full:=filepath.Join(rt.Root,filepath.FromSlash(f.Path))
		parsed,_:=parser.ParseFile(token.NewFileSet(),full,nil,parser.ParseComments|parser.AllErrors)
		if parsed==nil{continue}
		found:=make([]symbol,0,len(f.Symbols))
		for _,decl:=range parsed.Decls{
			switch d:=decl.(type){
			case *ast.FuncDecl:
				k:="function";if d.Recv!=nil{k="method"}
				found=append(found,symbol{d.Name.Name,f.Path,f.Language,k})
			case *ast.GenDecl:
				for _,spec:=range d.Specs{
					switch item:=spec.(type){
					case *ast.TypeSpec:
						k:="type";found=append(found,symbol{item.Name.Name,f.Path,f.Language,k})
						if kind=="class"{found[len(found)-1].Kind="class"}
					case *ast.ValueSpec:
						k:="variable";if d.Tok==token.CONST{k="constant"}
						for _,name:=range item.Names{found=append(found,symbol{name.Name,f.Path,f.Language,k})}
					}
				}
			}
		}
		for _,entry:=range found{
			if !strings.Contains(strings.ToLower(entry.Name),query){continue}
			if kind!="any"&&entry.Kind!=kind{continue}
			out=append(out,entry)
			if len(out)>=max{return map[string]any{"symbols":out,"truncated":true,"indexed_languages":indexedLanguages(rt.Index)},nil}
		}
	}
	return map[string]any{"symbols":out,"truncated":false,"indexed_languages":indexedLanguages(rt.Index)},nil
}
func indexedLanguages(i *repository.Index)[]string{m:=map[string]bool{};for _,f:=range i.Files{m[f.Language]=true};o:=[]string{};for k:=range m{o=append(o,k)};sort.Strings(o);return o}

func (rt ToolRuntime) applyEdits(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	items,_:=a["files"].([]any);plan:=generation.Plan{}
	for _,item:=range items{f,_:=item.(map[string]any);path:=stringArg(f,"path");action:=stringArg(f,"action");if _,err:=rt.resolve(path,action=="delete");err!=nil{return nil,err};if action=="delete"{if err:=rt.Capabilities.Require(security.DeleteFile);err!=nil{return nil,err}};plan.Files=append(plan.Files,generation.FileChange{Path:path,Action:action,Content:stringArg(f,"content")})}
	verifyRequested:=boolArg(a,"verify",true)
	verifierAvailable:=false
	for _,manifest:=range []string{"go.mod","package.json","composer.json","requirements.txt","pyproject.toml"}{if _,e:=os.Stat(filepath.Join(rt.Root,manifest));e==nil{verifierAvailable=true;break}}
	var verifyFn func()VerificationResult
	if verifyRequested&&verifierAvailable {
		verifyFn=func()VerificationResult{paths:=[]string{};for _,f:=range plan.Files{paths=append(paths,f.Path)};v,e:=verify.Detect(rt.Root,paths);if e!=nil{return VerificationResult{Output:e.Error()}};return VerificationResult{Passed:v.Passed,Output:v.Output}}
	}
	executor:=Executor{Root:rt.Root,Capabilities:rt.Capabilities}
	result,err:=executor.Execute(plan,verifyFn)
	if err!=nil{return map[string]any{"applied":false,"changed":result.Changed,"verification":result.Output},err}
	if verifyRequested&&!verifierAvailable{result.Output="Verification skipped: no supported project manifest was found."}
	if rt.Index!=nil{_ = rt.Index.Refresh()};if rt.Store!=nil{_ = rt.Store.MarkTouched(result.Changed);_ = rt.Store.RefreshHashes(result.Changed)}
	return map[string]any{"applied":true,"changed":result.Changed,"verified":verifyRequested&&verifierAvailable,"verification":result.Output},nil
}

func (rt ToolRuntime) projectHealth(a map[string]any)(any,error){
	out:=map[string]any{"workspace":rt.Root,"manifests":[]string{}}
	for _,name:=range []string{"go.mod","package.json","composer.json","pyproject.toml","requirements.txt","Cargo.toml","pom.xml","Makefile"}{if _,e:=os.Stat(filepath.Join(rt.Root,name));e==nil{out["manifests"]=append(out["manifests"].([]string),name)}}
	if boolArg(a,"include_runtimes",true){runtimes:=map[string]string{};for _,name:=range []string{"go","node","npm","php","python","python3","cargo","java","make"}{if p,e:=exec.LookPath(name);e==nil{runtimes[name]=p}};out["runtimes"]=runtimes}
	if boolArg(a,"include_git",true){s,e:=gitlocal.StatusOf(rt.Root);if e==nil{out["git"]=s}else{out["git_error"]=e.Error()}}
	out["read_only"]=true
	return out,nil
}

func (rt ToolRuntime) terminalRun(ctx context.Context,a map[string]any)(any,error){
	exe:=stringArg(a,"executable");args:=stringArray(a["args"]);if err:=rt.Capabilities.Require(security.RunBuild);err!=nil&&exe!="go"&&exe!="npm"&&exe!="node"&&exe!="python"&&exe!="python3"&&exe!="php"&&exe!="make"&&exe!="cargo"{return nil,err}
	if exe=="php"&&len(args)==2&&args[0]=="-l"{resolved,err:=rt.resolve(args[1],false);if err!=nil{return nil,err};args[1]=resolved}
	decision:=security.Authorize(security.Command{Name:exe,Args:args});if !decision.Allowed{return nil,fmt.Errorf("command blocked by security policy: %s",decision.Reason)}
	cwd:=rt.Root;if rel:=stringArg(a,"cwd");rel!=""{p,e:=rt.resolve(rel,false);if e!=nil{return nil,e};info,e:=os.Stat(p);if e!=nil||!info.IsDir(){return nil,errors.New("cwd must be an existing workspace directory")};cwd=p}
	timeout:=time.Duration(intArg(a,"timeout_seconds",60))*time.Second;cctx,cancel:=context.WithTimeout(ctx,timeout);defer cancel()
	cmd:=exec.CommandContext(cctx,exe,args...);cmd.Dir=cwd;cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");var stdout,stderr limitedBuffer;stdout.limit=64<<10;stderr.limit=64<<10;cmd.Stdout=&stdout;cmd.Stderr=&stderr
	err:=cmd.Run();exit:=0;if err!=nil{var ee *exec.ExitError;if errors.As(err,&ee){exit=ee.ExitCode()}else if cctx.Err()==nil{return nil,fmt.Errorf("run command: %w",err)}else{exit=-1}}
	return map[string]any{"exit_code":exit,"stdout":stdout.String(),"stderr":stderr.String(),"timed_out":errors.Is(cctx.Err(),context.DeadlineExceeded),"error":errorString(err)},nil
}
type limitedBuffer struct{bytes.Buffer;limit int}
func(b *limitedBuffer)Write(p []byte)(int,error){n:=len(p);remaining:=b.limit-b.Len();if remaining>0{if remaining>len(p){remaining=len(p)};_,_=b.Buffer.Write(p[:remaining])};return n,nil}
func errorString(err error)string{if err==nil{return ""};return err.Error()}

func (rt ToolRuntime) verifyRun(ctx context.Context,a map[string]any)(any,error){
	checks:=stringArray(a["checks"]);timeout:=time.Duration(intArg(a,"timeout_seconds",120))*time.Second;requested:=stringArray(a["paths"]);validated:=make([]string,0,len(requested))
	for _,path:=range requested{resolved,err:=rt.resolve(path,false);if err!=nil{return nil,fmt.Errorf("verification path %q: %w",path,err)};info,err:=os.Stat(resolved);if err!=nil{return nil,err};if !info.IsDir()&&!info.Mode().IsRegular(){return nil,fmt.Errorf("verification path %q is not a regular file or directory",path)};validated=append(validated,filepath.ToSlash(path))}
	results:=[]map[string]any{}
	for _,check:=range checks{
		if err:=ctx.Err();err!=nil{return nil,err}
		exe,args,err:=checkCommand(rt.Root,check,validated)
		entry:=map[string]any{"check":check,"status":"skipped","exit_code":-1,"duration_ms":int64(0),"diagnostics":[]any{}}
		if len(validated)>0 {entry["requested_paths"]=validated}
		if err!=nil{entry["diagnostic"]=err.Error();results=append(results,entry);continue}
		if len(validated)>0&&exe!="go"&&exe!="gofmt"{entry["scope"]="project-wide; the manifest script does not support path-scoped execution"}else if len(validated)>0{entry["scope"]="requested paths"}else{entry["scope"]="project"}
		if _,e:=exec.LookPath(exe);e!=nil{entry["diagnostic"]=exe+" is not installed";results=append(results,entry);continue}
		decision:=security.Authorize(security.Command{Name:exe,Args:args});if !decision.Allowed{entry["diagnostic"]="blocked by security policy: "+decision.Reason;results=append(results,entry);continue}
		cctx,cancel:=context.WithTimeout(ctx,timeout);start:=time.Now();cmd:=exec.CommandContext(cctx,exe,args...);cmd.Dir=rt.Root;var out limitedBuffer;out.limit=256<<10;cmd.Stdout=&out;cmd.Stderr=&out;runErr:=cmd.Run();cancel();entry["duration_ms"]=time.Since(start).Milliseconds();entry["exit_code"]=0;entry["status"]="passed"
		if runErr!=nil{entry["status"]="failed";var ee *exec.ExitError;if errors.As(runErr,&ee){entry["exit_code"]=ee.ExitCode()}else{entry["exit_code"]=-1};entry["diagnostic"]=out.String()}else if check=="format"&&exe=="gofmt"&&strings.TrimSpace(out.String())!=""{entry["status"]="failed";entry["exit_code"]=1;entry["diagnostic"]=out.String()}else{entry["diagnostic"]=out.String()}
		results=append(results,entry)
	}
	return map[string]any{"checks":results,"all_passed":allPassed(results),"count":len(results)},nil
}
func checkCommand(root,check string,paths []string)(string,[]string,error){
	has:=func(name string)bool{_,err:=os.Stat(filepath.Join(root,name));return err==nil}
	goPackages:=[]string{"./..."}
	if len(paths)>0 {goPackages=packagePaths(root,paths)}
	switch check{
	case "format":
		if has("go.mod"){args:=[]string{"-l"};if len(paths)==0{return "gofmt",[]string{"-l","."},nil};files,err:=goFilesForPaths(root,paths);if err!=nil{return "",nil,err};if len(files)==0{return "",nil,errors.New("no Go files found in the requested paths")};return "gofmt",append(args,files...),nil}
		if has("package.json"){return "npm",[]string{"run","format:check"},nil}
		return "",nil,errors.New("no supported format-check command detected")
	case "lint":
		if has("go.mod"){return "go",append([]string{"vet"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"run","lint"},nil}
		return "",nil,errors.New("no supported lint command detected")
	case "test":
		if has("go.mod"){return "go",append([]string{"test"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"test"},nil}
		return "",nil,errors.New("no supported test command detected")
	case "build":
		if has("go.mod"){return "go",append([]string{"build"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"run","build"},nil}
		return "",nil,errors.New("no supported build command detected")
	case "typecheck":
		if has("go.mod"){return "go",append([]string{"test","-run","^$"},goPackages...),nil}
		if has("package.json"){return "npm",[]string{"run","typecheck"},nil}
		return "",nil,errors.New("no supported typecheck command detected")
	default:return "",nil,fmt.Errorf("unsupported check %q",check)
	}
}
func packagePaths(root string,paths []string)[]string{
	seen:=map[string]bool{};out:=[]string{}
	for _,rel:=range paths{full:=filepath.Join(root,filepath.FromSlash(rel));info,err:=os.Stat(full);if err!=nil{continue};dir:=full;if !info.IsDir(){dir=filepath.Dir(full)};relative,err:=filepath.Rel(root,dir);if err!=nil{continue};pkg:="./"+filepath.ToSlash(relative);if relative=="."{pkg="./..."};if !seen[pkg]{seen[pkg]=true;out=append(out,pkg)}}
	sort.Strings(out);if len(out)==0{return []string{"./..."}};return out
}
func goFilesForPaths(root string,paths []string)([]string,error){
	var files []string;seen:=map[string]bool{}
	for _,rel:=range paths{full:=filepath.Join(root,filepath.FromSlash(rel));info,err:=os.Stat(full);if err!=nil{return nil,err};if info.Mode().IsRegular(){if filepath.Ext(full)==".go"{seen[full]=true};continue}
		err=filepath.WalkDir(full,func(path string,d os.DirEntry,e error)error{if e!=nil{return e};if d.IsDir()&&path!=full&&(d.Name()==".git"||d.Name()==".aicli"||d.Name()=="vendor"){return filepath.SkipDir};if d.IsDir(){return nil};if filepath.Ext(path)==".go"{seen[path]=true};return nil});if err!=nil{return nil,err}
	}
	for path:=range seen{rel,err:=filepath.Rel(root,path);if err!=nil{return nil,err};files=append(files,filepath.ToSlash(rel))};sort.Strings(files);return files,nil
}
func allPassed(results []map[string]any)bool{if len(results)==0{return false};for _,r:=range results{if r["status"]!="passed"{return false}};return true}

func (rt ToolRuntime) gitStatus(a map[string]any)(any,error){s,e:=gitlocal.StatusOf(rt.Root);if e!=nil{return nil,e};if !boolArg(a,"include_untracked",true){filtered:=s.Files[:0];for _,f:=range s.Files{if f.Index!='?'&&f.Worktree!='?'{filtered=append(filtered,f)}};s.Files=filtered;s.Clean=len(filtered)==0};return s,nil}
func (rt ToolRuntime) gitDiff(a map[string]any)(any,error){
	path:=stringArg(a,"path")
	if path!=""{if _,e:=rt.resolve(path,false);e!=nil{return nil,e}}
	args:=[]string{"diff","-U"+strconv.Itoa(intArg(a,"context_lines",3))}
	if boolArg(a,"staged",false){args=append(args,"--cached")}
	if path!=""{args=append(args,"--",path)}else{args=append(args,"--")}
	diff,e:=runGitOutput(context.Background(),rt.Root,args...);if e!=nil{return nil,e}
	return map[string]any{"diff":truncate(diff,256<<10),"truncated":len(diff)>256<<10},nil
}
func ctxOrBackground()context.Context{return context.Background()}
func diffArgs(staged bool,path string)[]string{args:=[]string{"diff"};if staged{args=append(args,"--cached")};return append(args,"--",path)}
func runGitOutput(ctx context.Context,root string,args ...string)(string,error){cmd:=exec.CommandContext(ctx,"git",args...);cmd.Dir=root;b,e:=cmd.CombinedOutput();if e!=nil{return "",fmt.Errorf("git %v: %s",args,strings.TrimSpace(string(b)))};return string(b),nil}
func (rt ToolRuntime) gitCommit(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	if e:=rt.Capabilities.Require(security.RunGit);e!=nil{return nil,e}
	head,e:=runGitOutput(ctx,rt.Root,"rev-parse","HEAD");if e!=nil{return nil,e};if strings.TrimSpace(head)!=stringArg(a,"expected_head"){return nil,errors.New("repository HEAD changed; refresh status and try again")}
	paths:=stringArray(a["paths"]);for _,p:=range paths{if _,e:=rt.resolve(p,true);e!=nil{return nil,e}}
	status,e:=gitlocal.StatusOf(rt.Root);if e!=nil{return nil,e};allowed:=map[string]bool{};for _,p:=range paths{allowed[filepath.ToSlash(p)]=true};for _,f:=range status.Files{if !allowed[f.Path]{continue};if f.Index!=' '&&f.Index!='?'{return nil,fmt.Errorf("selected path %s already has staged changes; refusing to commit unrelated index content",f.Path)}}
	addArgs:=append([]string{"add","--"},paths...);cmd:=exec.CommandContext(ctx,"git",addArgs...);cmd.Dir=rt.Root;if out,e:=cmd.CombinedOutput();e!=nil{return nil,fmt.Errorf("git add failed: %s",strings.TrimSpace(string(out)))}
	commitArgs:=append([]string{"commit","-m",stringArg(a,"message"),"--"},paths...);cmd=exec.CommandContext(ctx,"git",commitArgs...);cmd.Dir=rt.Root;out,e:=cmd.CombinedOutput();if e!=nil{return nil,fmt.Errorf("git commit failed: %s",strings.TrimSpace(string(out)))}
	newHead,e:=runGitOutput(ctx,rt.Root,"rev-parse","HEAD");if e!=nil{return nil,e};return map[string]any{"committed":true,"head":strings.TrimSpace(newHead),"output":strings.TrimSpace(string(out))},nil
}

func (rt ToolRuntime) githubClient()( *github.Client,error){token,e:=config.GitHubToken();if e!=nil{return nil,e};configured,e:=config.GitHubTokenConfigured();if e!=nil{return nil,e};c:=github.New(token);c.DisableCLI=configured&&strings.TrimSpace(token)=="";if !c.Authenticated(){return nil,errors.New("GitHub is not authenticated; connect with the supported GitHub browser/device login first")};return c,nil}
func (rt ToolRuntime) githubAuth(ctx context.Context,a map[string]any)(any,error){
	action:=stringArg(a,"action")
	switch action{
	case "status": c,e:=rt.githubClient();if e!=nil{return map[string]any{"authenticated":false,"message":e.Error()},nil};u,e:=c.Profile(ctx);if e!=nil{return nil,e};return map[string]any{"authenticated":true,"user":u.Login,"profile_url":u.HTMLURL},nil
	case "login_start":
		if _,err:=exec.LookPath("gh");err!=nil{return nil,errors.New("GitHub CLI (gh) is required for browser sign-in")}
		cctx,cancel:=context.WithTimeout(ctx,120*time.Second);defer cancel()
		cmd:=exec.CommandContext(cctx,"gh","auth","login","--web","--hostname","github.com");var out limitedBuffer;out.limit=16<<10;cmd.Stdout=&out;cmd.Stderr=&out
		if err:=cmd.Run();err!=nil{return map[string]any{"started":false,"output":out.String(),"timed_out":errors.Is(cctx.Err(),context.DeadlineExceeded)},fmt.Errorf("GitHub browser sign-in did not complete: %w",err)}
		return map[string]any{"started":true,"authenticated":true,"output":out.String()},nil
	case "login_status":
		if _,err:=exec.LookPath("gh");err!=nil{return map[string]any{"authenticated":false,"message":"GitHub CLI is not installed"},nil}
		cctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();cmd:=exec.CommandContext(cctx,"gh","auth","status","--hostname","github.com");out,err:=cmd.CombinedOutput()
		return map[string]any{"authenticated":err==nil,"output":truncate(string(out),4000)},nil
	case "disconnect":
		if err:=config.ClearGitHubToken();err!=nil{return nil,err}
		return map[string]any{"disconnected":true,"note":"Stored Studio token cleared. This does not revoke or log out the underlying GitHub CLI session."},nil
	default:return nil,errors.New("unsupported GitHub auth action")
	}
}
func (rt ToolRuntime) githubRepositories(ctx context.Context,a map[string]any)(any,error){c,e:=rt.githubClient();if e!=nil{return nil,e};if stringArg(a,"action")=="get"{return c.Repository(ctx,stringArg(a,"repository"))};return c.Repositories(ctx,intArg(a,"page",1),intArg(a,"per_page",30))}
func (rt ToolRuntime) githubBranches(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){c,e:=rt.githubClient();if e!=nil{return nil,e};repo:=stringArg(a,"repository");if stringArg(a,"action")=="list"{return c.BranchesPage(ctx,repo,intArg(a,"page",1),100)};if e=c.CreateBranch(ctx,repo,stringArg(a,"branch"),stringArg(a,"base"));e!=nil{return nil,e};return map[string]any{"created":true,"branch":stringArg(a,"branch")},nil}
func (rt ToolRuntime) githubSync(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	op:=stringArg(a,"operation");if op=="push"{}
	c,e:=rt.githubClient();if e!=nil{return nil,e};if e=rt.Capabilities.Require(security.RunGit);e!=nil{return nil,e}
	status,e:=gitlocal.StatusOf(rt.Root);if e!=nil{return nil,e};if !status.Clean{return nil,errors.New("sync refused: commit or stash local changes first")}
	head,e:=runGitOutput(ctx,rt.Root,"rev-parse","HEAD");if e!=nil{return nil,e};if expected:=stringArg(a,"expected_head");expected!=""&&strings.TrimSpace(head)!=expected{return nil,errors.New("local HEAD changed; refresh repository status")}
	" + `).MatchString(remote)||strings.HasPrefix(remote,"-"){return nil,errors.New("remote must be a valid local Git remote name")};branch:=stringArg(a,"branch");if branch!=""&&!validGitRefName(branch){return nil,errors.New("branch must be a valid simple Git ref name")}
	token:=c.Token;var output string;if op=="pull"{output,e=github.Pull(ctx,rt.Root,remote,branch,token)}else{output,e=github.Push(ctx,rt.Root,remote,branch,token)};if e!=nil{return nil,e};return map[string]any{"operation":op,"output":output},nil
}
func (rt ToolRuntime) githubPullRequests(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	c,e:=rt.githubClient();if e!=nil{return nil,e};repo:=stringArg(a,"repository");n:=intArg(a,"number",0)
	switch stringArg(a,"action"){
	case "list":return c.PullRequests(ctx,repo,stringArg(a,"state"))
	case "create":return c.CreatePullRequest(ctx,repo,github.CreatePullRequestRequest{Title:stringArg(a,"title"),Body:stringArg(a,"body"),Head:stringArg(a,"head"),Base:stringArg(a,"base"),Draft:boolArg(a,"draft",false)})
	case "comment":return c.AddComment(ctx,repo,n,stringArg(a,"body"))
	case "review":return map[string]any{"submitted":true},c.Review(ctx,repo,n,github.ReviewRequest{Body:stringArg(a,"body"),Event:stringArg(a,"event")})
	case "merge": return c.MergePullRequest(ctx,repo,n,stringArg(a,"merge_method"),stringArg(a,"expected_head_sha"))
	default:return nil,errors.New("unsupported pull request action")
	}
}
func (rt ToolRuntime) githubIssues(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	c,e:=rt.githubClient();if e!=nil{return nil,e};repo:=stringArg(a,"repository");n:=intArg(a,"number",0)
	switch stringArg(a,"action"){
	case "list":return c.Issues(ctx,repo,stringArg(a,"state"))
	case "create":var labels,assignees []string;if _,ok:=a["labels"];ok{labels=stringArray(a["labels"])};if _,ok:=a["assignees"];ok{assignees=stringArray(a["assignees"])};return c.CreateIssueDetailed(ctx,repo,stringArg(a,"title"),stringArg(a,"body"),labels,assignees)
	case "comment":return c.AddComment(ctx,repo,n,stringArg(a,"body"))
	case "update":
		if e=rt.approve(ctx,"github.issues.update",raw);e!=nil{return nil,e}
		update:=github.UpdateIssueRequest{Title:stringArg(a,"title"),Body:stringArg(a,"body"),State:stringArg(a,"state"),Labels:stringArray(a["labels"]),Assignees:stringArray(a["assignees"])}
		if _,ok:=a["labels"];!ok{update.Labels=nil};if _,ok:=a["assignees"];!ok{update.Assignees=nil}
		return c.UpdateIssue(ctx,repo,n,update)
	default:return nil,errors.New("unsupported issue action")
	}
}
func (rt ToolRuntime) githubActions(ctx context.Context,a map[string]any,raw json.RawMessage)(any,error){
	c,e:=rt.githubClient();if e!=nil{return nil,e}
	switch stringArg(a,"action"){
	case "list_runs":return c.WorkflowRunsPage(ctx,stringArg(a,"repository"),intArg(a,"page",1),intArg(a,"per_page",30))
	case "list_jobs":return c.WorkflowJobsPage(ctx,stringArg(a,"repository"),int64(intArg(a,"run_id",0)),intArg(a,"page",1),intArg(a,"per_page",30))
	case "job_logs":
		if _,e:=exec.LookPath("gh");e!=nil{return nil,errors.New("GitHub Actions job logs require the authenticated gh CLI")}
		cmd:=exec.CommandContext(ctx,"gh","run","view","--job",strconv.Itoa(intArg(a,"job_id",0)),"--log","--repo",stringArg(a,"repository"))
		var out limitedBuffer;out.limit=256<<10;cmd.Stdout=&out;cmd.Stderr=&out
		if e:=cmd.Run();e!=nil{return map[string]any{"logs":truncate(out.String(),256<<10),"truncated":out.Len()>=256<<10},fmt.Errorf("retrieve GitHub job logs: %w",e)}
		return map[string]any{"logs":out.String(),"truncated":out.Len()>=256<<10},nil
	case "rerun_failed":if _,e=exec.LookPath("gh");e==nil{
		cmd:=exec.CommandContext(ctx,"gh","run","rerun",strconv.Itoa(intArg(a,"run_id",0)),"--failed","--repo",stringArg(a,"repository"))
		out,runErr:=cmd.CombinedOutput();if runErr!=nil{return nil,fmt.Errorf("rerun failed GitHub Actions jobs: %s",truncate(string(out),4000))}
		return map[string]any{"rerun_requested":true,"output":truncate(string(out),4000)},nil
	}
	return nil,errors.New("rerunning only failed jobs requires authenticated gh CLI")
	default:return nil,errors.New("unsupported GitHub Actions action")
	}
}
func (rt ToolRuntime) contextGet(a map[string]any)(any,error){
	scope:=stringArg(a,"scope");limit:=intArg(a,"max_items",10);query:=strings.ToLower(stringArg(a,"query"))
	switch scope{
	case "task":
		if query==""{return map[string]any{"scope":scope,"available":false,"message":"Supply a task_id in query to retrieve a saved checkpoint."},nil}
		if strings.ContainsAny(query,"/\\")||query=="."||query==".."{return nil,errors.New("task query must be a simple checkpoint identifier")}
		path:=filepath.Join(rt.Root,".aicli","checkpoints",query+".json");data,err:=os.ReadFile(path)
		if os.IsNotExist(err){return map[string]any{"scope":scope,"available":false,"message":"No saved checkpoint exists for that task_id."},nil};if err!=nil{return nil,err}
		var checkpoint map[string]any;if err=json.Unmarshal(data,&checkpoint);err!=nil{return nil,fmt.Errorf("checkpoint is invalid: %w",err)}
		return map[string]any{"scope":scope,"available":true,"checkpoint":checkpoint},nil
	case "project":if rt.Index==nil{return map[string]any{"scope":scope,"available":false,"message":"Repository index unavailable."},nil};return map[string]any{"scope":scope,"available":true,"root":rt.Root,"files":rt.Index.Relevant(query,limit),"summary":rt.Index.Summary()},nil
	case "recent_history":if rt.Store==nil{return nil,errors.New("workspace history store unavailable")};h,e:=rt.Store.History(200);if e!=nil{return nil,e};out:=[]map[string]string{};for i:=len(h)-1;i>=0&&len(out)<limit;i--{if query==""||strings.Contains(strings.ToLower(h[i].Content),query){out=append(out,map[string]string{"role":h[i].Role,"content":truncate(h[i].Content,2000)})}};return map[string]any{"scope":scope,"available":true,"messages":out},nil
	default:return nil,errors.New("unsupported context scope")
	}
}
func (rt ToolRuntime) checkpoint(a map[string]any)(any,error){
	id:=stringArg(a,"task_id");if strings.ContainsAny(id,"/\\")||id=="."||id==".."{return nil,errors.New("task_id must be a simple identifier")}
	dir:=filepath.Join(rt.Root,".aicli","checkpoints");if e:=os.MkdirAll(dir,0700);e!=nil{return nil,e}
	summary:=stringArg(a,"summary");verification:=stringArg(a,"verification")
	if containsCredentialLikeText(summary)||containsCredentialLikeText(verification){return nil,errors.New("checkpoint appears to contain credential-like text; remove secrets before saving")}
	data:=map[string]any{"task_id":id,"summary":summary,"completed":a["completed"],"pending":a["pending"],"files_touched":a["files_touched"],"verification":verification,"saved_at":time.Now().UTC()}
	b,e:=json.MarshalIndent(data,"","  ");if e!=nil{return nil,e};target:=filepath.Join(dir,id+".json");tmp:=target+".tmp";if e=os.WriteFile(tmp,b,0600);e!=nil{return nil,e};if e=os.Rename(tmp,target);e!=nil{_ = os.Remove(tmp);return nil,e};return map[string]any{"saved":true,"task_id":id,"path":filepath.ToSlash(filepath.Join(".aicli","checkpoints",id+".json"))},nil
}

func intArg(a map[string]any,key string,def int)int{v,ok:=a[key];if !ok{return def};switch n:=v.(type){case json.Number:i,e:=strconv.Atoi(n.String());if e==nil{return i};case float64:return int(n)};return def}
func stringArg(a map[string]any,key string)string{v,_:=a[key].(string);return v}
func boolArg(a map[string]any,key string,def bool)bool{v,ok:=a[key].(bool);if !ok{return def};return v}
func stringArray(v any)[]string{arr,_:=v.([]any);out:=make([]string,0,len(arr));for _,x:=range arr{if s,ok:=x.(string);ok{out=append(out,s)}};return out}
func truncate(s string,max int)string{if len(s)>max{return s[:max]};return s}
func jsonString(v any)string{b,_:=json.Marshal(v);return string(b)}

func containsCredentialLikeText(s string) bool {
	lower:=strings.ToLower(s)
	for _,marker:=range []string{"ghp_","github_pat_","bearer ","api_key=","api-key=","password=","secret=","private_key"}{if strings.Contains(lower,marker){return true}}
	return false
}

func validRepositoryIdentifier(value string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(value)
}
func validGitRefName(value string) bool {
	if value==""||strings.HasPrefix(value,"-")||strings.HasPrefix(value,"/")||strings.HasSuffix(value,"/")||strings.Contains(value,"..")||strings.Contains(value,"//")||strings.ContainsAny(value," ~^:?*[\\"){return false}
	for _,part:=range strings.Split(value,"/"){if part==""||strings.HasPrefix(part,".")||strings.HasSuffix(part,".lock"){return false}}
	return true
}
