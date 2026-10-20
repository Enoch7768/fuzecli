package catalogruntime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func TestStreamConsumesSSEChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	p := New("test", "", server.URL, "", 10000, 1000, 500)
	stream, err := p.Stream(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, provider.RequestOptions{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	var done bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		content += chunk.Delta
		done = done || chunk.Done
	}
	if content != "hello world" {
		t.Fatalf("content = %q", content)
	}
	if !done {
		t.Fatal("expected stream completion")
	}
}

func TestReadLimitedBodyRejectsOversizedResponse(t *testing.T) {
	body, err := readLimitedBody(strings.NewReader("12345"), 4)
	if err == nil {
		t.Fatal("expected oversized body to be rejected")
	}
	if body != nil {
		t.Fatalf("body should be nil on limit failure, got %q", body)
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadLimitedBodyAcceptsBodyAtLimit(t *testing.T) {
	body, err := readLimitedBody(strings.NewReader("1234"), 4)
	if err != nil {
		t.Fatalf("body at the limit should be accepted: %v", err)
	}
	if string(body) != "1234" {
		t.Fatalf("body = %q", body)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated body read failure")
}

func TestReadLimitedBodyPropagatesReadErrors(t *testing.T) {
	_, err := readLimitedBody(failingReader{}, 16)
	if err == nil || !strings.Contains(err.Error(), "simulated body read failure") {
		t.Fatalf("expected underlying read error, got %v", err)
	}
}

func TestReadLimitedBodyAllowsEmptyBody(t *testing.T) {
	body, err := readLimitedBody(io.LimitReader(strings.NewReader(""), 1), 0)
	if err != nil {
		t.Fatalf("empty body should be accepted: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("expected empty body, got %q", body)
	}
}
