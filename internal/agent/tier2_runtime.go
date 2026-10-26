package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/security"
)

const tier2OutputLimit = 64 << 10

func (rt ToolRuntime) executeTier2(ctx context.Context, name string, raw json.RawMessage, a map[string]any) (any, error) {
	switch name {
	case "package.inspect": return rt.packageInspect(a)
	case "package.install": return rt.packageChange(ctx, name, raw, a, false)
	case "package.update": return rt.packageChange(ctx, name, raw, a, true)
	case "package.audit": return rt.packageAudit(ctx, a)
	case "docs.search": return rt.docsSearch(a)
	case "docs.generate": return rt.docsGenerate(ctx, raw, a)
	case "build.inspect": return rt.buildInspect(a)
	case "build.run": return rt.buildRun(ctx, raw, a)
	case "api.inspect": return rt.apiInspect(a)
	case "database.migrate": return rt.databaseMigrate(ctx, raw, a)
	case "container.inspect": return rt.containerInspect(ctx, a)
	case "container.run": return rt.containerRun(ctx, raw, a)
	default: return nil, fmt.Errorf("Tier 2 handler not implemented: %s", name)
	}
}

func (rt ToolRuntime) packageInspect(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	manager := stringArg(a, "manager"); if manager == "" { manager = "auto" }
	maxItems := intArg(a, "max_items", 100)
	manifests := []string{"go.mod","package.json","pnpm-lock.yaml","yarn.lock","bun.lockb","bun.lock","requirements.txt","pyproject.toml","Pipfile","composer.json","Cargo.toml"}
	found := []map[string]any{}
	for _, rel := range manifests {
		if manager != "auto" && !manifestMatchesManager(rel, manager) { continue }
		p, err := rt.resolve(rel, false); if err != nil { continue }
		data, err := os.ReadFile(p); if err != nil { continue }
		entry := map[string]any{"path": rel, "bytes": len(data)}
		switch rel {
		case "package.json", "composer.json":
			var doc map[string]any
			if json.Unmarshal(data, &doc) == nil {
				entry["name"] = doc["name"]
				deps := map[string]any{}
				for _, key := range []string{"dependencies","devDependencies","require","require-dev"} {
					if m, ok := doc[key].(map[string]any); ok { for k,v := range m { deps[k] = v } }
				}
				entry["dependencies"] = tier2BoundedMap(deps, maxItems)
				if scripts, ok := doc["scripts"].(map[string]any); ok { entry["scripts"] = tier2BoundedMap(scripts, 100) }
			}
		case "go.mod":
			lines := strings.Split(string(data), "\n"); deps := []string{}
			for _, line := range lines { t := strings.TrimSpace(line); if strings.HasPrefix(t,"require ") || (strings.HasPrefix(t,"github.com/") || strings.HasPrefix(t,"golang.org/") || strings.HasPrefix(t,"modernc.org/")) { deps = append(deps,t) } }
			if len(deps)>maxItems { deps=deps[:maxItems] }; entry["declared_dependencies"]=deps
		case "requirements.txt":
			deps:=[]string{}; for _, line:=range strings.Split(string(data),"\n") { t:=strings.TrimSpace(line); if t!="" && !strings.HasPrefix(t,"#") { deps=append(deps,t) } }; if len(deps)>maxItems {deps=deps[:maxItems]}; entry["declared_dependencies"]=deps
		case "pyproject.toml", "Pipfile", "Cargo.toml":
			entry["note"]="manifest detected; dependency declarations are available in the file"
		}
		found=append(found,entry)
	}
	return map[string]any{"manager":manager,"manifests":found,"count":len(found)},nil
}

func manifestMatchesManager(path, manager string) bool {
	switch manager {
	case "go": return path=="go.mod"
	case "npm","pnpm","yarn","bun": return path=="package.json" || path=="pnpm-lock.yaml"&&manager=="pnpm" || path=="yarn.lock"&&manager=="yarn" || (path=="bun.lock"||path=="bun.lockb")&&manager=="bun"
	case "python": return path=="requirements.txt"||path=="pyproject.toml"||path=="Pipfile"
	case "php": return path=="composer.json"
	}
	return true
}
func tier2BoundedMap(input map[string]any, limit int) map[string]any {
	keys:=make([]string,0,len(input)); for k:=range input {keys=append(keys,k)}; sort.Strings(keys)
	out:=map[string]any{}; for i,k:=range keys {if i>=limit {break};out[k]=input[k]}; return out
}

var safePackageToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+@:-]{0,199}$`)
var safeContainerImage = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)

func (rt ToolRuntime) packageChange(ctx context.Context, name string, raw json.RawMessage, a map[string]any, update bool) (any,error) {
	if err:=rt.Capabilities.Require(security.RunBuild); err!=nil {return nil,err}
	if err:=rt.Capabilities.Require(security.InstallDependency); err!=nil {return nil,err}
	manager:=stringArg(a,"manager")
	packages:=tier2StringArrayArg(a,"packages")
	for _,p:=range packages {
		if strings.HasPrefix(p,"-") || strings.ContainsAny(p,"\x00\r\n;|&$") {return nil,fmt.Errorf("unsafe package specifier %q",p)}
		if manager=="python" { if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}([<>=!~]{1,2}[A-Za-z0-9.*+_-]{1,50})?$`).MatchString(p) {return nil,fmt.Errorf("unsupported Python package specifier %q",p)} } else if !safePackageToken.MatchString(p) {return nil,fmt.Errorf("unsupported package specifier %q",p)}
	}
	if update && len(packages)==0 { /* update all supported declared dependencies */ }
	exe,args,err:=packageCommand(manager,packages,boolArg(a,"dev",false),update)
	if err!=nil{return nil,err}
	if boolArg(a,"dry_run",false) {return map[string]any{"executed":false,"manager":manager,"command":append([]string{exe},args...),"reason":"dry run; no files or packages changed"},nil}
	if err:=rt.Capabilities.Require(security.NetworkAccess); err!=nil { return nil,err }
	if !hasToolApproval(ctx,name,raw){return nil,fmt.Errorf("%s requires explicit approval for this exact operation",name)}
	timeout:=time.Duration(intArg(a,"timeout_seconds",180))*time.Second
	result,err:=rt.runTier2Command(ctx,exe,args,".",timeout)
	if err!=nil{return result,err}
	return result,nil
}

func packageCommand(manager string, packages []string, dev, update bool) (string,[]string,error) {
	switch manager {
	case "go":
		if update {args:=[]string{"get","-u"}; if len(packages)==0 {args=append(args,"./...")} else {args=append(args,packages...)}; return "go",args,nil}
		if len(packages)==0{return "",nil,errors.New("go install requires at least one module/package")}
		return "go",append([]string{"get"},packages...),nil
	case "npm","pnpm","yarn","bun":
		if update {
			switch manager {case "npm": return "npm",append([]string{"update"},packages...),nil; case "pnpm":return "pnpm",append([]string{"update"},packages...),nil;case "yarn":return "yarn",append([]string{"up"},packages...),nil;default:return "bun",append([]string{"update"},packages...),nil}
		}
		switch manager {case "npm": args:=[]string{"install"};if dev{args=append(args,"--save-dev")};return "npm",append(args,packages...),nil
		case "pnpm":args:=[]string{"add"};if dev{args=append(args,"-D")};return "pnpm",append(args,packages...),nil
		case "yarn":args:=[]string{"add"};if dev{args=append(args,"-D")};return "yarn",append(args,packages...),nil
		default:args:=[]string{"add"};if dev{args=append(args,"-d")};return "bun",append(args,packages...),nil}
	case "python":
		if len(packages)==0{return "",nil,errors.New("Python package operation requires package names")}
		args:=[]string{"-m","pip","install"};if update{args=append(args,"--upgrade")};return "python",append(args,packages...),nil
	case "php":
		if update {args:=[]string{"update"};if len(packages)>0{args=append(args,packages...)};return "composer",args,nil}
		if len(packages)==0{return "",nil,errors.New("Composer require needs package names")}
		args:=[]string{"require"};if dev{args=append(args,"--dev")};return "composer",append(args,packages...),nil
	default:return "",nil,fmt.Errorf("unsupported package manager %q",manager)
	}
}

func (rt ToolRuntime) packageAudit(ctx context.Context,a map[string]any)(any,error) {
	if err:=rt.Capabilities.Require(security.RunBuild);err!=nil{return nil,err}
	manager:=stringArg(a,"manager");if manager==""||manager=="auto" {
		if tier2Exists(rt.Root,"go.mod"){manager="go"}else if tier2Exists(rt.Root,"package.json"){manager="npm"}else if tier2Exists(rt.Root,"composer.json"){manager="php"}else if tier2Exists(rt.Root,"requirements.txt")||tier2Exists(rt.Root,"pyproject.toml"){manager="python"}else{return nil,errors.New("no supported dependency manifest found")}
	}
	var exe string;var args []string
	switch manager {case "go":if _,e:=exec.LookPath("govulncheck");e==nil{exe="govulncheck";args=[]string{"./..."} }else{return map[string]any{"executed":false,"available":false,"manager":manager,"message":"govulncheck is not installed; go mod verify checks module integrity, not known vulnerabilities"},nil}
	case "npm":exe="npm";args=[]string{"audit","--json"}
	case "pnpm":exe="pnpm";args=[]string{"audit","--json"}
	case "yarn":exe="yarn";args=[]string{"audit","--json"}
	case "php":exe="composer";args=[]string{"audit","--format=json"}
	case "python":if _,e:=exec.LookPath("pip-audit");e==nil{exe="pip-audit";args=[]string{"--format","json"}}else{return map[string]any{"executed":false,"manager":manager,"available":false,"message":"pip-audit is not installed; no dependency was installed automatically"},nil}
	default:return nil,fmt.Errorf("unsupported package manager %q",manager)}
	if err:=rt.Capabilities.Require(security.NetworkAccess); err!=nil { return nil,err }
	return rt.runTier2Command(ctx,exe,args,".",time.Duration(intArg(a,"timeout_seconds",120))*time.Second)
}

func (rt ToolRuntime) docsSearch(a map[string]any)(any,error) {
	if err:=rt.Capabilities.Require(security.ReadWorkspace);err!=nil{return nil,err}
	query:=strings.ToLower(stringArg(a,"query"));limit:=intArg(a,"max_results",30)
	paths:=tier2StringArrayArg(a,"paths");allowed:=map[string]bool{}
	for _,p:=range paths{resolved,e:=rt.resolve(p,false);if e!=nil{return nil,e};info,e:=os.Stat(resolved);if e!=nil{return nil,e};if info.IsDir(){return nil,fmt.Errorf("docs.search paths must be files, not directories: %s",p)};allowed[filepath.ToSlash(filepath.Clean(p))]=true}
	results:=[]map[string]any{}
	err:=filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,e error)error{
		if e!=nil{return nil};if path==rt.Root{return nil};rel,_:=filepath.Rel(rt.Root,path);rel=filepath.ToSlash(rel)
		if d.IsDir(){if rel==".git"||rel==".aicli"||strings.HasPrefix(rel,".git/")||strings.HasPrefix(rel,".aicli/"){return filepath.SkipDir};return nil}
		if len(allowed)>0&&!allowed[rel]{return nil}
		lower:=strings.ToLower(rel);if !(strings.HasSuffix(lower,".md")||strings.HasSuffix(lower,".mdx")||strings.HasSuffix(lower,".rst")||strings.HasSuffix(lower,".txt")) {return nil}
		resolved,er:=rt.resolve(rel,false);if er!=nil{return nil};data,er:=os.ReadFile(resolved);if er!=nil||len(data)>1<<20{return nil}
		for i,line:=range strings.Split(string(data),"\n"){if strings.Contains(strings.ToLower(line),query){results=append(results,map[string]any{"path":rel,"line":i+1,"text":tier2TruncateString(strings.TrimSpace(line),500)});if len(results)>=limit{return errToolWalkLimit}}}
		return nil
	})
	if err!=nil&&!errors.Is(err,errToolWalkLimit){return nil,err}
	return map[string]any{"query":query,"results":results,"count":len(results),"truncated":errors.Is(err,errToolWalkLimit)},nil
}
func tier2TruncateString(s string,n int)string{if len(s)>n{return s[:n]+"…"};return s}

func (rt ToolRuntime) docsGenerate(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.WriteWorkspace);err!=nil{return nil,err}
	rel:=filepath.ToSlash(filepath.Clean(stringArg(a,"path")))
	if !strings.HasPrefix(rel,"docs/")||!strings.HasSuffix(strings.ToLower(rel),".md"){return nil,errors.New("docs.generate only creates .md files inside docs/")}
	path,err:=rt.resolve(rel,false);if err!=nil{return nil,err}
	title:=strings.TrimSpace(stringArg(a,"title"));body:=stringArg(a,"content");if !strings.HasPrefix(strings.TrimSpace(body),"#"){body="# "+title+"\n\n"+body}
	if len(body)>50000{return nil,errors.New("generated documentation exceeds 50 KB")}
	if _,err:=os.Stat(path);err==nil{return nil,errors.New("docs.generate never overwrites an existing file")}else if !os.IsNotExist(err){return nil,err}
	if boolArg(a,"dry_run",false){return map[string]any{"executed":false,"path":rel,"bytes":len(body)},nil}
	if err:=func() error { if hasToolApproval(ctx, "docs.generate", raw) { return nil }; return errors.New("docs.generate requires explicit approval for this exact operation") }();err!=nil{return nil,err}
	if err:=os.MkdirAll(filepath.Dir(path),0755);err!=nil{return nil,err}
	f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0644);if err!=nil{return nil,err}
	if _,err=f.WriteString(body+"\n");err!=nil{_ = f.Close();_ = os.Remove(path);return nil,err}
	if err=f.Close();err!=nil{_ = os.Remove(path);return nil,err}
	return map[string]any{"executed":true,"path":rel,"bytes":len(body)+1},nil
}

func (rt ToolRuntime) buildInspect(a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.ReadWorkspace);err!=nil{return nil,err}
	max:=intArg(a,"max_items",50);items:=[]map[string]any{}
	for _,rel:=range []string{"go.mod","Makefile","CMakeLists.txt","Cargo.toml","package.json","pyproject.toml","requirements.txt","composer.json","Dockerfile","docker-compose.yml","compose.yaml","vite.config.js","vite.config.ts","tsconfig.json"}{
		p,e:=rt.resolve(rel,false);if e!=nil{continue};data,e:=os.ReadFile(p);if e!=nil{continue}
		item:=map[string]any{"path":rel,"bytes":len(data)}
		if rel=="package.json"{var doc map[string]any;if json.Unmarshal(data,&doc)==nil{item["scripts"]=tier2BoundedMap(tier2AsAnyMap(doc["scripts"]),max);item["package_manager"]=doc["packageManager"]}}
		items=append(items,item)
	}
	tools:=map[string]bool{};for _,name:=range []string{"go","npm","pnpm","yarn","bun","python","php","composer","docker","podman","make","cargo"}{if _,e:=exec.LookPath(name);e==nil{tools[name]=true}}
	return map[string]any{"manifests":items,"available_tools":tools,"count":len(items)},nil
}
func tier2AsAnyMap(v any)map[string]any{m,_:=v.(map[string]any);if m==nil{return map[string]any{}};return m}

func (rt ToolRuntime) buildRun(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.RunBuild);err!=nil{return nil,err}
	target:=stringArg(a,"target");if target==""||target=="auto"{if tier2Exists(rt.Root,"go.mod"){target="go"}else if tier2Exists(rt.Root,"package.json"){target="npm"}else if tier2Exists(rt.Root,"composer.json"){target="php"}else if tier2Exists(rt.Root,"pyproject.toml"){target="python"}else{return nil,errors.New("could not detect a supported build system")}}
	args:=tier2StringArrayArg(a,"args");var exe string
	if len(args)==0{switch target{case "go":exe="go";args=[]string{"build","./..."};case "npm":exe="npm";args=[]string{"run","build"};case "pnpm":exe="pnpm";args=[]string{"run","build"};case "yarn":exe="yarn";args=[]string{"build"};case "bun":exe="bun";args=[]string{"run","build"};case "python":exe="python";args=[]string{"-m","compileall","-q","."};case "php":return map[string]any{"executed":false,"message":"PHP projects do not have a universal build command; pass an explicitly supported verification argument only after configuring a project script."},nil}}else{switch target{case "go":exe="go";case "npm","pnpm","yarn","bun":exe=target;case "python":exe="python";case "php":exe="php";default:return nil,fmt.Errorf("unsupported build target %q",target)}}
	decision:=security.Authorize(security.Command{Name:exe,Args:args})
	if !decision.Allowed{return nil,fmt.Errorf("build command blocked by command policy: %s",decision.Reason)}
	if boolArg(a,"dry_run",false){return map[string]any{"executed":false,"command":append([]string{exe},args...),"reason":"dry run"},nil}
	if err:=func() error { if hasToolApproval(ctx, "build.run", raw) { return nil }; return errors.New("build.run requires explicit approval for this exact operation") }();err!=nil{return nil,err}
	return rt.runTier2Command(ctx,exe,args,".",time.Duration(intArg(a,"timeout_seconds",180))*time.Second)
}

func (rt ToolRuntime) apiInspect(a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.ReadWorkspace);err!=nil{return nil,err}
	max:=intArg(a,"max_results",100);specified:=stringArg(a,"path")
	routeRE:=regexp.MustCompile(`(?i)(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s*[^A-Za-z0-9/]*(/[A-Za-z0-9_./:{}-]*)`)
	results:=[]map[string]any{};files:=[]string{}
	if specified!=""{p,e:=rt.resolve(specified,false);if e!=nil{return nil,e};data,e:=os.ReadFile(p);if e!=nil{return nil,e};files=append(files,specified);_ = data}else{
		_ = filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,e error)error{if e!=nil{return nil};if d.IsDir(){rel,_:=filepath.Rel(rt.Root,path);if rel==".git"||rel==".aicli"{return filepath.SkipDir};return nil};rel,_:=filepath.Rel(rt.Root,path);rel=filepath.ToSlash(rel);ext:=strings.ToLower(filepath.Ext(rel));if len(files)<300&&(ext==".go"||ext==".php"||ext==".js"||ext==".ts"||ext==".py"||ext==".yaml"||ext==".yml"||ext==".json")&&!strings.Contains(rel,"node_modules/"){files=append(files,rel)};return nil})
	}
	for _,rel:=range files{p,e:=rt.resolve(rel,false);if e!=nil{continue};data,e:=os.ReadFile(p);if e!=nil||len(data)>2<<20{continue}
		if strings.Contains(strings.ToLower(filepath.Base(rel)),"openapi")||strings.Contains(strings.ToLower(filepath.Base(rel)),"swagger"){
			var doc map[string]any;if json.Unmarshal(data,&doc)==nil{if paths,ok:=doc["paths"].(map[string]any);ok{for path,rawMethods:=range paths{methods,ok:=rawMethods.(map[string]any);if !ok{continue};for method,detail:=range methods{if strings.EqualFold(method,"parameters")||strings.EqualFold(method,"servers")||strings.EqualFold(method,"summary")||strings.EqualFold(method,"description")||method=="$ref"{continue};summary:="";if m,ok:=detail.(map[string]any);ok{summary=fmt.Sprint(m["summary"]);if summary=="<nil>"{summary=""}};results=append(results,map[string]any{"path":path,"method":strings.ToUpper(method),"summary":summary,"source":rel});if len(results)>=max{return map[string]any{"routes":results,"count":len(results),"truncated":true},nil}}}}}
			if !json.Valid(data) {
				parsed := parseOpenAPIYAML(rel, data, max-len(results))
				results = append(results, parsed...)
				if len(results) >= max { return map[string]any{"routes":results[:max],"count":max,"truncated":true}, nil }
			}
		}
		for lineNo,line:=range strings.Split(string(data),"\n"){for _,match:=range routeRE.FindAllStringSubmatch(line,-1){results=append(results,map[string]any{"method":strings.ToUpper(match[1]),"path":match[2],"source":rel,"line":lineNo+1});if len(results)>=max{return map[string]any{"routes":results,"count":len(results),"truncated":true},nil}}}
	}
	return map[string]any{"routes":results,"count":len(results),"scanned_files":len(files),"truncated":false},nil
}

func (rt ToolRuntime) databaseMigrate(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.WriteWorkspace);err!=nil{return nil,err}
	dbRel:=stringArg(a,"database_path");sqlRel:=stringArg(a,"migration_path")
	dbPath,err:=rt.resolve(dbRel,false);if err!=nil{return nil,err};sqlPath,err:=rt.resolve(sqlRel,false);if err!=nil{return nil,err}
	if strings.ToLower(filepath.Ext(sqlRel))!=".sql"{return nil,errors.New("migration_path must point to a .sql file")}
	dbInfo,err:=os.Stat(dbPath);if err!=nil{return nil,err};if dbInfo.IsDir(){return nil,errors.New("database_path must be a SQLite file")}
	sqlInfo,err:=os.Stat(sqlPath);if err!=nil{return nil,err};if sqlInfo.Size()>1<<20{return nil,errors.New("migration SQL exceeds 1 MiB")}
	sqlBytes,err:=os.ReadFile(sqlPath);if err!=nil{return nil,err};sqlText:=strings.TrimSpace(string(sqlBytes));if sqlText==""{return nil,errors.New("migration SQL is empty")}
	if strings.ContainsRune(sqlText,0)||regexp.MustCompile(`(?i)\b(PRAGMA|ATTACH|DETACH|VACUUM|LOAD_EXTENSION)\b`).MatchString(sqlText){return nil,errors.New("migration contains a blocked SQLite administrative statement")}
	if boolArg(a,"dry_run",false){return map[string]any{"executed":false,"database":dbRel,"migration":sqlRel,"bytes":len(sqlText),"statement_count":strings.Count(sqlText,";")+1},nil}
	if err:=func() error { if hasToolApproval(ctx, "database.migrate", raw) { return nil }; return errors.New("database.migrate requires explicit approval for this exact operation") }();err!=nil{return nil,err}
	dsn:="file:"+filepath.ToSlash(dbPath)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db,err:=sql.Open("sqlite",dsn);if err!=nil{return nil,err};defer db.Close()
	db.SetMaxOpenConns(1)
	timeout:=time.Duration(intArg(a,"timeout_seconds",30))*time.Second
	mctx,cancel:=context.WithTimeout(ctx,timeout);defer cancel()
	tx,err:=db.BeginTx(mctx,nil);if err!=nil{return nil,err}
	if _,err=tx.ExecContext(mctx,sqlText);err!=nil{_ = tx.Rollback();return map[string]any{"executed":false,"committed":false,"database":dbRel,"migration":sqlRel,"error":err.Error()},fmt.Errorf("migration rolled back: %w",err)}
	if err=tx.Commit();err!=nil{return nil,err}
	return map[string]any{"executed":true,"committed":true,"database":dbRel,"migration":sqlRel,"bytes":len(sqlText)},nil
}

func (rt ToolRuntime) containerInspect(ctx context.Context,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.RunBuild);err!=nil{return nil,err}
	engine:=stringArg(a,"engine");if engine==""||engine=="auto"{if _,e:=exec.LookPath("docker");e==nil{engine="docker"}else if _,e:=exec.LookPath("podman");e==nil{engine="podman"}else{return map[string]any{"available":false,"message":"Neither docker nor podman is installed or available on PATH"},nil}}
	target:=stringArg(a,"target");if target==""{target="version"}
	var args []string;switch target{case "version":args=[]string{"--version"};case "containers":args=[]string{"ps","--format","{{.ID}}\t{{.Image}}\t{{.Status}}\t{{.Names}}"};case "images":args=[]string{"images","--format","{{.Repository}}:{{.Tag}}\t{{.ID}}\t{{.Size}}"};default:return nil,fmt.Errorf("unsupported container inspection target %q",target)}
	return rt.runTier2Command(ctx,engine,args,".",time.Duration(intArg(a,"timeout_seconds",15))*time.Second)
}
func (rt ToolRuntime) containerRun(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.RunBuild);err!=nil{return nil,err}
	engine:=stringArg(a,"engine");if engine==""{engine="docker"};image:=stringArg(a,"image")
	if !safeContainerImage.MatchString(image)||strings.Contains(image,".."){return nil,errors.New("container image name contains unsupported characters")}
	args:=[]string{"run","--rm","--network","bridge","--read-only","--cap-drop","ALL","--security-opt","no-new-privileges"}
	for _,port:=range tier2StringArrayArg(a,"ports"){
		parts:=strings.Split(port,":");if len(parts)!=2{return nil,fmt.Errorf("invalid port mapping %q",port)}
		host,e1:=strconv.Atoi(parts[0]);container,e2:=strconv.Atoi(parts[1]);if e1!=nil||e2!=nil{return nil,fmt.Errorf("invalid numeric port mapping %q",port)}
		if host<1||host>65535||container<1||container>65535{return nil,fmt.Errorf("port mapping out of range: %q",port)}
		args=append(args,"-p",fmt.Sprintf("127.0.0.1:%d:%d",host,container))
	}
	args=append(args,image)
	command:=tier2StringArrayArg(a,"command");args=append(args,command...)
	if boolArg(a,"dry_run",false){return map[string]any{"executed":false,"command":append([]string{engine},args...),"reason":"dry run; container not started"},nil}
	if err:=rt.Capabilities.Require(security.NetworkAccess);err!=nil{return nil,err}
	if err:=func() error { if hasToolApproval(ctx, "container.run", raw) { return nil }; return errors.New("container.run requires explicit approval for this exact operation") }();err!=nil{return nil,err}
	return rt.runTier2Command(ctx,engine,args,".",time.Duration(intArg(a,"timeout_seconds",60))*time.Second)
}

func (rt ToolRuntime) runTier2Command(ctx context.Context,exe string,args []string,cwd string,timeout time.Duration)(map[string]any,error){
	if timeout<=0{timeout=60*time.Second}
	if _,err:=exec.LookPath(exe);err!=nil{return map[string]any{"executed":false,"available":false,"executable":exe,"error":err.Error()},err}
	workDir:=rt.Root
	if cwd!="."&&cwd!=""{resolved,err:=rt.resolve(cwd,false);if err!=nil{return nil,err};info,err:=os.Stat(resolved);if err!=nil{return nil,err};if !info.IsDir(){return nil,errors.New("command working directory is not a directory")};workDir=resolved}
	cctx,cancel:=context.WithTimeout(ctx,timeout);defer cancel()
	cmd,cmdErr:=workspaceExecCommand(cctx,exe,args);if cmdErr!=nil{return nil,cmdErr};cmd.Dir=workDir
	cmd.Env=append(os.Environ(),"CI=1","NO_COLOR=1")
	output,err:=cmd.CombinedOutput();truncated:=len(output)>tier2OutputLimit;if truncated{output=output[:tier2OutputLimit]}
	result:=map[string]any{"executed":true,"success":err==nil,"command":append([]string{exe},args...),"exit_code":0,"output":redactHTTPSecrets(string(output)),"truncated":truncated,"timed_out":errors.Is(cctx.Err(),context.DeadlineExceeded)}
	if err!=nil{var exitErr *exec.ExitError;if errors.As(err,&exitErr){result["exit_code"]=exitErr.ExitCode()}else{result["exit_code"]=-1};result["error"]=redactHTTPSecrets(err.Error());return result,nil}
	return result,nil
}
func tier2StringArrayArg(a map[string]any,key string)[]string{raw,_:=a[key].([]any);out:=make([]string,0,len(raw));for _,v:=range raw{if s,ok:=v.(string);ok{out=append(out,s)}};return out}
func tier2Exists(root,rel string)bool{info,err:=os.Stat(filepath.Join(root,filepath.FromSlash(rel)));return err==nil&&!info.IsDir()}


func parseOpenAPIYAML(source string, data []byte, limit int) []map[string]any {
	if limit <= 0 { return nil }
	pathRE := regexp.MustCompile(`^\s{2}(/[^:]+):\s*(?:#.*)?$`)
	methodRE := regexp.MustCompile(`^\s{4}(get|post|put|patch|delete|options|head):\s*(?:#.*)?$`)
	summaryRE := regexp.MustCompile(`^\s{6}summary:\s*(.+?)\s*$`)
	var currentPath string
	var out []map[string]any
	for lineNo, line := range strings.Split(string(data), "\n") {
		if match := pathRE.FindStringSubmatch(line); len(match) == 2 { currentPath = strings.TrimSpace(match[1]); continue }
		if match := methodRE.FindStringSubmatch(line); len(match) == 2 && currentPath != "" {
			out = append(out, map[string]any{"path": currentPath, "method": strings.ToUpper(match[1]), "source": source, "line": lineNo+1})
			if len(out) >= limit { return out }
			continue
		}
		if match := summaryRE.FindStringSubmatch(line); len(match) == 2 && len(out) > 0 { out[len(out)-1]["summary"] = strings.Trim(strings.TrimSpace(match[1]), "\"\x27") }
	}
	return out
}