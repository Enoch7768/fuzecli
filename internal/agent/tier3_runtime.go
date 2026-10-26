package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/security"
	"github.com/gorilla/websocket"
)

const tier3ImageMaxBytes = 64 << 20

type tier3CDPTarget struct {
	ID string `json:"id"`
	Type string `json:"type"`
	Title string `json:"title"`
	URL string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func (rt ToolRuntime) executeTier3(ctx context.Context, name string, raw json.RawMessage, a map[string]any) (any, error) {
	switch name {
	case "image.inspect":
		return rt.imageInspect(a)
	case "image.compare":
		return rt.imageCompare(a)
	case "browser.open":
		return rt.browserOpen(ctx, a)
	case "browser.click":
		return rt.browserClick(ctx, a)
	case "browser.type":
		return rt.browserType(ctx, a)
	case "browser.select":
		return rt.browserSelect(ctx, a)
	case "browser.screenshot":
		return rt.browserScreenshot(ctx, raw, a)
	case "browser.inspect":
		return rt.browserInspect(ctx, a)
	case "browser.console":
		return rt.browserConsole(ctx, a)
	case "browser.network":
		return rt.browserNetwork(ctx, a)
	case "deploy.inspect":
		return rt.deployInspect(a)
	case "deploy.run":
		return rt.deployRun(ctx, raw, a)
	default:
		return nil, fmt.Errorf("unsupported Tier 3 tool %q", name)
	}
}

func (rt ToolRuntime) imageInspect(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil {
		return nil, err
	}
	rel := stringArg(a, "path")
	path, err := rt.resolve(rel, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("image.inspect path must be a file")
	}
	if info.Size() > tier3ImageMaxBytes {
		return nil, fmt.Errorf("image exceeds the %d-byte inspection limit", tier3ImageMaxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("unsupported or invalid image (supported formats: PNG, JPEG, GIF): %w", err)
	}
	return map[string]any{
		"path": filepath.ToSlash(rel), "format": strings.ToLower(format),
		"width": cfg.Width, "height": cfg.Height, "pixels": int64(cfg.Width) * int64(cfg.Height),
		"size_bytes": info.Size(), "mod_time": info.ModTime().UTC().Format(time.RFC3339),
	}, nil
}

func (rt ToolRuntime) imageCompare(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil {
		return nil, err
	}
	left, err := rt.decodeWorkspaceImage(stringArg(a, "path_a"))
	if err != nil {
		return nil, fmt.Errorf("path_a: %w", err)
	}
	right, err := rt.decodeWorkspaceImage(stringArg(a, "path_b"))
	if err != nil {
		return nil, fmt.Errorf("path_b: %w", err)
	}
	maxPixels := intArg(a, "max_pixels", 250000)
	lb, rb := left.Bounds(), right.Bounds()
	lw, lh, rw, rh := lb.Dx(), lb.Dy(), rb.Dx(), rb.Dy()
	lpixels, rpixels := int64(lw)*int64(lh), int64(rw)*int64(rh)
	if lpixels > 16_000_000 || rpixels > 16_000_000 {
		return nil, errors.New("image.compare limits each decoded image to 16 million pixels")
	}
	width, height := min(lw, rw), min(lh, rh)
	total := int64(width) * int64(height)
	if total == 0 {
		return nil, errors.New("image dimensions must be non-zero")
	}
	step := int(math.Ceil(math.Sqrt(float64(total) / float64(maxPixels))))
	if step < 1 {
		step = 1
	}
	var absolute float64
	var exact, sampled int64
	for y := 0; y < height; y += step {
		for x := 0; x < width; x += step {
			r1, g1, b1, a1 := left.At(lb.Min.X+x, lb.Min.Y+y).RGBA()
			r2, g2, b2, a2 := right.At(rb.Min.X+x, rb.Min.Y+y).RGBA()
			diff := (math.Abs(float64(r1)-float64(r2)) + math.Abs(float64(g1)-float64(g2)) + math.Abs(float64(b1)-float64(b2)) + math.Abs(float64(a1)-float64(a2))) / (4 * 65535)
			absolute += diff
			if diff == 0 {
				exact++
			}
			sampled++
		}
	}
	mean := absolute / float64(sampled)
	return map[string]any{
		"path_a": stringArg(a, "path_a"), "path_b": stringArg(a, "path_b"),
		"dimensions_a": map[string]int{"width": lw, "height": lh},
		"dimensions_b": map[string]int{"width": rw, "height": rh},
		"same_dimensions": lw == rw && lh == rh,
		"sampled_pixels": sampled, "mean_absolute_channel_difference": math.Round(mean*1_000_000) / 1_000_000,
		"exact_sample_match_ratio": math.Round(float64(exact)/float64(sampled)*1_000_000) / 1_000_000,
		"comparison": "pixel-level sampled comparison only; does not establish semantic or perceptual equivalence",
	}, nil
}

func (rt ToolRuntime) decodeWorkspaceImage(rel string) (image.Image, error) {
	path, err := rt.resolve(rel, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("image path must be a file")
	}
	if info.Size() > tier3ImageMaxBytes {
		return nil, fmt.Errorf("image exceeds the %d-byte limit", tier3ImageMaxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil, errors.New("image dimensions are invalid or exceed 16 million pixels")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	return img, err
}

func tier3CDPBase() (string, error) {
	raw := strings.TrimSpace(os.Getenv("FUZECLI_BROWSER_CDP_URL"))
	if raw == "" {
		raw = "http://127.0.0.1:9222"
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
		return "", errors.New("FUZECLI_BROWSER_CDP_URL must be an HTTP(S) URL to a local Chrome DevTools endpoint")
	}
	host := strings.Trim(u.Hostname(), "[]")
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("Chrome DevTools endpoint must use localhost or a loopback IP address")
	}
	return strings.TrimRight(raw, "/"), nil
}

func tier3BrowserURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", errors.New("browser URL must be a valid HTTP(S) URL without embedded credentials")
	}
	return u.String(), nil
}

func tier3CDPHTTP(ctx context.Context, method, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Chrome DevTools is unavailable at the configured local endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Chrome DevTools returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func tier3Target(ctx context.Context, targetID string) (tier3CDPTarget, error) {
	base, err := tier3CDPBase()
	if err != nil {
		return tier3CDPTarget{}, err
	}
	data, err := tier3CDPHTTP(ctx, http.MethodGet, base+"/json/list")
	if err != nil {
		return tier3CDPTarget{}, err
	}
	var targets []tier3CDPTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return tier3CDPTarget{}, err
	}
	for _, target := range targets {
		if target.ID == targetID && target.Type == "page" && strings.HasPrefix(target.WebSocketDebuggerURL, "ws") {
			return target, nil
		}
	}
	return tier3CDPTarget{}, fmt.Errorf("Chrome target %q was not found; use browser.open to create a target", targetID)
}

func tier3CDPCommand(ctx context.Context, target tier3CDPTarget, method string, params map[string]any) (map[string]any, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, Proxy: nil}
	headers := http.Header{}
	headers.Set("Origin", "http://localhost")
	conn, _, err := dialer.DialContext(ctx, target.WebSocketDebuggerURL, headers)
	if err != nil {
		return nil, fmt.Errorf("could not connect to the local Chrome target: %w", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	request := map[string]any{"id": 1, "method": method}
	if params != nil {
		request["params"] = params
	}
	if err := conn.WriteJSON(request); err != nil {
		return nil, err
	}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var message struct {
			ID int `json:"id"`
			Result map[string]any `json:"result"`
			Error *struct {
				Code int `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		if message.ID != 1 {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("Chrome DevTools %s failed (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	}
}

func tier3Eval(ctx context.Context, target tier3CDPTarget, expression string) (any, error) {
	result, err := tier3CDPCommand(ctx, target, "Runtime.evaluate", map[string]any{
		"expression": expression, "returnByValue": true, "awaitPromise": true, "userGesture": true,
	})
	if err != nil {
		return nil, err
	}
	if details, ok := result["exceptionDetails"].(map[string]any); ok {
		text := "JavaScript evaluation failed"
		if exception, ok := details["exception"].(map[string]any); ok {
			if description, ok := exception["description"].(string); ok {
				text = description
			}
		}
		return nil, errors.New(text)
	}
	remote, _ := result["result"].(map[string]any)
	return remote["value"], nil
}

func tier3JSString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func tier3InstallConsoleCapture(ctx context.Context, target tier3CDPTarget) {
	const expression = `(()=>{if(window.__fuzecliConsoleBuffer)return true;const b=[];Object.defineProperty(window,'__fuzecliConsoleBuffer',{value:b,configurable:true});for(const k of ['log','info','warn','error','debug']){const old=console[k].bind(console);console[k]=(...a)=>{try{b.push({type:k,time:new Date().toISOString(),message:a.map(x=>{try{return typeof x==='string'?x:JSON.stringify(x)}catch(e){return String(x)}}).join(' ').slice(0,2000)});if(b.length>500)b.shift()}catch(e){}old(...a)}}return true})()`
	_, _ = tier3Eval(ctx, target, expression)
}

func (rt ToolRuntime) browserOpen(ctx context.Context, a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.NetworkAccess); err != nil {
		return nil, err
	}
	pageURL, err := tier3BrowserURL(stringArg(a, "url"))
	if err != nil {
		return nil, err
	}
	base, err := tier3CDPBase()
	if err != nil {
		return nil, err
	}
	data, err := tier3CDPHTTP(ctx, http.MethodPut, base+"/json/new?"+url.QueryEscape(pageURL))
	if err != nil {
		return nil, err
	}
	var target tier3CDPTarget
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, fmt.Errorf("Chrome returned an invalid target response: %w", err)
	}
	if target.ID == "" {
		return nil, errors.New("Chrome created a target without an ID")
	}
	target, err = tier3Target(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	_, _ = tier3CDPCommand(ctx, target, "Page.enable", nil)
	_, _ = tier3CDPCommand(ctx, target, "Runtime.enable", nil)
	tier3InstallConsoleCapture(ctx, target)
	wait := time.Duration(intArg(a, "wait_ms", 500)) * time.Millisecond
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	info, _ := tier3Eval(ctx, target, `({title:document.title,url:location.href,readyState:document.readyState})`)
	return map[string]any{"target_id": target.ID, "url": pageURL, "page": info, "console_capture": "instrumented after initial navigation; earlier messages may be missing"}, nil
}

func (rt ToolRuntime) browserClick(ctx context.Context, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	expression := `(()=>{const s=` + tier3JSString(stringArg(a, "selector")) + `;const e=document.querySelector(s);if(!e)throw new Error('No element matches selector: '+s);e.click();return {tag:e.tagName,text:(e.innerText||e.value||'').toString().slice(0,500)}})()`
	result, err := tier3Eval(ctx, target, expression)
	if err != nil {
		return nil, err
	}
	return map[string]any{"clicked": true, "element": result}, nil
}

func (rt ToolRuntime) browserType(ctx context.Context, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	selector := tier3JSString(stringArg(a, "selector"))
	focus := `(()=>{const s=` + selector + `;const e=document.querySelector(s);if(!e)throw new Error('No element matches selector: '+s);e.focus();return {tag:e.tagName,editable:!!(e.isContentEditable||('value' in e))}})()`
	value, err := tier3Eval(ctx, target, focus)
	if err != nil {
		return nil, err
	}
	meta, _ := value.(map[string]any)
	if meta["editable"] != true {
		return nil, errors.New("selected element is not an input, textarea, or contenteditable element")
	}
	if _, err := tier3CDPCommand(ctx, target, "Input.insertText", map[string]any{"text": stringArg(a, "text")}); err != nil {
		return nil, err
	}
	return map[string]any{"typed": true, "selector": stringArg(a, "selector"), "characters": len([]rune(stringArg(a, "text")))}, nil
}

func (rt ToolRuntime) browserSelect(ctx context.Context, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	expression := `(()=>{const s=` + tier3JSString(stringArg(a, "selector")) + `;const v=` + tier3JSString(stringArg(a, "value")) + `;const e=document.querySelector(s);if(!e)throw new Error('No element matches selector: '+s);if(!(e instanceof HTMLSelectElement))throw new Error('Selected element is not an HTML select');if(!Array.from(e.options).some(o=>o.value===v))throw new Error('No option has the requested value');e.value=v;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return {selected:e.value,label:e.selectedOptions[0]?.text||''}})()`
	result, err := tier3Eval(ctx, target, expression)
	if err != nil {
		return nil, err
	}
	return map[string]any{"selected": result}, nil
}

func (rt ToolRuntime) browserScreenshot(ctx context.Context, raw json.RawMessage, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	format := stringArg(a, "format")
	if format == "" {
		format = "png"
	}
	params := map[string]any{"format": format, "captureBeyondViewport": false, "fromSurface": true}
	if format == "jpeg" {
		params["quality"] = intArg(a, "quality", 70)
	}
	result, err := tier3CDPCommand(ctx, target, "Page.captureScreenshot", params)
	if err != nil {
		return nil, err
	}
	data, _ := result["data"].(string)
	if data == "" {
		return nil, errors.New("Chrome returned an empty screenshot")
	}
	if path := stringArg(a, "path"); path != "" {
		if err := rt.Capabilities.Require(security.WriteWorkspace); err != nil {
			return nil, err
		}
		if !hasToolApproval(ctx, "browser.screenshot", raw) {
			return nil, errors.New("saving a browser screenshot requires explicit approval for this exact operation")
		}
		rel := filepath.ToSlash(filepath.Clean(path))
		if strings.EqualFold(filepath.Ext(rel), "") {
			rel += "." + format
		}
		if strings.ToLower(filepath.Ext(rel)) != "."+format {
			return nil, fmt.Errorf("screenshot path extension must match %s format", format)
		}
		resolved, err := rt.resolve(rel, false)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(resolved); err == nil {
			return nil, errors.New("screenshot destination already exists; refusing to overwrite")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, err
		}
		if len(decoded) > tier3ImageMaxBytes {
			return nil, errors.New("screenshot exceeds the 64 MiB save limit")
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(resolved, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write(decoded); err != nil {
			_ = f.Close()
			_ = os.Remove(resolved)
			return nil, err
		}
		if err = f.Close(); err != nil {
			_ = os.Remove(resolved)
			return nil, err
		}
		return map[string]any{"saved": true, "path": rel, "format": format, "bytes": len(decoded)}, nil
	}
	if len(data) > 900_000 {
		return nil, errors.New("screenshot data is too large to return inline; provide a workspace path to save it")
	}
	return map[string]any{"format": format, "base64": data, "data_uri_prefix": "data:image/" + format + ";base64,", "bytes_base64": len(data)}, nil
}

func (rt ToolRuntime) browserInspect(ctx context.Context, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	maxChars := intArg(a, "max_chars", 6000)
	selector := stringArg(a, "selector")
	expression := `(()=>{const s=` + tier3JSString(selector) + `;const e=s?document.querySelector(s):null;return {title:document.title,url:location.href,readyState:document.readyState,visibleText:(document.body?.innerText||'').slice(0,` + fmt.Sprint(maxChars) + `),selector:s||null,element:e?{tag:e.tagName,id:e.id||'',className:typeof e.className==='string'?e.className:'',text:(e.innerText||e.value||'').toString().slice(0,2000),outerHTML:e.outerHTML.slice(0,3000)}:null}})()`
	result, err := tier3Eval(ctx, target, expression)
	if err != nil {
		return nil, err
	}
	if selector != "" {
		if m, ok := result.(map[string]any); ok && m["element"] == nil {
			return nil, fmt.Errorf("no element matches selector %q", selector)
		}
	}
	return result, nil
}

func (rt ToolRuntime) browserConsole(ctx context.Context, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	max := intArg(a, "max_entries", 50)
	expression := `(()=>{const b=window.__fuzecliConsoleBuffer||[];const out=b.slice(-` + fmt.Sprint(max) + `);if(` + fmt.Sprint(boolArg(a, "clear", false)) + `)b.length=0;return {instrumented:!!window.__fuzecliConsoleBuffer,entries:out,count:out.length,note:window.__fuzecliConsoleBuffer?'Captures console calls after instrumentation only':'Console capture is not installed for this page; reopen it with browser.open'}})()`
	return tier3Eval(ctx, target, expression)
}

func (rt ToolRuntime) browserNetwork(ctx context.Context, a map[string]any) (any, error) {
	target, err := tier3Target(ctx, stringArg(a, "target_id"))
	if err != nil {
		return nil, err
	}
	max := intArg(a, "max_entries", 50)
	expression := `(()=>{const entries=performance.getEntriesByType('resource').slice(-` + fmt.Sprint(max) + `).map(e=>{let u;try{u=new URL(e.name);u.username='';u.password='';u.search='';u.hash=''}catch(u){}return {url:u?u.origin+u.pathname:'[invalid-url]',initiatorType:e.initiatorType,durationMs:Math.round(e.duration*100)/100,transferSize:e.transferSize||null,encodedBodySize:e.encodedBodySize||null}});return {entries,count:entries.length,totalResourceEntries:performance.getEntriesByType('resource').length,note:'Resource timing only; HTTP status codes, response bodies, and failed requests are not available through this view'}})()`
	return tier3Eval(ctx, target, expression)
}

func (rt ToolRuntime) deployInspect(a map[string]any) (any, error) {
	if err := rt.Capabilities.Require(security.ReadWorkspace); err != nil {
		return nil, err
	}
	provider := stringArg(a, "provider")
	files := []string{"vercel.json", "netlify.toml", "netlify.json", "fly.toml", "railway.json", "railway.toml", "Dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml", "Procfile"}
	found := []string{}
	for _, rel := range files {
		if _, err := rt.resolve(rel, false); err != nil {
			continue
		}
		if info, err := os.Stat(filepath.Join(rt.Root, filepath.FromSlash(rel))); err == nil && !info.IsDir() {
			found = append(found, rel)
		}
	}
	providers := map[string]string{"vercel": "vercel", "netlify": "netlify", "fly": "fly", "railway": "railway"}
	available := map[string]bool{}
	for name, exe := range providers {
		_, err := exec.LookPath(exe)
		available[name] = err == nil
	}
	detected := []string{}
	if tier3Exists(rt.Root, "vercel.json") { detected = append(detected, "vercel") }
	if tier3Exists(rt.Root, "netlify.toml") || tier3Exists(rt.Root, "netlify.json") { detected = append(detected, "netlify") }
	if tier3Exists(rt.Root, "fly.toml") { detected = append(detected, "fly") }
	if tier3Exists(rt.Root, "railway.json") || tier3Exists(rt.Root, "railway.toml") { detected = append(detected, "railway") }
	if provider != "" && provider != "auto" {
		detected = []string{provider}
	}
	sort.Strings(found)
	sort.Strings(detected)
	return map[string]any{"manifest_files": found, "detected_providers": detected, "cli_available": available, "deployment_executed": false, "notes": []string{"Inspection does not authenticate, build, or deploy.", "CLI presence does not prove project linkage or valid provider credentials."}}, nil
}

func (rt ToolRuntime) deployRun(ctx context.Context, raw json.RawMessage, a map[string]any) (any, error) {
	provider := stringArg(a, "provider")
	directory := strings.TrimSpace(stringArg(a, "directory"))
	if directory == "" { directory = "." }
	if directory != "." {
		resolved, err := rt.resolve(directory, false)
		if err != nil { return nil, err }
		info, err := os.Stat(resolved)
		if err != nil { return nil, err }
		if !info.IsDir() { return nil, errors.New("deployment directory must be a workspace directory") }
	}
	production := boolArg(a, "production", false)
	var exe string
	var args []string
	switch provider {
	case "vercel":
		exe = "vercel"
		args = []string{"deploy", "--yes"}
		if production { args = append(args, "--prod") }
		if directory != "." { args = append(args, directory) }
	case "netlify":
		exe = "netlify"
		args = []string{"deploy", "--dir", directory, "--message", "FuzeCLI deployment"}
		if production { args = append(args, "--prod") }
	case "fly":
		exe = "fly"
		args = []string{"deploy", "--yes"}
		if production { args = append(args, "--strategy", "rolling") }
		if directory != "." { args = append(args, directory) }
	case "railway":
		exe = "railway"
		args = []string{"up", "--detach"}
		if production { args = append(args, "--environment", "production") }
		if directory != "." { args = append(args, "--path-as-root", directory) }
	default:
		return nil, fmt.Errorf("unsupported deployment provider %q", provider)
	}
	command := append([]string{exe}, args...)
	if boolArg(a, "dry_run", false) {
		return map[string]any{"executed": false, "provider": provider, "command": command, "working_directory": directory, "production": production, "reason": "dry run; no deployment or network request was made"}, nil
	}
	if err := rt.Capabilities.Require(security.RunBuild); err != nil { return nil, err }
	if err := rt.Capabilities.Require(security.NetworkAccess); err != nil { return nil, err }
	if !hasToolApproval(ctx, "deploy.run", raw) { return nil, errors.New("deploy.run requires explicit approval for this exact operation") }
	if _, err := exec.LookPath(exe); err != nil { return nil, fmt.Errorf("deployment CLI %q is not installed", exe) }
	timeout := time.Duration(intArg(a, "timeout_seconds", 300)) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, args...)
	cmd.Dir = rt.Root
	var stdout, stderr strings.Builder
	cmd.Stdout = &tier3LimitedWriter{builder: &stdout, limit: 32 << 10}
	cmd.Stderr = &tier3LimitedWriter{builder: &stderr, limit: 32 << 10}
	err := cmd.Run()
	result := map[string]any{"executed": true, "success": true, "provider": provider, "command": command, "exit_code": 0, "stdout": redactHTTPSecrets(stdout.String()), "stderr": redactHTTPSecrets(stderr.String()), "production": production}
	if err != nil {
		result["success"] = false
		result["exit_code"] = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) { result["exit_code"] = exitErr.ExitCode() }
		if runCtx.Err() != nil { result["error"] = runCtx.Err().Error() } else { result["error"] = err.Error() }
		return result, nil
	}
	return result, nil
}

type tier3LimitedWriter struct {
	builder *strings.Builder
	limit int
}

func (w *tier3LimitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - w.builder.Len()
	if remaining > 0 {
		if len(p) > remaining { p = p[:remaining] }
		_, _ = w.builder.Write(p)
	}
	return n, nil
}

func tier3Exists(root, rel string) bool {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && !info.IsDir()
}

