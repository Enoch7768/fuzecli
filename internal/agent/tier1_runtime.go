package agent

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/security"
	"github.com/Enoch7768/fuzecli/internal/workspace"
	_ "modernc.org/sqlite"
)

const tier1MaxBody = 256 << 10

func (rt ToolRuntime) executeTier1(ctx context.Context, name string, raw json.RawMessage, a map[string]any) (any, error) {
	switch name {
	case "workspace.read_files": return rt.readFiles(a)
	case "workspace.find_references": return rt.findReferences(ctx, a)
	case "workspace.apply_patch": return rt.applyPatch(ctx, raw, a)
	case "workspace.file_info": return rt.fileInfo(a)
	case "workspace.tree": return rt.tree(a)
	case "workspace.rename": return rt.rename(ctx, raw, a)
	case "terminal.run_parallel": return rt.runParallel(ctx, a)
	case "terminal.process": return rt.manageProcess(ctx, a)
	case "terminal.output": return rt.processOutput(a)
	case "environment.inspect": return rt.inspectEnvironment(a)
	case "test.list": return rt.listTests(a)
	case "test.run": return rt.runTest(ctx, a)
	case "debug.analyze": return rt.analyzeDebug(a)
	case "git.log": return rt.gitLog(ctx, a)
	case "git.show": return rt.gitShow(ctx, a)
	case "git.branch": return rt.gitBranch(ctx, raw, a)
	case "git.stash": return rt.gitStash(ctx, raw, a)
	case "web.search": return rt.webSearch(ctx, a)
	case "web.open": return rt.webOpen(ctx, a)
	case "github.search_code": return rt.githubSearchCode(ctx, a)
	case "github.read_file": return rt.githubReadFile(ctx, a)
	case "http.request": return rt.httpRequest(ctx, a)
	case "database.inspect": return rt.databaseInspect(a)
	case "database.schema": return rt.databaseSchema(ctx, a)
	case "database.query": return rt.databaseQuery(ctx, a)
	default: return nil, fmt.Errorf("Tier 1 handler not implemented: %s", name)
	}
}

func (rt ToolRuntime) readFiles(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	items, _ := a["files"].([]any)
	out := make([]map[string]any, 0, len(items))
	total := 0
	for _, item := range items {
		f, _ := item.(map[string]any)
		path := stringArg(f, "path")
		resolved, err := rt.resolve(path, false)
		if err != nil { out = append(out, map[string]any{"path": path, "error": err.Error()}); continue }
		info, err := os.Stat(resolved)
		if err != nil { out = append(out, map[string]any{"path": path, "error": err.Error()}); continue }
		if !info.Mode().IsRegular() { out = append(out, map[string]any{"path": path, "error": "path is not a regular file"}); continue }
		if info.Size() > 2<<20 { out = append(out, map[string]any{"path": path, "error": "file exceeds the 2 MiB read limit"}); continue }
		data, err := os.ReadFile(resolved)
		if err != nil { out = append(out, map[string]any{"path": path, "error": err.Error()}); continue }
		if !isUTF8Text(data) { out = append(out, map[string]any{"path": path, "error": "file is not UTF-8 text"}); continue }
		lines := strings.Split(string(data), "\n")
		start, end := intArg(f, "start_line", 1), intArg(f, "end_line", len(lines))
		if start < 1 || end < start { out = append(out, map[string]any{"path": path, "error": "invalid line range"}); continue }
		if start > len(lines) { out = append(out, map[string]any{"path": path, "content": "", "start_line": start, "end_line": end, "total_lines": len(lines)}); continue }
		if end > len(lines) { end = len(lines) }
		content := strings.Join(lines[start-1:end], "\n")
		if total+len(content) > 1<<20 { out = append(out, map[string]any{"path": path, "error": "combined output exceeded the 1 MiB limit"}); break }
		total += len(content)
		out = append(out, map[string]any{"path": filepath.ToSlash(path), "content": content, "start_line": start, "end_line": end, "total_lines": len(lines)})
	}
	return map[string]any{"files": out, "bytes_returned": total}, nil
}

func (rt ToolRuntime) findReferences(ctx context.Context, a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	symbol := stringArg(a, "symbol")
	result, err := rt.searchCode(ctx, map[string]any{"query": symbol, "paths": a["paths"], "max_results": intArg(a, "max_results", 50), "case_sensitive": boolArg(a, "case_sensitive", true)})
	if err != nil { return nil, err }
	return map[string]any{"symbol": symbol, "references": result, "note": "Textual references are reported; review results to distinguish declarations, comments, and actual usages."}, nil
}

func (rt ToolRuntime) applyPatch(ctx context.Context, raw json.RawMessage, a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.WriteWorkspace); err != nil { return nil, err }
	path := stringArg(a, "path")
	resolved, err := rt.resolve(path, false); if err != nil { return nil, err }
	info, err := os.Stat(resolved); if err != nil { return nil, err }
	if !info.Mode().IsRegular() { return nil, errors.New("patch target must be a regular file") }
	data, err := os.ReadFile(resolved); if err != nil { return nil, err }
	if len(data) > 2<<20 || !isUTF8Text(data) { return nil, errors.New("patch target must be UTF-8 text no larger than 2 MiB") }
	oldText, newText := stringArg(a, "old_text"), stringArg(a, "new_text")
	if oldText == "" { return nil, errors.New("old_text must not be empty") }
	count := strings.Count(string(data), oldText)
	if count == 0 { return nil, errors.New("old_text was not found; reread the file before patching") }
	if count != 1 { return nil, fmt.Errorf("old_text matched %d times; provide a larger unique context", count) }
	updated := strings.Replace(string(data), oldText, newText, 1)
	if len(updated) > 2<<20 { return nil, errors.New("patched file exceeds the 2 MiB limit") }
	if boolArg(a, "dry_run", false) { return map[string]any{"dry_run": true, "applied": false, "path": filepath.ToSlash(path), "old_text_matches": count, "before_bytes": len(data), "after_bytes": len(updated)}, nil }
	if !hasToolApproval(ctx, "workspace.apply_patch", raw) { return nil, errors.New("patch requires explicit approval for this exact change") }
	tmp, err := os.CreateTemp(filepath.Dir(resolved), ".fuzecli-patch-*"); if err != nil { return nil, err }
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(info.Mode().Perm()); err == nil { _, err = tmp.WriteString(updated) }
	if closeErr := tmp.Close(); err == nil { err = closeErr }
	if err != nil { return nil, err }
	if err = os.Rename(tmpName, resolved); err != nil { return nil, err }
	return map[string]any{"applied": true, "path": filepath.ToSlash(path), "before_bytes": len(data), "after_bytes": len(updated), "replacements": 1}, nil
}

func languageForTier1(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go": return "go"
	case ".ts", ".tsx", ".mts", ".cts": return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs": return "javascript"
	case ".py", ".pyi": return "python"
	case ".php": return "php"
	case ".html", ".htm": return "html"
	case ".css", ".scss", ".sass": return "css"
	case ".json": return "json"
	case ".md", ".mdx": return "markdown"
	case ".sql", ".sqlite", ".db": return "sql/sqlite"
	case ".rs": return "rust"
	case ".java": return "java"
	case ".c", ".h": return "c"
	case ".cpp", ".hpp", ".cc", ".hh": return "cpp"
	case ".yml", ".yaml": return "yaml"
	case ".xml": return "xml"
	case ".sh", ".bash": return "shell"
	case ".toml": return "toml"
	default: return "text"
	}
}

func (rt ToolRuntime) fileInfo(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	rel := stringArg(a, "path"); path, err := rt.resolve(rel, false); if err != nil { return nil, err }
	info, err := os.Stat(path); if err != nil { return nil, err }
	out := map[string]any{"path": filepath.ToSlash(rel), "size": info.Size(), "modified_time": info.ModTime().UTC().Format(time.RFC3339Nano), "is_directory": info.IsDir(), "language": languageForTier1(rel), "permissions": info.Mode().Perm().String()}
	if info.IsDir() { out["binary"] = false; out["encoding"] = "not_applicable"; return out, nil }
	f, err := os.Open(path); if err != nil { return nil, err }; defer f.Close()
	sample := make([]byte, 8192); n, readErr := f.Read(sample); if readErr != nil && readErr != io.EOF { return nil, readErr }; sample = sample[:n]
	binary := bytes.IndexByte(sample, 0) >= 0 || !utf8.Valid(sample)
	out["binary"] = binary
	if binary { out["encoding"] = "binary" } else { out["encoding"] = "UTF-8" }
	if !binary && info.Size() <= 64<<20 {
		if _, err := f.Seek(0, io.SeekStart); err != nil { return nil, err }
		scanner := bufio.NewScanner(f); scanner.Buffer(make([]byte, 64*1024), 2<<20); lines := 0
		for scanner.Scan() { lines++ }
		if err := scanner.Err(); err != nil { return nil, fmt.Errorf("count lines: %w", err) }
		out["line_count"] = lines
	} else { out["line_count"] = nil }
	return out, nil
}

func (rt ToolRuntime) tree(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	rel := stringArg(a, "path"); if rel == "" { rel = "." }
	root := rt.Root
	if rel != "." { var err error; root, err = rt.resolve(rel, false); if err != nil { return nil, err } }
	info, err := os.Stat(root); if err != nil { return nil, err }; if !info.IsDir() { return nil, errors.New("tree path must be a directory") }
	maxDepth, maxEntries := intArg(a, "max_depth", 5), intArg(a, "max_entries", 500)
	policy, err := workspace.LoadIgnorePolicy(rt.Root); if err != nil { return nil, err }
	type entry struct { Path string `json:"path"`; Type string `json:"type"`; Depth int `json:"depth"` }
	out := make([]entry, 0); truncated := false
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil { return walkErr }; if path == root { return nil }
		relPath, e := filepath.Rel(rt.Root, path); if e != nil { return e }; relPath = filepath.ToSlash(relPath)
		if d.IsDir() && (relPath == ".git" || strings.HasPrefix(relPath, ".git/") || relPath == ".aicli" || strings.HasPrefix(relPath, ".aicli/") || policy.Ignored(relPath)) { return filepath.SkipDir }
		depth := strings.Count(filepath.ToSlash(strings.TrimPrefix(path, root)), "/") + 1
		if depth > maxDepth { if d.IsDir() { return filepath.SkipDir }; return nil }
		out = append(out, entry{Path: relPath, Type: map[bool]string{true:"directory", false:"file"}[d.IsDir()], Depth: depth})
		if len(out) >= maxEntries { truncated = true; return errToolWalkLimit }
		return nil
	})
	if err != nil && !errors.Is(err, errToolWalkLimit) { return nil, err }
	sort.Slice(out, func(i,j int) bool { return out[i].Path < out[j].Path })
	return map[string]any{"root": filepath.ToSlash(rel), "entries": out, "count": len(out), "truncated": truncated}, nil
}

func (rt ToolRuntime) rename(ctx context.Context, raw json.RawMessage, a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.WriteWorkspace); err != nil { return nil, err }
	src, dst := stringArg(a, "source"), stringArg(a, "destination")
	source, err := rt.resolve(src, true); if err != nil { return nil, err }
	dest, err := rt.resolve(dst, true); if err != nil { return nil, err }
	if filepath.Clean(source) == filepath.Clean(dest) { return nil, errors.New("source and destination are the same path") }
	if _, err := os.Lstat(dest); err == nil { return nil, errors.New("destination already exists; refusing to overwrite") } else if !os.IsNotExist(err) { return nil, err }
	if _, err := os.Stat(source); err != nil { return nil, err }
	if _, err := os.Stat(filepath.Dir(dest)); err != nil { return nil, errors.New("destination parent directory must already exist") }
	if boolArg(a, "dry_run", false) { return map[string]any{"dry_run": true, "renamed": false, "source": filepath.ToSlash(src), "destination": filepath.ToSlash(dst)}, nil }
	if !hasToolApproval(ctx, "workspace.rename", raw) { return nil, errors.New("rename requires explicit approval for this exact operation") }
	if err := os.Rename(source, dest); err != nil { return nil, err }
	return map[string]any{"renamed": true, "source": filepath.ToSlash(src), "destination": filepath.ToSlash(dst)}, nil
}

func (rt ToolRuntime) runParallel(ctx context.Context, a map[string]any) (any, error) {
	items, _ := a["commands"].([]any)
	type outcome struct { Index int `json:"index"`; Executable string `json:"executable"`; ExitCode int `json:"exit_code"`; Stdout string `json:"stdout"`; Stderr string `json:"stderr"`; Error string `json:"error,omitempty"`; TimedOut bool `json:"timed_out"` }
	out := make([]outcome, len(items)); var wg sync.WaitGroup
	for i, rawItem := range items {
		i, item := i, rawItem.(map[string]any); wg.Add(1)
		go func() {
			defer wg.Done()
			exe, args := stringArg(item,"executable"), stringArray(item["args"])
			out[i] = outcome{Index:i, Executable:exe, ExitCode:-1}
			if err := rt.Capabilities.Require(security.RunBuild); err != nil { out[i].Error = err.Error(); return }
			decision := security.Authorize(security.Command{Name:exe,Args:args}); if !decision.Allowed { out[i].Error = "blocked by security policy: "+decision.Reason; return }
			cwd := rt.Root
			if rel := stringArg(item,"cwd"); rel != "" { p,err:=rt.resolve(rel,false); if err!=nil { out[i].Error=err.Error(); return }; inf,err:=os.Stat(p); if err!=nil||!inf.IsDir(){out[i].Error="cwd must be an existing workspace directory";return}; cwd=p }
			timeout:=time.Duration(intArg(item,"timeout_seconds",60))*time.Second; cctx,cancel:=context.WithTimeout(ctx,timeout); defer cancel()
			cmd:=exec.CommandContext(cctx,exe,args...); cmd.Dir=cwd; cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0")
			var stdout,stderr limitedBuffer; stdout.limit=32<<10; stderr.limit=32<<10; cmd.Stdout=&stdout; cmd.Stderr=&stderr
			err:=cmd.Run(); out[i].Stdout=stdout.String(); out[i].Stderr=stderr.String(); out[i].TimedOut=errors.Is(cctx.Err(),context.DeadlineExceeded)
			if err==nil {out[i].ExitCode=0} else {var ee *exec.ExitError; if errors.As(err,&ee){out[i].ExitCode=ee.ExitCode()} else {out[i].Error=err.Error()}}
		}()
	}
	wg.Wait()
	return map[string]any{"results":out,"count":len(out),"parallel":true},nil
}

type tier1Log struct { mu sync.Mutex; data []byte; limit int }
func (b *tier1Log) Write(p []byte) (int,error) { b.mu.Lock(); defer b.mu.Unlock(); n:=len(p); if b.limit<=0 { b.limit=128<<10 }; if len(p)>=b.limit { b.data=append(b.data[:0],p[len(p)-b.limit:]...) } else { if len(b.data)+len(p)>b.limit { drop:=len(b.data)+len(p)-b.limit; b.data=append([]byte(nil),b.data[drop:]...) }; b.data=append(b.data,p...) }; return n,nil }
func (b *tier1Log) String(max int) string { b.mu.Lock(); defer b.mu.Unlock(); if max>0&&len(b.data)>max{return string(b.data[len(b.data)-max:])}; return string(b.data) }
type managedProcess struct { id, executable, cwd string; args []string; cmd *exec.Cmd; stdout, stderr *tier1Log; started time.Time; done chan struct{}; mu sync.Mutex; exitCode *int; err string }
type processManager struct { mu sync.Mutex; items map[string]*managedProcess; seq uint64 }
var tier1Managers sync.Map
func managerFor(root string) *processManager { if v,ok:=tier1Managers.Load(root); ok { return v.(*processManager) }; m:=&processManager{items:map[string]*managedProcess{}}; actual,_:=tier1Managers.LoadOrStore(root,m); return actual.(*processManager) }

func (rt ToolRuntime) manageProcess(ctx context.Context, a map[string]any) (any,error) {
	m:=managerFor(rt.Root); action:=stringArg(a,"action"); id:=stringArg(a,"process_id")
	if action=="start" {
		exe:=stringArg(a,"executable"); args:=stringArray(a["args"])
		if err:=rt.Capabilities.Require(security.RunBuild);err!=nil{return nil,err}
		decision:=security.AuthorizeManagedProcess(security.Command{Name:exe,Args:args});if !decision.Allowed{return nil,fmt.Errorf("managed process blocked by security policy: %s",decision.Reason)}
		cwd:=rt.Root;if rel:=stringArg(a,"cwd");rel!=""{p,e:=rt.resolve(rel,false);if e!=nil{return nil,e};inf,e:=os.Stat(p);if e!=nil||!inf.IsDir(){return nil,errors.New("cwd must be an existing workspace directory")};cwd=p}
		m.mu.Lock();m.seq++;id=fmt.Sprintf("proc-%x-%x",time.Now().UnixNano(),m.seq);m.mu.Unlock()
		p:=&managedProcess{id:id,executable:exe,args:args,cwd:cwd,started:time.Now().UTC(),done:make(chan struct{}),stdout:&tier1Log{limit:128<<10},stderr:&tier1Log{limit:128<<10}}
		p.cmd=exec.CommandContext(context.Background(),exe,args...);p.cmd.Dir=cwd;p.cmd.Env=append(os.Environ(),"GIT_TERMINAL_PROMPT=0");p.cmd.Stdout=p.stdout;p.cmd.Stderr=p.stderr
		if err:=p.cmd.Start();err!=nil{return nil,err}
		m.mu.Lock();m.items[id]=p;m.mu.Unlock()
		go func(){err:=p.cmd.Wait();p.mu.Lock();if p.cmd.ProcessState!=nil {code:=p.cmd.ProcessState.ExitCode();p.exitCode=&code};if err!=nil{p.err=err.Error()};p.mu.Unlock();close(p.done)}()
		return map[string]any{"started":true,"process_id":id,"executable":exe,"args":args,"started_at":p.started.Format(time.RFC3339Nano)},nil
	}
	m.mu.Lock();p:=m.items[id];m.mu.Unlock();if p==nil{return nil,fmt.Errorf("managed process %q was not found in this running FuzeCLI session",id)}
	switch action {
	case "status":
		status:="running";select{case <-p.done:status="exited";default:}
		p.mu.Lock();defer p.mu.Unlock();return map[string]any{"process_id":id,"status":status,"exit_code":p.exitCode,"error":p.err,"started_at":p.started.Format(time.RFC3339Nano)},nil
	case "stop":
		select{case <-p.done:return map[string]any{"stopped":false,"process_id":id,"status":"already_exited"},nil;default:}
		if p.cmd.Process==nil{return nil,errors.New("process has no operating-system handle")}
		if err:=p.cmd.Process.Kill();err!=nil&&!strings.Contains(strings.ToLower(err.Error()),"process already finished"){return nil,err}
		select{case <-p.done:case <-time.After(3*time.Second):return nil,errors.New("process did not exit after termination request")}
		return map[string]any{"stopped":true,"process_id":id},nil
	case "restart":
		select{case <-p.done:default:if p.cmd.Process!=nil{_ = p.cmd.Process.Kill()};select{case <-p.done:case <-time.After(3*time.Second):return nil,errors.New("process did not stop for restart")}}
		restartCwd:="";if p.cwd!=rt.Root{restartCwd=filepath.ToSlash(strings.TrimPrefix(p.cwd,rt.Root+string(os.PathSeparator)))};return rt.manageProcess(ctx,map[string]any{"action":"start","executable":p.executable,"args":p.args,"cwd":restartCwd})
	default:return nil,errors.New("unsupported process action")
	}
}
func (rt ToolRuntime) processOutput(a map[string]any)(any,error){id:=stringArg(a,"process_id");m:=managerFor(rt.Root);m.mu.Lock();p:=m.items[id];m.mu.Unlock();if p==nil{return nil,fmt.Errorf("managed process %q was not found in this running FuzeCLI session",id)};max:=intArg(a,"max_bytes",16384);stdout,stderr:=p.stdout.String(max),p.stderr.String(max);return map[string]any{"process_id":id,"stdout":stdout,"stderr":stderr,"truncated":len(stdout)>=max||len(stderr)>=max},nil}

func (rt ToolRuntime) inspectEnvironment(a map[string]any)(any,error){
	capabilities:=[]string{};for capability,allowed:=range rt.Capabilities{if allowed{capabilities=append(capabilities,string(capability))}};sort.Strings(capabilities)
	out:=map[string]any{"os":runtime.GOOS,"architecture":runtime.GOARCH,"go_runtime":runtime.Version(),"cpu_count":runtime.NumCPU(),"working_directory":rt.Root,"capabilities":capabilities}
	commands:=[]string{"go","node","npm","python","python3","php","cargo","rustc","java","javac","docker","git","sqlite3","pytest","uv","pip","pip3","composer","make"}
	available:=map[string]any{}
	for _,name:=range commands{path,err:=exec.LookPath(name);if err!=nil{continue};entry:=map[string]any{"path":path};if boolArg(a,"check_versions",true){args:=[]string{"--version"};if name=="go"{args=[]string{"version"}};ctx,cancel:=context.WithTimeout(context.Background(),2*time.Second);cmd:=exec.CommandContext(ctx,name,args...);b,e:=cmd.CombinedOutput();cancel();if e==nil{entry["version"]=truncate(strings.TrimSpace(string(b)),500)}else{entry["version_error"]=truncate(e.Error(),200)}};available[name]=entry}
	out["runtimes"]=available
	return out,nil
}

func (rt ToolRuntime) listTests(a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	rel:=stringArg(a,"path");root:=rt.Root;if rel!=""&&rel!="."{p,e:=rt.resolve(rel,false);if e!=nil{return nil,e};root=p}
	info,e:=os.Stat(root);if e!=nil{return nil,e};if !info.IsDir(){root=filepath.Dir(root)}
	max:=intArg(a,"max_results",100);type testEntry struct{Path string `json:"path"`;Kind string `json:"kind"`;Command []string `json:"command"`}
	out:=[]testEntry{};truncated:=false
	policy,e:=workspace.LoadIgnorePolicy(rt.Root);if e!=nil{return nil,e}
	e=filepath.WalkDir(root,func(path string,d os.DirEntry,walkErr error)error{if walkErr!=nil{return walkErr};if path==root{return nil};relPath,er:=filepath.Rel(rt.Root,path);if er!=nil{return er};relPath=filepath.ToSlash(relPath);if d.IsDir()&&(relPath==".git"||strings.HasPrefix(relPath,".git/")||relPath==".aicli"||strings.HasPrefix(relPath,".aicli/")||policy.Ignored(relPath)){return filepath.SkipDir};if d.IsDir(){return nil};name:=d.Name();var ent *testEntry
		if strings.HasSuffix(name,"_test.go"){ent=&testEntry{Path:relPath,Kind:"go",Command:[]string{"go","test","./"+filepath.ToSlash(filepath.Dir(relPath))}}}
		if strings.HasSuffix(name,".test.js")||strings.HasSuffix(name,".spec.js")||strings.HasSuffix(name,".test.ts")||strings.HasSuffix(name,".spec.ts")||strings.HasSuffix(name,".test.tsx")||strings.HasSuffix(name,".spec.tsx"){ent=&testEntry{Path:relPath,Kind:"javascript/typescript",Command:[]string{"npm","test","--",relPath}}}
		if strings.HasPrefix(name,"test_")&&strings.HasSuffix(name,".py")||strings.HasSuffix(name,"_test.py"){ent=&testEntry{Path:relPath,Kind:"python",Command:[]string{"python","-m","pytest",relPath}}}
		if ent!=nil{out=append(out,*ent);if len(out)>=max{truncated=true;return errToolWalkLimit}};return nil})
	if e!=nil&&!errors.Is(e,errToolWalkLimit){return nil,e}
	if len(out)==0 {if _,e:=os.Stat(filepath.Join(rt.Root,"go.mod"));e==nil{out=append(out,testEntry{Path:".",Kind:"go-suite",Command:[]string{"go","test","./..."}})};if data,e:=os.ReadFile(filepath.Join(rt.Root,"package.json"));e==nil{var p struct{Scripts map[string]string `json:"scripts"`};if json.Unmarshal(data,&p)==nil{for _,key:=range []string{"test","test:unit","test:e2e"}{if p.Scripts[key]!=""&&len(out)<max{out=append(out,testEntry{Path:"package.json",Kind:key,Command:[]string{"npm","run",key}})}}}}}
	return map[string]any{"tests":out,"count":len(out),"truncated":truncated},nil
}

func (rt ToolRuntime) runTest(ctx context.Context, a map[string]any) (any, error) {
	if boolArg(a, "dry_run", false) {
		return map[string]any{"dry_run":true, "executed":false, "path":stringArg(a,"path"), "name":stringArg(a,"name"), "suite":stringArg(a,"suite")}, nil
	}
	if err := rt.Capabilities.Require(security.RunTests); err != nil { return nil, err }
	path, name, suite := stringArg(a,"path"), stringArg(a,"name"), stringArg(a,"suite")
	timeout := intArg(a,"timeout_seconds",120)
	exe, args := "", []string{}
	if _, err := os.Stat(filepath.Join(rt.Root,"go.mod")); err == nil {
		pkg := "./..."
		if path != "" {
			resolved, err := rt.resolve(path,false); if err != nil { return nil, err }
			info, err := os.Stat(resolved); if err != nil { return nil, err }
			dir := resolved; if !info.IsDir() { dir = filepath.Dir(resolved) }
			rel, err := filepath.Rel(rt.Root,dir); if err != nil { return nil, err }
			pkg = "./"+filepath.ToSlash(rel); if rel == "." { pkg = "./..." }
		}
		if (suite=="e2e" || suite=="component") && path=="" { return nil, fmt.Errorf("Go has no universal %s suite convention; provide a specific test path",suite) }
		exe = "go"; args = []string{"test"}
		if name != "" { args = append(args,"-run","^"+regexp.QuoteMeta(name)+"$") }
		if suite == "unit" { args = append(args,"-short") }
		if suite == "integration" { args = append(args,"-tags","integration") }
		args = append(args,pkg)
	} else if _, err := os.Stat(filepath.Join(rt.Root,"package.json")); err == nil {
		exe = "npm"
		script := ""
		if suite != "" && suite != "all" {
			data, readErr := os.ReadFile(filepath.Join(rt.Root,"package.json"))
			if readErr == nil {
				var manifest struct { Scripts map[string]string `json:"scripts"` }
				if json.Unmarshal(data,&manifest) == nil && manifest.Scripts["test:"+suite] != "" { script = "test:"+suite }
			}
			if script == "" && path == "" && name == "" { return nil, fmt.Errorf("package.json does not declare a test:%s script",suite) }
		}
		if script != "" { args = []string{"run",script,"--"} } else { args = []string{"test","--"} }
		if path != "" { if _,err := rt.resolve(path,false); err != nil { return nil, err }; args = append(args,filepath.ToSlash(path)) }
		if name != "" { args = append(args,"-t",name) }
	} else {
		if path == "" && suite == "" { return nil, errors.New("no supported test runner detected; provide a project manifest or explicit test path") }
		exe = "python"
		if _, err := exec.LookPath(exe); err != nil { exe = "python3" }
		args = []string{"-m","pytest"}
		if path == "" && suite != "" && suite != "all" {
			candidate := filepath.Join("tests",suite)
			if _,err := os.Stat(filepath.Join(rt.Root,candidate)); os.IsNotExist(err) { candidate = filepath.Join("test",suite) }
			if _,err := os.Stat(filepath.Join(rt.Root,candidate)); err != nil { return nil, fmt.Errorf("could not find a Python test directory for suite %q; provide path",suite) }
			path = filepath.ToSlash(candidate)
		}
		if path != "" { if _,err := rt.resolve(path,false); err != nil { return nil, err }; args = append(args,filepath.ToSlash(path)) }
		if name != "" { args = append(args,"-k",name) }
	}
	if err := securityCommandAllowed(exe,args); err != nil { return nil, err }
	cctx, cancel := context.WithTimeout(ctx,time.Duration(timeout)*time.Second); defer cancel()
	cmd := exec.CommandContext(cctx,exe,args...); cmd.Dir = rt.Root; cmd.Env = append(os.Environ(),"GIT_TERMINAL_PROMPT=0")
	var stdout,stderr limitedBuffer; stdout.limit = 64<<10; stderr.limit = 64<<10; start := time.Now(); cmd.Stdout = &stdout; cmd.Stderr = &stderr
	err := cmd.Run(); exit := 0
	if err != nil { var ee *exec.ExitError; if errors.As(err,&ee) { exit = ee.ExitCode() } else if cctx.Err() != nil { exit = -1 } else { return nil, err } }
	return map[string]any{"executed":true,"passed":err==nil,"exit_code":exit,"stdout":stdout.String(),"stderr":stderr.String(),"duration_ms":time.Since(start).Milliseconds(),"timed_out":errors.Is(cctx.Err(),context.DeadlineExceeded),"command":append([]string{exe},args...)}, nil
}

func securityCommandAllowed(exe string,args []string)error{d:=security.Authorize(security.Command{Name:exe,Args:args});if !d.Allowed{return fmt.Errorf("command blocked by security policy: %s",d.Reason)};return nil}

func (rt ToolRuntime) analyzeDebug(a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	errText:=stringArg(a,"error");lower:=strings.ToLower(errText);causes:=[]string{};next:=[]string{}
	add:=func(needle,cause,step string){if strings.Contains(lower,needle){causes=append(causes,cause);next=append(next,step)}}
	add("connection refused","The target process is not listening on the requested host/port, or a firewall is rejecting the connection.","Check whether the server process is running, then inspect its startup logs and port.")
	add("http error 422","The server received the request but rejected its content or validation rules.","Inspect the response body and compare the payload with the endpoint's expected schema.")
	add("unauthorized","Authentication is missing, invalid, or lacks permission.","Check the configured authentication flow and required scopes without printing secrets.")
	add("permission denied","The process lacks permission to access the resource.","Check the effective user, file permissions, and application capability policy.")
	add("no such file or directory","A path or executable could not be found.","Verify the path relative to the workspace and check whether the executable is installed.")
	add("undefined","A value or symbol may be missing or accessed before initialization.","Inspect the stack trace location and the value's initialization path.")
	add("nil pointer","Code likely dereferenced a nil pointer.","Inspect the exact stack frame and trace where the value should have been initialized.")
	add("syntaxerror","The parser rejected the source or generated syntax.","Run the language parser/compiler and inspect the first reported line and surrounding code.")
	add("timeout","The operation exceeded its timeout or is waiting indefinitely.","Check process output, external dependencies, and whether the timeout is appropriate.")
	add("out of memory","The process may have exceeded its memory limit.","Inspect dataset/buffer sizes, memory usage, and whether work can be streamed or bounded.")
	if len(causes)==0{causes=append(causes,"No known error signature matched; the message alone is insufficient to identify a root cause.");next=append(next,"Capture the full error and stack trace, identify the first application-owned frame, and inspect the relevant source and test.") }
	relevant:=[]any{}
	if path:=stringArg(a,"path");path!=""{p,e:=rt.resolve(path,false);if e==nil{data,er:=os.ReadFile(p);if er==nil{relevant=append(relevant,map[string]any{"path":path,"excerpt":truncate(string(data),6000)})}}}
	for _,line:=range strings.Split(errText,"\n"){if len(relevant)>=intArg(a,"max_suggestions",8){break};for _,part:=range regexp.MustCompile(`[A-Za-z0-9_./-]+\.(go|py|js|ts|tsx|php|java|rs):[0-9]+`).FindAllString(line,-1){file:=strings.Split(part,":")[0];p,e:=rt.resolve(file,false);if e==nil{data,er:=os.ReadFile(p);if er==nil{relevant=append(relevant,map[string]any{"path":file,"excerpt":truncate(string(data),2500)})}}}}
	return map[string]any{"probable_causes":uniqueStrings(causes),"recommended_investigation":uniqueStrings(next),"relevant_files":relevant,"performed_changes":false,"confidence":"heuristic"},nil
}
func uniqueStrings(in []string)[]string{seen:=map[string]bool{};out:=[]string{};for _,s:=range in{if !seen[s]{seen[s]=true;out=append(out,s)}};return out}

func (rt ToolRuntime) gitLog(ctx context.Context,a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.RunGit); err != nil { return nil, err }
	args:=[]string{"log","--format=%H%x09%h%x09%an%x09%aI%x09%s","-n",strconv.Itoa(intArg(a,"limit",20))}
	if author:=stringArg(a,"author");author!=""{args=append(args,"--author="+author)}
	if path:=stringArg(a,"path");path!=""{if _,e:=rt.resolve(path,false);e!=nil{return nil,e};args=append(args,"--",path)}
	out,e:=runGitOutput(ctx,rt.Root,args...);if e!=nil{return nil,e};commits:=[]map[string]string{}
	for _,line:=range strings.Split(strings.TrimSpace(out),"\n"){if line==""{continue};parts:=strings.SplitN(line,"\t",5);if len(parts)==5{commits=append(commits,map[string]string{"sha":parts[0],"short_sha":parts[1],"author":parts[2],"date":parts[3],"subject":parts[4]})}}
	return map[string]any{"commits":commits,"count":len(commits)},nil
}
func (rt ToolRuntime) gitShow(ctx context.Context,a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.RunGit); err != nil { return nil, err }
	rev:=stringArg(a,"revision");if rev==""{rev="HEAD"};if strings.HasPrefix(rev,"-")||!regexp.MustCompile(`^[A-Za-z0-9._/-]+(\^\{commit\}|~[0-9]+|\^[0-9]+)?$`).MatchString(rev){return nil,errors.New("invalid Git revision")}
	verified,e:=runGitOutput(ctx,rt.Root,"rev-parse","--verify",rev+"^{commit}");if e!=nil{return nil,e}
	args:=[]string{"show","--format=fuller"};if boolArg(a,"stat_only",false){args=append(args,"--stat")}else{args=append(args,"--patch","-U3")}
	args=append(args,strings.TrimSpace(verified))
	if path:=stringArg(a,"path");path!=""{if _,e:=rt.resolve(path,false);e!=nil{return nil,e};args=append(args,"--",path)}
	out,e:=runGitOutput(ctx,rt.Root,args...);if e!=nil{return nil,e}
	return map[string]any{"revision":strings.TrimSpace(verified),"output":truncate(out,256<<10),"truncated":len(out)>256<<10},nil
}
func (rt ToolRuntime) gitBranch(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.RunGit); err != nil { return nil, err }
	action:=stringArg(a,"action");name:=stringArg(a,"name")
	if action=="list"{out,e:=runGitOutput(ctx,rt.Root,"branch","--format=%(HEAD)%09%(refname:short)%09%(objectname:short)");if e!=nil{return nil,e};branches:=[]map[string]any{};for _,line:=range strings.Split(strings.TrimSpace(out),"\n"){p:=strings.Split(line,"\t");if len(p)==3{branches=append(branches,map[string]any{"current":strings.TrimSpace(p[0])=="*","name":p[1],"sha":p[2]})}};return map[string]any{"branches":branches},nil}
	if !validGitRefName(name){return nil,errors.New("name must be a valid Git branch name")}
	if boolArg(a,"dry_run",false){return map[string]any{"dry_run":true,"changed":false,"action":action,"name":name,"start_point":stringArg(a,"start_point")},nil}
	if !hasToolApproval(ctx,"git.branch",raw){return nil,errors.New("branch mutation requires explicit approval")}
	switch action{
	case "create":args:=[]string{"branch",name};if start:=stringArg(a,"start_point");start!=""{if strings.HasPrefix(start,"-"){return nil,errors.New("invalid start_point")};args=append(args,start)};out,e:=runGitOutput(ctx,rt.Root,args...);if e!=nil{return nil,e};return map[string]any{"created":true,"name":name,"output":out},nil
	case "switch":status,e:=gitStatusClean(rt.Root);if e!=nil{return nil,e};if !status{return nil,errors.New("refusing to switch branches while the working tree has uncommitted changes; commit or stash first")};out,e:=runGitOutput(ctx,rt.Root,"switch","--",name);if e!=nil{return nil,e};return map[string]any{"switched":true,"name":name,"output":out},nil
	case "delete":current,e:=runGitOutput(ctx,rt.Root,"branch","--show-current");if e!=nil{return nil,e};if strings.TrimSpace(current)==name{return nil,errors.New("cannot delete the currently checked-out branch")};out,e:=runGitOutput(ctx,rt.Root,"branch","-d","--",name);if e!=nil{return nil,e};return map[string]any{"deleted":true,"name":name,"output":out},nil
	default:return nil,errors.New("unsupported branch action")
	}
}
func gitStatusClean(root string)(bool,error){out,e:=runGitOutput(context.Background(),root,"status","--porcelain");if e!=nil{return false,e};return strings.TrimSpace(out)=="",nil}

func (rt ToolRuntime) gitStash(ctx context.Context,raw json.RawMessage,a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.RunGit); err != nil { return nil, err }
	action:=stringArg(a,"action");ref:=stringArg(a,"stash_ref");if ref==""{ref="stash@{0}"}
	if action=="list"{out,e:=runGitOutput(ctx,rt.Root,"stash","list","--format=%gd%x09%gs");if e!=nil{return nil,e};items:=[]map[string]string{};for _,line:=range strings.Split(strings.TrimSpace(out),"\n"){if line==""{continue};p:=strings.SplitN(line,"\t",2);if len(p)==2{items=append(items,map[string]string{"ref":p[0],"message":p[1]})}};return map[string]any{"stashes":items},nil}
	if action!="push"&&!regexp.MustCompile(`^stash@\{[0-9]+\}$`).MatchString(ref){return nil,errors.New("stash_ref must be like stash@{0}")}
	if boolArg(a,"dry_run",false){return map[string]any{"dry_run":true,"changed":false,"action":action,"stash_ref":ref},nil}
	if !hasToolApproval(ctx,"git.stash",raw){return nil,errors.New("stash mutation requires explicit approval")}
	var args []string
	switch action{
	case "push":args=[]string{"stash","push"};if boolArg(a,"include_untracked",false){args=append(args,"--include-untracked")};if msg:=stringArg(a,"message");msg!=""{args=append(args,"-m",msg)}
	case "apply":args=[]string{"stash","apply",ref}
	case "pop":args=[]string{"stash","pop",ref}
	case "drop":args=[]string{"stash","drop",ref}
	default:return nil,errors.New("unsupported stash action")
	}
	out,e:=runGitOutput(ctx,rt.Root,args...);if e!=nil{return nil,e};return map[string]any{"action":action,"stash_ref":ref,"output":out},nil
}

func (rt ToolRuntime) webSearch(ctx context.Context,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.NetworkAccess);err!=nil{return nil,err}
	query:=stringArg(a,"query");endpoint:="https://html.duckduckgo.com/html/?q="+url.QueryEscape(query)
	req,e:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if e!=nil{return nil,e};req.Header.Set("User-Agent","FuzeCLI/1.0 (technical research)")
	client:=&http.Client{Timeout:12*time.Second,CheckRedirect:func(req *http.Request,via []*http.Request)error{if len(via)>3{return errors.New("too many redirects")};if req.URL.Hostname()!="html.duckduckgo.com"{return errors.New("search redirected to an unapproved host")};return nil}}
	resp,e:=client.Do(req);if e!=nil{return nil,e};defer resp.Body.Close();data,e:=io.ReadAll(io.LimitReader(resp.Body,2<<20));if e!=nil{return nil,e};if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("web search returned HTTP %d",resp.StatusCode)}
	html:=string(data);resultRE:=regexp.MustCompile(`(?is)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`);snippetRE:=regexp.MustCompile(`(?is)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>|<td[^>]+class="result__snippet"[^>]*>(.*?)</td>`)
	matches:=resultRE.FindAllStringSubmatch(html,intArg(a,"max_results",5));results:=[]map[string]string{}
	for _,m:=range matches{link:=htmlUnescape(stripHTML(m[1]));title:=htmlUnescape(stripHTML(m[2]));if strings.HasPrefix(link,"//"){link="https:"+link};if !strings.HasPrefix(link,"https://"){continue};results=append(results,map[string]string{"title":truncate(strings.TrimSpace(title),300),"url":link})}
	_ = snippetRE
	return map[string]any{"query":query,"results":results,"count":len(results),"source":"DuckDuckGo HTML results","note":"Search snippets may be incomplete; open primary documentation to verify details."},nil
}
func (rt ToolRuntime) webOpen(ctx context.Context,a map[string]any)(any,error){
	if err:=rt.Capabilities.Require(security.NetworkAccess);err!=nil{return nil,err}
	u,e:=url.Parse(stringArg(a,"url"));if e!=nil||u.Scheme!="https"||u.Hostname()==""||u.User!=nil{return nil,errors.New("url must be an HTTPS URL without embedded credentials")}
	if !hostInAllowlist(u.Hostname(),os.Getenv("FUZECLI_WEB_ALLOWED_HOSTS")){return nil,errors.New("web host is not allowlisted; add its hostname to FUZECLI_WEB_ALLOWED_HOSTS")};if !isPublicNetworkHost(u.Hostname()){return nil,errors.New("web host resolves to a local, private, or reserved address")}
	req,e:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if e!=nil{return nil,e};req.Header.Set("User-Agent","FuzeCLI/1.0")
	client:=&http.Client{Timeout:15*time.Second,CheckRedirect:func(r *http.Request,via []*http.Request)error{if len(via)>3||r.URL.Scheme!="https"||!hostInAllowlist(r.URL.Hostname(),os.Getenv("FUZECLI_WEB_ALLOWED_HOSTS"))||!isPublicNetworkHost(r.URL.Hostname()){return errors.New("redirect target is not allowlisted or is not public")};return nil}}
	resp,e:=client.Do(req);if e!=nil{return nil,e};defer resp.Body.Close();body,e:=io.ReadAll(io.LimitReader(resp.Body,2<<20));if e!=nil{return nil,e};if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("web open returned HTTP %d",resp.StatusCode)}
	text:=string(body);if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")),"html"){text=stripHTML(regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`).ReplaceAllString(text," "));text=htmlUnescape(text)}
	max:=intArg(a,"max_chars",12000);return map[string]any{"url":resp.Request.URL.String(),"status":resp.StatusCode,"content_type":resp.Header.Get("Content-Type"),"text":truncate(strings.TrimSpace(text),max),"truncated":len(text)>max},nil
}
func isPublicNetworkHost(host string) bool { host = strings.ToLower(strings.TrimSuffix(host,".")); if host=="localhost" || strings.HasSuffix(host,".localhost") || strings.HasSuffix(host,".local") { return false }; ips,err:=net.LookupIP(host); if err!=nil || len(ips)==0 { return false }; for _,ip:=range ips { if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() { return false } }; return true }
func hostInAllowlist(host,raw string)bool{host=strings.ToLower(strings.TrimSuffix(host,"."));for _,item:=range strings.Split(raw,","){allowed:=strings.ToLower(strings.TrimSpace(item));if allowed!=""&&host==strings.TrimSuffix(allowed,"."){return true}};return false}
var htmlTagRE=regexp.MustCompile(`(?s)<[^>]*>`)
func stripHTML(s string)string{s=htmlTagRE.ReplaceAllString(s," ");s=strings.ReplaceAll(s,"&nbsp;"," ");s=strings.ReplaceAll(s,"&#39;","'");s=strings.ReplaceAll(s,"&quot;",`"`);s=strings.ReplaceAll(s,"&amp;","&");s=strings.ReplaceAll(s,"&lt;","<");s=strings.ReplaceAll(s,"&gt;",">");return strings.Join(strings.Fields(s)," ")}
func htmlUnescape(s string)string{r:=strings.NewReplacer("&amp;","&","&quot;",`"`,"&#39;","'","&lt;","<","&gt;",">","&nbsp;"," ");return r.Replace(s)}

func githubRequest(ctx context.Context,cToken,method,path string,out any)error{
	if strings.TrimSpace(cToken)==""{
		if _,e:=exec.LookPath("gh");e!=nil{return errors.New("GitHub is not authenticated")}
		args:=[]string{"api","--hostname","github.com","--method",method,strings.TrimPrefix(path,"/")};cmd:=exec.CommandContext(ctx,"gh",args...);b,e:=cmd.Output();if e!=nil{var ee *exec.ExitError;if errors.As(e,&ee)&&len(ee.Stderr)>0{return fmt.Errorf("GitHub CLI request failed: %s",truncate(string(ee.Stderr),1000))};return e};if out!=nil{return json.Unmarshal(b,out)};return nil
	}
	req,e:=http.NewRequestWithContext(ctx,method,"https://api.github.com"+path,nil);if e!=nil{return e};req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28");req.Header.Set("Authorization","Bearer "+cToken)
	client:=&http.Client{Timeout:15*time.Second};resp,e:=client.Do(req);if e!=nil{return e};defer resp.Body.Close();b,e:=io.ReadAll(io.LimitReader(resp.Body,1<<20));if e!=nil{return e};if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("GitHub API returned HTTP %d: %s",resp.StatusCode,truncate(string(b),1000))};if out!=nil{return json.Unmarshal(b,out)};return nil
}
func (rt ToolRuntime) githubSearchCode(ctx context.Context,a map[string]any)(any,error){
	c,e:=rt.githubClient();if e!=nil{return nil,e};q:=stringArg(a,"query");if repo:=stringArg(a,"repository");repo!=""{if !validRepositoryIdentifier(repo){return nil,errors.New("repository must be owner/name")};q+=" repo:"+repo}
	var data struct{Total int `json:"total_count"`;Items []struct{Name string `json:"name"`;Path string `json:"path"`;HTMLURL string `json:"html_url"`;Repository struct{FullName string `json:"full_name"`} `json:"repository"`} `json:"items"`}
	path:="/search/code?q="+url.QueryEscape(q)+"&per_page="+strconv.Itoa(intArg(a,"per_page",20))
	if e=githubRequest(ctx,c.Token,http.MethodGet,path,&data);e!=nil{return nil,e}
	return map[string]any{"total_count":data.Total,"items":data.Items},nil
}
func (rt ToolRuntime) githubReadFile(ctx context.Context,a map[string]any)(any,error){
	c,e:=rt.githubClient();if e!=nil{return nil,e};repo,path:=stringArg(a,"repository"),stringArg(a,"path");if !validRepositoryIdentifier(repo){return nil,errors.New("repository must be owner/name")}
	parts:=strings.Split(path,"/");for i,p:=range parts{parts[i]=url.PathEscape(p)}
	apiPath:="/repos/"+repo+"/contents/"+strings.Join(parts,"/");if ref:=stringArg(a,"ref");ref!=""{apiPath+="?ref="+url.QueryEscape(ref)}
	var data struct{Type string `json:"type"`;Encoding string `json:"encoding"`;Content string `json:"content"`;Size int `json:"size"`;Path string `json:"path"`;SHA string `json:"sha"`;HTMLURL string `json:"html_url"`}
	if e=githubRequest(ctx,c.Token,http.MethodGet,apiPath,&data);e!=nil{return nil,e};if data.Type!="file"{return nil,errors.New("GitHub path is not a file")};if data.Size>intArg(a,"max_bytes",100000){return nil,errors.New("GitHub file exceeds max_bytes")};content,e:=base64.StdEncoding.DecodeString(strings.ReplaceAll(data.Content,"\n",""));if e!=nil{return nil,e};if !utf8.Valid(content){return nil,errors.New("GitHub file is not UTF-8 text")}
	return map[string]any{"repository":repo,"path":data.Path,"sha":data.SHA,"url":data.HTMLURL,"content":string(content)},nil
}

func (rt ToolRuntime) httpRequest(ctx context.Context,a map[string]any)(any,error){
	method:=stringArg(a,"method");u,e:=url.Parse(stringArg(a,"url"));if e!=nil||u.Hostname()==""||u.User!=nil||(u.Scheme!="http"&&u.Scheme!="https"){return nil,errors.New("url must be a valid HTTP or HTTPS URL without embedded credentials")}
	host:=strings.ToLower(u.Hostname());local:=host=="localhost"||host=="127.0.0.1"||host=="::1"
	if local { if u.Scheme!="http"&&u.Scheme!="https"{return nil,errors.New("unsupported local URL scheme")} } else {
		if u.Scheme!="https"{return nil,errors.New("remote HTTP requests must use HTTPS")}
		if err:=rt.Capabilities.Require(security.NetworkAccess);err!=nil{return nil,err}
		if !hostInAllowlist(host,os.Getenv("FUZECLI_HTTP_ALLOWED_HOSTS")){return nil,errors.New("remote host is not allowlisted; add its hostname to FUZECLI_HTTP_ALLOWED_HOSTS")}
		ips,er:=net.LookupIP(host);if er!=nil{return nil,er};for _,ip:=range ips{if ip.IsPrivate()||ip.IsLoopback()||ip.IsLinkLocalUnicast()||ip.IsLinkLocalMulticast()||ip.IsUnspecified(){return nil,errors.New("remote hostname resolves to a private or reserved address")}}
	}
	var body io.Reader;if value,ok:=a["body"].(string);ok{body=strings.NewReader(value)}
	req,e:=http.NewRequestWithContext(ctx,method,u.String(),body);if e!=nil{return nil,e}
	if headers,ok:=a["headers"].(map[string]any);ok{for k,v:=range headers{value,ok:=v.(string);if !ok{return nil,fmt.Errorf("header %q value must be a string",k)};if strings.ContainsAny(k+value,"\r\n"){return nil,errors.New("headers cannot contain CR/LF")};if strings.EqualFold(k,"Host")||strings.EqualFold(k,"Proxy-Authorization"){return nil,fmt.Errorf("header %q is not permitted",k)};req.Header.Set(k,value)}}
	timeout:=time.Duration(intArg(a,"timeout_seconds",10))*time.Second
	client:=&http.Client{Timeout:timeout,CheckRedirect:func(r *http.Request,via []*http.Request)error{if len(via)>=3{return errors.New("too many redirects")};rh:=strings.ToLower(r.URL.Hostname());if local { if rh=="localhost"||rh=="127.0.0.1"||rh=="::1" { return nil }; return errors.New("redirect target is not localhost") }; if r.URL.Scheme=="https"&&hostInAllowlist(rh,os.Getenv("FUZECLI_HTTP_ALLOWED_HOSTS")) { return nil }; return errors.New("redirect target is not allowlisted")}}
	resp,e:=client.Do(req);if e!=nil{return nil,e};defer resp.Body.Close();data,e:=io.ReadAll(io.LimitReader(resp.Body,tier1MaxBody+1));if e!=nil{return nil,e};truncated:=len(data)>tier1MaxBody;if truncated{data=data[:tier1MaxBody]}
	headers:=map[string]string{};for k,v:=range resp.Header{if strings.EqualFold(k,"Set-Cookie")||strings.EqualFold(k,"Authorization")||strings.EqualFold(k,"Proxy-Authenticate"){continue};headers[k]=strings.Join(v,", ")}
	return map[string]any{"status":resp.StatusCode,"status_text":resp.Status,"headers":headers,"body":redactHTTPSecrets(string(data)),"truncated":truncated,"url":resp.Request.URL.String()},nil
}

func (rt ToolRuntime) databaseInspect(a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	max:=intArg(a,"max_results",30);policy,e:=workspace.LoadIgnorePolicy(rt.Root);if e!=nil{return nil,e};dbs:=[]string{};configKeys:=[]string{"DATABASE_URL","DB_HOST","DB_NAME","DB_DATABASE","SQLITE_PATH","MYSQL_URL","POSTGRES_URL"}
	entries,e:=os.ReadDir(rt.Root);if e!=nil{return nil,e};_ = entries
	e=filepath.WalkDir(rt.Root,func(path string,d os.DirEntry,walkErr error)error{if walkErr!=nil{return walkErr};if path==rt.Root{return nil};rel,er:=filepath.Rel(rt.Root,path);if er!=nil{return er};rel=filepath.ToSlash(rel);if d.IsDir()&&(rel==".git"||strings.HasPrefix(rel,".git/")||rel==".aicli"||strings.HasPrefix(rel,".aicli/")||policy.Ignored(rel)){return filepath.SkipDir};if d.IsDir(){return nil};ext:=strings.ToLower(filepath.Ext(rel));if ext==".sqlite"||ext==".sqlite3"||ext==".db"{dbs=append(dbs,rel);if len(dbs)>=max{return errToolWalkLimit}};return nil})
	if e!=nil&&!errors.Is(e,errToolWalkLimit){return nil,e};present:=[]string{};for _,k:=range configKeys{if strings.TrimSpace(os.Getenv(k))!=""{present=append(present,k)}}
	return map[string]any{"sqlite_files":dbs,"count":len(dbs),"truncated":errors.Is(e,errToolWalkLimit),"configuration_keys_present":present,"credentials_exposed":false,"supported_database":"SQLite workspace files only"},nil
}
func (rt ToolRuntime) openSQLite(rel string)(*sql.DB,error){
	path,e:=rt.resolve(rel,false);if e!=nil{return nil,e};inf,e:=os.Stat(path);if e!=nil{return nil,e};if !inf.Mode().IsRegular(){return nil,errors.New("database path must be a regular file")}
	ext:=strings.ToLower(filepath.Ext(path));if ext!=".db"&&ext!=".sqlite"&&ext!=".sqlite3"{return nil,errors.New("only SQLite .db, .sqlite, and .sqlite3 files are supported")}
	sqlitePath:=filepath.ToSlash(path);if runtime.GOOS=="windows"&&!strings.HasPrefix(sqlitePath,"/"){sqlitePath="/"+sqlitePath};u:=url.URL{Scheme:"file",Path:sqlitePath};dsn:=u.String()+"?mode=ro";db,e:=sql.Open("sqlite",dsn);if e!=nil{return nil,e};db.SetMaxOpenConns(1);db.SetMaxIdleConns(1);ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel();if e=db.PingContext(ctx);e!=nil{db.Close();return nil,e};return db,nil
}
func (rt ToolRuntime) databaseSchema(ctx context.Context, a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	db, err := rt.openSQLite(stringArg(a, "path"))
	if err != nil { return nil, err }
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT name, type, sql FROM sqlite_master WHERE type IN ('table','view','index','trigger') AND name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if err != nil { return nil, err }
	type dbObject struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Definition string `json:"definition"`
		Columns []map[string]any `json:"columns,omitempty"`
		ForeignKeys []map[string]any `json:"foreign_keys,omitempty"`
		Indexes []map[string]any `json:"indexes,omitempty"`
	}
	objects := []dbObject{}
	for rows.Next() {
		var name, typ string
		var definition sql.NullString
		if err = rows.Scan(&name, &typ, &definition); err != nil { rows.Close(); return nil, err }
		objects = append(objects, dbObject{Name:name, Type:typ, Definition:definition.String})
		if len(objects) >= 500 { rows.Close(); return nil, errors.New("schema exceeds 500 objects") }
	}
	if err = rows.Err(); err != nil { rows.Close(); return nil, err }
	if err = rows.Close(); err != nil { return nil, err }
	for i := range objects {
		if objects[i].Type != "table" && objects[i].Type != "view" { continue }
		safeName := strings.ReplaceAll(objects[i].Name, "`", "``")
		cols, er := db.QueryContext(ctx, "PRAGMA table_info(`"+safeName+"`)")
		if er != nil { return nil, er }
		columns := []map[string]any{}
		for cols.Next() {
			var cid, notnull, pk int
			var colName, colType string
			var def sql.NullString
			if er = cols.Scan(&cid, &colName, &colType, &notnull, &def, &pk); er != nil { cols.Close(); return nil, er }
			columns = append(columns, map[string]any{"name":colName, "type":colType, "not_null":notnull != 0, "default":def.String, "primary_key_position":pk})
		}
		if er = cols.Err(); er != nil { cols.Close(); return nil, er }
		if er = cols.Close(); er != nil { return nil, er }
		objects[i].Columns = columns
		if objects[i].Type != "table" { continue }
		fks, er := db.QueryContext(ctx, "PRAGMA foreign_key_list(`"+safeName+"`)")
		if er != nil { return nil, er }
		foreignKeys := []map[string]any{}
		for fks.Next() {
			var id, seq int
			var targetTable, fromCol, onUpdate, onDelete, match string
			var toCol sql.NullString
			if er = fks.Scan(&id, &seq, &targetTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); er != nil { fks.Close(); return nil, er }
			foreignKeys = append(foreignKeys, map[string]any{"id":id,"sequence":seq,"table":targetTable,"from":fromCol,"to":toCol.String,"on_update":onUpdate,"on_delete":onDelete,"match":match})
		}
		if er = fks.Err(); er != nil { fks.Close(); return nil, er }
		if er = fks.Close(); er != nil { return nil, er }
		objects[i].ForeignKeys = foreignKeys
		idxRows, er := db.QueryContext(ctx, "PRAGMA index_list(`"+safeName+"`)")
		if er != nil { return nil, er }
		type indexMeta struct { name, origin string; unique, partial int }
		indexMetas := []indexMeta{}
		for idxRows.Next() {
			var meta indexMeta
			var seq int
			if er = idxRows.Scan(&seq, &meta.name, &meta.unique, &meta.origin, &meta.partial); er != nil { idxRows.Close(); return nil, er }
			indexMetas = append(indexMetas, meta)
		}
		if er = idxRows.Err(); er != nil { idxRows.Close(); return nil, er }
		if er = idxRows.Close(); er != nil { return nil, er }
		indexes := []map[string]any{}
		for _, meta := range indexMetas {
			safeIndex := strings.ReplaceAll(meta.name, "`", "``")
			infoRows, infoErr := db.QueryContext(ctx, "PRAGMA index_info(`"+safeIndex+"`)")
			if infoErr != nil { return nil, infoErr }
			indexColumns := []string{}
			for infoRows.Next() {
				var seqno, cid int
				var col sql.NullString
				if infoErr = infoRows.Scan(&seqno, &cid, &col); infoErr != nil { infoRows.Close(); return nil, infoErr }
				if col.Valid { indexColumns = append(indexColumns, col.String) }
			}
			if infoErr = infoRows.Err(); infoErr != nil { infoRows.Close(); return nil, infoErr }
			if infoErr = infoRows.Close(); infoErr != nil { return nil, infoErr }
			indexes = append(indexes, map[string]any{"name":meta.name,"unique":meta.unique != 0,"origin":meta.origin,"partial":meta.partial != 0,"columns":indexColumns})
		}
		objects[i].Indexes = indexes
	}
	return map[string]any{"path":stringArg(a,"path"), "objects":objects, "count":len(objects), "read_only":true}, nil
}

func (rt ToolRuntime) databaseQuery(ctx context.Context,a map[string]any)(any,error){
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil { return nil, err }
	query:=strings.TrimSpace(stringArg(a,"query"));if query==""{return nil,errors.New("query must not be empty")};if strings.Contains(query,";") { trimmed:=strings.TrimSuffix(query,";");if strings.Contains(trimmed,";"){return nil,errors.New("only one SQL statement is allowed")};query=trimmed }
	first:=strings.ToUpper(strings.Fields(query)[0]);if first!="SELECT"&&first!="WITH"&&first!="EXPLAIN"&&first!="PRAGMA"{return nil,errors.New("database.query is read-only; only SELECT, WITH, EXPLAIN, and safe PRAGMA queries are allowed")}
	if first=="PRAGMA"{lower:=strings.ToLower(strings.TrimSpace(query));allowed:=false;for _,p:=range []string{"pragma table_info(","pragma table_info (","pragma foreign_key_list(","pragma foreign_key_list (","pragma index_list(","pragma index_list (","pragma index_info(","pragma index_info (","pragma database_list","pragma compile_options","pragma integrity_check","pragma quick_check","pragma foreign_keys","pragma user_version","pragma schema_version"}{if strings.HasPrefix(lower,p){allowed=true;break}};if !allowed{return nil,errors.New("this PRAGMA is not in the read-only allowlist")}}
	db,e:=rt.openSQLite(stringArg(a,"path"));if e!=nil{return nil,e};defer db.Close()
	cctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();if _,e=db.ExecContext(cctx,"PRAGMA query_only=ON");e!=nil{return nil,e}
	rows,e:=db.QueryContext(cctx,query);if e!=nil{return nil,e};defer rows.Close();cols,e:=rows.Columns();if e!=nil{return nil,e};max:=intArg(a,"max_rows",50);out:=[]map[string]any{}
	for rows.Next(){vals:=make([]any,len(cols));ptrs:=make([]any,len(cols));for i:=range vals{ptrs[i]=&vals[i]};if e=rows.Scan(ptrs...);e!=nil{return nil,e};row:=map[string]any{};for i,c:=range cols{switch v:=vals[i].(type){case []byte:row[c]=string(v);default:row[c]=v}};out=append(out,row);if len(out)>=max{break}}
	if e=rows.Err();e!=nil{return nil,e};return map[string]any{"columns":cols,"rows":out,"row_count":len(out),"truncated":len(out)>=max,"read_only":true},nil
}

var httpSecretRE = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|authorization)(["']?\s*[:=]\s*["']?)[^"'\s,;&}]+`)
func redactHTTPSecrets(s string) string { return httpSecretRE.ReplaceAllString(s, "$1$2[REDACTED]") }
