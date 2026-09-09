package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Enoch7768/fuzecli/internal/app"
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
	mux.HandleFunc("/api/state", s.state)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/chat", s.chat)

	server := &http.Server{
		Addr:              s.Addr,
		Handler:           logging(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

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

		ctx := context.Background()
		if request.Code {
			_, err := s.App.Ask(ctx, request.Prompt, request.Provider, request.Model, request.Yes)
			if err != nil {
				s.Progress.Publish(progress.Event{Type: "error", Status: "failed", Message: err.Error()})
				return
			}
			s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Message: "Generation complete"})
			return
		}

		s.Progress.Publish(progress.Event{Type: "chat", Status: "generating", Provider: request.Provider, Model: request.Model, Message: "Receiving response"})
		if err := s.App.ChatRequest(ctx, request.Prompt); err != nil {
			s.Progress.Publish(progress.Event{Type: "error", Status: "failed", Message: err.Error()})
			return
		}
		s.Progress.Publish(progress.Event{Type: "completed", Status: "completed", Message: "Response complete"})
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
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

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}
