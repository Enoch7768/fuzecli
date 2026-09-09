package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/progress"
)

//go:embed static/*
var assets embed.FS

type Server struct {
	App      *app.App
	Progress *progress.Hub
	Addr     string
	mu       sync.Mutex
	busy     bool
}

func New(a *app.App, hub *progress.Hub) *Server {
	return &Server{App: a, Progress: hub, Addr: "127.0.0.1:8787"}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.Handle("/static/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("/api/state", s.state)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/chat", s.chat)
	mux.HandleFunc("/api/config", s.config)
	mux.HandleFunc("/api/models", s.models)
	mux.HandleFunc("/api/workspace", s.workspace)
	mux.HandleFunc("/api/plan", s.plan)

	server := &http.Server{Addr: s.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Progress.Last())
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Progress.Subscribe()
	defer cancel()
	last := s.Progress.Last()
	if !last.Timestamp.IsZero() {
		writeSSE(w, last)
		flusher.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, event)
			flusher.Flush()
		}
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Prompt   string `json:"prompt"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Code     bool   `json:"code"`
		Yes      bool   `json:"yes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}
	request.Prompt = stringsTrim(request.Prompt)
	if request.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "FuzeCLI is already working on another request"})
		return
	}
	s.busy = true
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}()
		if request.Provider != "" {
			s.App.Config.DefaultProvider = request.Provider
		}
		if request.Provider != "" && request.Model != "" {
			pc, ok := s.App.Config.Providers[request.Provider]
			if ok {
				pc.DefaultModel = request.Model
				s.App.Config.Providers[request.Provider] = pc
			}
		}
		ctx := context.Background()
		if request.Code {
			_, err := s.App.Ask(ctx, request.Prompt, request.Provider, request.Model, request.Yes)
			if err != nil {
				s.Progress.Publish(progress.Event{Type: "error", Status: "failed", Provider: request.Provider, Model: request.Model, Message: err.Error()})
				return
			}
			s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Provider: request.Provider, Model: request.Model, Message: "Generation complete"})
			return
		}
		s.Progress.Publish(progress.Event{Type: "chat", Status: "generating", Provider: request.Provider, Model: request.Model, Message: "Receiving response"})
		if err := s.App.ChatRequest(ctx, request.Prompt, request.Provider, request.Model); err != nil {
			s.Progress.Publish(progress.Event{Type: "error", Status: "failed", Provider: request.Provider, Model: request.Model, Message: err.Error()})
			return
		}
		s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Provider: request.Provider, Model: request.Model, Message: "Response complete"})
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		c := s.App.Config
		writeJSON(w, http.StatusOK, map[string]any{"default_provider": c.DefaultProvider, "providers": c.Providers, "fallback_order": c.FallbackOrder, "verification": c.Verification})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid settings request"})
		return
	}
	if request.Provider == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider is required"})
		return
	}
	pc, ok := s.App.Config.Providers[request.Provider]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown provider: " + request.Provider})
		return
	}
	if request.Model != "" {
		pc.DefaultModel = request.Model
		s.App.Config.Providers[request.Provider] = pc
	}
	s.App.Config.DefaultProvider = request.Provider
	if err := config.Save(s.App.Config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "provider": request.Provider, "model": pc.DefaultModel})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		providerName = s.App.Config.DefaultProvider
	}
	if providerName == "auto" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "select a specific provider to load models"})
		return
	}
	models, err := s.App.Registry.ListModels(r.Context(), providerName)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not load models: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": providerName, "models": models})
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	if s.App.Store == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "workspace is not initialized"})
		return
	}
	paths, err := s.App.Store.Touched()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read workspace files: " + err.Error()})
		return
	}
	entries := make([]map[string]any, 0, len(paths))
	for _, rel := range paths {
		path := filepath.Join(s.App.Store.Root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		entries = append(entries, map[string]any{"path": rel, "size": info.Size(), "modified": info.ModTime()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": s.App.Store.Root, "files": entries})
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	if s.App.Store == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "workspace is not initialized"})
		return
	}
	path := generation.ProjectPlanPath(s.App.Store.Root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"exists": false})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read project plan: " + err.Error()})
		return
	}
	var plan generation.ProjectPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "project plan is invalid: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exists": true, "plan": plan})
}

func writeSSE(w http.ResponseWriter, event progress.Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func stringsTrim(value string) string {
	start := 0
	end := len(value)
	for start < end {
		switch value[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			goto right
		}
	}
right:
	for end > start {
		switch value[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			break
		}
		if end == start {
			break
		}
	}
	return value[start:end]
}
