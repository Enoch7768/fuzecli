package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubAuthRejectsLegacyPATPost(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/v1/github/auth", strings.NewReader(`{"token":""}`))
	response := httptest.NewRecorder()
	server.githubAuth(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if strings.Contains(response.Body.String(), `{"token"`) {
		t.Fatal("response must not echo request data")
	}
}

func TestGitHubAuthRejectsLegacyPATPayload(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/v1/github/auth", strings.NewReader(`{"token":"","secret":"should-not-be-accepted"}`))
	response := httptest.NewRecorder()
	server.githubAuth(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if strings.Contains(response.Body.String(), "should-not-be-accepted") {
		t.Fatal("response must not echo unknown request fields")
	}
}

func TestGitHubAuthRejectsUnsupportedMethod(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/v1/github/auth", nil)
	response := httptest.NewRecorder()
	server.githubAuth(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
