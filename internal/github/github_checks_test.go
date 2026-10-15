package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPullRequestChecksSummarizesHeadCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "Improve checks", "state": "open",
				"head": map[string]string{"ref": "feature/checks", "sha": "abc123"},
				"base": map[string]string{"ref": "main", "sha": "def456"},
			})
		case "/repos/acme/app/commits/abc123/check-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": []map[string]any{
				{"id": 1, "name": "tests", "status": "completed", "conclusion": "success"},
				{"id": 2, "name": "lint", "status": "in_progress"},
			}})
		case "/repos/acme/app/commits/abc123/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending", "statuses": []map[string]any{
				{"context": "coverage", "state": "success"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New("test-token")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	summary, err := client.PullRequestChecks(context.Background(), "acme/app", 7)
	if err != nil {
		t.Fatalf("PullRequestChecks() error = %v", err)
	}
	if summary.HeadSHA != "abc123" {
		t.Fatalf("head SHA = %q, want abc123", summary.HeadSHA)
	}
	if summary.State != "pending" {
		t.Fatalf("state = %q, want pending", summary.State)
	}
	if len(summary.Checks) != 2 || len(summary.Statuses) != 1 {
		t.Fatalf("unexpected check/status counts: %d/%d", len(summary.Checks), len(summary.Statuses))
	}
}

func TestPullRequestChecksFailureTakesPrecedence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/app/pulls/8":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 8, "head": map[string]string{"sha": "deadbeef"},
			})
		case "/repos/acme/app/commits/deadbeef/check-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": []map[string]any{
				{"name": "tests", "status": "completed", "conclusion": "failure"},
			}})
		case "/repos/acme/app/commits/deadbeef/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New("test-token")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	summary, err := client.PullRequestChecks(context.Background(), "acme/app", 8)
	if err != nil {
		t.Fatalf("PullRequestChecks() error = %v", err)
	}
	if summary.State != "failure" {
		t.Fatalf("state = %q, want failure", summary.State)
	}
}

func TestPullRequestChecksRejectsInvalidNumber(t *testing.T) {
	client := New("test-token")
	_, err := client.PullRequestChecks(context.Background(), "acme/app", 0)
	if err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("error = %v, want positive-number validation", err)
	}
}
