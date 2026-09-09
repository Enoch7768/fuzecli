package provider_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"fuzecli/internal/provider"
	"fuzecli/internal/provider/anthropic"
	"fuzecli/internal/provider/gemini"
	"fuzecli/internal/provider/groq"
	"fuzecli/internal/provider/llamacpp"
	"fuzecli/internal/provider/openai"
)

func TestOpenAISend(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test","choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer s.Close()
	p := openai.New("secret", s.URL)
	got, err := p.Send(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, provider.RequestOptions{Model: "test"})
	if err != nil || got.Content != "hello" {
		t.Fatalf("send failed: %#v %v", got, err)
	}
}

func TestGroqUsesOpenAICompatibleEndpoint(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test","choices":[{"message":{"content":"groq"}}]}`))
	}))
	defer s.Close()
	p := groq.New("k", s.URL)
	got, err := p.Send(context.Background(), nil, provider.RequestOptions{Model: "test"})
	if err != nil || got.ProviderName != "groq" {
		t.Fatalf("send failed: %#v %v", got, err)
	}
}

func TestGeminiSend(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" {
			t.Fatalf("missing key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gemini"}]}}]}`))
	}))
	defer s.Close()
	p := gemini.New("k", s.URL)
	got, err := p.Send(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, provider.RequestOptions{Model: "model"})
	if err != nil || got.Content != "gemini" {
		t.Fatalf("send failed: %#v %v", got, err)
	}
}

func TestAnthropicSend(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Fatalf("missing key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test","content":[{"type":"text","text":"anthropic"}]}`))
	}))
	defer s.Close()
	p := anthropic.New("k", s.URL)
	got, err := p.Send(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, provider.RequestOptions{Model: "test"})
	if err != nil || got.Content != "anthropic" {
		t.Fatalf("send failed: %#v %v", got, err)
	}
}

func TestLlamaCppSend(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"local","choices":[{"message":{"content":"local"}}]}`))
	}))
	defer s.Close()
	p := llamacpp.New(s.URL)
	got, err := p.Send(context.Background(), nil, provider.RequestOptions{Model: "local"})
	if err != nil || got.Content != "local" {
		t.Fatalf("send failed: %#v %v", got, err)
	}
}

func TestRateLimitClassification(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer s.Close()
	p := openai.New("k", s.URL)
	_, err := p.Send(context.Background(), nil, provider.RequestOptions{Model: "test"})
	if !isRateLimited(err) {
		t.Fatalf("expected typed rate limit, got %v", err)
	}
}
func isRateLimited(err error) bool { return errors.Is(err, provider.ErrRateLimited) }
