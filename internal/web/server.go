package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/diagnostics"
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
	mux.HandleFunc("/api/history", s.history)
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "FuzeCLI web interface could not be loaded"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "live updates are not supported by this connection"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Progress.Subscribe()
	defer cancel()
	last := s.Progress.Last()
	if !last.Timestamp.IsZero() {
		writeSSE(w, last.Type, last)
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
			writeSSE(w, event.Type, event)
			flusher.Flush()
		}
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST is required for chat requests"})
		return
	}

	var request struct {
		Prompt   string `json:"prompt"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Code     bool   `json:"code"`
		Yes      bool   `json:"yes"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("invalid chat request: %w", err), request.Provider, request.Model))
		return
	}

	request.Prompt = strings.TrimSpace(request.Prompt)
	request.Provider = strings.TrimSpace(request.Provider)
	request.Model = strings.TrimSpace(request.Model)
	if request.Prompt == "" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("prompt is required"), request.Provider, request.Model))
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeUserError(w, http.StatusConflict, diagnostics.Interpret(fmt.Errorf("FuzeCLI is already working on another request"), request.Provider, request.Model))
		return
	}
	s.busy = true
	s.mu.Unlock()

	job := fmt.Sprintf("job-%d", time.Now().UnixNano())
	go s.runChatJob(job, request.Prompt, request.Provider, request.Model, request.Code, request.Yes)

	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "job_id": job})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET is required for history"})
		return
	}
	if s.App.Store == nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(fmt.Errorf("workspace not initialized; run aicli init"), "", ""))
		return
	}
	messages, err := s.App.Store.History(200)
	if err != nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, "", ""))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (s *Server) runChatJob(job, prompt, providerName, model string, code, yes bool) {
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	start := time.Now()
	if code {
		s.Progress.Publish(progress.Event{Type: "progress", Status: "planning", Message: "Analyzing the request and building a resumable project plan", Provider: providerName, Model: model, ElapsedMillis: 0})
		result := make(chan error, 1)
		go func() {
			_, err := s.App.Ask(context.Background(), prompt, providerName, model, yes)
			result <- err
		}()
		s.monitorProjectPlan(start, providerName, model, result)
		return
	}

	s.Progress.Publish(progress.Event{Type: "progress", Status: "generating", Message: "Receiving response", Provider: providerName, Model: model, ElapsedMillis: 0})
	err := s.App.ChatStreamRequest(context.Background(), prompt, providerName, model, func(delta string) {
		s.Progress.Publish(progress.Event{Type: "chat_token", Status: "streaming", Message: delta, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	})
	if err != nil {
		u := diagnostics.Interpret(err, providerName, model)
		s.publishError(start, u)
		return
	}
	s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Provider: providerName, Model: model, Message: "Response complete", ElapsedMillis: time.Since(start).Milliseconds()})
}

func (s *Server) monitorProjectPlan(start time.Time, providerName, model string, result <-chan error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			if err != nil {
				u := diagnostics.Interpret(err, providerName, model)
				s.publishError(start, u)
				return
			}
			s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Message: "Generation complete", Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
			return
		case <-ticker.C:
			event := s.projectProgressEvent(start, providerName, model)
			s.Progress.Publish(event)
		}
	}
}

func (s *Server) publishError(start time.Time, u diagnostics.UserError) {
	s.Progress.Publish(progress.Event{
		Type: "job_error",
		Status: "failed",
		Provider: u.Provider,
		Model: u.Model,
		Message: u.Message,
		ErrorTitle: u.Title,
		ErrorMessage: u.Message,
		ErrorRecovery: u.Recovery,
		ErrorTechnical: u.Technical,
		RetryAfter: u.RetryAfter,
		HTTPStatus: u.StatusCode,
		ElapsedMillis: time.Since(start).Milliseconds(),
	})
}

func (s *Server) projectProgressEvent(start time.Time, providerName, model string) progress.Event {
	event := progress.Event{Type: "progress", Status: "generating", Provider: providerName, Model: model, Message: "Generating the next safe batch", ElapsedMillis: time.Since(start).Milliseconds()}
	if s.App.Store == nil {
		return event
	}
	data, err := os.ReadFile(generation.ProjectPlanPath(s.App.Store.Root))
	if err != nil {
		if os.IsNotExist(err) {
			event.Status = "planning"
			event.Message = "Building the project plan"
		}
		return event
	}
	var plan generation.ProjectPlan
	if json.Unmarshal(data, &plan) != nil {
		event.Status = "planning"
		event.Message = "Reading the project plan"
		return event
	}
	event.Project = plan.Project
	event.TotalFiles = len(plan.Files)
	event.CompletedFiles = len(plan.Files) - len(generation.PendingFiles(plan))
	pending := generation.PendingFiles(plan)
	if len(pending) == 0 {
		event.Status = "verifying"
		event.Message = "Final verification is running"
	} else {
		event.Status = "generating"
		event.CurrentFile = pending[0].Path
		event.BatchNumber = (event.CompletedFiles / generation.PlannerBatchSize) + 1
		event.TotalBatches = (event.TotalFiles + generation.PlannerBatchSize - 1) / generation.PlannerBatchSize
		event.Message = fmt.Sprintf("Generating %s", pending[0].Path)
	}
	return event
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		c := s.App.Config
		providers := make(map[string]any, len(c.Providers))
		for name, p := range c.Providers {
			providers[name] = map[string]any{"default_model": p.DefaultModel, "base_url": p.BaseURL, "configured": p.APIKey != "" || name == "llamacpp"}
		}
		writeJSON(w, http.StatusOK, map[string]any{"default_provider": c.DefaultProvider, "providers": providers, "fallback_order": c.FallbackOrder, "verification": c.Verification})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or POST is required for settings"})
		return
	}

	var request struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("invalid settings request: %w", err), "", ""))
		return
	}
	request.Provider = strings.TrimSpace(request.Provider)
	request.Model = strings.TrimSpace(request.Model)
	if request.Provider == "" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("provider is required"), "", request.Model))
		return
	}
	pc, ok := s.App.Config.Providers[request.Provider]
	if !ok {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("unknown provider %q", request.Provider), request.Provider, request.Model))
		return
	}
	if request.Model == "" {
		request.Model = pc.DefaultModel
	}
	if request.Model == "" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("no model configured for %s", request.Provider), request.Provider, request.Model))
		return
	}
	pc.DefaultModel = request.Model
	s.App.Config.Providers[request.Provider] = pc
	s.App.Config.DefaultProvider = request.Provider
	if err := config.Save(s.App.Config); err != nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, request.Provider, request.Model))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "provider": request.Provider, "model": request.Model})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	providerName := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerName == "" {
		providerName = s.App.Config.DefaultProvider
	}
	if providerName == "auto" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("models requires a specific provider"), providerName, ""))
		return
	}
	models, err := s.App.Registry.ListModels(r.Context(), providerName)
	if err != nil {
		writeUserError(w, http.StatusBadGateway, diagnostics.Interpret(err, providerName, ""))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": providerName, "models": models})
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	if s.App.Store == nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(fmt.Errorf("workspace not initialized; run aicli init"), "", ""))
		return
	}
	paths, err := s.App.Store.Touched()
	if err != nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, "", ""))
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
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(fmt.Errorf("workspace not initialized; run aicli init"), "", ""))
		return
	}
	path := generation.ProjectPlanPath(s.App.Store.Root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"exists": false})
			return
		}
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, "", ""))
		return
	}
	var plan generation.ProjectPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(fmt.Errorf("project plan JSON is invalid: %w", err), "", ""))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exists": true, "plan": plan})
}

func writeSSE(w http.ResponseWriter, eventType string, event progress.Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

func writeUserError(w http.ResponseWriter, status int, err diagnostics.UserError) {
	writeJSON(w, status, map[string]any{"error": err.Message, "title": err.Title, "recovery": err.Recovery, "retryable": err.Retryable, "provider": err.Provider, "model": err.Model, "retry_after": err.RetryAfter, "status_code": err.StatusCode, "technical": err.Technical})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
