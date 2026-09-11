package web

import (
	"archive/zip"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	assetDir string
	mu       sync.Mutex
	busy     bool
}

func New(a *app.App, hub *progress.Hub) *Server {
	assetDir, _ := os.Getwd()
	return &Server{App: a, Progress: hub, Addr: "127.0.0.1:8787", assetDir: assetDir}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.Handle("/static/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("/icon.png", s.logo)
	mux.HandleFunc("/icon-mark.png", s.logo)
	mux.HandleFunc("/api/state", s.state)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/chat", s.chat)
	mux.HandleFunc("/api/session", s.session)
	mux.HandleFunc("/api/history", s.history)
	mux.HandleFunc("/api/config", s.config)
	mux.HandleFunc("/api/models", s.models)
	mux.HandleFunc("/api/workspace", s.workspace)
	mux.HandleFunc("/api/plan", s.plan)
	mux.HandleFunc("/api/download", s.download)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secureHeaders(w)
		mux.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: s.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
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

func (s *Server) logo(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Path)
	if name != "icon.png" && name != "icon-mark.png" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.assetDir, name)
	if s.App.Store != nil {
		workspacePath := filepath.Join(s.App.Store.Root, name)
		if info, err := os.Stat(workspacePath); err == nil && !info.IsDir() && info.Size() <= 2<<20 {
			path = workspacePath
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > 2<<20 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
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
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
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
	if !allowLocalOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST is required for chat requests"})
		return
	}
	limitBody(w, r, 768<<10)
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
	if len(request.Prompt) > 700<<10 {
		writeUserError(w, http.StatusRequestEntityTooLarge, diagnostics.Interpret(fmt.Errorf("prompt is too large"), request.Provider, request.Model))
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

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if !allowLocalOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST is required for session setup"})
		return
	}
	if s.App.Store == nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(fmt.Errorf("workspace not initialized; run aicli init"), "", ""))
		return
	}
	limitBody(w, r, 32<<10)
	var request struct {
		Memory   string `json:"memory"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("invalid session request: %w", err), request.Provider, request.Model))
		return
	}
	request.Memory = strings.ToLower(strings.TrimSpace(request.Memory))
	request.Provider = strings.TrimSpace(request.Provider)
	request.Model = strings.TrimSpace(request.Model)
	if request.Memory != "continue" && request.Memory != "clear" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("memory must be either continue or clear"), request.Provider, request.Model))
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
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()
	if request.Memory == "clear" {
		if err := s.App.Store.ClearMemory(); err != nil {
			writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, request.Provider, request.Model))
			return
		}
	}
	start := time.Now()
	s.Progress.Publish(progress.Event{Type: "progress", Status: "planning", Message: "Preparing your session", Provider: request.Provider, Model: request.Model, ElapsedMillis: 0})
	welcome, err := s.App.SessionWelcome(r.Context(), request.Provider, request.Model)
	if err != nil {
		u := diagnostics.Interpret(err, request.Provider, request.Model)
		s.publishError(start, u)
		writeUserError(w, http.StatusBadGateway, u)
		return
	}
	s.Progress.Publish(progress.Event{Type: "progress", Status: "completed", Message: "Session ready.", Provider: request.Provider, Model: request.Model, ElapsedMillis: time.Since(start).Milliseconds()})
	writeJSON(w, http.StatusOK, map[string]any{"ready": true, "memory": request.Memory, "welcome": welcome, "provider": request.Provider, "model": modelOrDefault(request.Model, welcome)})
}

func modelOrDefault(requested, _ string) string {
	return strings.TrimSpace(requested)
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
		s.Progress.Publish(progress.Event{Type: "progress", Status: "planning", Message: "Understanding your request and identifying the work", Provider: providerName, Model: model, ElapsedMillis: 0})
		s.Progress.Publish(progress.Event{Type: "chat_token", Status: "planning", Message: "Thinking through the implementation plan…\n", Provider: providerName, Model: model, ElapsedMillis: 0})
		tickerStop := make(chan struct{})
		go s.publishPlanningHeartbeat(tickerStop, start, providerName, model)
		result := make(chan error, 1)
		go func() {
			_, err := s.App.Ask(context.Background(), prompt, providerName, model, yes)
			result <- err
		}()
		s.monitorProjectPlan(start, providerName, model, result, tickerStop)
		return
	}
	before := map[string]struct{}{}
	if s.App.Store != nil {
		if paths, err := s.App.Store.Touched(); err == nil {
			for _, path := range paths {
				before[path] = struct{}{}
			}
		}
	}
	s.Progress.Publish(progress.Event{Type: "progress", Status: "planning", Message: "Analyzing the request and preparing context", Provider: providerName, Model: model, ElapsedMillis: 0})
	s.Progress.Publish(progress.Event{Type: "chat_token", Status: "planning", Message: "Analyzing the request…\n", Provider: providerName, Model: model, ElapsedMillis: 0})
	time.Sleep(120 * time.Millisecond)
	s.Progress.Publish(progress.Event{Type: "progress", Status: "generating", Message: fmt.Sprintf("Waiting for %s to respond", providerName), Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	s.Progress.Publish(progress.Event{Type: "chat_token", Status: "generating", Message: fmt.Sprintf("Preparing %s and waiting for the first response…\n", model), Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	err := s.App.ChatStreamRequest(context.Background(), prompt, providerName, model, func(delta string) {
		s.Progress.Publish(progress.Event{Type: "chat_token", Status: "streaming", Message: delta, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	})
	if err != nil {
		u := diagnostics.Interpret(err, providerName, model)
		s.publishError(start, u)
		return
	}
	generated := []string{}
	if s.App.Store != nil {
		if paths, readErr := s.App.Store.Touched(); readErr == nil {
			for _, path := range paths {
				if _, ok := before[path]; !ok {
					generated = append(generated, path)
				}
			}
		}
	}
	message := "Response complete"
	if len(generated) > 0 {
		message = fmt.Sprintf("Generated %d file%s and saved the code to your workspace.", len(generated), pluralSuffix(len(generated)))
		s.Progress.Publish(progress.Event{Type: "generated", Status: "completed", Message: message, GeneratedFiles: generated, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	}
	s.Progress.Publish(progress.Event{Type: "chat_end", Status: "completed", Message: message, GeneratedFiles: generated, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	s.Progress.Publish(progress.Event{Type: "progress", Status: "completed", Message: message, GeneratedFiles: generated, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
}

func (s *Server) publishPlanningHeartbeat(stop <-chan struct{}, start time.Time, providerName, model string) {
	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.Progress.Publish(progress.Event{Type: "progress", Status: "planning", Message: "Working through the project structure and dependencies", Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
		}
	}
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func (s *Server) monitorProjectPlan(start time.Time, providerName, model string, result <-chan error, heartbeatStop chan<- struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	defer close(heartbeatStop)
	for {
		select {
		case err := <-result:
			if err != nil {
				u := diagnostics.Interpret(err, providerName, model)
				s.publishError(start, u)
				return
			}
			s.publishCodeCompletion(start, providerName, model)
			return
		case <-ticker.C:
			event := s.projectProgressEvent(start, providerName, model)
			s.Progress.Publish(event)
		}
	}
}

func (s *Server) publishCodeCompletion(start time.Time, providerName, model string) {
	message := "Code generation completed successfully."
	generated := []string{}
	if s.App.Store != nil {
		if data, err := os.ReadFile(generation.ProjectPlanPath(s.App.Store.Root)); err == nil {
			var plan generation.ProjectPlan
			if json.Unmarshal(data, &plan) == nil {
				completed := len(plan.Files) - len(generation.PendingFiles(plan))
				for _, file := range plan.Files {
					if file.Status == "completed" {
						generated = append(generated, file.Path)
					}
				}
				if len(plan.Files) > 0 {
					message = fmt.Sprintf("Done. Generated %d of %d planned files for %s. The files are saved in your workspace.", completed, len(plan.Files), plan.Project)
				}
			}
		}
	}
	s.Progress.Publish(progress.Event{Type: "progress", Status: "verifying", Message: "Final verification complete", GeneratedFiles: generated, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	s.Progress.Publish(progress.Event{Type: "chat_token", Status: "completed", Message: "\n\n" + message + "\n", GeneratedFiles: generated, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
	s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Message: message, GeneratedFiles: generated, Provider: providerName, Model: model, ElapsedMillis: time.Since(start).Milliseconds()})
}

func (s *Server) publishError(start time.Time, u diagnostics.UserError) {
	s.Progress.Publish(progress.Event{Type: "job_error", Status: "failed", Provider: u.Provider, Model: u.Model, Message: u.Message, ErrorTitle: u.Title, ErrorMessage: u.Message, ErrorRecovery: u.Recovery, ErrorTechnical: u.Technical, RetryAfter: u.RetryAfter, HTTPStatus: u.StatusCode, ElapsedMillis: time.Since(start).Milliseconds()})
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
			providers[name] = map[string]any{"default_model": p.DefaultModel, "base_url": p.BaseURL, "configured": p.APIKey != "" || name == "llamacpp", "api_key_configured": p.APIKey != ""}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"default_provider": c.DefaultProvider, "providers": providers, "fallback_order": c.FallbackOrder, "verification": c.Verification})
		return
	}
	if !allowLocalOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or POST is required for settings"})
		return
	}
	limitBody(w, r, 32<<10)
	var request struct {
		Provider    string  `json:"provider"`
		Model       string  `json:"model"`
		APIKey      *string `json:"api_key"`
		ClearAPIKey bool    `json:"clear_api_key"`
		BaseURL     string  `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("invalid settings request: %w", err), "", ""))
		return
	}
	request.Provider = strings.TrimSpace(request.Provider)
	request.Model = strings.TrimSpace(request.Model)
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	if request.Provider == "" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("provider is required"), "", request.Model))
		return
	}
	pc, ok := s.App.Config.Providers[request.Provider]
	if !ok {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("unknown provider %q", request.Provider), request.Provider, request.Model))
		return
	}
	if request.APIKey != nil {
		key := strings.TrimSpace(*request.APIKey)
		if err := validateSecret(key); err != nil {
			writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(err, request.Provider, request.Model))
			return
		}
		pc.APIKey = key
	}
	if request.ClearAPIKey {
		pc.APIKey = ""
	}
	if request.Model != "" {
		pc.DefaultModel = request.Model
	}
	if request.BaseURL != "" {
		if err := validateBaseURL(request.BaseURL); err != nil {
			writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(err, request.Provider, request.Model))
			return
		}
		pc.BaseURL = request.BaseURL
	}
	if pc.DefaultModel == "" && request.Provider != "llamacpp" {
		writeUserError(w, http.StatusBadRequest, diagnostics.Interpret(fmt.Errorf("no model configured for %s", request.Provider), request.Provider, request.Model))
		return
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeUserError(w, http.StatusConflict, diagnostics.Interpret(fmt.Errorf("settings cannot change while FuzeCLI is working"), request.Provider, request.Model))
		return
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()
	s.App.Config.Providers[request.Provider] = pc
	s.App.Config.DefaultProvider = request.Provider
	if err := config.Save(s.App.Config); err != nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, request.Provider, request.Model))
		return
	}
	s.App.ReloadProviders()
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "provider": request.Provider, "model": pc.DefaultModel, "api_key_configured": pc.APIKey != "", "base_url": pc.BaseURL})
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

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET is required for downloads"})
		return
	}
	if s.App.Store == nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(fmt.Errorf("workspace not initialized; run aicli init"), "", ""))
		return
	}
	paths, err := s.App.Store.Touched()
	if err != nil {
		writeUserError(w, http.StatusInternalServerError, diagnostics.Interpret(err, "", ""))
		return
	}
	if len(paths) == 0 {
		writeUserError(w, http.StatusNotFound, diagnostics.Interpret(fmt.Errorf("no generated files are available to download"), "", ""))
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="fuzecli-workspace.zip"`)
	archive := zip.NewWriter(w)
	for _, rel := range paths {
		path, err := generation.Resolve(s.App.Store.Root, rel)
		if err != nil {
			_ = archive.Close()
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entry := filepath.ToSlash(rel)
		writer, err := archive.Create(entry)
		if err != nil {
			_ = archive.Close()
			return
		}
		if _, err := writer.Write(data); err != nil {
			_ = archive.Close()
			return
		}
	}
	_ = archive.Close()
}

func validateSecret(value string) error {
	if len(value) > 4096 {
		return fmt.Errorf("API key is too long")
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("API key contains invalid control characters")
	}
	return nil
}

func validateBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base URL must use HTTP or HTTPS")
	}
	return nil
}

func allowLocalOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request origin rejected"})
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request origin rejected"})
		return false
	}
	if parsed.Host != r.Host {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request origin rejected"})
		return false
	}
	w.Header().Set("Vary", "Origin")
	return true
}

func limitBody(w http.ResponseWriter, r *http.Request, max int64) {
	r.Body = http.MaxBytesReader(w, r.Body, max)
}

func secureHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
}

func writeSSE(w http.ResponseWriter, eventType string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeUserError(w http.ResponseWriter, status int, value diagnostics.UserError) {
	writeJSON(w, status, map[string]any{"error": value.Message, "title": value.Title, "recovery": value.Recovery, "technical": value.Technical, "provider": value.Provider, "model": value.Model, "status": value.StatusCode, "retry_after": value.RetryAfter})
}
