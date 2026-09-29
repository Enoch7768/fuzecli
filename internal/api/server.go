package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/generation"
)

type Server struct {
	service        *Service
	token          string
	uiSession      string
	runtimePreview *runtimePreviewManager
	workbench *workbenchRuntime
	limiter        *requestLimiter
	chatSlots      chan struct{}
}

func NewServer(service *Service, token string) *Server {
	session := make([]byte, 24)
	_, _ = rand.Read(session)
	return &Server{
		service: service,
		token: strings.TrimSpace(token),
		uiSession: hex.EncodeToString(session),
		runtimePreview: &runtimePreviewManager{},
		workbench: newWorkbenchRuntime(),
		limiter: newRequestLimiter(),
		chatSlots: make(chan struct{}, 4),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.web)
	mux.HandleFunc("/assets/app.js", s.assetJS)
	mux.HandleFunc("/assets/styles.css", s.assetCSS)
	mux.HandleFunc("/v1/health", s.health)
	mux.HandleFunc("/v1/config", s.config)
	mux.HandleFunc("/v1/models", s.models)
	mux.HandleFunc("/v1/memory/refresh", s.memoryRefresh)
	mux.Handle("/v1/chat", s.withChatSlot(http.HandlerFunc(s.chat)))
	mux.HandleFunc("/v1/events", s.events)
	mux.HandleFunc("/v1/file", s.file)
	mux.HandleFunc("/v1/upload", s.upload)
	mux.HandleFunc("/preview/", s.preview)
	mux.HandleFunc("/v1/preview/runtime", s.runtimePreviewHandler)
	mux.HandleFunc("/v1/files", s.files)
	mux.HandleFunc("/v1/history", s.history)
	mux.HandleFunc("/v1/touched", s.touched)
	mux.HandleFunc("/v1/telemetry", s.telemetry)
	mux.HandleFunc("/v1/terminal", s.terminal)
	mux.HandleFunc("/v1/lsp", s.lsp)
	mux.HandleFunc("/v1/debug", s.debug)
	return s.securityHeaders(s.origin(s.auth(requestContext(s.rateLimit(mux)))))
}

func (s *Server) ListenAndServe(addr string) error {
	if s == nil || s.service == nil {
		return errors.New("API service is not configured")
	}
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:8787"
	}
	if err := RequireExternalToken(addr, s.token); err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second}
	return server.ListenAndServe()
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/preview/") && isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if s.token != "" && strings.TrimSpace(r.Header.Get("Authorization")) == "Bearer "+s.token {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie("fuzecli_ui")
		if err == nil && cookie.Value == s.uiSession && isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
	})
}

func (s *Server) origin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == "/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			if s.token != "" && strings.TrimSpace(r.Header.Get("Authorization")) == "Bearer "+s.token {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, "browser origin is required")
			return
		}
		if !strings.HasPrefix(origin, "http://127.0.0.1:") && !strings.HasPrefix(origin, "http://localhost:") && !strings.HasPrefix(origin, "http://[::1]:") {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self'; img-src 'self' data:; connect-src 'self' https://cdn.jsdelivr.net; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; frame-src 'self' http://127.0.0.1:* http://localhost:*; child-src 'self' http://127.0.0.1:* http://localhost:*; worker-src 'self' blob:; form-action 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) web(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "fuzecli_ui", Value: s.uiSession, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(webIndex)
}

func (s *Server) assetJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write(webJS)
}

func (s *Server) assetCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(webCSS)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "workspace": s.service.Root()})
}

func (s *Server) memoryRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.service.RefreshMemory(); err != nil {
		writeError(w, classifyServiceError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "conversation memory refreshed"})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	flusher, ok := w.(http.Flusher)
	if !ok { writeError(w, http.StatusInternalServerError, "event streaming is unavailable"); return }
	events, unsubscribe := s.service.SubscribeStudioEvents()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok { return }
			data, err := json.Marshal(event)
			if err != nil { continue }
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var req ChatRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	result, err := s.service.Chat(r.Context(), req)
	if err != nil {
		writeError(w, classifyServiceError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload: "+err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "at least one file is required")
		return
	}
	written := make([]string, 0, len(files))
	for _, header := range files {
		if header.Size < 0 || header.Size > MaxAttachmentBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "each uploaded file must be 2 MiB or smaller")
			return
		}
		name := filepath.ToSlash(strings.TrimSpace(header.Filename))
		if name == "" || filepath.Base(name) != filepath.Base(header.Filename) || strings.Contains(name, "..") {
			writeError(w, http.StatusBadRequest, "invalid uploaded filename")
			return
		}
		src, err := header.Open()
		if err != nil { writeError(w, http.StatusBadRequest, "open uploaded file failed"); return }
		data, err := io.ReadAll(io.LimitReader(src, MaxAttachmentBytes+1))
		_ = src.Close()
		if err != nil { writeError(w, http.StatusBadRequest, "read uploaded file failed"); return }
		if len(data) > MaxAttachmentBytes { writeError(w, http.StatusRequestEntityTooLarge, "uploaded file is too large for AI attachment context"); return }
		if err := s.service.WriteUploadedFile(name, data); err != nil {
			writeError(w, classifyServiceError(err), err.Error())
			return
		}
		written = append(written, name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "files": written})
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Path) == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		if err := s.service.WriteFile(req.Path, req.Content); err != nil {
			writeError(w, classifyServiceError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": filepath.ToSlash(req.Path)})
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	content, err := s.service.ReadFile(path)
	if err != nil {
		writeError(w, classifyServiceError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "content": content})
}

func (s *Server) runtimePreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if s.runtimePreview != nil {
			s.runtimePreview.Stop()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "runtime preview is local-only")
		return
	}
	if s.runtimePreview == nil {
		s.runtimePreview = &runtimePreviewManager{}
	}
	info, err := s.runtimePreview.Start(s.service.Root(), r.URL.Query().Get("runtime"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/preview/")
	if rel == "" {
		rel = "index.html"
	}
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "/"))
	ext := strings.ToLower(filepath.Ext(rel))
	allowed := map[string]string{".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".json": "application/json; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif", ".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf"}
	contentType, ok := allowed[ext]
	if !ok {
		writeError(w, http.StatusForbidden, "file type is not available in preview")
		return
	}
	path, err := generation.Resolve(s.service.Root(), rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "preview file not found")
		return
	}
	if info.Size() > 2<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "preview file is too large")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read preview file failed")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'self' https: data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval' https:; style-src 'self' 'unsafe-inline' https:; img-src 'self' data: blob: https:; font-src 'self' data: https:; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; form-action 'none'")
	if ext == ".html" || ext == ".htm" {
		data = injectPreviewEditor(data)
	}
	_, _ = w.Write(data)
}

func injectPreviewEditor(data []byte) []byte {
	const editor = "<script data-fuzecli-preview-editor=\"true\">\n(() => {\nconst send=(type,payload={})=>parent.postMessage({source:\"fuzecli-preview\",type,...payload},\"*\");\nlet selected=null;\nconst style=document.createElement(\"style\");\nstyle.textContent=\".fuzecli-selected{outline:2px solid #d9ff63!important;outline-offset:2px!important;cursor:pointer!important}.fuzecli-hover{outline:1px dashed rgba(217,255,99,.75)!important;outline-offset:1px!important}.fuzecli-editing{outline:2px solid #ffffff!important;outline-offset:2px!important}\";\ndocument.head.appendChild(style);\nconst pathFor=el=>{if(!el||!el.parentElement)return\"html\";const parts=[];while(el&&el.nodeType===1&&el!==document.documentElement){let index=1,node=el;while(node=node.previousElementSibling)index++;parts.unshift(el.tagName.toLowerCase()+\":nth-child(\"+index+\")\");el=el.parentElement}return\"html>\"+parts.join(\">\")};\nconst details=el=>{const s=getComputedStyle(el);send(\"select\",{path:pathFor(el),tag:el.tagName.toLowerCase(),text:el.innerText||\"\",color:s.color,background:s.backgroundColor,fontSize:s.fontSize,padding:s.padding,radius:s.borderRadius,src:el.getAttribute(\"src\")||\"\",href:el.getAttribute(\"href\")||\"\"})};\nconst select=el=>{if(!(el instanceof Element)||el===document.documentElement||el===document.body)return;if(selected)selected.classList.remove(\"fuzecli-selected\");selected=el;selected.classList.add(\"fuzecli-selected\");details(el)};\nconst changed=()=>send(\"changed\",{html:\"<!doctype html>\\n\"+document.documentElement.outerHTML});\nconst make=(kind)=>{let el;if(kind===\"section\"){el=document.createElement(\"section\");el.innerHTML=\"<div><h2>New section</h2><p>Describe this section.</p></div>\";el.style.padding=\"64px 32px\";el.style.minHeight=\"180px\"}else if(kind===\"heading\"){el=document.createElement(\"h2\");el.textContent=\"New heading\"}else if(kind===\"text\"){el=document.createElement(\"p\");el.textContent=\"New text block\"}else if(kind===\"button\"){el=document.createElement(\"a\");el.href=\"#\";el.textContent=\"New button\";el.style.display=\"inline-block\";el.style.padding=\"12px 18px\"}else if(kind===\"image\"){el=document.createElement(\"img\");el.src=\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='800' height='450'%3E%3Crect width='100%25' height='100%25' fill='%231b1e21'/%3E%3Ctext x='50%25' y='50%25' fill='%23d9ff63' font-size='32' text-anchor='middle' dominant-baseline='middle'%3EFuzeCLI image%3C/text%3E%3C/svg%3E\";el.alt=\"New image\";el.style.maxWidth=\"100%\"}else{el=document.createElement(\"div\");el.textContent=\"New container\"}return el};\nconst insert=kind=>{const el=make(kind);const parent=selected&&selected!==document.body?selected.parentElement||document.body:document.body;parent.appendChild(el);select(el);changed()};\ndocument.addEventListener(\"mouseover\",e=>{if(e.target instanceof Element&&e.target!==selected)e.target.classList.add(\"fuzecli-hover\")},true);\ndocument.addEventListener(\"mouseout\",e=>{if(e.target instanceof Element)e.target.classList.remove(\"fuzecli-hover\")},true);\ndocument.addEventListener(\"click\",e=>{if(e.target instanceof Element){e.preventDefault();e.stopPropagation();select(e.target)}},true);\ndocument.addEventListener(\"dblclick\",e=>{if(!(e.target instanceof Element)||e.target===document.body||e.target===document.documentElement)return;e.preventDefault();e.stopPropagation();select(e.target);if([\"INPUT\",\"TEXTAREA\",\"IMG\"].includes(e.target.tagName))return;e.target.contentEditable=\"true\";e.target.classList.add(\"fuzecli-editing\");e.target.focus();const finish=()=>{e.target.contentEditable=\"false\";e.target.classList.remove(\"fuzecli-editing\");changed();details(e.target);e.target.removeEventListener(\"blur\",finish)};e.target.addEventListener(\"blur\",finish);});\nwindow.addEventListener(\"message\",e=>{const d=e.data;if(!d||d.source!==\"fuzecli-editor\")return;if(d.type===\"style\"&&selected){for(const[key,value]of Object.entries(d.styles||{}))if([\"color\",\"backgroundColor\",\"fontSize\",\"padding\",\"borderRadius\"].includes(key))selected.style[key]=value;changed();details(selected)}if(d.type===\"text\"&&selected){selected.textContent=d.value||\"\";changed();details(selected)}if(d.type===\"attr\"&&selected){if(d.src!==undefined&&selected.tagName===\"IMG\")selected.src=d.src;if(d.href!==undefined&&selected.tagName===\"A\")selected.href=d.href;if(d.alt!==undefined&&selected.tagName===\"IMG\")selected.alt=d.alt;changed();details(selected)}if(d.type===\"add\")insert(d.kind);if(d.type===\"duplicate\"&&selected){const copy=selected.cloneNode(true);selected.parentElement?.insertBefore(copy,selected.nextSibling);select(copy);changed()}if(d.type===\"delete\"&&selected){if(selected!==document.body&&selected!==document.documentElement&&selected.parentElement){const next=selected.parentElement;selected.remove();selected=null;send(\"select\",{path:\"\",tag:\"\",text:\"\"});changed()}}if(d.type===\"move\"&&selected){if(d.direction===\"up\"&&selected.previousElementSibling)selected.parentElement.insertBefore(selected,selected.previousElementSibling);if(d.direction===\"down\"&&selected.nextElementSibling)selected.parentElement.insertBefore(selected.nextElementSibling,selected);changed();details(selected)}if(d.type===\"request-html\")changed()});\nsend(\"ready\");\n})();\n</script>"
	lower := strings.ToLower(string(data))
	if i := strings.LastIndex(lower, "</body>"); i >= 0 {
		out := make([]byte, 0, len(data)+len(editor))
		out = append(out, data[:i]...)
		out = append(out, editor...)
		out = append(out, data[i:]...)
		return out
	}
	return append(data, []byte(editor)...)
}
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	files, err := s.service.ListFiles(r.URL.Query().Get("prefix"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	history, err := s.service.History(200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": history})
}

func (s *Server) touched(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	paths, err := s.service.Touched()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": paths})
}

func (s *Server) telemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	value, err := s.service.Telemetry()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	value, err := s.service.Models(r.Context(), r.URL.Query().Get("provider"))
	if err != nil {
		writeError(w, classifyServiceError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		var req struct{ Provider, Model string }
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		if err := s.service.SetProvider(req.Provider, req.Model); err != nil {
			writeError(w, classifyServiceError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	value, err := s.service.Config()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func classifyServiceError(err error) int {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "provider"), strings.Contains(message, "model"), strings.Contains(message, "rate limit"), strings.Contains(message, "api key"):
		return http.StatusBadGateway
	case strings.Contains(message, "not found"):
		return http.StatusNotFound
	case strings.Contains(message, "unauthorized"):
		return http.StatusUnauthorized
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	if s.workbench == nil { writeError(w, http.StatusServiceUnavailable, "workbench runtime unavailable"); return }
	if !isLoopbackRequest(r) && s.token == "" { writeError(w, http.StatusForbidden, "terminal is local-only"); return }
	conn, err := s.workbench.upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	s.workbench.terminal.handleWS(conn, s.service.Root())
}

func (s *Server) lsp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	if s.workbench == nil { writeError(w, http.StatusServiceUnavailable, "workbench runtime unavailable"); return }
	language := strings.TrimSpace(r.URL.Query().Get("language"))
	if _, ok := lspForLanguage(language); !ok { writeError(w, http.StatusBadRequest, "unsupported language server: "+language); return }
	conn, err := s.workbench.upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	if err := bridgeLSP(conn, s.service.Root(), language); err != nil { _ = conn.WriteJSON(map[string]any{"type":"error","message":err.Error()}) }
	_ = conn.Close()
}

func (s *Server) debug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	if s.workbench == nil { writeError(w, http.StatusServiceUnavailable, "workbench runtime unavailable"); return }
	language := strings.TrimSpace(r.URL.Query().Get("language"))
	if language == "" { writeError(w, http.StatusBadRequest, "debug language is required"); return }
	conn, err := s.workbench.upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	if err := bridgeDAP(conn, s.service.Root(), language); err != nil { _ = conn.WriteJSON(map[string]any{"type":"error","message":err.Error()}) }
	_ = conn.Close()
}
