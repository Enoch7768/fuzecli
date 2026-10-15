package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/workspace"
)

func TestProjectHealthReportsDetectedProjectAndTests(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":      "module example.test/project\n\ngo 1.25\n",
		"main_test.go": "package main\nfunc TestExample() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	server := NewServer(NewService(&app.App{Store: store}), "")
	request := httptest.NewRequest(http.MethodGet, "/v1/project/health", nil)
	response := httptest.NewRecorder()
	server.projectHealth(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var result projectHealthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Overall == "" {
		t.Fatal("expected an overall health status")
	}
	if len(result.Manifests) != 1 || result.Manifests[0] != "go.mod" {
		t.Fatalf("manifests = %v, want [go.mod]", result.Manifests)
	}
	foundTests := false
	for _, check := range result.Checks {
		if check.ID == "tests" && check.Status == "ok" {
			foundTests = true
		}
	}
	if !foundTests {
		t.Fatalf("expected test files to be detected, checks = %+v", result.Checks)
	}
}

func TestProjectHealthRejectsNonGet(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/v1/project/health", nil)
	response := httptest.NewRecorder()
	server.projectHealth(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
