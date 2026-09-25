package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestLimiterEnforcesWindow(t *testing.T) {
	l := newRequestLimiter()
	if !l.allow("client", 2, time.Minute) {
		t.Fatal("first request should be allowed")
	}
	if !l.allow("client", 2, time.Minute) {
		t.Fatal("second request should be allowed")
	}
	if l.allow("client", 2, time.Minute) {
		t.Fatal("third request should be rejected")
	}
}

func TestRequestLimitForPath(t *testing.T) {
	if got, _ := requestLimitForPath("/v1/chat", "POST"); got != 12 {
		t.Fatalf("chat limit = %d, want 12", got)
	}
	if got, _ := requestLimitForPath("/v1/upload", "POST"); got != 20 {
		t.Fatalf("upload limit = %d, want 20", got)
	}
	if got, _ := requestLimitForPath("/v1/file", "POST"); got != 60 {
		t.Fatalf("file limit = %d, want 60", got)
	}
}

func TestRequestClientKeyUsesRemoteHost(t *testing.T) {
	r := httptest.NewRequest("GET", "http://127.0.0.1/", nil)
	r.RemoteAddr = "127.0.0.1:8787"
	if got := requestClientKey(r); got != "127.0.0.1" {
		t.Fatalf("client key = %q, want 127.0.0.1", got)
	}
}
