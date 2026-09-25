package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/workspace"
)

func TestRequireExternalToken(t *testing.T) {
	if err := RequireExternalToken("127.0.0.1:8787", ""); err != nil {
		t.Fatalf("loopback without token should be allowed: %v", err)
	}
	if err := RequireExternalToken("0.0.0.0:8787", ""); err == nil {
		t.Fatal("non-loopback without token should be rejected")
	}
	if err := RequireExternalToken("0.0.0.0:8787", "secret"); err != nil {
		t.Fatalf("non-loopback with token should be allowed: %v", err)
	}
}

func TestSecurityHeaders(t *testing.T) {
	server := NewServer(nil, "")
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, name := range []string{
		"Content-Security-Policy",
		"X-Frame-Options",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"Permissions-Policy",
	} {
		if rec.Header().Get(name) == "" {
			t.Fatalf("missing security header %s", name)
		}
	}
}


func TestAttachmentLimitsAndDeduplication(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc := NewService(&app.App{Store: store})

	data := bytes.Repeat([]byte("x"), 100<<10)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	attachments, err := svc.ReadAttachments([]string{"large.txt", "large.txt"})
	if err != nil {
		t.Fatalf("attachment read failed: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected duplicate attachment to be removed, got %d", len(attachments))
	}
	if attachments[0].SizeBytes != int64(len(data)) {
		t.Fatalf("attachment size = %d, want %d", attachments[0].SizeBytes, len(data))
	}

	tooLarge := bytes.Repeat([]byte("x"), MaxAttachmentBytes+1)
	if err := os.WriteFile(filepath.Join(root, "too-large.txt"), tooLarge, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadAttachments([]string{"too-large.txt"}); err == nil {
		t.Fatal("expected per-file attachment limit error")
	}

	parts := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("part-%d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), bytes.Repeat([]byte("y"), 2<<20), 0600); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, name)
	}
	if _, err := svc.ReadAttachments(parts); err == nil {
		t.Fatal("expected total attachment limit error")
	}
}
