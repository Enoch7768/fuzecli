package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	service   *Service
	token     string
	uiSession string
}

func NewServer(service *Service, token string) *Server {
	session := make([]byte, 24)
	_, _ = rand.Read(session)
	return &Server{service: service, token: strings.TrimSpace(token), uiSession: hex.EncodeToString(session)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.web)
	mux.HandleFunc("/assets/app.js", s.assetJS)
	mux.HandleFunc("/assets/styles.css", s.assetCSS)
	mux.HandleFunc("/v1/health", s.health)
	mux.HandleFunc("/v1/config", s.config)
	mux.HandleFunc("/v1/chat", s.chat)
	mux.HandleFunc("/v1/file", s.file)
	mux.HandleFunc("/preview/", s.preview)
	mux.HandleFunc("/v1/files", s.files)
	mux.HandleFunc("/v1/history", s.history)
	mux.HandleFunc("/v1/touched", s.touched)
	return s.securityHeaders(s.origin(s.auth(mux)))
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
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/preview/") {
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
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
	http.SetCookie(w, &http.Cookie{Name: "fuzecli_ui", Value: s.uiSession, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
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

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		var req struct {
			Path string `json:"path"`
			Content string `json:"content"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil { writeError(w, http.StatusBadRequest, "invalid request: "+err.Error()); return }
		if strings.TrimSpace(req.Path) == "" { writeError(w, http.StatusBadRequest, "path is required"); return }
		if err := s.service.WriteFile(req.Path, req.Content); err != nil { writeError(w, classifyServiceError(err), err.Error()); return }
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": filepath.ToSlash(req.Path)})
		return
	}
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
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

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	rel := strings.TrimPrefix(r.URL.Path, "/preview/")
	if rel == "" { rel = "index.html" }
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "/"))
	ext := strings.ToLower(filepath.Ext(rel))
	allowed := map[string]string{".html":"text/html; charset=utf-8",".htm":"text/html; charset=utf-8",".css":"text/css; charset=utf-8",".js":"text/javascript; charset=utf-8",".mjs":"text/javascript; charset=utf-8",".json":"application/json; charset=utf-8",".svg":"image/svg+xml",".png":"image/png",".jpg":"image/jpeg",".jpeg":"image/jpeg",".webp":"image/webp",".gif":"image/gif",".ico":"image/x-icon",".woff":"font/woff",".woff2":"font/woff2",".ttf":"font/ttf",".otf":"font/otf"}
	contentType, ok := allowed[ext]; if !ok { writeError(w, http.StatusForbidden, "file type is not available in preview"); return }
	path, err := generation.Resolve(s.service.Root(), rel); if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	info, err := os.Stat(path); if err != nil || info.IsDir() { writeError(w, http.StatusNotFound, "preview file not found"); return }
	if info.Size() > 2<<20 { writeError(w, http.StatusRequestEntityTooLarge, "preview file is too large"); return }
	data, err := os.ReadFile(path); if err != nil { writeError(w, http.StatusInternalServerError, "read preview file failed"); return }
	w.Header().Set("Content-Type", contentType); w.Header().Set("Cache-Control", "no-store"); w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'self' https: data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval' https:; style-src 'self' 'unsafe-inline' https:; img-src 'self' data: blob: https:; font-src 'self' data: https:; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; form-action 'none'")
	if ext == ".html" || ext == ".htm" { data = injectPreviewEditor(data) }
	_, _ = w.Write(data)
}

func injectPreviewEditor(data []byte) []byte {
	const editor = "<script>\n(() => {\nconst send=(type,payload={})=>parent.postMessage({source:\"fuzecli-preview\",type,...payload},\"*\");\nlet selected=null;\nconst style=document.createElement(\"style\");\nstyle.textContent=\".fuzecli-selected{outline:2px solid #6f8cff!important;outline-offset:2px!important;cursor:pointer!important}.fuzecli-hover{outline:1px dashed rgba(111,140,255,.7)!important;outline-offset:1px!important}\";\ndocument.head.appendChild(style);\nconst pathFor=el=>{if(!el||!el.parentElement)return\"html\";const parts=[];while(el&&el.nodeType===1&&el!==document.documentElement){let index=1,node=el;while(node=node.previousElementSibling)index++;parts.unshift(el.tagName.toLowerCase()+\":nth-child(\"+index+\")\");el=el.parentElement}return\"html>\"+parts.join(\">\")};\nconst select=el=>{if(!(el instanceof Element))return;if(selected)selected.classList.remove(\"fuzecli-selected\");selected=el;selected.classList.add(\"fuzecli-selected\");const s=getComputedStyle(el);send(\"select\",{path:pathFor(el),tag:el.tagName.toLowerCase(),text:el.innerText||\"\",color:s.color,background:s.backgroundColor,fontSize:s.fontSize,padding:s.padding,radius:s.borderRadius})};\ndocument.addEventListener(\"mouseover\",e=>{if(e.target instanceof Element&&e.target!==selected)e.target.classList.add(\"fuzecli-hover\")},true);\ndocument.addEventListener(\"mouseout\",e=>{if(e.target instanceof Element)e.target.classList.remove(\"fuzecli-hover\")},true);\ndocument.addEventListener(\"click\",e=>{if(e.target instanceof Element){e.preventDefault();e.stopPropagation();select(e.target)}},true);\nwindow.addEventListener(\"message\",e=>{const d=e.data;if(!d||d.source!==\"fuzecli-editor\")return;if(d.type===\"style\"&&selected){for(const[key,value]of Object.entries(d.styles||{}))if([\"color\",\"backgroundColor\",\"fontSize\",\"padding\",\"borderRadius\"].includes(key))selected.style[key]=value;send(\"changed\",{html:\"<!doctype html>\\n\"+document.documentElement.outerHTML})}if(d.type===\"text\"&&selected){selected.textContent=d.value||\"\";send(\"changed\",{html:\"<!doctype html>\\n\"+document.documentElement.outerHTML})}if(d.type===\"request-html\")send(\"changed\",{html:\"<!doctype html>\\n\"+document.documentElement.outerHTML})});\nsend(\"ready\");\n})();\n</script>"
	lower := strings.ToLower(string(data))
	if i := strings.LastIndex(lower, "</body>"); i >= 0 { out := make([]byte, 0, len(data)+len(editor)); out = append(out, data[:i]...); out = append(out, editor...); out = append(out, data[i:]...); return out }
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
