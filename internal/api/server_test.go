package api

import (
	"net/http/httptest"
	"testing"
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
	handler := server.securityHeaders(httpHandlerFunc(func(w *httptest.ResponseRecorder, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "http://127.0.0.1/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	for _, name := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy", "Permissions-Policy"} {
		if rec.Header().Get(name) == "" {
			t.Fatalf("missing security header %s", name)
		}
	}
}

type httpHandlerFunc func(*httptest.ResponseRecorder, *httptest.Request)

func (f httpHandlerFunc) ServeHTTP(w http.ResponseWriter, r *httptest.Request) {
	f(w.(*httptest.ResponseRecorder), r.(*httptest.Request))
}
