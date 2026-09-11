package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateSecret(t *testing.T) {
	if err := validateSecret("abc123"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if err := validateSecret("abc\ndef"); err == nil {
		t.Fatal("control-character key accepted")
	}
	if err := validateSecret(makeString(4097)); err == nil {
		t.Fatal("oversized key accepted")
	}
}

func TestValidateBaseURL(t *testing.T) {
	for _, value := range []string{"https://example.com/v1", "http://127.0.0.1:11434"} {
		if err := validateBaseURL(value); err != nil {
			t.Fatalf("valid URL rejected: %v", err)
		}
	}
	for _, value := range []string{"javascript:alert(1)", "file:///tmp/model", "not a url"} {
		if err := validateBaseURL(value); err == nil {
			t.Fatalf("unsafe URL accepted: %q", value)
		}
	}
}

func TestAllowLocalOrigin(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/config", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	if !allowLocalOrigin(recorder, req) {
		t.Fatal("local origin rejected")
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/config", nil)
	req.Header.Set("Origin", "https://evil.example")
	if allowLocalOrigin(recorder, req) {
		t.Fatal("foreign origin accepted")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func makeString(size int) string {
	b := make([]byte, size)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
