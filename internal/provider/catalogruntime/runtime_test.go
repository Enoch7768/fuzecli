package catalogruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
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
