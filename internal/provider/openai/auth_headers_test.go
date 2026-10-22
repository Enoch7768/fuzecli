package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func TestEmptyAPIKeyOmitsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header = %q; want no header for an empty API key", got)
		}

		switch r.URL.Path {
		case "/v1/chat/completions":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode request: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if stream, _ := payload["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"hello"}}]}`)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New("", server.URL+"/v1")
	messages := []provider.Message{{Role: "user", Content: "hello"}}
	opts := provider.RequestOptions{Model: "test-model", MaxTokens: 32}

	t.Run("chat completion", func(t *testing.T) {
		response, err := p.Send(context.Background(), messages, opts)
		if err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		if response.Content != "hello" {
			t.Fatalf("response content = %q, want hello", response.Content)
		}
	})

	t.Run("model discovery", func(t *testing.T) {
		models, err := p.ListModels(context.Background())
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(models) != 1 || models[0] != "test-model" {
			t.Fatalf("models = %#v, want [test-model]", models)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		chunks, err := p.Stream(context.Background(), messages, opts)
		if err != nil {
			t.Fatalf("Stream() error = %v", err)
		}

		var got string
		for chunk := range chunks {
			if chunk.Error != nil {
				t.Fatalf("stream error = %v", chunk.Error)
			}
			got += chunk.Delta
		}
		if got != "hello" {
			t.Fatalf("stream content = %q, want hello", got)
		}
	})
}

func TestConfiguredAPIKeySendsBearerAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer test-key"; got != want {
			t.Errorf("Authorization header = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
	}))
	defer server.Close()

	p := New("test-key", server.URL+"/v1")
	if _, err := p.ListModels(context.Background()); err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
}
