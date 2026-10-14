package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubAuthRejectsEmptyToken(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/v1/github/auth", strings.NewReader(`{"token":""}`))
	response := httptest.NewRecorder()
	server.githubAuth(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if strings.Contains(response.Body.String(), "token") && strings.Contains(response.Body.String(), `{"token"`) {
		t.Fatal("response must not echo submitted token data")
	}
}

func TestGitHubAuthRejectsUnknownRequestFields(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/v1/github/auth", strings.NewReader(`{"token":"","secret":"should-not-be-accepted"}`))
	response := httptest.NewRecorder()
	server.githubAuth(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
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
